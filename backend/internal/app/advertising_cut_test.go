package app

import (
	"encoding/json"
	"github.com/google/uuid"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
)

func TestAdvertisingCutFingerprint(t *testing.T) {
	draft := advertisingTestDraft()
	settings := AdvertisingProductionSettings{Selections: map[string]AdvertisingSelection{}}
	media := map[string]AdvertisingProbedMedia{}
	for _, shot := range draft.Shots {
		settings.Selections[shot.ID] = AdvertisingSelection{VideoSubmissionID: shot.ID}
		media[shot.ID] = AdvertisingProbedMedia{Resource: model.Resource{ID: shot.ID, UserID: "user", MimeType: "video/mp4", Status: model.ResourceStatusReady}, DurationMs: 6000, Width: 640, Height: 360}
	}
	snapshot := AdvertisingRevisionDetail{Snapshot: AdvertisingSnapshot{Draft: draft}}
	first, err := advertisingCutProvenance(snapshot, settings, media, "pad")
	if err != nil {
		t.Fatal(err)
	}
	hash := first.Fingerprint
	settings.TTSModelID = "unused-default"
	settings.VoiceID = "unused-voice"
	volume := .7
	settings.MusicVolume = &volume
	again, err := advertisingCutProvenance(snapshot, settings, media, "pad")
	if err != nil || again.Fingerprint != hash {
		t.Fatalf("unused defaults stale: %v", err)
	}
	selection := settings.Selections[draft.Shots[0].ID]
	selection.VideoStartMs = 100
	settings.Selections[draft.Shots[0].ID] = selection
	changed, _ := advertisingCutProvenance(snapshot, settings, media, "pad")
	if changed.Fingerprint == hash {
		t.Fatal("trim did not invalidate")
	}
	selection.VideoStartMs = 0
	settings.Selections[draft.Shots[0].ID] = selection
	changed, _ = advertisingCutProvenance(snapshot, settings, media, "crop")
	if changed.Fingerprint == hash {
		t.Fatal("fit did not invalidate")
	}
	raw, _ := json.Marshal(first)
	if len(raw) == 0 {
		t.Fatal("missing provenance")
	}
	for _, change := range []string{"text", "resource", "task", "music", "unused-audio"} {
		copyDraft := snapshot
		copyDraft.Snapshot.Draft.Shots = append([]AdvertisingShot{}, draft.Shots...)
		copyMedia := map[string]AdvertisingProbedMedia{}
		for k, v := range media {
			copyMedia[k] = v
		}
		copySettings := settings
		copySettings.Selections = map[string]AdvertisingSelection{}
		for k, v := range settings.Selections {
			copySettings.Selections[k] = v
		}
		switch change {
		case "text":
			copyDraft.Snapshot.Draft.Shots[0].OnScreenText = "new price"
		case "resource":
			m := copyMedia[draft.Shots[0].ID]
			m.Resource.ETag = "changed"
			copyMedia[draft.Shots[0].ID] = m
		case "task":
			m := copyMedia[draft.Shots[0].ID]
			m.TaskID = "different-task"
			copyMedia[draft.Shots[0].ID] = m
		case "music":
			copySettings.MusicResourceID = "music"
			copyMedia["music"] = AdvertisingProbedMedia{Resource: model.Resource{ID: "music", MimeType: "audio/wav"}, DurationMs: 5000}
		case "unused-audio":
			v := copySettings.Selections[draft.Shots[0].ID]
			v.AudioSubmissionID = "unused"
			copySettings.Selections[draft.Shots[0].ID] = v
		}
		next, err := advertisingCutProvenance(copyDraft, copySettings, copyMedia, "pad")
		if err != nil {
			t.Fatal(err)
		}
		if (next.Fingerprint == hash) != (change == "unused-audio") {
			t.Fatal("dependency matrix", change)
		}
	}
}

func TestAdvertisingCutReapprovePreservesTrim(t *testing.T) {
	s, _, project, p, shot, sub := reviewFixture(t)
	current, _ := s.GetAdvertisingProduction("user", project, p)
	current.Settings.Selections[shot] = AdvertisingSelection{VideoSubmissionID: sub, VideoStartMs: 500}
	saved, err := s.SaveAdvertisingProductionSettings("user", project, p, 1, current.Settings)
	if err != nil {
		t.Fatal(err)
	}
	reviewed, err := s.CreateAdvertisingReview("user", project, p, AdvertisingReviewRequest{ClientOperationID: uuid.NewString(), ExpectedProductionRevision: saved.Production.Revision, Checkpoint: "candidate", SubmissionID: sub, Decision: "approve", Reason: "same trimmed candidate"})
	if err != nil {
		t.Fatal(err)
	}
	if reviewed.Production.Settings.Selections[shot].VideoStartMs != 500 {
		t.Fatal("same candidate lost explicit trim")
	}
}

