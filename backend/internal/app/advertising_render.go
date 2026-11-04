package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
)

type AdvertisingRenderAttemptDetail struct {
	model.AdvertisingRenderAttempt
	Task       *model.Task `json:"task"`
	ResourceID string      `json:"resourceId,omitempty"`
	Available  bool        `json:"available"`
}

func (s *Service) advertisingRenderDetail(repo *repository.Repository, userID string, row *model.AdvertisingRenderAttempt) (AdvertisingRenderAttemptDetail, error) {
	result := AdvertisingRenderAttemptDetail{AdvertisingRenderAttempt: *row}
	task, err := repo.TaskForUser(userID, row.TaskID)
	if err != nil {
		return result, err
	}
	result.Task = taskForOutput(*task)
	if task.Status == model.TaskStatusSucceeded {
		var output timelineRenderOutput
		if json.Unmarshal([]byte(task.ResultJSON), &output) == nil && output.ResourceID != "" {
			resource, err := repo.ResourceForUser(userID, output.ResourceID)
			if err == nil && resource.Status == model.ResourceStatusReady && resource.MimeType == "video/mp4" && resource.DurationMs > 0 && s.advertisingResourceReadable(userID, resource) == nil {
				result.ResourceID, result.Available = resource.ID, true
			}
		}
	}
	return result, nil
}

func (s *Service) CreateAdvertisingRender(userID, projectID, productionID, assemblyID, operationID string) (AdvertisingRenderAttemptDetail, error) {
	var result AdvertisingRenderAttemptDetail
	if _, err := uuid.Parse(operationID); err != nil || len(operationID) != 36 {
		return result, BadAuthRequest("操作键必须为UUID")
	}
	assembly, err := s.GetAdvertisingAssembly(userID, projectID, productionID, assemblyID)
	if err != nil {
		return result, err
	}
	prior, err := s.repo.AdvertisingRenderForOperation(assemblyID, operationID)
	if err == nil {
		return s.advertisingRenderDetail(s.repo, userID, prior)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return result, err
	}
	var timeline renderProject
	if err := json.Unmarshal([]byte(assembly.TimelineJSON), &timeline); err != nil {
		return result, err
	}
	task, err := s.prepareTimelineRenderTask(userID, TimelineRenderCreateRequest{ProjectID: projectID, Timeline: timeline})
	if err != nil {
		return result, err
	}
	for _, clip := range timeline.Clips {
		id, ok := mediaResourceID(clip)
		if !ok {
			continue
		}
		resource, err := s.repo.ResourceForUser(userID, id)
		if err != nil {
			return result, advertisingCandidateUnavailable()
		}
		if err := s.advertisingResourceReadable(userID, resource); err != nil {
			return result, err
		}
	}
	policy, err := s.RuntimePolicy()
	if err != nil {
		return result, err
	}
	created := false
	s.storageMu.Lock()
	err = s.repo.AdvertisingProductionTransaction(userID, projectID, productionID, func(repo *repository.Repository) error {
		prior, err := repo.AdvertisingRenderForOperation(assemblyID, operationID)
		if err == nil {
			result, err = s.advertisingRenderDetail(repo, userID, prior)
			return err
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		current, err := repo.AdvertisingAssembly(productionID, assemblyID)
		if err != nil {
			return err
		}
		if current.TimelineJSON != assembly.TimelineJSON {
			return advertisingConflict("assembly_changed", "组装快照已变化")
		}
		writer := Service{repo: repo}
		if err := writer.validateTimelineRenderAccess(userID, projectID, timeline); err != nil {
			return err
		}
		if err := createTaskWithStorageQuotaRepository(repo, task, nil, policy); err != nil {
			return err
		}
		row := model.AdvertisingRenderAttempt{ID: newID(), AssemblyID: assemblyID, ClientOperationID: operationID, TaskID: task.ID, CreatedAt: time.Now().UTC()}
		if err := repo.CreateAdvertisingRender(&row); err != nil {
			return err
		}
		result, err = s.advertisingRenderDetail(repo, userID, &row)
		created = err == nil
		return err
	})
	s.storageMu.Unlock()
	if errors.Is(err, repository.ErrActiveTaskLimit) {
		return result, BadAuthRequest(fmt.Sprintf("同时排队或运行的任务最多%d个", policy.Task.ActiveTaskLimit))
	}
	if err == nil && created {
		s.wakeTaskDispatcher()
		s.recordActivity(userID, "task", 1)
	}
	return result, advertisingProductionFailure(err)
}

func (s *Service) ListAdvertisingRenderAttempts(userID, projectID, productionID, assemblyID string, page, size int) (AdvertisingPage[AdvertisingRenderAttemptDetail], error) {
	result := AdvertisingPage[AdvertisingRenderAttemptDetail]{Items: []AdvertisingRenderAttemptDetail{}, Page: page, PageSize: size}
	if err := advertisingPagination(page, size); err != nil {
		return result, err
	}
	if _, err := s.GetAdvertisingAssembly(userID, projectID, productionID, assemblyID); err != nil {
		return result, err
	}
	rows, total, err := s.repo.AdvertisingRenders(assemblyID, page, size)
	if err != nil {
		return result, err
	}
	result.Total = total
	for _, row := range rows {
		item, err := s.advertisingRenderDetail(s.repo, userID, &row)
		if err != nil {
			return result, err
		}
		result.Items = append(result.Items, item)
	}
	return result, nil
}

func (s *Service) registerAdvertisingRenderOutput(repo *repository.Repository, task model.Task) error {
	if !strings.HasPrefix(task.ProjectID, "ad_") || task.Type != model.TaskTypeTimelineRender {
		return nil
	}
	attempt, err := repo.AdvertisingRenderForTask(task.ID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	assembly, err := repo.AdvertisingAssemblyForTask(task.ID)
	if err != nil {
		return err
	}
	production, err := repo.AdvertisingProductionForUser(task.UserID, task.ProjectID, assembly.ProductionID)
	if err != nil {
		return err
	}
	var output timelineRenderOutput
	if err := json.Unmarshal([]byte(task.ResultJSON), &output); err != nil {
		return err
	}
	resource, err := repo.ResourceForUser(task.UserID, output.ResourceID)
	if err != nil {
		return err
	}
	if resource.Status != model.ResourceStatusReady || resource.MimeType != "video/mp4" || resource.DurationMs <= 0 {
		return advertisingCandidateUnavailable()
	}
	return repo.ReplaceAdvertisingReferences(production.ProjectID, "render:"+attempt.ID, []string{resource.ID})
}
