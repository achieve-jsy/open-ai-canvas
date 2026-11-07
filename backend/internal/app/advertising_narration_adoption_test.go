package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
	"yingce/backend/internal/model"
)

func TestAdvertisingCandidateRejectsLongNarrationAdoption(t *testing.T) {
	s, db, project, production, req := advertisingBatchFixture(t)
	seedResource(t, db, "long-audio", "user", "audio/wav")
	if err := db.Model(&model.Resource{}).Where("id = ?", "long-audio").Updates(map[string]any{"duration_ms": 5001, "object_key": "voice.wav"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(s.dataDir, "resources"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.dataDir, "resources", "voice.wav"), []byte("readability fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(mediaCheckpoint{Mode: "audio", Items: []mediaCheckpointItem{{ResourceID: "long-audio"}}})
	checkpoint, err := s.encryptSettingSecret(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	taskID := "long-audio-task"
	now := time.Now()
	for _, row := range []any{&model.Task{ID: taskID, UserID: "user", ProjectID: project, Type: "canvas_audio", Status: model.TaskStatusSucceeded, MediaRecoveryJSON: checkpoint, MediaStage: "completed", ResultJSON: `{"audio":{"storageKey":"resource:long-audio"}}`}, &model.AdvertisingSubmission{ID: "long-audio-sub", ProductionID: production, ShotID: req.ShotID, Kind: "audio", ClientOperationID: "long-audio-operation", TaskID: &taskID, ApprovedAt: &now, RequestJSON: "{}", QuoteJSON: "{}"}} {
		if err = db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err = s.repo.ReplaceAdvertisingReferences(project, "candidate:long-audio-sub", []string{"long-audio"}); err != nil {
		t.Fatal(err)
	}
	settings := AdvertisingProductionSettings{Selections: map[string]AdvertisingSelection{req.ShotID: {AudioSubmissionID: "long-audio-sub"}}}
	if _, err = s.SaveAdvertisingProductionSettings("user", project, production, 1, settings); err == nil {
		t.Fatal("adopted narration longer than shot")
	}
	if err = db.Model(&model.Resource{}).Where("id = ?", "long-audio").Update("duration_ms", 5000).Error; err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveAdvertisingProductionSettings("user", project, production, 1, settings); err != nil {
		t.Fatalf("exact fit rejected: %v", err)
	}
}
