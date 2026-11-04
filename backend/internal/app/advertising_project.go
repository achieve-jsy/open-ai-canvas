package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"gorm.io/gorm"
	"yingce/backend/internal/kernel"
	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
)

type AdvertisingCreateRequest struct {
	Name  string            `json:"name"`
	Draft *AdvertisingDraft `json:"draft,omitempty"`
}
type AdvertisingSaveRequest struct {
	Name             string           `json:"name"`
	Draft            AdvertisingDraft `json:"draft"`
	ExpectedRevision int64            `json:"expectedRevision"`
}
type AdvertisingResource struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	MimeType   string `json:"mimeType"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	DurationMs int64  `json:"durationMs"`
	Available  bool   `json:"available"`
}
type AdvertisingDetail struct {
	Project                   model.Project         `json:"project"`
	Draft                     AdvertisingDraft      `json:"draft"`
	Revision                  int64                 `json:"revision"`
	LatestConfirmedRevisionID *string               `json:"latestConfirmedRevisionId,omitempty"`
	Resources                 []AdvertisingResource `json:"resources"`
}
type AdvertisingSnapshot struct {
	Name      string                `json:"name"`
	Draft     AdvertisingDraft      `json:"draft"`
	Resources []AdvertisingResource `json:"resources"`
}
type AdvertisingRevisionDetail struct {
	ID             string              `json:"id"`
	ProjectID      string              `json:"projectId"`
	SourceRevision int64               `json:"sourceRevision"`
	Snapshot       AdvertisingSnapshot `json:"snapshot"`
	CreatedAt      time.Time           `json:"createdAt"`
}
type AdvertisingConfirmation struct {
	Snapshot        AdvertisingRevisionDetail `json:"snapshot"`
	CurrentRevision int64                     `json:"currentRevision"`
}
type AdvertisingListItem struct {
	repository.AdvertisingProjectSummary
	ProductName string `json:"productName"`
	DurationMs  int64  `json:"durationMs"`
}
type AdvertisingList struct {
	Items    []AdvertisingListItem `json:"items"`
	Page     int                   `json:"page"`
	PageSize int                   `json:"pageSize"`
	Total    int64                 `json:"total"`
}
type AdvertisingRevisionSummary struct {
	ID             string    `json:"id"`
	SourceRevision int64     `json:"sourceRevision"`
	CreatedAt      time.Time `json:"createdAt"`
}
type AdvertisingRevisionList struct {
	Items    []AdvertisingRevisionSummary `json:"items"`
	Page     int                          `json:"page"`
	PageSize int                          `json:"pageSize"`
	Total    int64                        `json:"total"`
}

func advertisingError(status int, reason kernel.ErrorReason, message string) error {
	err := NewAppError(status, message)
	err.Reason = reason
	return err
}
func advertisingFailure(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return NotFound("广告项目或版本不存在")
	}
	if errors.Is(err, repository.ErrAdvertisingRevisionConflict) {
		return advertisingError(409, "advertising_revision_conflict", "草稿已更新，请查看服务端版本后重试")
	}
	if errors.Is(err, repository.ErrProjectNameConflict) {
		return advertisingError(409, ReasonProjectNameConflict, "项目名称已存在")
	}
	return err
}
func advertisingAccess(userID, projectID string) error {
	if strings.TrimSpace(userID) == "" {
		return Unauthorized("请先登录")
	}
	if projectID != "" && !advertisingID.MatchString(projectID) {
		return NotFound("广告项目不存在")
	}
	return nil
}
func advertisingPagination(page, size int) error {
	if page < 1 || page > 1000000 || size < 1 || size > 100 {
		return BadAuthRequest("分页参数不合法")
	}
	return nil
}
func advertisingValidate(name string, d AdvertisingDraft, confirm bool) error {
	if err := advertisingText("name", name, 120, true); err != nil {
		return advertisingError(400, "advertising_invalid_document", err.Error())
	}
	if err := validateAdvertisingDraft(d, confirm); err != nil {
		return advertisingError(400, "advertising_invalid_document", err.Error())
	}
	return nil
}

func advertisingResources(repo *repository.Repository, userID string, d AdvertisingDraft, strict bool) ([]AdvertisingResource, error) {
	ids := advertisingResourceIDs(d)
	rows, err := repo.ResourcesForUserIDs(userID, ids)
	if err != nil {
		return nil, err
	}
	byID := map[string]model.Resource{}
	for _, row := range rows {
		byID[row.ID] = row
	}
	result := make([]AdvertisingResource, 0, len(ids))
	for _, id := range ids {
		row, found := byID[id]
		mime := strings.ToLower(row.MimeType)
		image := slices.Contains([]string{"image/png", "image/jpeg", "image/webp"}, mime)
		allowed := image || slices.Contains([]string{"video/mp4", "audio/mpeg", "audio/mp4", "audio/wav", "audio/x-wav"}, mime)
		if slices.Contains(d.ProductResourceIDs, id) || d.Brand.LogoResourceID == id {
			allowed = image
		}
		available := found && row.Status == model.ResourceStatusReady && allowed
		if strict && !available {
			return nil, advertisingError(400, "advertising_resource_unavailable", "资源未就绪、类型不符或无权访问: "+id)
		}
		// Resource has no canonical title; use its stable ID rather than a signed URL or storage path.
		result = append(result, AdvertisingResource{ID: id, Title: id, MimeType: row.MimeType, Width: row.Width, Height: row.Height, DurationMs: row.DurationMs, Available: available})
	}
	return result, nil
}
func advertisingDetail(repo *repository.Repository, userID, id string) (AdvertisingDetail, error) {
	row, err := repo.AdvertisingProjectForUser(userID, id)
	if err != nil {
		return AdvertisingDetail{}, err
	}
	project, err := repo.ProjectForUser(userID, id)
	if err != nil {
		return AdvertisingDetail{}, err
	}
	var draft AdvertisingDraft
	if err := json.Unmarshal([]byte(row.DraftJSON), &draft); err != nil {
		return AdvertisingDetail{}, fmt.Errorf("读取广告草稿失败: %w", err)
	}
	resources, err := advertisingResources(repo, userID, draft, false)
	if err != nil {
		return AdvertisingDetail{}, err
	}
	return AdvertisingDetail{Project: *project, Draft: draft, Revision: row.Revision, LatestConfirmedRevisionID: row.LatestConfirmedRevisionID, Resources: resources}, nil
}
func advertisingRevisionDetail(row *model.AdvertisingRevision) (AdvertisingRevisionDetail, error) {
	detail := AdvertisingRevisionDetail{ID: row.ID, ProjectID: row.ProjectID, SourceRevision: row.SourceRevision, CreatedAt: row.CreatedAt}
	err := json.Unmarshal([]byte(row.SnapshotJSON), &detail.Snapshot)
	return detail, err
}

func (s *Service) CreateAdvertisingProject(userID string, req AdvertisingCreateRequest) (AdvertisingDetail, error) {
	if err := advertisingAccess(userID, ""); err != nil {
		return AdvertisingDetail{}, err
	}
	draft := normalizeAdvertisingDraft(AdvertisingDraft{SchemaVersion: 1, Brief: AdvertisingBrief{DurationMs: 15000}})
	if req.Draft != nil {
		draft = normalizeAdvertisingDraft(*req.Draft)
	}
	name := strings.TrimSpace(req.Name)
	if err := advertisingValidate(name, draft, false); err != nil {
		return AdvertisingDetail{}, err
	}
	raw, err := json.Marshal(draft)
	if err != nil {
		return AdvertisingDetail{}, err
	}
	now := time.Now()
	id := newID()
	var detail AdvertisingDetail
	err = s.repo.Transaction(func(repo *repository.Repository) error {
		if _, err := advertisingResources(repo, userID, draft, true); err != nil {
			return err
		}
		project := model.Project{ID: id, UserID: userID, Name: name, Type: "advertising", AspectRatio: "9:16", Status: model.ProjectStatusActive, Revision: 1, CreatedAt: now, UpdatedAt: now}
		if err := repo.CreateProject(&project); err != nil {
			return err
		}
		if err := repo.CreateAdvertisingProject(&model.AdvertisingProject{ProjectID: id, Revision: 1, DraftJSON: string(raw), CreatedAt: now, UpdatedAt: now}); err != nil {
			return err
		}
		if err := repo.ReplaceAdvertisingReferences(id, "draft", advertisingResourceIDs(draft)); err != nil {
			return err
		}
		var err error
		detail, err = advertisingDetail(repo, userID, id)
		return err
	})
	return detail, advertisingFailure(err)
}
func (s *Service) GetAdvertisingProject(userID, id string) (AdvertisingDetail, error) {
	if err := advertisingAccess(userID, id); err != nil {
		return AdvertisingDetail{}, err
	}
	var detail AdvertisingDetail
	err := s.repo.Transaction(func(repo *repository.Repository) error {
		var err error
		detail, err = advertisingDetail(repo, userID, id)
		return err
	})
	return detail, advertisingFailure(err)
}
func (s *Service) SaveAdvertisingProject(userID, id string, req AdvertisingSaveRequest) (AdvertisingDetail, error) {
	if err := advertisingAccess(userID, id); err != nil {
		return AdvertisingDetail{}, err
	}
	if req.ExpectedRevision < 1 {
		return AdvertisingDetail{}, BadAuthRequest("expectedRevision必须为正整数")
	}
	name := strings.TrimSpace(req.Name)
	draft := normalizeAdvertisingDraft(req.Draft)
	if err := advertisingValidate(name, draft, false); err != nil {
		return AdvertisingDetail{}, err
	}
	raw, err := json.Marshal(draft)
	if err != nil {
		return AdvertisingDetail{}, err
	}
	var detail AdvertisingDetail
	err = s.repo.AdvertisingTransaction(userID, id, func(repo *repository.Repository) error {
		row, err := repo.AdvertisingProjectForUser(userID, id)
		if err != nil {
			return err
		}
		if row.Revision != req.ExpectedRevision {
			return repository.ErrAdvertisingRevisionConflict
		}
		if _, err := advertisingResources(repo, userID, draft, true); err != nil {
			return err
		}
		now := time.Now()
		if err := repo.CASAdvertisingProject(id, row.Revision, map[string]any{"draft_json": string(raw), "revision": row.Revision + 1, "updated_at": now}); err != nil {
			return err
		}
		if err := repo.RenameAdvertisingProject(userID, id, name, now); err != nil {
			return err
		}
		if err := repo.ReplaceAdvertisingReferences(id, "draft", advertisingResourceIDs(draft)); err != nil {
			return err
		}
		detail, err = advertisingDetail(repo, userID, id)
		return err
	})
	return detail, advertisingFailure(err)
}
func (s *Service) ConfirmAdvertisingProject(userID, id string, expected int64) (AdvertisingConfirmation, error) {
	if err := advertisingAccess(userID, id); err != nil {
		return AdvertisingConfirmation{}, err
	}
	if expected < 1 {
		return AdvertisingConfirmation{}, BadAuthRequest("expectedRevision必须为正整数")
	}
	var result AdvertisingConfirmation
	err := s.repo.AdvertisingTransaction(userID, id, func(repo *repository.Repository) error {
		detail, err := advertisingDetail(repo, userID, id)
		if err != nil {
			return err
		}
		result.CurrentRevision = detail.Revision
		prior, err := repo.AdvertisingRevisionForSource(id, expected)
		if err == nil {
			result.Snapshot, err = advertisingRevisionDetail(prior)
			return err
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if detail.Revision != expected {
			return repository.ErrAdvertisingRevisionConflict
		}
		if err := advertisingValidate(detail.Project.Name, detail.Draft, true); err != nil {
			return err
		}
		resources, err := advertisingResources(repo, userID, detail.Draft, true)
		if err != nil {
			return err
		}
		if detail.LatestConfirmedRevisionID != nil {
			last, err := repo.AdvertisingRevisionForUser(userID, id, *detail.LatestConfirmedRevisionID)
			if err != nil {
				return err
			}
			old, err := advertisingRevisionDetail(last)
			if err != nil {
				return err
			}
			if old.Snapshot.Name == detail.Project.Name && reflect.DeepEqual(normalizeAdvertisingDraft(old.Snapshot.Draft), normalizeAdvertisingDraft(detail.Draft)) {
				result.Snapshot = old
				return nil
			}
		}
		snapshot := AdvertisingSnapshot{Name: detail.Project.Name, Draft: detail.Draft, Resources: resources}
		raw, err := json.Marshal(snapshot)
		if err != nil {
			return err
		}
		now := time.Now()
		version := model.AdvertisingRevision{ID: newID(), ProjectID: id, SourceRevision: expected, SnapshotJSON: string(raw), CreatedAt: now}
		if err := repo.CreateAdvertisingRevision(&version); err != nil {
			return err
		}
		if err := repo.ReplaceAdvertisingReferences(id, version.ID, advertisingResourceIDs(detail.Draft)); err != nil {
			return err
		}
		if err := repo.CASAdvertisingProject(id, expected, map[string]any{"latest_confirmed_revision_id": version.ID, "revision": expected + 1, "updated_at": now}); err != nil {
			return err
		}
		result.Snapshot, err = advertisingRevisionDetail(&version)
		result.CurrentRevision = expected + 1
		return err
	})
	return result, advertisingFailure(err)
}
func (s *Service) ListAdvertisingProjects(userID string, page, size int) (AdvertisingList, error) {
	result := AdvertisingList{Items: []AdvertisingListItem{}, Page: page, PageSize: size}
	if err := advertisingAccess(userID, ""); err != nil {
		return result, err
	}
	if err := advertisingPagination(page, size); err != nil {
		return result, err
	}
	rows, total, err := s.repo.AdvertisingProjects(userID, page, size)
	if err != nil {
		return result, err
	}
	result.Total = total
	for _, row := range rows {
		var d AdvertisingDraft
		if err := json.Unmarshal([]byte(row.DraftJSON), &d); err != nil {
			return result, err
		}
		result.Items = append(result.Items, AdvertisingListItem{AdvertisingProjectSummary: row, ProductName: d.Brief.ProductName, DurationMs: d.Brief.DurationMs})
	}
	return result, nil
}
func (s *Service) ListAdvertisingRevisions(userID, id string, page, size int) (AdvertisingRevisionList, error) {
	result := AdvertisingRevisionList{Items: []AdvertisingRevisionSummary{}, Page: page, PageSize: size}
	if err := advertisingAccess(userID, id); err != nil {
		return result, err
	}
	if err := advertisingPagination(page, size); err != nil {
		return result, err
	}
	if _, err := s.repo.AdvertisingProjectForUser(userID, id); err != nil {
		return result, advertisingFailure(err)
	}
	rows, total, err := s.repo.AdvertisingRevisions(id, page, size)
	if err != nil {
		return result, err
	}
	result.Total = total
	for _, row := range rows {
		result.Items = append(result.Items, AdvertisingRevisionSummary{ID: row.ID, SourceRevision: row.SourceRevision, CreatedAt: row.CreatedAt})
	}
	return result, nil
}
func (s *Service) DeleteAdvertisingProject(userID, id string, expected int64) error {
	if err := advertisingAccess(userID, id); err != nil {
		return err
	}
	if expected < 1 {
		return BadAuthRequest("expectedRevision必须为正整数")
	}
	err := s.repo.AdvertisingTransaction(userID, id, func(repo *repository.Repository) error {
		if err := repo.CASAdvertisingProject(id, expected, map[string]any{"revision": expected + 1}); err != nil {
			return err
		}
		if err := repo.GuardAdvertisingDeletion(userID, id); err != nil {
			return err
		}
		if err := repo.DeleteAdvertisingContent(id); err != nil {
			return err
		}
		return deleteProjectWithRepository(repo, userID, id)
	})
	if errors.Is(err, repository.ErrProjectHasActiveTasks) {
		return advertisingError(409, "advertising_active_tasks", "广告项目仍有活动任务，请等待完成或取消")
	}
	return advertisingFailure(err)
}

func (s *Service) GetAdvertisingRevision(userID, id, versionID string) (AdvertisingRevisionDetail, error) {
	if err := advertisingAccess(userID, id); err != nil {
		return AdvertisingRevisionDetail{}, err
	}
	if !advertisingID.MatchString(versionID) {
		return AdvertisingRevisionDetail{}, NotFound("版本不存在")
	}
	row, err := s.repo.AdvertisingRevisionForUser(userID, id, versionID)
	if err != nil {
		return AdvertisingRevisionDetail{}, advertisingFailure(err)
	}
	return advertisingRevisionDetail(row)
}
