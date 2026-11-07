package app

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
)

func advertisingBatchFixture(t *testing.T) (*Service, *gorm.DB, string, string, AdvertisingBatchRequest) {
	t.Helper()
	s, db, _ := agentMediaFixture(t)
	draft := advertisingTestDraft()
	draft.ProductResourceIDs = []string{"ref-one"}
	draft.Shots[0].ReferenceResourceIDs = []string{"ref-one"}
	ad, err := s.CreateAdvertisingProject("user", AdvertisingCreateRequest{Name: "Batch", Draft: &draft})
	if err != nil {
		t.Fatal(err)
	}
	confirmed, err := s.ConfirmAdvertisingProject("user", ad.Project.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.CreateAdvertisingProduction("user", ad.Project.ID, confirmed.Snapshot.ID)
	if err != nil {
		t.Fatal(err)
	}
	return s, db, ad.Project.ID, p.Production.ID, AdvertisingBatchRequest{AdvertisingSubmissionRequest: AdvertisingSubmissionRequest{ClientOperationID: uuid.NewString(), ShotID: draft.Shots[0].ID, Kind: "video", ModelID: "video-cm"}, Count: 2, Adjustment: "保持商品标识清晰"}
}
func batchApprovals(b AdvertisingBatchDetail) []AdvertisingBatchApproval {
	items := []AdvertisingBatchApproval{}
	for _, item := range b.Items {
		items = append(items, AdvertisingBatchApproval{SubmissionID: item.ID, QuoteHash: item.Quote.QuoteHash})
	}
	return items
}
func requireBatchConflict(t *testing.T, err error) {
	t.Helper()
	var e *AppError
	if !errors.As(err, &e) || e.Status != 409 {
		t.Fatalf("expected conflict, got %v", err)
	}
}
func batchCounts(t *testing.T, db *gorm.DB, want int64) {
	t.Helper()
	for _, row := range []any{&model.Task{}, &model.BillingOrder{}} {
		var n int64
		if err := db.Model(row).Count(&n).Error; err != nil || n != want {
			t.Fatalf("%T count=%d want=%d err=%v", row, n, want, err)
		}
	}
}
func TestAdvertisingBatchPrepareAndAtomicApproval(t *testing.T) {
	s, db, project, production, req := advertisingBatchFixture(t)
	for _, count := range []int{0, 4, -1} {
		bad := req
		bad.Count = count
		if _, err := s.PrepareAdvertisingBatch("user", project, production, bad); err == nil {
			t.Fatal("bad count accepted")
		}
	}
	bad := req
	bad.Adjustment = strings.Repeat("字", 1001)
	if _, err := s.PrepareAdvertisingBatch("user", project, production, bad); err == nil {
		t.Fatal("long adjustment accepted")
	}
	if _, err := s.PrepareAdvertisingBatch("other", project, production, req); err == nil {
		t.Fatal("foreign prepare")
	}
	b, err := s.PrepareAdvertisingBatch("user", project, production, req)
	if err != nil {
		t.Fatal(err)
	}
	batchCounts(t, db, 0)
	if len(b.Items) != 2 || b.Items[0].ID == b.Items[1].ID {
		t.Fatalf("bad members: %+v", b)
	}
	for _, item := range b.Items {
		if !strings.Contains(item.PublicRequest["prompt"].(string), req.Adjustment) {
			t.Fatal("adjustment not frozen")
		}
	}
	again, err := s.PrepareAdvertisingBatch("user", project, production, req)
	if err != nil || again.Batch.ID != b.Batch.ID || again.Items[1].ID != b.Items[1].ID {
		t.Fatalf("replay: %+v %v", again, err)
	}
	bad = req
	bad.Adjustment = "different"
	_, err = s.PrepareAdvertisingBatch("user", project, production, bad)
	requireBatchConflict(t, err)
	recovered, err := s.GetAdvertisingBatchForOperation("user", project, production, req.ClientOperationID)
	if err != nil || recovered.Batch.ID != b.Batch.ID {
		t.Fatalf("GET recovery %v", err)
	}
	if _, err = s.GetAdvertisingBatch("other", project, production, b.Batch.ID); err == nil {
		t.Fatal("foreign read")
	}
	approvals := batchApprovals(b)
	_, err = s.ExecuteAdvertisingBatch("user", project, production, b.Batch.ID, approvals[:1])
	requireBatchConflict(t, err)
	_, err = s.ExecuteAdvertisingSubmission("user", project, production, b.Items[0].ID, b.Items[0].Quote.QuoteHash)
	requireBatchConflict(t, err)
	badApprovals := append([]AdvertisingBatchApproval(nil), approvals...)
	badApprovals[1].QuoteHash = "changed"
	_, err = s.ExecuteAdvertisingBatch("user", project, production, b.Batch.ID, badApprovals)
	requireBatchConflict(t, err)
	batchCounts(t, db, 0)
	if _, err = s.ExecuteAdvertisingBatch("other", project, production, b.Batch.ID, approvals); err == nil {
		t.Fatal("foreign execute")
	}
	admitted, err := s.ExecuteAdvertisingBatch("user", project, production, b.Batch.ID, approvals)
	if err != nil {
		t.Fatal(err)
	}
	batchCounts(t, db, 2)
	if admitted.Batch.ApprovedAt == nil || admitted.Batch.ApprovalHash == "" {
		t.Fatal("missing immutable approval")
	}
	for i, item := range admitted.Items {
		if item.Task == nil || item.Task.AuthorizedChargeMicrocredits != b.Items[i].Quote.AmountMicrocredits {
			t.Fatal("missing task approval")
		}
	}
	if err = db.Model(&model.Task{}).Where("id = ?", admitted.Items[0].Task.ID).Updates(map[string]any{"status": model.TaskStatusSucceeded}).Error; err != nil {
		t.Fatal(err)
	}
	if err = db.Model(&model.Task{}).Where("id = ?", admitted.Items[1].Task.ID).Updates(map[string]any{"status": model.TaskStatusFailed}).Error; err != nil {
		t.Fatal(err)
	}
	replay, err := s.ExecuteAdvertisingBatch("user", project, production, b.Batch.ID, approvals)
	if err != nil || replay.Items[0].Task.ID != admitted.Items[0].Task.ID || replay.Items[1].Task.ID != admitted.Items[1].Task.ID {
		t.Fatalf("partial outcomes retried: %v", err)
	}
	batchCounts(t, db, 2)
}
func TestAdvertisingBatchAllOrNoneExpiredOrRepriced(t *testing.T) {
	for _, mode := range []string{"expired", "repriced", "budget"} {
		t.Run(mode, func(t *testing.T) {
			s, db, project, production, req := advertisingBatchFixture(t)
			b, err := s.PrepareAdvertisingBatch("user", project, production, req)
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "expired":
				err = db.Model(&model.AdvertisingSubmission{}).Where("id = ?", b.Items[1].ID).Update("expires_at", time.Now().Add(-time.Hour)).Error
			case "repriced":
				err = db.Model(&model.ChannelModelPriceTier{}).Where("id = ?", "video-tier").Update("unit_price_microcredits", 999).Error
			case "budget":
				err = db.Model(&model.CreditAccount{}).Where("user_id = ?", "user").Update("available_microcredits", 7).Error
			}
			if err != nil {
				t.Fatal(err)
			}
			_, err = s.ExecuteAdvertisingBatch("user", project, production, b.Batch.ID, batchApprovals(b))
			if err == nil {
				t.Fatal("invalid member admitted")
			}
			batchCounts(t, db, 0)
			fresh, err := s.GetAdvertisingBatch("user", project, production, b.Batch.ID)
			if err != nil || fresh.Batch.ApprovedAt != nil {
				t.Fatal("approval escaped rollback")
			}
		})
	}
}
func TestAdvertisingBatchTwoServicesAndLegacyGuard(t *testing.T) {
	s, db, project, production, req := advertisingBatchFixture(t)
	checkAdvertisingBatchTwoServices(t, s, db, project, production, req)
}
func checkAdvertisingBatchTwoServices(t *testing.T, s *Service, db *gorm.DB, project, production string, req AdvertisingBatchRequest) {
	// Independent Service mutexes and repositories share only the database.
	s2 := &Service{repo: repository.New(db), dataDir: s.dataDir}
	a, err := s.PrepareAdvertisingBatch("user", project, production, req)
	if err != nil {
		t.Fatal(err)
	}
	req.ClientOperationID = uuid.NewString()
	b, err := s.PrepareAdvertisingBatch("user", project, production, req)
	if err != nil {
		t.Fatal(err)
	}
	legacyReq := req.AdvertisingSubmissionRequest
	legacyReq.ClientOperationID = uuid.NewString()
	legacy, err := s.PrepareAdvertisingSubmission("user", project, production, legacyReq)
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for i, svc := range []*Service{s, s2} {
		wg.Add(1)
		go func(i int, svc *Service) {
			defer wg.Done()
			<-start
			batch := []AdvertisingBatchDetail{a, b}[i]
			_, e := svc.ExecuteAdvertisingBatch("user", project, production, batch.Batch.ID, batchApprovals(batch))
			errs <- e
		}(i, svc)
	}
	close(start)
	wg.Wait()
	close(errs)
	success := 0
	for err := range errs {
		if err == nil {
			success++
		} else {
			requireBatchConflict(t, err)
		}
	}
	if success != 1 {
		t.Fatalf("admitted %d batches", success)
	}
	batchCounts(t, db, 2)
	_, err = s.ExecuteAdvertisingSubmission("user", project, production, legacy.ID, legacy.Quote.QuoteHash)
	requireBatchConflict(t, err)
	activity, err := s.GetAdvertisingActivity("user", project, production)
	if err != nil || len(activity.Items) != 2 || len(activity.Batches) != 1 {
		t.Fatalf("activity %+v %v", activity, err)
	}
	if _, err = s.GetAdvertisingActivity("other", project, production); err == nil {
		t.Fatal("foreign activity")
	}
	// Another shot may proceed; concurrent approval of its same batch shares IDs.
	req.ShotID = advertisingTestDraft().Shots[1].ID
	req.ClientOperationID = uuid.NewString()
	next, err := s.PrepareAdvertisingBatch("user", project, production, req)
	if err != nil {
		t.Fatal(err)
	}
	start = make(chan struct{})
	results := make(chan AdvertisingBatchDetail, 2)
	errs = make(chan error, 2)
	for _, svc := range []*Service{s, s2} {
		wg.Add(1)
		go func(svc *Service) {
			defer wg.Done()
			<-start
			result, e := svc.ExecuteAdvertisingBatch("user", project, production, next.Batch.ID, batchApprovals(next))
			results <- result
			errs <- e
		}(svc)
	}
	close(start)
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	first, second := <-results, <-results
	for i := range first.Items {
		if first.Items[i].Task.ID != second.Items[i].Task.ID {
			t.Fatal("same batch duplicated task")
		}
	}
	batchCounts(t, db, 4)
}
func TestAdvertisingBatchUnresolvedNeverTimesOut(t *testing.T) {
	for _, updates := range []map[string]any{
		{"execution_diagnostic_json": `{"submissionOutcome":"unknown"}`},
		{"provider_cancel_status": model.ProviderCancelStatusRequested},
		{"provider_cancel_status": model.ProviderCancelStatusUncertain},
		{"media_recovery_json": "encrypted-pending", "media_stage": "register"},
		{"provider_request_id": "known-provider-id", "poll_stage": "processing"},
	} {
		t.Run(strings.ReplaceAll(strings.TrimSpace(creationHash(updates)), "/", "_"), func(t *testing.T) {
			s, db, project, production, req := advertisingBatchFixture(t)
			req.Count = 1
			b, err := s.PrepareAdvertisingBatch("user", project, production, req)
			if err != nil {
				t.Fatal(err)
			}
			b, err = s.ExecuteAdvertisingBatch("user", project, production, b.Batch.ID, batchApprovals(b))
			if err != nil {
				t.Fatal(err)
			}
			updates["status"] = model.TaskStatusFailed
			updates["updated_at"] = time.Now().Add(-365 * 24 * time.Hour)
			if err = db.Model(&model.Task{}).Where("id = ?", b.Items[0].Task.ID).Updates(updates).Error; err != nil {
				t.Fatal(err)
			}
			req.ClientOperationID = uuid.NewString()
			next, err := s.PrepareAdvertisingBatch("user", project, production, req)
			if err != nil {
				t.Fatal(err)
			}
			_, err = s.ExecuteAdvertisingBatch("user", project, production, next.Batch.ID, batchApprovals(next))
			requireBatchConflict(t, err)
			batchCounts(t, db, 1)
		})
	}
}

