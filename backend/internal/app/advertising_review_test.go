package app

import (
	"encoding/json"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
)

func reviewFixture(t *testing.T) (*Service, *gorm.DB, string, string, string, string) {
	t.Helper()
	s, db, project, p, req := advertisingBatchFixture(t)
	seedResource(t, db, "review-media", "user", "video/mp4")
	if err := db.Model(&model.Resource{}).Where("id = ?", "review-media").Updates(map[string]any{"duration_ms": 6000, "object_key": "review.mp4"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(s.dataDir, "resources"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.dataDir, "resources", "review.mp4"), []byte("readability fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	task := model.Task{ID: "review-task", UserID: "user", ProjectID: project, Type: "canvas_video", Status: model.TaskStatusSucceeded, ResultJSON: `{"storageKey":"resource:review-media"}`, InputJSON: `{"apiKey":"secret-leak","url":"https://example.invalid/?signature=secret"}`}
	now := time.Now().UTC()
	sub := model.AdvertisingSubmission{ID: "review-sub", ProductionID: p, ShotID: req.ShotID, Kind: "video", ClientOperationID: uuid.NewString(), TaskID: &task.ID, ApprovedAt: &now, RequestJSON: "{}", QuoteJSON: "{}"}
	for _, v := range []any{&task, &sub} {
		if err := db.Create(v).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := s.repo.ReplaceAdvertisingReferences(project, "candidate:"+sub.ID, []string{"review-media"}); err != nil {
		t.Fatal(err)
	}
	return s, db, project, p, req.ShotID, sub.ID
}

func TestAdvertisingReviewCandidateCASReplayRollback(t *testing.T) {
	s, db, project, p, shot, sub := reviewFixture(t)
	req := AdvertisingReviewRequest{ClientOperationID: uuid.NewString(), ExpectedProductionRevision: 1, Checkpoint: "candidate", SubmissionID: sub, Decision: "approve", Reason: "checked product"}
	if _, err := s.CreateAdvertisingReview("other", project, p, req); err == nil {
		t.Fatal("foreign review")
	}
	if err := db.Exec("CREATE TRIGGER fail_review_event BEFORE INSERT ON advertising_events WHEN NEW.kind = 'candidate_reviewed' BEGIN SELECT RAISE(ABORT, 'audit failed'); END").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateAdvertisingReview("user", project, p, req); err == nil {
		t.Fatal("audit failure committed")
	}
	current, _ := s.GetAdvertisingProduction("user", project, p)
	if current.Production.Revision != 1 || current.Settings.Selections[shot].VideoSubmissionID != "" {
		t.Fatal("selection escaped rollback")
	}
	if err := db.Exec("DROP TRIGGER fail_review_event").Error; err != nil {
		t.Fatal(err)
	}
	first, err := s.CreateAdvertisingReview("user", project, p, req)
	if err != nil {
		t.Fatal(err)
	}
	if first.Production.Production.Revision != 2 || first.Production.Settings.Selections[shot].VideoSubmissionID != sub {
		t.Fatalf("not selected: %+v", first)
	}
	again, err := s.CreateAdvertisingReview("user", project, p, req)
	if err != nil || again.Review.ID != first.Review.ID {
		t.Fatalf("replay: %+v %v", again, err)
	}
	req.Reason = "changed"
	if _, err = s.CreateAdvertisingReview("user", project, p, req); err == nil {
		t.Fatal("samekey changed payload")
	}
	req.ClientOperationID = uuid.NewString()
	req.Decision = "reject"
	req.ExpectedProductionRevision = 2
	if _, err = s.CreateAdvertisingReview("user", project, p, req); err == nil {
		t.Fatal("selected reject without clear intent")
	}
	req.ClearSelected = true
	req.ExpectedProductionRevision = 1
	if _, err = s.CreateAdvertisingReview("user", project, p, req); err == nil {
		t.Fatal("stale clear")
	}
	req.ExpectedProductionRevision = 2
	cleared, err := s.CreateAdvertisingReview("user", project, p, req)
	if err != nil {
		t.Fatal(err)
	}
	if cleared.Production.Settings.Selections[shot].VideoSubmissionID != "" || cleared.Production.Production.Revision != 3 {
		t.Fatal("not cleared")
	}
	original, err := s.GetAdvertisingReviewForOperation("user", project, p, first.Review.ClientOperationID)
	if err != nil || original.Production.Production.Revision != 2 {
		t.Fatal("lost immutable operation response", err)
	}
	if _, err := s.repo.ResourceForUser("user", "review-media"); err != nil {
		t.Fatal("rejection deleted media")
	}
}

func TestAdvertisingReviewIssuesBoundsResolution(t *testing.T) {
	s, db, project, p, _, sub := reviewFixture(t)
	at := int64(6001)
	req := AdvertisingIssueRequest{ClientOperationID: uuid.NewString(), SubmissionID: sub, AtMs: &at, Category: "product", Note: "logo distortion"}
	for _, v := range []int64{-1, 6001} {
		at = v
		if _, err := s.CreateAdvertisingIssue("user", project, p, req); err == nil {
			t.Fatal("invalid time", v)
		}
	}
	at = 6000
	first, err := s.CreateAdvertisingIssue("user", project, p, req)
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.CreateAdvertisingIssue("user", project, p, req)
	if err != nil || again.ID != first.ID {
		t.Fatal("issue replay", err)
	}
	req.Note = "changed"
	if _, err := s.CreateAdvertisingIssue("user", project, p, req); err == nil {
		t.Fatal("issue payload mismatch")
	}
	update := AdvertisingIssueResolution{ExpectedRevision: 1, Status: "resolved", ResolutionNote: "watched replacement"}
	if err := db.Exec("CREATE TRIGGER fail_issue_event BEFORE INSERT ON advertising_events WHEN NEW.kind = 'issue_resolved' BEGIN SELECT RAISE(ABORT, 'audit failed'); END").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.ResolveAdvertisingIssue("user", project, p, first.ID, update); err == nil {
		t.Fatal("resolution escaped audit")
	}
	if err := db.Exec("DROP TRIGGER fail_issue_event").Error; err != nil {
		t.Fatal(err)
	}
	done, err := s.ResolveAdvertisingIssue("user", project, p, first.ID, update)
	if err != nil || done.Revision != 2 || done.ResolvedBy != "user" {
		t.Fatalf("resolution %+v %v", done, err)
	}
	if _, err := s.ResolveAdvertisingIssue("user", project, p, first.ID, update); err == nil {
		t.Fatal("stale issue CAS")
	}
	update.ExpectedRevision = 2
	update.ResolutionNote = ""
	if _, err := s.ResolveAdvertisingIssue("user", project, p, first.ID, update); err == nil {
		t.Fatal("missing human note")
	}
}

func TestAdvertisingReviewRejectUnavailableSelection(t *testing.T) {
	s, db, project, p, shot, sub := reviewFixture(t)
	req := AdvertisingReviewRequest{ClientOperationID: uuid.NewString(), ExpectedProductionRevision: 1, Checkpoint: "candidate", SubmissionID: sub, Decision: "approve", Reason: "watched"}
	if _, err := s.CreateAdvertisingReview("user", project, p, req); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.Resource{}).Where("id = ?", "review-media").Update("object_key", "missing.mp4").Error; err != nil {
		t.Fatal(err)
	}
	req.ClientOperationID = uuid.NewString()
	req.ExpectedProductionRevision = 2
	req.Decision = "reject"
	req.ClearSelected = true
	result, err := s.CreateAdvertisingReview("user", project, p, req)
	if err != nil || result.Production.Settings.Selections[shot].VideoSubmissionID != "" {
		t.Fatalf("cannot explicitly clear unavailable selected candidate: %+v %v", result, err)
	}
}

func TestAdvertisingReviewClearWithOtherUnavailableMedia(t *testing.T) {
	for _, missingMusic := range []bool{false, true} {
		name := "two_missing_videos"
		if missingMusic {
			name = "two_missing_videos_and_unavailable_music"
		}
		t.Run(name, func(t *testing.T) {
			s, db, project, p, shot, sub := reviewFixture(t)
			current, err := s.GetAdvertisingProduction("user", project, p)
			if err != nil {
				t.Fatal(err)
			}
			secondShot := current.ConfirmedSnapshot.Snapshot.Draft.Shots[1].ID
			seedResource(t, db, "review-media-two", "user", "video/mp4")
			if err := db.Model(&model.Resource{}).Where("id = ?", "review-media-two").Updates(map[string]any{"duration_ms": 6000, "object_key": "review-two.mp4"}).Error; err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(s.dataDir, "resources", "review-two.mp4"), []byte("readability fixture"), 0600); err != nil {
				t.Fatal(err)
			}
			now := time.Now().UTC()
			task := model.Task{ID: "review-task-two", UserID: "user", ProjectID: project, Type: "canvas_video", Status: model.TaskStatusSucceeded, ResultJSON: `{"storageKey":"resource:review-media-two"}`}
			second := model.AdvertisingSubmission{ID: "review-sub-two", ProductionID: p, ShotID: secondShot, Kind: "video", ClientOperationID: uuid.NewString(), TaskID: &task.ID, ApprovedAt: &now, RequestJSON: "{}", QuoteJSON: "{}"}
			for _, row := range []any{&task, &second} {
				if err := db.Create(row).Error; err != nil {
					t.Fatal(err)
				}
			}
			if err := s.repo.ReplaceAdvertisingReferences(project, "candidate:"+second.ID, []string{"review-media-two"}); err != nil {
				t.Fatal(err)
			}
			for i, id := range []string{sub, second.ID} {
				if _, err := s.CreateAdvertisingReview("user", project, p, AdvertisingReviewRequest{ClientOperationID: uuid.NewString(), ExpectedProductionRevision: int64(i + 1), Checkpoint: "candidate", SubmissionID: id, Decision: "approve", Reason: "watched"}); err != nil {
					t.Fatal("setup adoption", err)
				}
			}
			settings := current.Settings
			settings.Selections = map[string]AdvertisingSelection{shot: {VideoSubmissionID: sub, VideoStartMs: 1000}, secondShot: {VideoSubmissionID: second.ID, VideoStartMs: 500}}
			settings.TTSModelID, settings.VoiceID = "keep-model", "keep-voice"
			if missingMusic {
				seedResource(t, db, "review-music", "user", "audio/wav")
				settings.MusicResourceID = "review-music"
			}
			if _, err := s.SaveAdvertisingProductionSettings("user", project, p, 3, settings); err != nil {
				t.Fatal(err)
			}
			// Both valid adopted files disappear. The saved music also becomes unavailable.
			for _, file := range []string{"review.mp4", "review-two.mp4"} {
				if err := os.Remove(filepath.Join(s.dataDir, "resources", file)); err != nil {
					t.Fatal(err)
				}
			}
			if missingMusic {
				if err := db.Model(&model.Resource{}).Where("id = ?", "review-music").Update("status", "pending").Error; err != nil {
					t.Fatal(err)
				}
			}
			assertState := func(revision, reviews, selections int64) {
				t.Helper()
				stored, err := s.GetAdvertisingProduction("user", project, p)
				if err != nil || stored.Production.Revision != revision || !reflect.DeepEqual(stored.Settings, settings) {
					t.Fatalf("unexpected production mutation: %+v %v", stored, err)
				}
				for _, check := range []struct {
					table, kind string
					want        int64
				}{{"advertising_reviews", "", reviews}, {"advertising_events", "candidate_reviewed", reviews}, {"advertising_events", "selection_changed", selections}} {
					q := db.Table(check.table).Where("production_id = ?", p)
					if check.kind != "" {
						q = q.Where("kind = ?", check.kind)
					}
					var n int64
					if err := q.Count(&n).Error; err != nil || n != check.want {
						t.Fatalf("%s/%s count=%d want=%d: %v", check.table, check.kind, n, check.want, err)
					}
				}
			}
			req := AdvertisingReviewRequest{ClientOperationID: uuid.NewString(), ExpectedProductionRevision: 4, Checkpoint: "candidate", SubmissionID: sub, Decision: "reject", ClearSelected: true, Reason: "media lost; clear only this selection"}
			if _, err := s.CreateAdvertisingReview("other", project, p, req); err == nil {
				t.Fatal("foreign clear accepted")
			}
			bad := req
			bad.ExpectedProductionRevision = 3
			_, err = s.CreateAdvertisingReview("user", project, p, bad)
			requireBatchConflict(t, err)
			bad = req
			bad.ClearSelected = false
			if _, err := s.CreateAdvertisingReview("user", project, p, bad); err == nil {
				t.Fatal("implicit clear accepted")
			}
			bad.Decision = "approve"
			if _, err := s.CreateAdvertisingReview("user", project, p, bad); err == nil {
				t.Fatal("unavailable approval accepted")
			}
			if _, err := s.SaveAdvertisingProductionSettings("user", project, p, 4, settings); err == nil {
				t.Fatal("unavailable user settings accepted")
			}
			assertState(4, 2, 4)
			first, err := s.CreateAdvertisingReview("user", project, p, req)
			if err != nil {
				t.Fatalf("explicit clear blocked by unrelated unavailable media: %v", err)
			}
			settings.Selections[shot] = AdvertisingSelection{}
			assertState(5, 3, 5)
			if first.Production.Production.Revision != 5 || !reflect.DeepEqual(first.Production.Settings, settings) {
				t.Fatal("clear response changed unrelated settings")
			}
			secondReq := req
			secondReq.ClientOperationID, secondReq.SubmissionID = uuid.NewString(), second.ID
			secondReq.ExpectedProductionRevision = 5
			if err := db.Exec("CREATE TRIGGER fail_clear_review BEFORE INSERT ON advertising_events WHEN NEW.kind = 'candidate_reviewed' BEGIN SELECT RAISE(ABORT, 'audit failed'); END").Error; err != nil {
				t.Fatal(err)
			}
			if _, err := s.CreateAdvertisingReview("user", project, p, secondReq); err == nil || !strings.Contains(err.Error(), "audit failed") {
				t.Fatalf("clear must reach audit and roll back its failure: %v", err)
			}
			assertState(5, 3, 5)
			if err := db.Exec("DROP TRIGGER fail_clear_review").Error; err != nil {
				t.Fatal(err)
			}
			if _, err := s.CreateAdvertisingReview("user", project, p, secondReq); err != nil {
				t.Fatalf("second clear blocked: %v", err)
			}
			settings.Selections[secondShot] = AdvertisingSelection{}
			assertState(6, 4, 6)
			// Compare wire responses, not time.Time location pointers after JSON/DB round trips.
			firstJSON, err := json.Marshal(first)
			if err != nil {
				t.Fatal(err)
			}
			again, err := s.CreateAdvertisingReview("user", project, p, req)
			if err != nil {
				t.Fatal(err)
			}
			againJSON, err := json.Marshal(again)
			if err != nil || string(againJSON) != string(firstJSON) {
				t.Fatal("replay did not preserve original response", err)
			}
			recovered, err := s.GetAdvertisingReviewForOperation("user", project, p, req.ClientOperationID)
			if err != nil {
				t.Fatal(err)
			}
			recoveredJSON, err := json.Marshal(recovered)
			if err != nil || string(recoveredJSON) != string(firstJSON) {
				t.Fatal("GET recovery lost original response", err)
			}
			assertState(6, 4, 6)
			for _, id := range []string{"review-media", "review-media-two"} {
				if _, err := s.repo.ResourceForUser("user", id); err != nil {
					t.Fatal("clear deleted resource", err)
				}
				if err := s.repo.DeleteResource("user", id); err == nil {
					t.Fatal("clear lost historical media protection")
				}
			}
		})
	}
}

func reviewRenderFixture(t *testing.T, s *Service, db *gorm.DB, project, p string) {
	t.Helper()
	now := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	for _, row := range []any{
		&model.AdvertisingAssembly{ID: "review-assembly", ProductionID: p, ClientOperationID: "assembly-op", SourceProductionRevision: 1, SelectionHash: "frozen-selection", TimelineJSON: "{}"},
		&model.Task{ID: "review-render-task", UserID: "user", ProjectID: project, Type: model.TaskTypeTimelineRender, Status: model.TaskStatusSucceeded, ResultJSON: `{"resourceId":"review-media"}`, CreatedAt: now, CompletedAt: &now},
		&model.AdvertisingRenderAttempt{ID: "review-render", AssemblyID: "review-assembly", ClientOperationID: "render-op", TaskID: "review-render-task"},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := s.repo.ReplaceAdvertisingReferences(project, "render:review-render", []string{"review-media"}); err != nil {
		t.Fatal(err)
	}
}

func TestAdvertisingReviewAssemblyIdentity(t *testing.T) {
	s, db, project, p, _, _ := reviewFixture(t)
	reviewRenderFixture(t, s, db, project, p)
	current, _ := s.GetAdvertisingProduction("user", project, p)
	var source model.Resource
	db.First(&source, "id = ?", "review-media")
	media := map[string]AdvertisingProbedMedia{}
	for i, shot := range current.ConfirmedSnapshot.Snapshot.Draft.Shots {
		id := "review-sub"
		taskID := "review-task"
		if i > 0 {
			id = "selected-" + shot.ID
			taskID = "selected-task-" + shot.ID
			now := time.Now()
			db.Create(&model.Task{ID: taskID, UserID: "user", ProjectID: project, Type: "canvas_video", Status: model.TaskStatusSucceeded, ResultJSON: `{"storageKey":"resource:review-media"}`})
			db.Create(&model.AdvertisingSubmission{ID: id, ProductionID: p, ShotID: shot.ID, Kind: "video", ClientOperationID: uuid.NewString(), TaskID: &taskID, ApprovedAt: &now})
			s.repo.ReplaceAdvertisingReferences(project, "candidate:"+id, []string{source.ID})
		}
		current.Settings.Selections[shot.ID] = AdvertisingSelection{VideoSubmissionID: id}
		media[id] = AdvertisingProbedMedia{Resource: source, TaskID: taskID, Width: 640, Height: 360, DurationMs: 6000}
	}
	settingsJSON, _ := json.Marshal(current.Settings)
	db.Model(&model.AdvertisingProduction{}).Where("id = ?", p).Update("settings_json", string(settingsJSON))
	provenance, err := advertisingCutProvenance(current.ConfirmedSnapshot, current.Settings, media, "pad")
	if err != nil {
		t.Fatal(err)
	}
	frozen, _ := json.Marshal(provenance)
	db.Model(&model.AdvertisingAssembly{}).Where("id = ?", "review-assembly").Updates(map[string]any{"provenance_json": string(frozen), "source_fingerprint": provenance.Fingerprint})
	req := AdvertisingReviewRequest{ClientOperationID: uuid.NewString(), ExpectedProductionRevision: 1, Checkpoint: "assembly", AssemblyID: "review-assembly", RenderAttemptID: "review-render", ResourceID: "review-media", Decision: "approve", Reason: "preview watched"}
	for _, id := range []string{"", "missing"} {
		bad := req
		bad.RenderAttemptID = id
		if _, err := s.CreateAdvertisingReview("user", project, p, bad); err == nil {
			t.Fatal("inferred render", id)
		}
	}
	for _, status := range []model.TaskStatus{model.TaskStatusQueued, model.TaskStatusFailed} {
		if err := db.Model(&model.Task{}).Where("id = ?", "review-render-task").Update("status", status).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := s.CreateAdvertisingReview("user", project, p, req); err == nil {
			t.Fatal("approved unavailable render")
		}
	}
	if err := db.Model(&model.Task{}).Where("id = ?", "review-render-task").Update("status", model.TaskStatusSucceeded).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE TRIGGER fail_assembly_review BEFORE INSERT ON advertising_events WHEN NEW.kind = 'assembly_reviewed' BEGIN SELECT RAISE(ABORT, 'audit failed'); END").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateAdvertisingReview("user", project, p, req); err == nil {
		t.Fatal("review escaped audit rollback")
	}
	if err := db.Exec("DROP TRIGGER fail_assembly_review").Error; err != nil {
		t.Fatal(err)
	}
	result, err := s.CreateAdvertisingReview("user", project, p, req)
	if err != nil || result.Review.RenderAttemptID != "review-render" || result.Review.ResourceID != "review-media" || result.Review.SelectionHash != "frozen-selection" {
		t.Fatalf("output identity: %+v %v", result, err)
	}
	at := int64(6001)
	issue := AdvertisingIssueRequest{ClientOperationID: uuid.NewString(), AssemblyID: req.AssemblyID, RenderAttemptID: req.RenderAttemptID, AtMs: &at, Category: "audio", Note: "check mix"}
	if _, err := s.CreateAdvertisingIssue("user", project, p, issue); err == nil {
		t.Fatal("assembly timestamp outside media")
	}
	at = 1000
	if _, err := s.CreateAdvertisingIssue("user", project, p, issue); err != nil {
		t.Fatal(err)
	}
	result.Production.Settings.Fit = "crop"
	if _, err := s.SaveAdvertisingProductionSettings("user", project, p, 1, result.Production.Settings); err != nil {
		t.Fatal(err)
	}
	req.ClientOperationID = uuid.NewString()
	req.ExpectedProductionRevision = 2
	if _, err := s.CreateAdvertisingReview("user", project, p, req); err == nil {
		t.Fatal("old assembly approved current version")
	}
}

func TestAdvertisingReviewHistoricalFactsAndPagination(t *testing.T) {
	s, db, project, p, shot, _ := reviewFixture(t)
	reviewRenderFixture(t, s, db, project, p)
	at := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	if err := db.Model(&model.Task{}).Where("id = ?", "review-task").Updates(map[string]any{"status": model.TaskStatusFailed, "created_at": at, "completed_at": at}).Error; err != nil {
		t.Fatal(err)
	}
	all, err := s.ListAdvertisingEvents("user", project, p, "", "", 1, 100)
	if err != nil {
		t.Fatal(err)
	}
	facts := 0
	for _, event := range all.Items {
		if !event.Fact {
			continue
		}
		facts++
		if !event.CreatedAt.Equal(at) || event.Kind != "task_created_fact" && event.Kind != "task_result_fact" {
			t.Fatalf("invented fact: %+v", event)
		}
	}
	if facts != 4 {
		t.Fatalf("want original candidate AND render facts, got %d", facts)
	}
	raw, _ := json.Marshal(all)
	if strings.Contains(string(raw), "secret-leak") || strings.Contains(string(raw), "signature=") || strings.Contains(string(raw), "apiKey") {
		t.Fatal("leaked task secrets")
	}
	first, _ := s.ListAdvertisingEvents("user", project, p, "", "", 1, 1)
	second, _ := s.ListAdvertisingEvents("user", project, p, "", "", 2, 1)
	if first.Items[0].ID == second.Items[0].ID || first.Items[0].ID != all.Items[0].ID || second.Items[0].ID != all.Items[1].ID {
		t.Fatal("unstable equal-time paging")
	}
	filtered, err := s.ListAdvertisingEvents("user", project, p, shot, "task_result_fact", 1, 20)
	if err != nil || filtered.Total != 1 || filtered.Items[0].Details.Status != "failed" {
		t.Fatalf("failed fact filter %+v %v", filtered, err)
	}
	if _, err := s.ListAdvertisingEvents("other", project, "", "", "", 1, 20); err == nil {
		t.Fatal("foreign history")
	}
	if _, err := s.ListAdvertisingEvents("user", project, "missing", "", "", 1, 20); err == nil {
		t.Fatal("missing production history")
	}
}

func TestAdvertisingReviewTwoServices(t *testing.T) {
	s, db, project, p, _, sub := reviewFixture(t)
	other := &Service{repo: repository.New(db), dataDir: s.dataDir}
	req := AdvertisingReviewRequest{ClientOperationID: uuid.NewString(), ExpectedProductionRevision: 1, Checkpoint: "candidate", SubmissionID: sub, Decision: "approve", Reason: "watched"}
	var wg sync.WaitGroup
	results := make(chan AdvertisingReviewResult, 2)
	errs := make(chan error, 2)
	for _, svc := range []*Service{s, other} {
		wg.Add(1)
		go func(svc *Service) {
			defer wg.Done()
			r, e := svc.CreateAdvertisingReview("user", project, p, req)
			results <- r
			errs <- e
		}(svc)
	}
	wg.Wait()
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	a, b := <-results, <-results
	if a.Review.ID != b.Review.ID || a.Production.Production.Revision != 2 {
		t.Fatal("same-key review applied twice")
	}
	var n int64
	if err := db.Model(&model.AdvertisingEvent{}).Where("kind = ?", "candidate_reviewed").Count(&n).Error; err != nil || n != 1 {
		t.Fatal("duplicate decision event", n, err)
	}
}

func TestAdvertisingReviewValidationAndDeletion(t *testing.T) {
	s, db, project, p, shot, sub := reviewFixture(t)
	req := AdvertisingReviewRequest{ClientOperationID: uuid.NewString(), ExpectedProductionRevision: 1, Checkpoint: "candidate", SubmissionID: sub, Decision: "approve", Reason: "watched"}
	for _, changes := range []map[string]any{{"duration_ms": 4999}, {"duration_ms": 0}, {"mime_type": "image/png"}, {"object_key": "missing.mp4"}, {"status": "pending"}} {
		if err := db.Model(&model.Resource{}).Where("id = ?", "review-media").Updates(changes).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := s.CreateAdvertisingReview("user", project, p, req); err == nil {
			t.Fatal("invalid media adopted", changes)
		}
		if err := db.Model(&model.Resource{}).Where("id = ?", "review-media").Updates(map[string]any{"duration_ms": 6000, "mime_type": "video/mp4", "object_key": "review.mp4", "status": "ready"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Model(&model.Task{}).Where("id = ?", "review-task").Update("status", model.TaskStatusFailed).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateAdvertisingReview("user", project, p, req); err == nil {
		t.Fatal("failed candidate adopted")
	}
	if err := db.Model(&model.Task{}).Where("id = ?", "review-task").Update("status", model.TaskStatusSucceeded).Error; err != nil {
		t.Fatal(err)
	}
	// A different confirmed version has a distinct production scope even for the same account.
	current, err := s.GetAdvertisingProduction("user", project, p)
	if err != nil {
		t.Fatal(err)
	}
	revision := model.AdvertisingRevision{ID: "other-revision", ProjectID: project, SourceRevision: 99, SnapshotJSON: "{}"}
	if err := db.Create(&revision).Error; err != nil {
		t.Fatal(err)
	}
	other := model.AdvertisingProduction{ID: "other-production", ProjectID: project, ConfirmedRevisionID: revision.ID, Revision: 1, SettingsJSON: "{}"}
	if err := db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateAdvertisingReview("user", project, other.ID, req); err == nil {
		t.Fatal("cross-version candidate adopted")
	}
	for _, reason := range []string{"", strings.Repeat("字", 2001)} {
		bad := req
		bad.Reason = reason
		if _, err := s.CreateAdvertisingReview("user", project, p, bad); err == nil {
			t.Fatal("invalid reason")
		}
	}
	if _, err := s.CreateAdvertisingReview("user", project, p, req); err != nil {
		t.Fatal(err)
	}
	issueReq := AdvertisingIssueRequest{ClientOperationID: uuid.NewString(), SubmissionID: sub, Category: "product", Note: "check logo"}
	for _, bad := range []AdvertisingIssueRequest{
		{ClientOperationID: uuid.NewString(), Category: "product", Note: "missing target"},
		{ClientOperationID: uuid.NewString(), SubmissionID: sub, AssemblyID: "assembly", RenderAttemptID: "render", Category: "product", Note: "two targets"},
		{ClientOperationID: uuid.NewString(), SubmissionID: sub, Category: strings.Repeat("字", 25), Note: "too long"},
		{ClientOperationID: uuid.NewString(), SubmissionID: sub, Category: "product", Note: strings.Repeat("字", 2001)},
	} {
		if _, err := s.CreateAdvertisingIssue("user", project, p, bad); err == nil {
			t.Fatal("invalid issue")
		}
	}
	issue, err := s.CreateAdvertisingIssue("user", project, p, issueReq)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ResolveAdvertisingIssue("other", project, p, issue.ID, AdvertisingIssueResolution{ExpectedRevision: 1, Status: "resolved", ResolutionNote: "forged"}); err == nil {
		t.Fatal("foreign resolution")
	}
	if _, err := s.ResolveAdvertisingIssue("user", project, other.ID, issue.ID, AdvertisingIssueResolution{ExpectedRevision: 1, Status: "resolved", ResolutionNote: "wrong scope"}); err == nil {
		t.Fatal("cross-production issue")
	}
	if _, err := s.repo.ResourceForUser("user", "review-media"); err != nil {
		t.Fatal(err)
	}
	if err := s.repo.DeleteResource("user", "review-media"); err == nil {
		t.Fatal("audit candidate not protected")
	}
	current.Settings.Selections[shot] = AdvertisingSelection{}
	if _, err := s.SaveAdvertisingProductionSettings("user", project, p, 2, current.Settings); err != nil {
		t.Fatal(err)
	}
	if err := s.repo.DeleteResource("user", "review-media"); err == nil {
		t.Fatal("cleared historical candidate not protected")
	}
	if err := s.DeleteAdvertisingProject("user", project, 2); err != nil {
		t.Fatal(err)
	}
	for _, row := range []any{&model.AdvertisingEvent{}, &model.AdvertisingReview{}, &model.AdvertisingIssue{}, &model.AdvertisingProduction{}} {
		var n int64
		if err := db.Model(row).Count(&n).Error; err != nil || n != 0 {
			t.Fatalf("orphan %T %d %v", row, n, err)
		}
	}
	if _, err := s.repo.ResourceForUser("user", "review-media"); err != nil {
		t.Fatal("deletion removed media", err)
	}
}

func TestAdvertisingReviewAuditTransactions(t *testing.T) {
	s, db, project, p, batchReq := advertisingBatchFixture(t)
	batch, err := s.PrepareAdvertisingBatch("user", project, p, batchReq)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE TRIGGER fail_admission_event BEFORE INSERT ON advertising_events WHEN NEW.kind = 'submission_admitted' BEGIN SELECT RAISE(ABORT, 'audit failed'); END").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.ExecuteAdvertisingBatch("user", project, p, batch.Batch.ID, batchApprovals(batch)); err == nil {
		t.Fatal("admission escaped audit")
	}
	batchCounts(t, db, 0)
	if err := db.Exec("DROP TRIGGER fail_admission_event").Error; err != nil {
		t.Fatal(err)
	}
	admitted, err := s.ExecuteAdvertisingBatch("user", project, p, batch.Batch.ID, batchApprovals(batch))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ExecuteAdvertisingBatch("user", project, p, batch.Batch.ID, batchApprovals(batch)); err != nil {
		t.Fatal(err)
	}
	var n int64
	if err := db.Model(&model.AdvertisingEvent{}).Where("kind = ?", "submission_admitted").Count(&n).Error; err != nil || n != 2 {
		t.Fatal("duplicate admission events", n, err)
	}
	seedResource(t, db, "output", "user", "video/mp4")
	task, err := s.repo.Task(admitted.Items[0].Task.ID)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, _ := json.Marshal(mediaCheckpoint{Mode: "video", Items: []mediaCheckpointItem{{ResourceID: "output"}}})
	task.MediaRecoveryJSON, err = s.encryptSettingSecret(string(checkpoint))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.Task{}).Where("id = ?", task.ID).Update("media_recovery_json", task.MediaRecoveryJSON).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE TRIGGER fail_output_event BEFORE INSERT ON advertising_events WHEN NEW.kind = 'candidate_output' BEGIN SELECT RAISE(ABORT, 'audit failed'); END").Error; err != nil {
		t.Fatal(err)
	}
	if err := s.saveTaskCompletionWithinStorageQuota(task, []byte(`{"storageKey":"resource:output"}`), nil, false); err == nil {
		t.Fatal("success escaped output audit")
	}
	fresh, err := s.repo.Task(task.ID)
	if err != nil || fresh.Status == model.TaskStatusSucceeded {
		t.Fatal("partial task completion", err)
	}
	if err := db.Exec("DROP TRIGGER fail_output_event").Error; err != nil {
		t.Fatal(err)
	}
	if err := s.saveTaskCompletionWithinStorageQuota(fresh, []byte(`{"storageKey":"resource:output"}`), nil, false); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.AdvertisingEvent{}).Where("kind = ?", "candidate_output").Count(&n).Error; err != nil || n != 1 {
		t.Fatal("missing output audit", n, err)
	}
	events, err := s.ListAdvertisingEvents("user", project, p, "", "", 1, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events.Items {
		if event.Fact && event.Details.TaskID == task.ID {
			t.Fatal("duplicate legacy projection for audited output")
		}
	}
}

func TestAdvertisingReviewLongNarration(t *testing.T) {
	s, db, project, p, _, sub := reviewFixture(t)
	if err := db.Model(&model.AdvertisingSubmission{}).Where("id = ?", sub).Update("kind", "audio").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.Task{}).Where("id = ?", "review-task").Updates(map[string]any{"type": "canvas_audio", "result_json": `{"audio":{"storageKey":"resource:review-media"}}`}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.Resource{}).Where("id = ?", "review-media").Update("mime_type", "audio/wav").Error; err != nil {
		t.Fatal(err)
	}
	req := AdvertisingReviewRequest{ClientOperationID: uuid.NewString(), ExpectedProductionRevision: 1, Checkpoint: "candidate", SubmissionID: sub, Decision: "approve", Reason: "listened"}
	if _, err := s.CreateAdvertisingReview("user", project, p, req); err == nil {
		t.Fatal("long narration adopted")
	}
	if err := db.Model(&model.Resource{}).Where("id = ?", "review-media").Update("duration_ms", 5000).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateAdvertisingReview("user", project, p, req); err != nil {
		t.Fatal("exact-fit narration rejected", err)
	}
}

func TestAdvertisingReviewConfirmationAndRenderRollback(t *testing.T) {
	s, db := advertisingTestService(t)
	seedResource(t, db, "resource", "owner", "image/png")
	draft := advertisingTestDraft()
	ad, err := s.CreateAdvertisingProject("owner", AdvertisingCreateRequest{Name: "Audit rollback", Draft: &draft})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE TRIGGER fail_confirmation BEFORE INSERT ON advertising_events WHEN NEW.kind = 'script_confirmed' BEGIN SELECT RAISE(ABORT, 'audit failed'); END").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.ConfirmAdvertisingProject("owner", ad.Project.ID, 1); err == nil {
		t.Fatal("confirmation escaped audit")
	}
	var n int64
	if err := db.Model(&model.AdvertisingRevision{}).Count(&n).Error; err != nil || n != 0 {
		t.Fatal("snapshot escaped rollback", n, err)
	}
	if err := db.Exec("DROP TRIGGER fail_confirmation").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.ConfirmAdvertisingProject("owner", ad.Project.ID, 1); err != nil {
		t.Fatal(err)
	}
	// Rendering records admission, not success; audit failure must roll back the queued task too.
	s, db, project, p, _, _ := reviewFixture(t)
	timeline, _ := json.Marshal(renderTestProject("resource:review-media"))
	assembly := model.AdvertisingAssembly{ID: "audit-assembly", ProductionID: p, ClientOperationID: "assembly-key", SourceProductionRevision: 1, TimelineJSON: string(timeline)}
	if err := db.Create(&assembly).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE TRIGGER fail_render_creation BEFORE INSERT ON advertising_events WHEN NEW.kind = 'render_created' BEGIN SELECT RAISE(ABORT, 'audit failed'); END").Error; err != nil {
		t.Fatal(err)
	}
	key := uuid.NewString()
	if _, err := s.CreateAdvertisingRender("user", project, p, assembly.ID, key); err == nil {
		t.Fatal("render creation escaped audit")
	}
	if err := db.Model(&model.AdvertisingRenderAttempt{}).Count(&n).Error; err != nil || n != 0 {
		t.Fatal("render attempt escaped rollback", n, err)
	}
	if err := db.Exec("DROP TRIGGER fail_render_creation").Error; err != nil {
		t.Fatal(err)
	}
	attempt, err := s.CreateAdvertisingRender("user", project, p, assembly.ID, key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateAdvertisingRender("user", project, p, assembly.ID, key); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.AdvertisingEvent{}).Where("kind = ?", "render_created").Count(&n).Error; err != nil || n != 1 {
		t.Fatal("render audit duplicated", n, err)
	}
	if err := db.Model(&model.AdvertisingEvent{}).Where("kind = ?", "render_output").Count(&n).Error; err != nil || n != 0 {
		t.Fatal("admission claimed success", n, err)
	}
	task, err := s.repo.Task(attempt.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	priorStatus := task.Status
	task.Status = model.TaskStatusSucceeded
	task.ResultJSON = `{"resourceId":"review-media"}`
	if err := db.Exec("CREATE TRIGGER fail_render_output BEFORE INSERT ON advertising_events WHEN NEW.kind = 'render_output' BEGIN SELECT RAISE(ABORT, 'audit failed'); END").Error; err != nil {
		t.Fatal(err)
	}
	complete := func() error {
		return s.repo.SaveTaskCompletionWithRegistration(task, priorStatus, nil, func(repo *repository.Repository) error { return s.registerAdvertisingRenderOutput(repo, *task) })
	}
	if err := complete(); err == nil {
		t.Fatal("render output escaped audit")
	}
	stored, err := s.repo.Task(task.ID)
	if err != nil || stored.Status != priorStatus {
		t.Fatal("render completion escaped rollback", err)
	}
	if err := db.Exec("DROP TRIGGER fail_render_output").Error; err != nil {
		t.Fatal(err)
	}
	if err := complete(); err != nil {
		t.Fatal(err)
	}
}

func TestAdvertisingReviewAuditConfirmation(t *testing.T) {
	s, db, project, _, _ := advertisingBatchFixture(t)
	var n int64
	if err := db.Table("advertising_events").Where("project_id = ? AND kind = ?", project, "script_confirmed").Count(&n).Error; err != nil || n != 1 {
		t.Fatalf("missing atomic confirmation audit: %d %v", n, err)
	}
	if _, err := s.ConfirmAdvertisingProject("user", project, 1); err != nil {
		t.Fatal(err)
	}
	if err := db.Table("advertising_events").Where("project_id = ? AND kind = ?", project, "script_confirmed").Count(&n).Error; err != nil || n != 1 {
		t.Fatalf("duplicate audit: %d %v", n, err)
	}
}
