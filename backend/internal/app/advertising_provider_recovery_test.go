package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"yingce/backend/internal/model"
	"yingce/backend/internal/platform"
	"yingce/backend/internal/repository"
)

func TestAdvertisingProviderRecoveryRegistersCheckpoint(t *testing.T) {
	t.Run("success", func(t *testing.T) { advertisingProviderRecoveryCheckpoint(t, false) })
	t.Run("registration_rollback_retry", func(t *testing.T) { advertisingProviderRecoveryCheckpoint(t, true) })
}

func advertisingProviderRecoveryCheckpoint(t *testing.T, rollback bool) {
	t.Helper()
	s, db, project, production, req := advertisingBatchFixture(t)
	s.coordinator = platform.NewCoordinatorWithRedis(nil, "ad-recovery-test")
	s.platform = nil
	q, err := s.PrepareAdvertisingSubmission("user", project, production, req.AdvertisingSubmissionRequest)
	if err != nil {
		t.Fatal(err)
	}
	q, err = s.ExecuteAdvertisingSubmission("user", project, production, q.ID, q.Quote.QuoteHash)
	if err != nil {
		t.Fatal(err)
	}
	videoPath := filepath.Join(t.TempDir(), "result.mp4")
	if output, err := exec.Command("ffmpeg", "-v", "error", "-y", "-f", "lavfi", "-i", "testsrc2=size=160x284:rate=10", "-t", "5", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-movflags", "+faststart", videoPath).CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg: %v %s", err, output)
	}
	video, err := os.ReadFile(videoPath)
	if err != nil {
		t.Fatal(err)
	}
	var creates, queries atomic.Int32
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			creates.Add(1)
			w.WriteHeader(500)
			return
		}
		if r.URL.Path == "/video.mp4" {
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write(video)
			return
		}
		queries.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "original-provider-id", "status": "succeeded", "content": map[string]any{"video_url": server.URL + "/video.mp4?secret=must-stay-encrypted"}})
	}))
	defer server.Close()
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")
	if err = db.Model(&model.ModelChannel{}).Where("id = ?", "channel").Update("base_url", server.URL).Error; err != nil {
		t.Fatal(err)
	}
	if err = db.Model(&model.Task{}).Where("id = ?", q.Task.ID).Updates(map[string]any{"status": model.TaskStatusFailed, "provider_request_id": "original-provider-id", "execution_diagnostic_json": `{"submissionOutcome":"unknown"}`}).Error; err != nil {
		t.Fatal(err)
	}
	if rollback {
		if err = db.Exec("CREATE TRIGGER reject_ad_candidate BEFORE INSERT ON advertising_resource_references WHEN NEW.scope_key LIKE 'candidate:%' BEGIN SELECT RAISE(ABORT, 'injected candidate failure'); END").Error; err != nil {
			t.Fatal(err)
		}
		if _, err = s.QueryFailedVideoTask(context.Background(), "user", q.Task.ID); err == nil || !strings.Contains(err.Error(), "injected candidate failure") {
			t.Fatalf("registration injection not reached: %v", err)
		}
		failed, err := s.repo.Task(q.Task.ID)
		if err != nil {
			t.Fatal(err)
		}
		checkpoint, err := s.decodeMediaCheckpoint(failed)
		if err != nil || len(checkpoint.Items) != 1 || checkpoint.Items[0].ResourceID == "" {
			t.Fatal("lost recovery checkpoint", err)
		}
		resourceID := checkpoint.Items[0].ResourceID
		if failed.Status != model.TaskStatusFailed || failed.ResultJSON != "" || failed.LeaseOwner != "" {
			t.Fatal("registration rollback exposed success or retained lease")
		}
		if _, err = s.getAdvertisingCandidateResource(s.repo, "user", production, req.ShotID, "video", q.ID); err == nil {
			t.Fatal("failed candidate became available")
		}
		if exists, err := s.repo.HasAdvertisingCandidateReference(project, q.ID, resourceID); err != nil || exists {
			t.Fatal("candidate reference escaped rollback", err)
		}
		order, err := s.repo.BillingOrder(q.Task.BillingOrderID)
		if err != nil || order.Status != model.BillingStatusUncertain {
			t.Fatal("failed registration did not preserve billing uncertainty", err)
		}
		if err = db.Exec("DROP TRIGGER reject_ad_candidate").Error; err != nil {
			t.Fatal(err)
		}
		// Retry the ready resource under a new failed/manual lease, not another generation.
		server.Close()
		failed.LeaseOwner = "manual-recovery:registration-retry"
		if err = s.repo.ClaimFailedTaskProviderRecovery(failed.ID, "user", failed.LeaseOwner, time.Minute); err != nil {
			t.Fatal(err)
		}
		output, err := s.resumeTaskMedia(context.Background(), failed)
		if err != nil {
			t.Fatal("retry unexpectedly needs provider/download", err)
		}
		if err = s.finishTaskMediaRecovery(failed, output, nil); err != nil {
			t.Fatal(err)
		}
		if err = s.repo.ReleaseTaskProviderRecovery(failed.ID, failed.LeaseOwner); err != nil {
			t.Fatal(err)
		}
		id, _ := taskOutputResource(failed.ResultJSON, failed.Type)
		if id != resourceID || failed.ID != q.Task.ID || failed.BillingOrderID != q.Task.BillingOrderID {
			t.Fatal("retry changed original identities")
		}
	} else {
		result, err := s.QueryFailedVideoTask(context.Background(), "user", q.Task.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !result.Recovered || !result.BillingSettled || result.Task.ID != q.Task.ID {
			t.Fatalf("recovery %+v", result)
		}
	}
	stored, err := s.repo.Task(q.Task.ID)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, err := s.decodeMediaCheckpoint(stored)
	if err != nil || len(checkpoint.Items) != 1 || checkpoint.Items[0].ResourceID == "" {
		t.Fatalf("checkpoint %+v %v", checkpoint, err)
	}
	if strings.Contains(stored.MediaRecoveryJSON, "must-stay-encrypted") {
		t.Fatal("plaintext provider URL persisted")
	}
	resource, err := s.getAdvertisingCandidateResource(s.repo, "user", production, req.ShotID, "video", q.ID)
	if err != nil || resource.ID != checkpoint.Items[0].ResourceID {
		t.Fatalf("recovered candidate unavailable %v", err)
	}
	var before, after int64
	if err = db.Model(&model.CreditLedgerEntry{}).Count(&before).Error; err != nil {
		t.Fatal(err)
	}
	if err = s.settleRecoveredBilling(stored, "original-provider-id"); err != nil {
		t.Fatal(err)
	}
	if err = db.Model(&model.CreditLedgerEntry{}).Count(&after).Error; err != nil || before != after {
		t.Fatal("duplicate settlement")
	}
	batchCounts(t, db, 1)
	if creates.Load() != 0 || queries.Load() != 1 {
		t.Fatalf("create=%d query=%d", creates.Load(), queries.Load())
	}
	// Stale manual lease cannot checkpoint or resurrect a cancelled task.
	stale := *stored
	stale.Status = model.TaskStatusFailed
	stale.LeaseOwner = "manual-recovery:stale"
	if err = db.Model(&model.Task{}).Where("id = ?", stored.ID).Updates(map[string]any{"status": model.TaskStatusCancelled, "lease_owner": stale.LeaseOwner, "lease_expires_at": time.Now().Add(time.Minute)}).Error; err != nil {
		t.Fatal(err)
	}
	if err = s.saveMediaCheckpoint(&stale, checkpoint, "register"); err == nil {
		t.Fatal("cancelled checkpoint accepted")
	}
	if _, err = s.QueryFailedVideoTask(context.Background(), "user", stored.ID); err == nil {
		t.Fatal("cancelled task queried")
	}
	if err = s.saveTaskCompletionWithinStorageQuota(&stale, []byte(stored.ResultJSON), nil, false); err == nil {
		t.Fatal("cancelled task resurrected")
	}
}