func TestAdvertisingBatchCloseAndBillingUncertain(t *testing.T) {
	s, db, project, production, req := advertisingBatchFixture(t)
	b, err := s.PrepareAdvertisingBatch("user", project, production, req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.CloseAdvertisingBatch("other", project, production, b.Batch.ID); err == nil {
		t.Fatal("foreign close")
	}
	closed, err := s.CloseAdvertisingBatch("user", project, production, b.Batch.ID)
	if err != nil || closed.Batch.ClosedAt == nil {
		t.Fatalf("close: %+v %v", closed, err)
	}
	_, err = s.ExecuteAdvertisingBatch("user", project, production, b.Batch.ID, batchApprovals(b))
	requireBatchConflict(t, err)
	batchCounts(t, db, 0)
	req.ClientOperationID = uuid.NewString()
	b, err = s.PrepareAdvertisingBatch("user", project, production, req)
	if err != nil {
		t.Fatal(err)
	}
	b, err = s.ExecuteAdvertisingBatch("user", project, production, b.Batch.ID, batchApprovals(b))
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.CloseAdvertisingBatch("user", project, production, b.Batch.ID)
	requireBatchConflict(t, err)
	for _, item := range b.Items {
		if err = db.Model(&model.Task{}).Where("id = ?", item.Task.ID).Update("status", model.TaskStatusFailed).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err = db.Model(&model.BillingOrder{}).Where("id = ?", b.Items[0].Task.BillingOrderID).Update("status", model.BillingStatusUncertain).Error; err != nil {
		t.Fatal(err)
	}
	// Put the unresolved batch outside the first page; activity remains authoritative.
	for range 21 {
		req.ClientOperationID = uuid.NewString()
		if _, err = s.PrepareAdvertisingBatch("user", project, production, req); err != nil {
			t.Fatal(err)
		}
	}
	page, err := s.ListAdvertisingBatches("user", project, production, 1, 20)
	if err != nil || len(page.Items) != 20 {
		t.Fatalf("page: %v", err)
	}
	for _, item := range page.Items {
		if item.Batch.ID == b.Batch.ID {
			t.Fatal("fixture active batch unexpectedly on first page")
		}
	}
	activity, err := s.GetAdvertisingActivity("user", project, production)
	if err != nil || len(activity.Items) != 1 || activity.Items[0].Task.ID != b.Items[0].Task.ID {
		t.Fatalf("uncertain billing activity: %+v %v", activity, err)
	}
	latest := page.Items[0]
	_, err = s.ExecuteAdvertisingBatch("user", project, production, latest.Batch.ID, batchApprovals(latest))
	requireBatchConflict(t, err)
	batchCounts(t, db, 2)
}
func TestAdvertisingBatchLegacyFirstBlocksBatch(t *testing.T) {
	s, db, project, production, req := advertisingBatchFixture(t)
	legacy, err := s.PrepareAdvertisingSubmission("user", project, production, req.AdvertisingSubmissionRequest)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err = s.ExecuteAdvertisingSubmission("user", project, production, legacy.ID, legacy.Quote.QuoteHash)
	if err != nil {
		t.Fatal(err)
	}
	req.ClientOperationID = uuid.NewString()
	batch, err := s.PrepareAdvertisingBatch("user", project, production, req)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.ExecuteAdvertisingBatch("user", project, production, batch.Batch.ID, batchApprovals(batch))
	requireBatchConflict(t, err)
	batchCounts(t, db, 1)
}

func TestAdvertisingBatchCancelledBeforeReconciliation(t *testing.T) {
	for _, tc := range []struct {
		name, requestID, outcome string
		status                   model.TaskStatus
		unresolved               bool
	}{
		{"accepted-with-id", "original-upstream", "accepted", model.TaskStatusRunning, true},
		{"known-id-without-diagnostic", "original-upstream", "", model.TaskStatusRunning, true},
		{"unknown-without-id", "", "unknown", model.TaskStatusRunning, true},
		{"accepted-without-id", "", "accepted", model.TaskStatusRunning, true},
		{"submitted-without-id", "", "submitted", model.TaskStatusRunning, true},
		{"rejected-before-dispatch", "", "rejected", model.TaskStatusRunning, false},
		{"queued-never-dispatched", "", "not_submitted", model.TaskStatusQueued, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, db, project, production, req := advertisingBatchFixture(t)
			req.Count = 1
			first, err := s.PrepareAdvertisingBatch("user", project, production, req)
			if err != nil {
				t.Fatal(err)
			}
			first, err = s.ExecuteAdvertisingBatch("user", project, production, first.Batch.ID, batchApprovals(first))
			if err != nil {
				t.Fatal(err)
			}
			id, orderID := first.Items[0].Task.ID, first.Items[0].Task.BillingOrderID
			if err = db.Model(&model.Task{}).Where("id = ?", id).Updates(map[string]any{"status": tc.status, "provider_request_id": tc.requestID, "poll_stage": "processing"}).Error; err != nil {
				t.Fatal(err)
			}
			// Stop at the durable CAS, before the async worker claims cancellation or updates billing.
			now := time.Now()
			ok, err := s.repo.CancelTaskIfStatus("user", id, tc.status, now, model.TaskCancellationIntent{Source: model.TaskCancellationWorkerContext, RequestedAt: now, Diagnostic: model.TaskExecutionDiagnostic{SubmissionOutcome: tc.outcome}})
			if err != nil || !ok {
				t.Fatalf("cancel: %v %v", ok, err)
			}
			s2 := &Service{repo: repository.New(db), dataDir: s.dataDir}
			req.ClientOperationID = uuid.NewString()
			next, err := s2.PrepareAdvertisingBatch("user", project, production, req)
			if err != nil {
				t.Fatal(err)
			}
			legacyReq := req.AdvertisingSubmissionRequest
			legacyReq.ClientOperationID = uuid.NewString()
			legacy, err := s2.PrepareAdvertisingSubmission("user", project, production, legacyReq)
			if err != nil {
				t.Fatal(err)
			}
			batchCounts(t, db, 1)
			activity, err := s2.GetAdvertisingActivity("user", project, production)
			if err != nil {
				t.Fatal(err)
			}
			if tc.unresolved {
				if len(activity.Items) != 1 || activity.Items[0].Task.ID != id {
					t.Errorf("cancelled upstream missing from activity: %+v", activity)
				}
				_, err = s2.ExecuteAdvertisingBatch("user", project, production, next.Batch.ID, batchApprovals(next))
				requireBatchConflict(t, err)
				_, err = s2.ExecuteAdvertisingSubmission("user", project, production, legacy.ID, legacy.Quote.QuoteHash)
				requireBatchConflict(t, err)
				batchCounts(t, db, 1)
				// Apply the same durable transitions as affirmative cancellation reconciliation.
				if err = s.repo.ClaimTaskProviderCancellation("user", id, now); err != nil {
					t.Fatal(err)
				}
				if err = s.taskBilling().RefundBilling(orderID, "upstream cancellation confirmed"); err != nil {
					t.Fatal(err)
				}
				if err = s.repo.UpdateTaskProviderCancellation(id, model.ProviderCancelStatusRequested, model.ProviderCancelStatusConfirmed, "", nil, &now); err != nil {
					t.Fatal(err)
				}
			} else if len(activity.Items) != 0 {
				t.Fatal("never-dispatched cancellation holds slot")
			}
			activity, err = s2.GetAdvertisingActivity("user", project, production)
			if err != nil || len(activity.Items) != 0 {
				t.Fatalf("resolved cancellation holds slot: %+v %v", activity, err)
			}
			replay, err := s2.ExecuteAdvertisingBatch("user", project, production, first.Batch.ID, batchApprovals(first))
			if err != nil || replay.Items[0].Task.ID != id || replay.Items[0].Task.Status != model.TaskStatusCancelled {
				t.Fatalf("cancelled task resurrected: %+v %v", replay, err)
			}
			batchCounts(t, db, 1)
			if _, err = s2.ExecuteAdvertisingBatch("user", project, production, next.Batch.ID, batchApprovals(next)); err != nil {
				t.Fatalf("fresh approved attempt blocked: %v", err)
			}
			batchCounts(t, db, 2)
		})
	}
}
