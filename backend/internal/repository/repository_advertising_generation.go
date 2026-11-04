package repository

import (
	"errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
	"yingce/backend/internal/model"
)

func (r *Repository) IsAdvertisingGenerationTask(taskID string) (bool, error) {
	var n int64
	err := r.db.Model(&model.AdvertisingSubmission{}).Where("task_id = ?", taskID).Count(&n).Error
	return n > 0, err
}

func (r *Repository) AdvertisingSubmissionForTask(taskID string) (*model.AdvertisingSubmission, error) {
	var row model.AdvertisingSubmission
	err := r.db.Where("task_id = ?", taskID).First(&row).Error
	return &row, err
}
func (r *Repository) HasAdvertisingCandidateReference(projectID, submissionID, resourceID string) (bool, error) {
	var count int64
	err := r.db.Model(&model.AdvertisingResourceReference{}).Where("project_id = ? AND scope_key = ? AND resource_id = ?", projectID, "candidate:"+submissionID, resourceID).Count(&count).Error
	return count == 1, err
}
func (r *Repository) AdvertisingSubmission(productionID, id string) (*model.AdvertisingSubmission, error) {
	var row model.AdvertisingSubmission
	err := r.db.Where("production_id = ? AND id = ?", productionID, id).First(&row).Error
	return &row, err
}
func (r *Repository) AdvertisingSubmissionForOperation(productionID, key string) (*model.AdvertisingSubmission, error) {
	var row model.AdvertisingSubmission
	err := r.db.Where("production_id = ? AND client_operation_id = ?", productionID, key).First(&row).Error
	return &row, err
}
func (r *Repository) CreateAdvertisingSubmission(row *model.AdvertisingSubmission) error {
	return r.db.Omit(clause.Associations).Create(row).Error
}
func (r *Repository) ApproveAdvertisingSubmission(id, taskID string, at time.Time) error {
	result := r.db.Model(&model.AdvertisingSubmission{}).Where("id = ? AND task_id IS NULL", id).Updates(map[string]any{"task_id": taskID, "approved_at": at})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrAdvertisingProductionConflict
	}
	return nil
}
func (r *Repository) AdvertisingSubmissions(productionID string, page, size int) ([]model.AdvertisingSubmission, int64, error) {
	rows := []model.AdvertisingSubmission{}
	var total int64
	q := r.db.Model(&model.AdvertisingSubmission{}).Where("production_id = ?", productionID)
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := q.Order("created_at DESC, id DESC").Limit(size).Offset((page - 1) * size).Find(&rows).Error
	return rows, total, err
}

var ErrAdvertisingProductionConflict = errors.New("advertising production revision conflict")

func (r *Repository) AdvertisingProductionForUser(userID, projectID, id string) (*model.AdvertisingProduction, error) {
	var row model.AdvertisingProduction
	err := r.db.Joins("JOIN projects ON projects.id = advertising_productions.project_id").Where("projects.user_id = ? AND projects.type = ? AND advertising_productions.project_id = ? AND advertising_productions.id = ?", userID, "advertising", projectID, id).First(&row).Error
	return &row, err
}
func (r *Repository) AdvertisingProductionForRevision(projectID, revisionID string) (*model.AdvertisingProduction, error) {
	var row model.AdvertisingProduction
	err := r.db.Where("project_id = ? AND confirmed_revision_id = ?", projectID, revisionID).First(&row).Error
	return &row, err
}
func (r *Repository) CreateAdvertisingProduction(row *model.AdvertisingProduction) error {
	return r.db.Omit(clause.Associations).Create(row).Error
}
func (r *Repository) AdvertisingProductionTransaction(userID, projectID, productionID string, fn func(*Repository) error) error {
	return r.AdvertisingTransaction(userID, projectID, func(repo *Repository) error {
		if _, err := repo.AdvertisingProductionForUser(userID, projectID, productionID); err != nil {
			return err
		}
		return fn(repo)
	})
}
func (r *Repository) CASAdvertisingProduction(id string, expected int64, settings string) error {
	result := r.db.Model(&model.AdvertisingProduction{}).Where("id = ? AND revision = ?", id, expected).Updates(map[string]any{"settings_json": settings, "revision": gorm.Expr("revision + 1"), "updated_at": time.Now().UTC()})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrAdvertisingProductionConflict
	}
	return nil
}

func (r *Repository) AdvertisingAssembly(productionID, id string) (*model.AdvertisingAssembly, error) {
	var row model.AdvertisingAssembly
	err := r.db.Where("production_id = ? AND id = ?", productionID, id).First(&row).Error
	return &row, err
}
func (r *Repository) AdvertisingAssemblyForOperation(productionID, key string) (*model.AdvertisingAssembly, error) {
	var row model.AdvertisingAssembly
	err := r.db.Where("production_id = ? AND client_operation_id = ?", productionID, key).First(&row).Error
	return &row, err
}
func (r *Repository) CreateAdvertisingAssembly(row *model.AdvertisingAssembly) error {
	return r.db.Omit(clause.Associations).Create(row).Error
}
func (r *Repository) AdvertisingAssemblies(productionID string, page, size int) ([]model.AdvertisingAssembly, int64, error) {
	rows := []model.AdvertisingAssembly{}
	var total int64
	q := r.db.Model(&model.AdvertisingAssembly{}).Where("production_id = ?", productionID)
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := q.Order("created_at DESC, id DESC").Limit(size).Offset((page - 1) * size).Find(&rows).Error
	return rows, total, err
}

func (r *Repository) AdvertisingRenderForOperation(assemblyID, key string) (*model.AdvertisingRenderAttempt, error) {
	var row model.AdvertisingRenderAttempt
	err := r.db.Where("assembly_id = ? AND client_operation_id = ?", assemblyID, key).First(&row).Error
	return &row, err
}
func (r *Repository) AdvertisingRenderForTask(taskID string) (*model.AdvertisingRenderAttempt, error) {
	var row model.AdvertisingRenderAttempt
	err := r.db.Where("task_id = ?", taskID).First(&row).Error
	return &row, err
}
func (r *Repository) AdvertisingAssemblyForTask(taskID string) (*model.AdvertisingAssembly, error) {
	var row model.AdvertisingAssembly
	err := r.db.Joins("JOIN advertising_render_attempts ON advertising_render_attempts.assembly_id = advertising_assemblies.id").Where("advertising_render_attempts.task_id = ?", taskID).First(&row).Error
	return &row, err
}
func (r *Repository) CreateAdvertisingRender(row *model.AdvertisingRenderAttempt) error {
	return r.db.Omit(clause.Associations).Create(row).Error
}
func (r *Repository) AdvertisingRenders(assemblyID string, page, size int) ([]model.AdvertisingRenderAttempt, int64, error) {
	rows := []model.AdvertisingRenderAttempt{}
	var total int64
	q := r.db.Model(&model.AdvertisingRenderAttempt{}).Where("assembly_id = ?", assemblyID)
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := q.Order("created_at DESC, id DESC").Limit(size).Offset((page - 1) * size).Find(&rows).Error
	return rows, total, err
}
