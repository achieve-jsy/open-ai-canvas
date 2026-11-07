package app

import (
	"errors"
	"io"
	"slices"
	"strings"
	"time"

	"gorm.io/gorm"
	"yingce/backend/internal/kernel"
	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
)

func advertisingCandidateUnavailable() error {
	return advertisingError(409, kernel.ErrorReason("advertising_candidate_unavailable"), "候选尚未就绪")
}

func (s *Service) registerAdvertisingTaskOutput(repo *repository.Repository, task model.Task) error {
	if !strings.HasPrefix(task.ProjectID, "ad_") || (task.Type != "canvas_video" && task.Type != "canvas_audio") {
		return nil
	}
	submission, err := repo.AdvertisingSubmissionForTask(task.ID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	production, err := repo.AdvertisingProductionForUser(task.UserID, task.ProjectID, submission.ProductionID)
	if err != nil {
		return err
	}
	checkpoint, err := s.decodeMediaCheckpoint(&task)
	if err != nil || checkpoint == nil || checkpoint.Mode != submission.Kind || len(checkpoint.Items) != 1 {
		return advertisingCandidateUnavailable()
	}
	id, _ := taskOutputResource(task.ResultJSON, task.Type)
	if id == "" || id != checkpoint.Items[0].ResourceID {
		return advertisingCandidateUnavailable()
	}
	resource, err := repo.ResourceForUser(task.UserID, id)
	if err != nil {
		return err
	}
	if !advertisingCandidateMediaReady(resource, submission.Kind) {
		return advertisingCandidateUnavailable()
	}
	if err := repo.ReplaceAdvertisingReferences(production.ProjectID, "candidate:"+submission.ID, []string{id}); err != nil {
		return err
	}
	return advertisingProductionEvent(repo, task.UserID, task.ProjectID, production.ID, "output:"+task.ID, "candidate_output", submission.ShotID, submission.ID, "", "", AdvertisingEventDetails{TaskID: task.ID, Status: "succeeded"})
}

func advertisingCandidateMediaReady(resource *model.Resource, kind string) bool {
	if resource.Status != model.ResourceStatusReady || resource.Size <= 0 {
		return false
	}
	if kind == "video" {
		return resource.MimeType == "video/mp4"
	}
	return kind == "audio" && slices.Contains([]string{"audio/mpeg", "audio/mp4", "audio/wav", "audio/x-wav"}, resource.MimeType)
}

func (s *Service) getAdvertisingCandidateResource(repo *repository.Repository, userID, productionID, shotID, kind, submissionID string) (*model.Resource, error) {
	submission, err := repo.AdvertisingSubmission(productionID, submissionID)
	if err != nil || submission.ShotID != shotID || submission.Kind != kind || submission.TaskID == nil || submission.ApprovedAt == nil {
		return nil, advertisingCandidateUnavailable()
	}
	task, err := repo.Task(*submission.TaskID)
	if err != nil || task.UserID != userID || task.Status != model.TaskStatusSucceeded {
		return nil, advertisingCandidateUnavailable()
	}
	id, _ := taskOutputResource(task.ResultJSON, task.Type)
	if id == "" {
		return nil, advertisingCandidateUnavailable()
	}
	resource, err := repo.ResourceForUser(userID, id)
	if err != nil || !advertisingCandidateMediaReady(resource, kind) || resource.DurationMs <= 0 || strings.TrimSpace(resource.ObjectKey) == "" {
		return nil, advertisingCandidateUnavailable()
	}
	production, err := repo.AdvertisingProductionForUser(userID, task.ProjectID, productionID)
	if err != nil {
		return nil, advertisingCandidateUnavailable()
	}
	exists, err := repo.HasAdvertisingCandidateReference(production.ProjectID, submissionID, id)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, advertisingCandidateUnavailable()
	}
	if err := s.advertisingResourceReadable(userID, resource); err != nil {
		return nil, err
	}
	return resource, nil
}

func (s *Service) advertisingResourceReadable(userID string, resource *model.Resource) error {
	// ponytail: bounded readability check holds the production transaction; preflight plus identity recheck if storage latency becomes a bottleneck.
	if resource.ObjectKey == "" || resource.Status != model.ResourceStatusReady {
		return advertisingCandidateUnavailable()
	}
	stream, err := s.openResourceRange(userID, resource, "bytes=0-0")
	if err != nil {
		return advertisingCandidateUnavailable()
	}
	defer stream.Body.Close()
	watchdog := time.AfterFunc(mediaProbeRemoteReadTimeout, func() { _ = stream.Body.Close() })
	defer watchdog.Stop()
	var first [1]byte
	if _, err := io.ReadFull(stream.Body, first[:]); err != nil {
		return advertisingCandidateUnavailable()
	}
	return nil
}
