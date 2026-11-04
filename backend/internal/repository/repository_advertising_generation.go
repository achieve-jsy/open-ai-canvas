package repository

import (
	"errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
	"yingce/backend/internal/model"
)

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
