package repository

import (
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"yingce/backend/internal/model"
)

func (r *Repository) AdvertisingPlan(projectID, operationID string) (*model.AdvertisingScriptPlan, error) {
	var row model.AdvertisingScriptPlan
	err := r.db.Where("project_id = ? AND operation_id = ?", projectID, operationID).First(&row).Error
	return &row, err
}

func (r *Repository) LatestAdvertisingPlan(projectID string) (*model.AdvertisingScriptPlan, error) {
	var row model.AdvertisingScriptPlan
	err := r.db.Where("project_id = ?", projectID).Order("created_at desc, id desc").First(&row).Error
	return &row, err
}

func (r *Repository) LockAdvertisingPlanUser(userID string) error {
	if r.db.Dialector.Name() == "sqlite" {
		return nil
	} // The project transaction already holds SQLite's writer lock.
	var user model.User
	return r.db.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", userID).First(&user).Error
}

func (r *Repository) CountAdvertisingPlansSince(userID string, since time.Time) (int64, error) {
	var count int64
	err := r.db.Model(&model.AdvertisingScriptPlan{}).Where("user_id = ? AND created_at >= ?", userID, since).Count(&count).Error
	return count, err
}

func (r *Repository) ActiveAdvertisingPlans(projectID string, since time.Time) (int64, error) {
	var count int64
	err := r.db.Model(&model.AdvertisingScriptPlan{}).Where("project_id = ? AND status = ? AND created_at >= ?", projectID, "running", since).Count(&count).Error
	return count, err
}

func (r *Repository) CreateAdvertisingPlan(row *model.AdvertisingScriptPlan) error {
	return r.db.Create(row).Error
}

func (r *Repository) FinishAdvertisingPlan(projectID, operationID, status, result, problem string) error {
	update := r.db.Model(&model.AdvertisingScriptPlan{}).Where("project_id = ? AND operation_id = ? AND status = ?", projectID, operationID, "running").Updates(map[string]any{"status": status, "result_json": result, "error": problem, "updated_at": time.Now()})
	if update.Error != nil {
		return update.Error
	}
	if update.RowsAffected != 1 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *Repository) FailStaleAdvertisingPlans(projectID string, before time.Time) error {
	return r.db.Model(&model.AdvertisingScriptPlan{}).Where("project_id = ? AND status = ? AND created_at < ?", projectID, "running", before).Updates(map[string]any{"status": "failed", "error": "规划超时，请新建一次规划", "updated_at": time.Now()}).Error
}

func (r *Repository) DeleteAdvertisingPlans(projectID string) error {
	return r.db.Where("project_id = ?", projectID).Delete(&model.AdvertisingScriptPlan{}).Error
}

func (r *Repository) LockAdvertisingPlanBudget() error {
	if r.db.Dialector.Name() == "postgres" {
		return r.db.Exec("LOCK TABLE advertising_script_plans IN SHARE ROW EXCLUSIVE MODE").Error
	}
	return nil
}

func (r *Repository) CountAllAdvertisingPlansSince(since time.Time) (int64, error) {
	var count int64
	err := r.db.Model(&model.AdvertisingScriptPlan{}).Where("created_at >= ?", since).Count(&count).Error
	return count, err
}

func (r *Repository) ActiveUserAdvertisingPlans(userID string, since time.Time) (int64, error) {
	var count int64
	err := r.db.Model(&model.AdvertisingScriptPlan{}).Where("user_id = ? AND status = ? AND created_at >= ?", userID, "running", since).Count(&count).Error
	return count, err
}
