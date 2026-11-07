package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"slices"
	"sort"
	"strings"
	"time"
	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
)

// This closed payload deliberately cannot carry task input, arbitrary metadata or media URLs.
type AdvertisingEventDetails struct {
	TaskID    string                `json:"taskId,omitempty"`
	ReviewID  string                `json:"reviewId,omitempty"`
	IssueID   string                `json:"issueId,omitempty"`
	Decision  string                `json:"decision,omitempty"`
	Status    string                `json:"status,omitempty"`
	Category  string                `json:"category,omitempty"`
	Note      string                `json:"note,omitempty"`
	AtMs      *int64                `json:"atMs,omitempty"`
	Selection *AdvertisingSelection `json:"selection,omitempty"`
}
type AdvertisingEventDetail struct {
	model.AdvertisingEvent
	Details AdvertisingEventDetails `json:"details"`
	Fact    bool                    `json:"fact"`
}

func appendAdvertisingEvent(repo *repository.Repository, row model.AdvertisingEvent, details AdvertisingEventDetails) error {
	row.ID = newID()
	if row.CreatedAt.IsZero() {
		row.CreatedAt = time.Now().UTC()
	}
	raw, err := json.Marshal(details)
	if err != nil {
		return err
	}
	row.DetailsJSON = string(raw)
	return repo.AppendAdvertisingEvent(&row)
}
func advertisingProductionEvent(repo *repository.Repository, user, project, p, key, kind, shot, sub, assembly, render string, details AdvertisingEventDetails) error {
	production, err := repo.AdvertisingProductionForUser(user, project, p)
	if err != nil {
		return err
	}
	revision, err := repo.AdvertisingRevisionForUser(user, project, production.ConfirmedRevisionID)
	if err != nil {
		return err
	}
	return appendAdvertisingEvent(repo, model.AdvertisingEvent{ProjectID: project, ProductionID: p, ActorID: user, EventKey: key, Kind: kind, ShotID: shot, SubmissionID: sub, AssemblyID: assembly, RenderAttemptID: render, SourceRevision: revision.SourceRevision, ProductionRevision: production.Revision}, details)
}
func (s *Service) ListAdvertisingEvents(user, project, production, shot, kind string, page, size int) (AdvertisingPage[AdvertisingEventDetail], error) {
	result := AdvertisingPage[AdvertisingEventDetail]{Items: []AdvertisingEventDetail{}, Page: page, PageSize: size}
	if err := advertisingAccess(user, project); err != nil {
		return result, err
	}
	if err := advertisingPagination(page, size); err != nil {
		return result, err
	}
	if _, err := s.repo.AdvertisingProjectForUser(user, project); err != nil {
		return result, advertisingFailure(err)
	}
	if production != "" {
		if _, err := s.GetAdvertisingProduction(user, project, production); err != nil {
			return result, err
		}
	}
	for name, value := range map[string]string{"shotId": shot, "kind": kind} {
		if err := advertisingText(name, value, 120, false); err != nil {
			return result, BadAuthRequest(err.Error())
		}
	}
	rows, err := s.repo.AdvertisingEvents(project, "", "", "")
	if err != nil {
		return result, err
	}
	all := []AdvertisingEventDetail{}
	seen := map[string]bool{}
	for _, row := range rows {
		item := AdvertisingEventDetail{AdvertisingEvent: row}
		if err := json.Unmarshal([]byte(row.DetailsJSON), &item.Details); err != nil {
			return result, err
		}
		all = append(all, item)
		seen[row.EventKey] = true
	}
	facts, err := s.repo.AdvertisingTaskFacts(project)
	if err != nil {
		return result, err
	}
	for _, f := range facts {
		for _, state := range []string{"created", "result"} {
			key := "task:" + f.TaskID + ":" + state
			if state == "created" && (seen["admitted:"+f.SubmissionID] || seen["render:"+f.RenderAttemptID]) || state == "result" && seen["output:"+f.TaskID] {
				continue
			}
			at := f.CreatedAt
			status := ""
			if state == "result" {
				if f.CompletedAt == nil {
					continue
				}
				at = *f.CompletedAt
				status = f.Status
				if status != "succeeded" && status != "failed" && status != "cancelled" {
					continue
				}
			}
			if at.IsZero() {
				continue
			}
			all = append(all, AdvertisingEventDetail{AdvertisingEvent: model.AdvertisingEvent{ID: key, ProjectID: project, ProductionID: f.ProductionID, ShotID: f.ShotID, SubmissionID: f.SubmissionID, AssemblyID: f.AssemblyID, RenderAttemptID: f.RenderAttemptID, Kind: "task_" + state + "_fact", ActorID: user, CreatedAt: at, SourceRevision: f.SourceRevision}, Details: AdvertisingEventDetails{TaskID: f.TaskID, Status: status}, Fact: true})
		}
	}
	// ponytail: merge persisted audit and legacy facts in memory; use SQL UNION/keyset paging if histories grow large.
	filtered := all[:0]
	for _, item := range all {
		if (production == "" || item.ProductionID == production) && (shot == "" || item.ShotID == shot) && (kind == "" || item.Kind == kind) {
			filtered = append(filtered, item)
		}
	}
	sort.Slice(filtered, func(i, j int) bool {
		if filtered[i].CreatedAt.Equal(filtered[j].CreatedAt) {
			return filtered[i].ID > filtered[j].ID
		}
		return filtered[i].CreatedAt.After(filtered[j].CreatedAt)
	})
	result.Total = int64(len(filtered))
	start := min((page-1)*size, len(filtered))
	result.Items = append(result.Items, filtered[start:min(start+size, len(filtered))]...)
	return result, nil
}