func TestAdvertisingCutReviewDelivery(t *testing.T) {
	s, db, project, p, _, subID := reviewFixture(t)
	current, _ := s.GetAdvertisingProduction("user", project, p)
	var resource model.Resource
	db.First(&resource, "id = ?", "review-media")
	media := map[string]AdvertisingProbedMedia{}
	for i, shot := range current.ConfirmedSnapshot.Snapshot.Draft.Shots {
		id := subID
		if i > 0 {
			id = "sub-" + shot.ID
			now := time.Now()
			taskID := "review-task"
			sub := model.AdvertisingSubmission{ID: id, ProductionID: p, ShotID: shot.ID, Kind: "video", ClientOperationID: uuid.NewString(), TaskID: &taskID, ApprovedAt: &now}
			// One task per submission: clone its public successful output.
			taskID = "task-" + shot.ID
			sub.TaskID = &taskID
			db.Create(&model.Task{ID: taskID, UserID: "user", ProjectID: project, Type: "canvas_video", Status: model.TaskStatusSucceeded, ResultJSON: `{"storageKey":"resource:review-media"}`})
			if err := db.Create(&sub).Error; err != nil {
				t.Fatal(err)
			}
			s.repo.ReplaceAdvertisingReferences(project, "candidate:"+id, []string{resource.ID})
		}
		current.Settings.Selections[shot.ID] = AdvertisingSelection{VideoSubmissionID: id}
		sub, _ := s.repo.AdvertisingSubmission(p, id)
		media[id] = AdvertisingProbedMedia{Resource: resource, TaskID: *sub.TaskID, DurationMs: 6000, Width: 640, Height: 360}
	}
	current, err := s.SaveAdvertisingProductionSettings("user", project, p, 1, current.Settings)
	if err != nil {
		t.Fatal(err)
	}
	frozen, err := advertisingCutProvenance(current.ConfirmedSnapshot, current.Settings, media, "pad")
	if err != nil {
		t.Fatal(err)
	}
	timeline, _ := buildAdvertisingTimeline(current.ConfirmedSnapshot, current.Settings, media, "pad")
	raw, _ := json.Marshal(timeline)
	provenance, _ := json.Marshal(frozen)
	text := string(provenance)
	assembly := model.AdvertisingAssembly{ID: "cut", ProductionID: p, ClientOperationID: uuid.NewString(), SourceProductionRevision: current.Production.Revision, SelectionHash: "legacy-immutable", TimelineJSON: string(raw), ProvenanceJSON: &text, SourceFingerprint: &frozen.Fingerprint}
	if err := s.repo.CreateAdvertisingAssembly(&assembly); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	unselected := model.AdvertisingSubmission{ID: "new-unselected", ProductionID: p, ShotID: current.ConfirmedSnapshot.Snapshot.Draft.Shots[0].ID, Kind: "video", ClientOperationID: uuid.NewString(), ApprovedAt: &now}
	if err := db.Create(&unselected).Error; err != nil {
		t.Fatal(err)
	}
	current.Settings.TTSModelID = "unused-model"
	current.Settings.VoiceID = "unused-voice"
	current, err = s.SaveAdvertisingProductionSettings("user", project, p, current.Production.Revision, current.Settings)
	if err != nil {
		t.Fatal(err)
	}
	renderKey := uuid.NewString()
	other := &Service{repo: repository.New(db), dataDir: s.dataDir}
	var start sync.WaitGroup
	start.Add(1)
	var completed sync.WaitGroup
	outputs := make(chan AdvertisingRenderAttemptDetail, 2)
	failures := make(chan error, 2)
	for _, svc := range []*Service{s, other} {
		completed.Add(1)
		go func(svc *Service) {
			defer completed.Done()
			start.Wait()
			r, e := svc.CreateAdvertisingRender("user", project, p, assembly.ID, renderKey)
			outputs <- r
			failures <- e
		}(svc)
	}
	start.Done()
	completed.Wait()
	close(outputs)
	close(failures)
	for e := range failures {
		if e != nil {
			t.Fatal(e)
		}
	}
	var render AdvertisingRenderAttemptDetail
	for r := range outputs {
		if render.ID != "" && render.ID != r.ID {
			t.Fatal("same key duplicated across services")
		}
		render = r
	}
	if _, err := s.CreateAdvertisingReview("user", project, p, AdvertisingReviewRequest{ClientOperationID: uuid.NewString(), ExpectedProductionRevision: current.Production.Revision, Checkpoint: "assembly", AssemblyID: assembly.ID, RenderAttemptID: render.ID, ResourceID: resource.ID, Decision: "approve", Reason: "not rendered"}); err == nil {
		t.Fatal("approved before real output")
	}
	// Different keys race on a fresh assembly: exactly one task may be admitted.
	raceAssembly := assembly
	raceAssembly.ID = "cut-race"
	raceAssembly.ClientOperationID = uuid.NewString()
	if err := s.repo.CreateAdvertisingAssembly(&raceAssembly); err != nil {
		t.Fatal(err)
	}
	var gate sync.WaitGroup
	gate.Add(1)
	var raced sync.WaitGroup
	raceErrors := make(chan error, 2)
	for _, svc := range []*Service{s, other} {
		raced.Add(1)
		go func(svc *Service) {
			defer raced.Done()
			gate.Wait()
			_, e := svc.CreateAdvertisingRender("user", project, p, raceAssembly.ID, uuid.NewString())
			raceErrors <- e
		}(svc)
	}
	gate.Done()
	raced.Wait()
	close(raceErrors)
	admitted := 0
	for e := range raceErrors {
		if e == nil {
			admitted++
		}
	}
	if admitted != 1 {
		t.Fatalf("different key admissions: %d", admitted)
	}
	// A separate Service shares only the DB, not the process mutex.
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := other.CreateAdvertisingRender("user", project, p, assembly.ID, uuid.NewString()); err == nil {
				t.Error("cross-key active render admitted")
			}
		}()
	}
	wg.Wait()
	db.Model(&model.Task{}).Where("id = ?", render.Task.ID).Updates(map[string]any{"status": model.TaskStatusFailed, "media_recovery_json": "unresolved"})
	if _, err := other.CreateAdvertisingRender("user", project, p, assembly.ID, uuid.NewString()); err == nil {
		t.Fatal("unresolved media state duplicated")
	}
	db.Model(&model.Task{}).Where("id = ?", render.Task.ID).Updates(map[string]any{"media_recovery_json": "", "status": model.TaskStatusSucceeded, "result_json": `{"resourceId":"review-media"}`})
	req := AdvertisingReviewRequest{ClientOperationID: uuid.NewString(), ExpectedProductionRevision: current.Production.Revision, Checkpoint: "assembly", AssemblyID: assembly.ID, RenderAttemptID: render.ID, ResourceID: resource.ID, Decision: "approve", Reason: "exact output"}
	approved, err := s.CreateAdvertisingReview("user", project, p, req)
	if err != nil {
		t.Fatal(err)
	}
	delivery := req
	delivery.ClientOperationID = uuid.NewString()
	delivery.Checkpoint = "delivery"
	delivery.AcceptedReviewID = approved.Review.ID
	accepted, err := s.CreateAdvertisingReview("user", project, p, delivery)
	if err != nil {
		t.Fatal(err)
	}
	if accepted.Review.AcceptedReviewID == nil || *accepted.Review.AcceptedReviewID != approved.Review.ID {
		t.Fatal("delivery not pinned")
	}
	replay, err := s.CreateAdvertisingReview("user", project, p, delivery)
	if err != nil || replay.Review.ID != accepted.Review.ID {
		t.Fatal("delivery replay")
	}
	mismatch := delivery
	mismatch.ClientOperationID = uuid.NewString()
	mismatch.ResourceID = "wrong"
	if _, err = s.CreateAdvertisingReview("user", project, p, mismatch); err == nil {
		t.Fatal("wrong output accepted")
	}
	rejected := req
	rejected.ClientOperationID = uuid.NewString()
	rejected.Decision = "reject"
	if _, err = s.CreateAdvertisingReview("user", project, p, rejected); err != nil {
		t.Fatal(err)
	}
	delivery.ClientOperationID = uuid.NewString()
	if _, err = s.CreateAdvertisingReview("user", project, p, delivery); err == nil {
		t.Fatal("superseded approval delivered")
	}
	req.ClientOperationID = uuid.NewString()
	approved, err = s.CreateAdvertisingReview("user", project, p, req)
	if err != nil {
		t.Fatal(err)
	}
	delivery.AcceptedReviewID = approved.Review.ID
	current.Settings.Fit = "crop"
	current, err = s.SaveAdvertisingProductionSettings("user", project, p, current.Production.Revision, current.Settings)
	if err != nil {
		t.Fatal(err)
	}
	delivery.ExpectedProductionRevision = current.Production.Revision
	if _, err = s.CreateAdvertisingReview("user", project, p, delivery); err == nil {
		t.Fatal("changed fit delivered")
	}
	current.Settings.Fit = "pad"
	current, err = s.SaveAdvertisingProductionSettings("user", project, p, current.Production.Revision, current.Settings)
	if err != nil {
		t.Fatal(err)
	}
	delivery.ExpectedProductionRevision = current.Production.Revision
	if err = os.Remove(filepath.Join(s.dataDir, "resources", "review.mp4")); err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateAdvertisingReview("user", project, p, delivery); err == nil {
		t.Fatal("missing file delivered")
	}
	old, _ := s.repo.AdvertisingAssembly(p, assembly.ID)
	if old.SelectionHash != "legacy-immutable" || *old.ProvenanceJSON != text {
		t.Fatal("mutated frozen history")
	}
}
