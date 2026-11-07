package repository

import (
	"gorm.io/gorm/clause"
	"time"
	"yingce/backend/internal/model"
)

func (r *Repository) AdvertisingBatch(productionID, id string) (*model.AdvertisingCandidateBatch, error) {
	var row model.AdvertisingCandidateBatch
	err := r.db.Where("production_id = ? AND id = ?", productionID, id).First(&row).Error
	return &row, err
}
func (r *Repository) AdvertisingBatchForOperation(productionID, key string) (*model.AdvertisingCandidateBatch, error) {
	var row model.AdvertisingCandidateBatch
	err := r.db.Where("production_id = ? AND client_operation_id = ?", productionID, key).First(&row).Error
	return &row, err
}
func (r *Repository) AdvertisingBatchMembers(productionID, id string) ([]model.AdvertisingSubmission, error) {
	rows := []model.AdvertisingSubmission{}
	err := r.db.Where("production_id = ? AND batch_id = ?", productionID, id).Order("candidate_index ASC").Find(&rows).Error
	return rows, err
}
func (r *Repository) AdvertisingBatches(productionID string, page, size int) ([]model.AdvertisingCandidateBatch, int64, error) {
	rows := []model.AdvertisingCandidateBatch{}
	var n int64
	q := r.db.Model(&model.AdvertisingCandidateBatch{}).Where("production_id = ?", productionID)
	if err := q.Count(&n).Error; err != nil {
		return nil, 0, err
	}
	err := q.Order("created_at DESC, id DESC").Offset((page - 1) * size).Limit(size).Find(&rows).Error
	return rows, n, err
}
func (r *Repository) CreateAdvertisingBatch(row *model.AdvertisingCandidateBatch) error {
	return r.db.Omit(clause.Associations).Create(row).Error
}
func (r *Repository) ApproveAdvertisingBatch(id, hash string, at time.Time) error {
	result := r.db.Model(&model.AdvertisingCandidateBatch{}).Where("id = ? AND approved_at IS NULL AND closed_at IS NULL", id).Updates(map[string]any{"approval_hash": hash, "approved_at": at})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrAdvertisingProductionConflict
	}
	return nil
}
func (r *Repository) CloseAdvertisingBatch(id string, at time.Time) error {
	result := r.db.Model(&model.AdvertisingCandidateBatch{}).Where("id = ? AND approved_at IS NULL AND NOT EXISTS (SELECT 1 FROM advertising_submissions WHERE batch_id = ? AND task_id IS NOT NULL)", id, id).Update("closed_at", at)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrAdvertisingProductionConflict
	}
	return nil
}

// No pagination: admission and activity must see even the oldest unresolved task.
// ponytail: scans production history; add an indexed unresolved-task query if history volume warrants it.
func (r *Repository) AdvertisingTaskSubmissions(productionID string) ([]model.AdvertisingSubmission, error) {
	rows := []model.AdvertisingSubmission{}
	err := r.db.Where("production_id = ? AND task_id IS NOT NULL", productionID).Order("created_at ASC, id ASC").Find(&rows).Error
	return rows, err
}
