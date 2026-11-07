package repository

import (
	"errors"
	"gorm.io/gorm/clause"
	"time"
	"yingce/backend/internal/model"
)

func (r *Repository) AppendAdvertisingEvent(row *model.AdvertisingEvent) error {
	return r.db.Omit(clause.Associations).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "project_id"}, {Name: "event_key"}}, DoNothing: true}).Create(row).Error
}
func (r *Repository) AdvertisingEvents(project, production, shot, kind string) ([]model.AdvertisingEvent, error) {
	rows := []model.AdvertisingEvent{}
	q := r.db.Where("project_id = ?", project)
	for column, value := range map[string]string{"production_id": production, "shot_id": shot, "kind": kind} {
		if value != "" {
			q = q.Where(column+" = ?", value)
		}
	}
	return rows, q.Order("created_at DESC, id DESC").Find(&rows).Error
}
func (r *Repository) CreateAdvertisingReview(row *model.AdvertisingReview) error {
	return r.db.Omit(clause.Associations).Create(row).Error
}
func (r *Repository) AdvertisingReviewForOperation(production, key string) (*model.AdvertisingReview, error) {
	var row model.AdvertisingReview
	err := r.db.Where("production_id = ? AND client_operation_id = ?", production, key).First(&row).Error
	return &row, err
}
func (r *Repository) LatestAdvertisingAssemblyReview(production string) (*model.AdvertisingReview, error) {
	var row model.AdvertisingReview
	err := r.db.Where("production_id = ? AND checkpoint = ?", production, "assembly").Order("created_at DESC, id DESC").First(&row).Error
	return &row, err
}
func (r *Repository) AdvertisingReviews(production string, page, size int) ([]model.AdvertisingReview, int64, error) {
	rows := []model.AdvertisingReview{}
	var total int64
	q := r.db.Model(&model.AdvertisingReview{}).Where("production_id = ?", production)
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := q.Order("created_at DESC, id DESC").Limit(size).Offset((page - 1) * size).Find(&rows).Error
	return rows, total, err
}
func (r *Repository) CreateAdvertisingIssue(row *model.AdvertisingIssue) error {
	return r.db.Omit(clause.Associations).Create(row).Error
}
func (r *Repository) AdvertisingIssue(production, id string) (*model.AdvertisingIssue, error) {
	var row model.AdvertisingIssue
	err := r.db.Where("production_id = ? AND id = ?", production, id).First(&row).Error
	return &row, err
}
func (r *Repository) AdvertisingIssueForOperation(production, key string) (*model.AdvertisingIssue, error) {
	var row model.AdvertisingIssue
	err := r.db.Where("production_id = ? AND client_operation_id = ?", production, key).First(&row).Error
	return &row, err
}

var ErrAdvertisingIssueConflict = errors.New("advertising issue revision conflict")

func (r *Repository) ResolveAdvertisingIssue(id string, expected int64, status, note, actor string) error {
	q := r.db.Model(&model.AdvertisingIssue{}).Where("id = ? AND revision = ?", id, expected).Updates(map[string]any{"revision": expected + 1, "status": status, "resolution_note": note, "resolved_by": actor, "updated_at": time.Now().UTC()})
	if q.Error != nil {
		return q.Error
	}
	if q.RowsAffected != 1 {
		return ErrAdvertisingIssueConflict
	}
	return nil
}
func (r *Repository) AdvertisingIssues(production string, page, size int) ([]model.AdvertisingIssue, int64, error) {
	rows := []model.AdvertisingIssue{}
	var total int64
	q := r.db.Model(&model.AdvertisingIssue{}).Where("production_id = ?", production)
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := q.Order("created_at DESC, id DESC").Limit(size).Offset((page - 1) * size).Find(&rows).Error
	return rows, total, err
}
func (r *Repository) AdvertisingRender(assembly, id string) (*model.AdvertisingRenderAttempt, error) {
	var row model.AdvertisingRenderAttempt
	err := r.db.Where("assembly_id = ? AND id = ?", assembly, id).First(&row).Error
	return &row, err
}

// Select only persisted task fact fields, never task input, credentials or URLs.
type AdvertisingTaskFact struct {
	TaskID, ProductionID, ShotID, SubmissionID, AssemblyID, RenderAttemptID, Status string
	SourceRevision                                                                  int64
	CreatedAt                                                                       time.Time
	CompletedAt                                                                     *time.Time
}

func (r *Repository) AdvertisingTaskFacts(project string) ([]AdvertisingTaskFact, error) {
	rows := []AdvertisingTaskFact{}
	err := r.db.Table("tasks t").Select("t.id AS task_id, p.id AS production_id, s.shot_id, s.id AS submission_id, t.status, v.source_revision, t.created_at, t.completed_at").Joins("JOIN advertising_submissions s ON s.task_id = t.id").Joins("JOIN advertising_productions p ON p.id = s.production_id").Joins("JOIN advertising_revisions v ON v.id = p.confirmed_revision_id").Where("p.project_id = ?", project).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	var renders []AdvertisingTaskFact
	err = r.db.Table("tasks t").Select("t.id AS task_id, p.id AS production_id, a.id AS assembly_id, r.id AS render_attempt_id, t.status, v.source_revision, t.created_at, t.completed_at").Joins("JOIN advertising_render_attempts r ON r.task_id = t.id").Joins("JOIN advertising_assemblies a ON a.id = r.assembly_id").Joins("JOIN advertising_productions p ON p.id = a.production_id").Joins("JOIN advertising_revisions v ON v.id = p.confirmed_revision_id").Where("p.project_id = ?", project).Scan(&renders).Error
	return append(rows, renders...), err
}
