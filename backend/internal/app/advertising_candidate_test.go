package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
)

func TestAdvertisingCandidateProtection(t *testing.T) {
	s, db, _ := agentMediaFixture(t)
	draft := advertisingTestDraft()
	draft.ProductResourceIDs = []string{"ref-one"}
	draft.Shots[0].ReferenceResourceIDs = []string{"ref-one"}
	ad, err := s.CreateAdvertisingProject("user", AdvertisingCreateRequest{Name: "Candidates", Draft: &draft})
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
	quote, err := s.PrepareAdvertisingSubmission("user", ad.Project.ID, p.Production.ID, AdvertisingSubmissionRequest{ClientOperationID: "10000000-0000-4000-8000-000000000040", ShotID: draft.Shots[0].ID, Kind: "video", ModelID: "video-cm"})
	if err != nil {
		t.Fatal(err)
	}
	approved, err := s.ExecuteAdvertisingSubmission("user", ad.Project.ID, p.Production.ID, quote.ID, quote.Quote.QuoteHash)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteAdvertisingProject("user", ad.Project.ID, confirmed.CurrentRevision); err == nil {
		t.Fatal("deleted advertising project with active task")
	}
	p.Settings.Selections[draft.Shots[0].ID] = AdvertisingSelection{VideoSubmissionID: quote.ID}
	if _, err := s.SaveAdvertisingProductionSettings("user", ad.Project.ID, p.Production.ID, 1, p.Settings); err == nil {
		t.Fatal("adopted unfinished task")
	}
	seedResource(t, db, "candidate-video", "user", "video/mp4")
	if err := db.Model(&model.Resource{}).Where("id = ?", "candidate-video").Update("duration_ms", 0).Error; err != nil {
		t.Fatal(err)
	}
	task, err := s.repo.Task(approved.Task.ID)
	if err != nil {
		t.Fatal(err)
	}
	result := []byte("{\"storageKey\":\"resource:candidate-video\"}")
	if err := s.saveTaskCompletionWithinStorageQuota(task, result, nil, false); err == nil {
		t.Fatal("accepted completion without recovered media")
	}
	task, err = s.repo.Task(approved.Task.ID)
	if err != nil || task.Status == model.TaskStatusSucceeded {
		t.Fatalf("partial completion: %+v %v", task, err)
	}
	checkpoint, _ := json.Marshal(mediaCheckpoint{Mode: "video", Items: []mediaCheckpointItem{{ResourceID: "candidate-video"}}})
	task.MediaRecoveryJSON, err = s.encryptSettingSecret(string(checkpoint))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.Task{}).Where("id = ?", task.ID).Update("media_recovery_json", task.MediaRecoveryJSON).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.saveTaskCompletionWithinStorageQuota(task, result, nil, false); err != nil {
		t.Fatal(err)
	}
	refs, err := s.repo.AdvertisingResourceReferences("user", []string{"candidate-video"})
	if err != nil || len(refs) != 1 {
		t.Fatalf("candidate not protected: %+v %v", refs, err)
	}
	if _, err := s.SaveAdvertisingProductionSettings("user", ad.Project.ID, p.Production.ID, 1, p.Settings); err == nil {
		t.Fatal("adopted unknown duration")
	}
	if err := db.Model(&model.Resource{}).Where("id = ?", "candidate-video").Updates(map[string]any{"duration_ms": draft.Shots[0].DurationMs + 1000, "object_key": "candidate.mp4"}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveAdvertisingProductionSettings("user", ad.Project.ID, p.Production.ID, 1, p.Settings); err == nil {
		t.Fatal("adopted missing physical file")
	}
	if err := os.MkdirAll(filepath.Join(s.dataDir, "resources"), 0700); err != nil {
		t.Fatal(err)
	}
	// Readability fixture only; real decoding and stream validation belong to assembly tests.
	if err := os.WriteFile(filepath.Join(s.dataDir, "resources", "candidate.mp4"), []byte("readable candidate"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveAdvertisingProductionSettings("user", ad.Project.ID, p.Production.ID, 1, p.Settings); err != nil {
		t.Fatal(err)
	}
	if _, err := s.getAdvertisingCandidateResource(s.repo, "user", p.Production.ID, draft.Shots[1].ID, "video", quote.ID); err == nil {
		t.Fatal("cross-shot candidate")
	}
	if _, err := s.getAdvertisingCandidateResource(s.repo, "user", p.Production.ID, draft.Shots[0].ID, "audio", quote.ID); err == nil {
		t.Fatal("wrong candidate type")
	}
	for _, status := range []model.TaskStatus{model.TaskStatusFailed, model.TaskStatusCancelled, model.TaskStatusRunning} {
		if err := db.Model(&model.Task{}).Where("id = ?", task.ID).Update("status", status).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := s.getAdvertisingCandidateResource(s.repo, "user", p.Production.ID, draft.Shots[0].ID, "video", quote.ID); err == nil {
			t.Fatalf("adopted %s task", status)
		}
	}
	if err := db.Model(&model.Task{}).Where("id = ?", task.ID).Update("status", model.TaskStatusSucceeded).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.getAdvertisingCandidateResource(s.repo, "other", p.Production.ID, draft.Shots[0].ID, "video", quote.ID); err == nil {
		t.Fatal("foreign candidate")
	}
	if _, err := s.getAdvertisingCandidateResource(s.repo, "user", "other-production", draft.Shots[0].ID, "video", quote.ID); err == nil {
		t.Fatal("cross-production candidate")
	}
	if err := s.repo.DeleteResource("user", "candidate-video"); err == nil {
		t.Fatal("deleted protected candidate")
	}
	p.Settings.Selections[draft.Shots[0].ID] = AdvertisingSelection{VideoSubmissionID: quote.ID, VideoStartMs: 1001}
	if _, err := s.SaveAdvertisingProductionSettings("user", ad.Project.ID, p.Production.ID, 2, p.Settings); err == nil {
		t.Fatal("adopted short trim interval")
	}
	p.Settings.Selections = map[string]AdvertisingSelection{}
	if _, err := s.SaveAdvertisingProductionSettings("user", ad.Project.ID, p.Production.ID, 2, p.Settings); err != nil {
		t.Fatal(err)
	}
	refs, err = s.repo.AdvertisingResourceReferences("user", []string{"candidate-video"})
	if err != nil || len(refs) != 1 {
		t.Fatalf("unselected candidate lost protection: %v %v", refs, err)
	}
	if err := s.DeleteAdvertisingProject("user", ad.Project.ID, confirmed.CurrentRevision); err != nil {
		t.Fatal(err)
	}
	refs, err = s.repo.AdvertisingResourceReferences("user", []string{"candidate-video"})
	if err != nil || len(refs) != 0 {
		t.Fatalf("deleted advertising references: %v %v", refs, err)
	}
	for _, row := range []any{&model.AdvertisingProduction{}, &model.AdvertisingSubmission{}} {
		var count int64
		if err := db.Model(row).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("orphan production data: %T count=%d %v", row, count, err)
		}
	}
	if _, err := s.repo.ResourceForUser("user", "candidate-video"); err != nil {
		t.Fatal("media removed", err)
	}
	if _, err := s.repo.Task(task.ID); err != nil {
		t.Fatal("task removed", err)
	}
	var orders int64
	if err := db.Model(&model.BillingOrder{}).Where("task_id = ?", task.ID).Count(&orders).Error; err != nil || orders != 1 {
		t.Fatalf("billing removed: %d %v", orders, err)
	}
	task.Status = model.TaskStatusRunning
	if err := s.saveTaskCompletionWithinStorageQuota(task, result, nil, false); err == nil {
		t.Fatal("late completion bypassed state fencing")
	}
}

func TestAdvertisingProductionDeletion(t *testing.T) {
	s, db := advertisingTestService(t)
	seedResource(t, db, "resource", "owner", "image/png")
	draft := advertisingTestDraft()
	ad, err := s.CreateAdvertisingProject("owner", AdvertisingCreateRequest{Name: "Deletion", Draft: &draft})
	if err != nil {
		t.Fatal(err)
	}
	confirmed, err := s.ConfirmAdvertisingProject("owner", ad.Project.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.CreateAdvertisingProduction("owner", ad.Project.ID, confirmed.Snapshot.ID)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	task := model.Task{ID: "generated-task", UserID: "owner", ProjectID: ad.Project.ID, Type: "canvas_video", Status: model.TaskStatusSucceeded}
	render := model.Task{ID: "render-task", UserID: "owner", ProjectID: ad.Project.ID, Status: model.TaskStatusRunning}
	canvas := model.CanvasProject{ID: "assembly-canvas", UserID: "owner", ProjectID: ad.Project.ID, PayloadJSON: "{}", Revision: 1}
	submission := model.AdvertisingSubmission{ID: "submission", ProductionID: p.Production.ID, ShotID: draft.Shots[0].ID, Kind: "video", ClientOperationID: "submission-operation", TaskID: &task.ID, ApprovedAt: &now}
	assembly := model.AdvertisingAssembly{ID: "assembly", ProductionID: p.Production.ID, ClientOperationID: "assembly-operation", CanvasID: &canvas.ID, TimelineJSON: "{}"}
	attempt := model.AdvertisingRenderAttempt{ID: "attempt", AssemblyID: assembly.ID, ClientOperationID: "render-operation", TaskID: render.ID}
	for _, row := range []any{&task, &render, &canvas, &submission, &assembly, &attempt} {
		if err := db.Omit("Production", "Task", "Canvas", "Assembly").Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, row := range []*model.Task{&task, &render} {
		if err := db.Delete(row).Error; err == nil {
			t.Fatal("task RESTRICT constraint missing")
		}
	}
	if err := s.DeleteAdvertisingProject("owner", ad.Project.ID, confirmed.CurrentRevision); err == nil {
		t.Fatal("deleted active render")
	}
	if err := db.Model(&model.Task{}).Where("id = ?", render.ID).Update("status", model.TaskStatusSucceeded).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteAdvertisingProject("owner", ad.Project.ID, confirmed.CurrentRevision); err != nil {
		t.Fatal(err)
	}
	for _, row := range []any{&model.AdvertisingRenderAttempt{}, &model.AdvertisingAssembly{}, &model.AdvertisingSubmission{}, &model.AdvertisingProduction{}, &model.AdvertisingRevision{}, &model.AdvertisingResourceReference{}} {
		var count int64
		if err := db.Model(row).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("remaining %T: %d %v", row, count, err)
		}
	}
	kept, err := s.repo.CanvasProjectForUser("owner", canvas.ID)
	if err != nil || kept.ProjectID != "" {
		t.Fatalf("canvas was not detached: %+v %v", kept, err)
	}
	if _, err := s.repo.ResourceForUser("owner", "resource"); err != nil {
		t.Fatal(err)
	}
	// A stale worker cannot commit its old running state after deletion.
	render.Status = model.TaskStatusSucceeded
	if err := s.repo.SaveTaskCompletionWithRegistration(&render, model.TaskStatusRunning, nil, func(repo *repository.Repository) error { return s.registerAdvertisingTaskOutput(repo, render) }); err == nil {
		t.Fatal("late worker revived scope")
	}
}