type AdvertisingReviewRequest struct {
	ResourceID                 string `json:"resourceId,omitempty"`
	AcceptedReviewID           string `json:"acceptedReviewId,omitempty"`
	ClientOperationID          string `json:"clientOperationId"`
	ExpectedProductionRevision int64  `json:"expectedProductionRevision"`
	Checkpoint                 string `json:"checkpoint"`
	SubmissionID               string `json:"submissionId,omitempty"`
	AssemblyID                 string `json:"assemblyId,omitempty"`
	RenderAttemptID            string `json:"renderAttemptId,omitempty"`
	Decision                   string `json:"decision"`
	Reason                     string `json:"reason"`
	ClearSelected              bool   `json:"clearSelected"`
}
type AdvertisingReviewResult struct {
	Review     model.AdvertisingReview     `json:"review"`
	Production AdvertisingProductionDetail `json:"production"`
}

func advertisingOperationKey(key string) error {
	if _, err := uuid.Parse(key); err != nil || len(key) != 36 {
		return BadAuthRequest("操作键必须为UUID")
	}
	return nil
}
func advertisingReviewResult(row *model.AdvertisingReview) (AdvertisingReviewResult, error) {
	r := AdvertisingReviewResult{Review: *row}
	err := json.Unmarshal([]byte(row.ProductionJSON), &r.Production)
	return r, err
}
func (s *Service) reviewedRender(repo *repository.Repository, user, p, assemblyID, renderID string) (*model.AdvertisingAssembly, *model.Resource, error) {
	assembly, err := repo.AdvertisingAssembly(p, assemblyID)
	if err != nil {
		return nil, nil, err
	}
	attempt, err := repo.AdvertisingRender(assemblyID, renderID)
	if err != nil {
		return nil, nil, err
	}
	detail, err := s.advertisingRenderDetail(repo, user, attempt)
	if err != nil {
		return nil, nil, err
	}
	if !detail.Available {
		return nil, nil, advertisingConflict("render_unavailable", "需要成功且当前可读的指定渲染结果")
	}
	resource, err := repo.ResourceForUser(user, detail.ResourceID)
	return assembly, resource, err
}
func (s *Service) CreateAdvertisingReview(user, project, p string, req AdvertisingReviewRequest) (AdvertisingReviewResult, error) {
	var result AdvertisingReviewResult
	if err := advertisingAccess(user, project); err != nil {
		return result, err
	}
	if err := advertisingOperationKey(req.ClientOperationID); err != nil {
		return result, err
	}
	if req.ExpectedProductionRevision < 1 || (req.Decision != "approve" && req.Decision != "reject") {
		return result, BadAuthRequest("修订和审片决定不合法")
	}
	if err := advertisingText("reason", req.Reason, 2000, true); err != nil {
		return result, BadAuthRequest(err.Error())
	}
	if req.Checkpoint == "candidate" {
		if req.SubmissionID == "" || req.AssemblyID != "" || req.RenderAttemptID != "" || req.ResourceID != "" || req.AcceptedReviewID != "" || req.ClearSelected && req.Decision != "reject" {
			return result, BadAuthRequest("候选审片目标不合法")
		}
	} else if req.Checkpoint == "assembly" || req.Checkpoint == "delivery" {
		if req.SubmissionID != "" || req.AssemblyID == "" || req.RenderAttemptID == "" || req.ResourceID == "" || req.ClearSelected || (req.Checkpoint == "assembly" && req.AcceptedReviewID != "") || (req.Checkpoint == "delivery" && (req.AcceptedReviewID == "" || req.Decision != "approve")) {
			return result, BadAuthRequest("成片审片必须指定组装和渲染尝试")
		}
	} else {
		return result, BadAuthRequest("检查点不合法")
	}
	req.ClientOperationID = strings.ToLower(req.ClientOperationID)
	hash := creationHash(req)
	err := s.repo.AdvertisingProductionTransaction(user, project, p, func(repo *repository.Repository) error {
		if prior, err := repo.AdvertisingReviewForOperation(p, req.ClientOperationID); err == nil {
			if prior.RequestHash != hash {
				return advertisingConflict("operation_conflict", "操作键已绑定其他审片决定")
			}
			result, err = advertisingReviewResult(prior)
			return err
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		current, err := advertisingProductionDetail(repo, user, project, p)
		if err != nil {
			return err
		}
		if current.Production.Revision != req.ExpectedProductionRevision {
			return repository.ErrAdvertisingProductionConflict
		}
		row := model.AdvertisingReview{ID: newID(), ProductionID: p, ClientOperationID: req.ClientOperationID, RequestHash: hash, Checkpoint: req.Checkpoint, SubmissionID: req.SubmissionID, AssemblyID: req.AssemblyID, RenderAttemptID: req.RenderAttemptID, SourceRevision: current.ConfirmedSnapshot.SourceRevision, SourceProductionRevision: current.Production.Revision, Decision: req.Decision, Reason: req.Reason, ClearSelected: req.ClearSelected, ActorID: user, CreatedAt: time.Now().UTC()}
		shot := ""
		if req.Checkpoint == "candidate" {
			sub, err := repo.AdvertisingSubmission(p, req.SubmissionID)
			if err != nil {
				return err
			}
			shot = sub.ShotID
			if !slices.ContainsFunc(current.ConfirmedSnapshot.Snapshot.Draft.Shots, func(item AdvertisingShot) bool { return item.ID == shot }) || (sub.Kind != "video" && sub.Kind != "audio") {
				return BadAuthRequest("候选不属于确认脚本镜头")
			}
			if req.Decision == "approve" {
				resource, err := s.getAdvertisingCandidateResource(repo, user, p, shot, sub.Kind, sub.ID)
				if err != nil {
					return err
				}
				row.ResourceID = resource.ID
			}
			selected := current.Settings.Selections[shot]
			isSelected := selected.VideoSubmissionID == sub.ID || selected.AudioSubmissionID == sub.ID
			if req.Decision == "reject" && isSelected && !req.ClearSelected {
				return advertisingConflict("clear_selected_required", "拒绝当前选用需明确清除选用")
			}
			if req.Decision == "approve" || isSelected {
				before := current.Settings.Selections[shot]
				if sub.Kind == "video" {
					selected.VideoSubmissionID = ""
					if req.Decision != "approve" || before.VideoSubmissionID != sub.ID {
						selected.VideoStartMs = 0
					}
					if req.Decision == "approve" {
						selected.VideoSubmissionID = sub.ID
					}
				} else {
					selected.AudioSubmissionID = ""
					if req.Decision == "approve" {
						selected.AudioSubmissionID = sub.ID
					}
				}
				current.Settings.Selections[shot] = selected
				// Rejection only removes a server-derived selection; unrelated media may be unavailable.
				if req.Decision == "approve" {
					if err := s.validateAdvertisingProductionSettings(repo, user, p, current.ConfirmedSnapshot.Snapshot, &current.Settings); err != nil {
						return err
					}
				}
				raw, err := json.Marshal(current.Settings)
				if err != nil {
					return err
				}
				if err := repo.CASAdvertisingProduction(p, req.ExpectedProductionRevision, string(raw)); err != nil {
					return err
				}
				if before != selected {
					if err := advertisingProductionEvent(repo, user, project, p, fmt.Sprintf("selection:%s:%d:%s", p, req.ExpectedProductionRevision+1, shot), "selection_changed", shot, sub.ID, "", "", AdvertisingEventDetails{Selection: &selected}); err != nil {
						return err
					}
				}
			}
		} else {
			assembly, resource, err := s.reviewedRender(repo, user, p, req.AssemblyID, req.RenderAttemptID)
			if err != nil {
				return err
			}
			valid, reason, err := s.advertisingCutCurrent(repo, user, current, assembly)
			if err != nil {
				return err
			}
			if !valid {
				return advertisingConflict("review_stale", reason)
			}
			if resource.ID != req.ResourceID {
				return advertisingConflict("render_changed", "指定的成片输出已变化，请重新查看")
			}
			if req.Checkpoint == "delivery" {
				latest, err := repo.LatestAdvertisingAssemblyReview(p)
				if err != nil || latest.ID != req.AcceptedReviewID || latest.Decision != "approve" || latest.AssemblyID != req.AssemblyID || latest.RenderAttemptID != req.RenderAttemptID || latest.ResourceID != req.ResourceID || latest.SelectionHash != assembly.SelectionHash {
					return advertisingConflict("review_stale", "交付必须引用同一输出的最新有效成片批准")
				}
				row.AcceptedReviewID = &req.AcceptedReviewID
			}
			row.ResourceID = resource.ID
			row.SelectionHash = assembly.SelectionHash
		}
		current, err = advertisingProductionDetail(repo, user, project, p)
		if err != nil {
			return err
		}
		raw, err := json.Marshal(current)
		if err != nil {
			return err
		}
		row.ProductionJSON = string(raw)
		if err := repo.CreateAdvertisingReview(&row); err != nil {
			return err
		}
		if err := advertisingProductionEvent(repo, user, project, p, "review:"+row.ID, req.Checkpoint+"_reviewed", shot, req.SubmissionID, req.AssemblyID, req.RenderAttemptID, AdvertisingEventDetails{ReviewID: row.ID, Decision: req.Decision, Note: req.Reason}); err != nil {
			return err
		}
		result = AdvertisingReviewResult{Review: row, Production: current}
		return nil
	})
	return result, advertisingProductionFailure(err)
}
func (s *Service) GetAdvertisingReviewForOperation(user, project, p, key string) (AdvertisingReviewResult, error) {
	if _, err := s.GetAdvertisingProduction(user, project, p); err != nil {
		return AdvertisingReviewResult{}, err
	}
	if err := advertisingOperationKey(key); err != nil {
		return AdvertisingReviewResult{}, err
	}
	row, err := s.repo.AdvertisingReviewForOperation(p, strings.ToLower(key))
	if err != nil {
		return AdvertisingReviewResult{}, advertisingFailure(err)
	}
	return advertisingReviewResult(row)
}
func (s *Service) ListAdvertisingReviews(user, project, p string, page, size int) (AdvertisingPage[model.AdvertisingReview], error) {
	result := AdvertisingPage[model.AdvertisingReview]{Items: []model.AdvertisingReview{}, Page: page, PageSize: size}
	if err := advertisingPagination(page, size); err != nil {
		return result, err
	}
	if _, err := s.GetAdvertisingProduction(user, project, p); err != nil {
		return result, err
	}
	var err error
	result.Items, result.Total, err = s.repo.AdvertisingReviews(p, page, size)
	return result, err
}

type AdvertisingIssueRequest struct {
	ClientOperationID string `json:"clientOperationId"`
	SubmissionID      string `json:"submissionId,omitempty"`
	AssemblyID        string `json:"assemblyId,omitempty"`
	RenderAttemptID   string `json:"renderAttemptId,omitempty"`
	AtMs              *int64 `json:"atMs,omitempty"`
	Category          string `json:"category"`
	Note              string `json:"note"`
}
type AdvertisingIssueResolution struct {
	ExpectedRevision int64  `json:"expectedRevision"`
	Status           string `json:"status"`
	ResolutionNote   string `json:"resolutionNote"`
}

func (s *Service) CreateAdvertisingIssue(user, project, p string, req AdvertisingIssueRequest) (model.AdvertisingIssue, error) {
	var result model.AdvertisingIssue
	if err := advertisingAccess(user, project); err != nil {
		return result, err
	}
	if err := advertisingOperationKey(req.ClientOperationID); err != nil {
		return result, err
	}
	if (req.SubmissionID == "") == (req.AssemblyID == "") || req.SubmissionID != "" && req.RenderAttemptID != "" || req.AssemblyID != "" && req.RenderAttemptID == "" {
		return result, BadAuthRequest("问题必须绑定一个候选或指定组装渲染")
	}
	for name, value := range map[string]string{"category": req.Category, "note": req.Note} {
		limit := 2000
		if name == "category" {
			limit = 24
		}
		if err := advertisingText(name, value, limit, true); err != nil {
			return result, BadAuthRequest(err.Error())
		}
	}
	if req.AtMs != nil && *req.AtMs < 0 {
		return result, BadAuthRequest("问题时间点不合法")
	}
	req.ClientOperationID = strings.ToLower(req.ClientOperationID)
	hash := creationHash(req)
	err := s.repo.AdvertisingProductionTransaction(user, project, p, func(repo *repository.Repository) error {
		if row, err := repo.AdvertisingIssueForOperation(p, req.ClientOperationID); err == nil {
			if row.RequestHash != hash {
				return advertisingConflict("operation_conflict", "操作键已绑定其他问题")
			}
			result = *row
			return nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var resource *model.Resource
		var err error
		shot := ""
		if req.SubmissionID != "" {
			sub, e := repo.AdvertisingSubmission(p, req.SubmissionID)
			if e != nil {
				return e
			}
			shot = sub.ShotID
			resource, err = s.getAdvertisingCandidateResource(repo, user, p, shot, sub.Kind, sub.ID)
		} else {
			_, resource, err = s.reviewedRender(repo, user, p, req.AssemblyID, req.RenderAttemptID)
		}
		if err != nil {
			return err
		}
		if req.AtMs != nil && *req.AtMs > resource.DurationMs {
			return BadAuthRequest("问题时间点超出实际媒体时长")
		}
		result = model.AdvertisingIssue{ID: newID(), ProductionID: p, ClientOperationID: req.ClientOperationID, RequestHash: hash, SubmissionID: req.SubmissionID, AssemblyID: req.AssemblyID, RenderAttemptID: req.RenderAttemptID, AtMs: req.AtMs, Category: req.Category, Note: req.Note, ActorID: user, Revision: 1, Status: "open"}
		if err := repo.CreateAdvertisingIssue(&result); err != nil {
			return err
		}
		return advertisingProductionEvent(repo, user, project, p, "issue:"+result.ID, "issue_created", shot, req.SubmissionID, req.AssemblyID, req.RenderAttemptID, AdvertisingEventDetails{IssueID: result.ID, Category: req.Category, Note: req.Note, AtMs: req.AtMs})
	})
	return result, advertisingProductionFailure(err)
}
func (s *Service) ResolveAdvertisingIssue(user, project, p, id string, req AdvertisingIssueResolution) (model.AdvertisingIssue, error) {
	var result model.AdvertisingIssue
	if err := advertisingAccess(user, project); err != nil {
		return result, err
	}
	if req.ExpectedRevision < 1 || req.Status != "resolved" && req.Status != "open" {
		return result, BadAuthRequest("问题状态或修订不合法")
	}
	if err := advertisingText("resolutionNote", req.ResolutionNote, 2000, true); err != nil {
		return result, BadAuthRequest(err.Error())
	}
	err := s.repo.AdvertisingProductionTransaction(user, project, p, func(repo *repository.Repository) error {
		row, err := repo.AdvertisingIssue(p, id)
		if err != nil {
			return err
		}
		if err := repo.ResolveAdvertisingIssue(id, req.ExpectedRevision, req.Status, req.ResolutionNote, user); err != nil {
			return err
		}
		shot := ""
		if row.SubmissionID != "" {
			sub, err := repo.AdvertisingSubmission(p, row.SubmissionID)
			if err != nil {
				return err
			}
			shot = sub.ShotID
		}
		if err := advertisingProductionEvent(repo, user, project, p, fmt.Sprintf("issue:%s:%d", id, req.ExpectedRevision+1), "issue_resolved", shot, row.SubmissionID, row.AssemblyID, row.RenderAttemptID, AdvertisingEventDetails{IssueID: id, Status: req.Status, Note: req.ResolutionNote}); err != nil {
			return err
		}
		row, err = repo.AdvertisingIssue(p, id)
		if err == nil {
			result = *row
		}
		return err
	})
	if errors.Is(err, repository.ErrAdvertisingIssueConflict) {
		err = advertisingConflict("issue_conflict", "问题已更新，请重新读取")
	}
	return result, advertisingProductionFailure(err)
}
func (s *Service) GetAdvertisingIssueForOperation(user, project, p, key string) (model.AdvertisingIssue, error) {
	if _, err := s.GetAdvertisingProduction(user, project, p); err != nil {
		return model.AdvertisingIssue{}, err
	}
	if err := advertisingOperationKey(key); err != nil {
		return model.AdvertisingIssue{}, err
	}
	row, err := s.repo.AdvertisingIssueForOperation(p, strings.ToLower(key))
	if err != nil {
		return model.AdvertisingIssue{}, advertisingFailure(err)
	}
	return *row, nil
}
func (s *Service) ListAdvertisingIssues(user, project, p string, page, size int) (AdvertisingPage[model.AdvertisingIssue], error) {
	result := AdvertisingPage[model.AdvertisingIssue]{Items: []model.AdvertisingIssue{}, Page: page, PageSize: size}
	if err := advertisingPagination(page, size); err != nil {
		return result, err
	}
	if _, err := s.GetAdvertisingProduction(user, project, p); err != nil {
		return result, err
	}
	var err error
	result.Items, result.Total, err = s.repo.AdvertisingIssues(p, page, size)
	return result, err
}