// Removing terminal reconciliation must leave these original orders/slots unresolved.
func TestAdvertisingProviderRecoveryTerminalReconciliation(t *testing.T) {
	for _, scenario := range []string{"failed", "cancelled", "http_error", "transport_error", "decode_error", "processing", "refund_failure", "lease_stolen", "lease_expired", "route_changed", "cancelled_during_query"} {
		t.Run(scenario, func(t *testing.T) {
			s, db, project, production, req := advertisingBatchFixture(t)
			s.coordinator = platform.NewCoordinatorWithRedis(nil, "ad-terminal-test")
			s.platform = nil
			q, err := s.PrepareAdvertisingSubmission("user", project, production, req.AdvertisingSubmissionRequest)
			if err != nil {
				t.Fatal(err)
			}
			q, err = s.ExecuteAdvertisingSubmission("user", project, production, q.ID, q.Quote.QuoteHash)
			if err != nil {
				t.Fatal(err)
			}
			originalTask, originalBill := q.Task.ID, q.Task.BillingOrderID
			if err = db.Model(&model.Task{}).Where("id = ?", originalTask).Updates(map[string]any{"status": model.TaskStatusFailed, "provider_request_id": "original-provider-id", "poll_stage": "processing", "execution_diagnostic_json": `{"submissionOutcome":"unknown"}`}).Error; err != nil {
				t.Fatal(err)
			}
			if err = s.taskBilling().MarkBillingUncertain(originalBill, "submission unknown"); err != nil {
				t.Fatal(err)
			}
			// Preparation remains side-effect-free and allowed; only execution is guarded.
			req.ClientOperationID = uuid.NewString()
			next, err := s.PrepareAdvertisingBatch("user", project, production, req)
			if err != nil {
				t.Fatal(err)
			}
			_, err = s.ExecuteAdvertisingBatch("user", project, production, next.Batch.ID, batchApprovals(next))
			requireBatchConflict(t, err)
			var creates, queries atomic.Int32
			var laterQueryFails atomic.Bool
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					creates.Add(1)
					w.WriteHeader(500)
					return
				}
				queries.Add(1)
				if laterQueryFails.Load() {
					http.Error(w, "unavailable", 502)
					return
				}
				if r.Header.Get("Authorization") == "" {
					t.Error("query omitted upstream authentication")
				}
				if !strings.Contains(r.URL.Path, "original-provider-id") {
					t.Error("query changed provider identity")
				}
				// A second Service cannot acquire the live lease while this request is in flight.
				other := &Service{repo: repository.New(db)}
				if claimErr := other.repo.ClaimFailedTaskProviderRecovery(originalTask, "user", "manual-recovery:competitor", time.Minute); !errors.Is(claimErr, repository.ErrTaskProviderRecoveryConflict) {
					t.Errorf("concurrent claim: %v", claimErr)
				}
				var updates map[string]any
				switch scenario {
				case "transport_error":
					conn, _, e := w.(http.Hijacker).Hijack()
					if e != nil {
						t.Error(e)
						return
					}
					_ = conn.Close()
					return
				case "http_error":
					http.Error(w, `{"status":"failed"}`, 502)
					return
				case "decode_error":
					_, _ = w.Write([]byte(`{"status":"failed"`))
					return
				case "lease_stolen":
					updates = map[string]any{"lease_owner": "manual-recovery:new-owner"}
				case "lease_expired":
					updates = map[string]any{"lease_expires_at": time.Now().Add(-time.Minute)}
				case "route_changed":
					updates = map[string]any{"route_run": 99}
				case "cancelled_during_query":
					updates = map[string]any{"status": model.TaskStatusCancelled}
				}
				if updates != nil {
					if e := db.Model(&model.Task{}).Where("id = ?", originalTask).Updates(updates).Error; e != nil {
						t.Error(e)
					}
				}
				status := "failed"
				if scenario == "cancelled" || scenario == "processing" {
					status = scenario
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"id": "original-provider-id", "status": status})
			}))
			defer server.Close()
			t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")
			if err = db.Model(&model.ModelChannel{}).Where("id = ?", "channel").Update("base_url", server.URL).Error; err != nil {
				t.Fatal(err)
			}
			if scenario == "refund_failure" {
				if err = db.Exec(`CREATE TRIGGER reject_terminal_refund BEFORE INSERT ON credit_ledger_entries WHEN NEW.type = 'refund' BEGIN SELECT RAISE(ABORT, 'injected refund failure'); END`).Error; err != nil {
					t.Fatal(err)
				}
			}
			var ledgerBefore int64
			if err = db.Model(&model.CreditLedgerEntry{}).Count(&ledgerBefore).Error; err != nil {
				t.Fatal(err)
			}
			accountBefore, err := s.repo.CreditAccount("user")
			if err != nil {
				t.Fatal(err)
			}
			_, queryErr := s.QueryFailedVideoTask(context.Background(), "user", originalTask)
			if scenario != "processing" && queryErr == nil {
				t.Fatal("expected provider or reconciliation error")
			}
			if scenario == "lease_stolen" || scenario == "lease_expired" || scenario == "route_changed" || scenario == "cancelled_during_query" {
				if !errors.Is(queryErr, repository.ErrTaskStateConflict) {
					t.Fatalf("missing lease/state fence: %v", queryErr)
				}
			}
			if scenario == "refund_failure" && !strings.Contains(queryErr.Error(), "injected refund failure") {
				t.Fatalf("refund injection not reached: %v", queryErr)
			}
			stored, err := s.repo.Task(originalTask)
			if err != nil {
				t.Fatal(err)
			}
			order, err := s.repo.BillingOrder(originalBill)
			if err != nil {
				t.Fatal(err)
			}
			resolved := scenario == "failed" || scenario == "cancelled"
			if stored.ID != originalTask || stored.BillingOrderID != originalBill || stored.ProviderRequestID != "original-provider-id" || order.TaskID != originalTask {
				t.Fatal("original identity changed")
			}
			if stored.Status != model.TaskStatusFailed && !(scenario == "cancelled_during_query" && stored.Status == model.TaskStatusCancelled) {
				t.Fatal("terminal task resurrected")
			}
			var ledgerAfter int64
			if err = db.Model(&model.CreditLedgerEntry{}).Count(&ledgerAfter).Error; err != nil {
				t.Fatal(err)
			}
			if resolved {
				if stored.PollStage != scenario || taskExecutionDiagnostic(stored).SubmissionOutcome == "unknown" || order.Status != model.BillingStatusRefunded || ledgerAfter != ledgerBefore+1 {
					t.Fatalf("terminal outcome not reconciled: stage=%s diagnostic=%s order=%s ledger=%d->%d", stored.PollStage, stored.ExecutionDiagnosticJSON, order.Status, ledgerBefore, ledgerAfter)
				}
				_, _ = s.QueryFailedVideoTask(context.Background(), "user", originalTask)
				laterQueryFails.Store(true)
				if _, err = s.QueryFailedVideoTask(context.Background(), "user", originalTask); err == nil {
					t.Fatal("expected subsequent HTTP error")
				}
				var repeated int64
				if err = db.Model(&model.CreditLedgerEntry{}).Count(&repeated).Error; err != nil || repeated != ledgerAfter {
					t.Fatal("duplicate refund", err)
				}
			} else {
				if stored.PollStage != "processing" || taskExecutionDiagnostic(stored).SubmissionOutcome != "unknown" || order.Status != model.BillingStatusUncertain || ledgerAfter != ledgerBefore {
					t.Fatalf("uncertainty or billing changed after failed query: %+v %+v", stored, order)
				}
			}
			accountAfter, err := s.repo.CreditAccount("user")
			if err != nil {
				t.Fatal(err)
			}
			delta := int64(0)
			if resolved {
				delta = order.AmountMicrocredits
			}
			if accountAfter.AvailableMicrocredits != accountBefore.AvailableMicrocredits+delta || accountAfter.ReservedMicrocredits != accountBefore.ReservedMicrocredits-delta {
				t.Fatal("account refund/rollback changed wrong amount")
			}
			if scenario == "lease_stolen" && stored.LeaseOwner != "manual-recovery:new-owner" {
				t.Fatal("stale release cleared new lease")
			}
			activity, err := s.GetAdvertisingActivity("user", project, production)
			if err != nil || (len(activity.Items) == 0) != resolved {
				t.Fatalf("activity resolved=%v items=%d err=%v", resolved, len(activity.Items), err)
			}
			batchCounts(t, db, 1)
			if creates.Load() != 0 || queries.Load() < 1 {
				t.Fatal("unexpected provider requests")
			}
			if scenario == "refund_failure" {
				if err = db.Exec("DROP TRIGGER reject_terminal_refund").Error; err != nil {
					t.Fatal(err)
				}
				_, _ = s.QueryFailedVideoTask(context.Background(), "user", originalTask)
				order, err = s.repo.BillingOrder(originalBill)
				if err != nil || order.Status != model.BillingStatusRefunded {
					t.Fatal("refund retry did not reconcile original bill", err)
				}
				resolved = true
			}
			// The test changed the channel URL after the first quote; approve a fresh quote.
			req.ClientOperationID = uuid.NewString()
			next, err = s.PrepareAdvertisingBatch("user", project, production, req)
			if err != nil {
				t.Fatal(err)
			}
			_, err = s.ExecuteAdvertisingBatch("user", project, production, next.Batch.ID, batchApprovals(next))
			if resolved {
				if err != nil {
					t.Fatal("resolved slot not released", err)
				}
			} else {
				requireBatchConflict(t, err)
			}
		})
	}
}

func TestAdvertisingProviderRecoveryWithoutIDDoesNotCreate(t *testing.T) {
	s, db, project, production, req := advertisingBatchFixture(t)
	s.coordinator = platform.NewCoordinatorWithRedis(nil, "ad-recovery-test")
	s.platform = nil
	q, err := s.PrepareAdvertisingSubmission("user", project, production, req.AdvertisingSubmissionRequest)
	if err != nil {
		t.Fatal(err)
	}
	q, err = s.ExecuteAdvertisingSubmission("user", project, production, q.ID, q.Quote.QuoteHash)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Model(&model.Task{}).Where("id = ?", q.Task.ID).Updates(map[string]any{"status": model.TaskStatusFailed, "execution_diagnostic_json": `{"submissionOutcome":"unknown"}`}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err = s.QueryFailedVideoTask(context.Background(), "user", q.Task.ID); err == nil {
		t.Fatal("missing ID accepted")
	}
	activity, err := s.GetAdvertisingActivity("user", project, production)
	if err != nil || len(activity.Items) != 1 {
		t.Fatal("unknown task released")
	}
	batchCounts(t, db, 1)
}
