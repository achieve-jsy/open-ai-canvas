package repository

import (
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/mattn/go-sqlite3"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"yingce/backend/internal/model"
)

var ErrAdvertisingRevisionConflict = errors.New("广告草稿已更新，请重新读取")

// Acquire the write lock before any reads on SQLite to avoid read-to-write upgrades.
func (r *Repository) AdvertisingTransaction(userID, projectID string, fn func(*Repository) error) error {
	for attempt := 0; attempt < 3; attempt++ {
		err := r.Transaction(func(repo *Repository) error {
			owned := repo.db.Model(&model.Project{}).Select("id").Where("id = ? AND user_id = ? AND type = ?", projectID, userID, "advertising")
			q := repo.db.Model(&model.AdvertisingProject{}).Where("project_id IN (?)", owned)
			if repo.db.Dialector.Name() == "sqlite" {
				result := q.UpdateColumn("revision", gorm.Expr("revision"))
				if result.Error != nil {
					return result.Error
				}
				if result.RowsAffected != 1 {
					return gorm.ErrRecordNotFound
				}
			} else {
				var row model.AdvertisingProject
				if err := q.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row).Error; err != nil {
					return err
				}
			}
			return fn(repo)
		})
		var busy sqlite3.Error
		if errors.As(err, &busy) && (busy.Code == sqlite3.ErrBusy || busy.Code == sqlite3.ErrLocked) && attempt < 2 {
			continue
		}
		return err
	}
	return ErrAdvertisingRevisionConflict
}
func (r *Repository) AdvertisingProjectForUser(userID, projectID string) (*model.AdvertisingProject, error) {
	var row model.AdvertisingProject
	err := r.db.Joins("JOIN projects ON projects.id = advertising_projects.project_id").Where("projects.user_id = ? AND projects.type = ? AND advertising_projects.project_id = ?", userID, "advertising", projectID).First(&row).Error
	return &row, err
}
func (r *Repository) CreateAdvertisingProject(row *model.AdvertisingProject) error {
	return r.db.Omit(clause.Associations).Create(row).Error
}
func (r *Repository) CASAdvertisingProject(projectID string, expected int64, updates map[string]any) error {
	result := r.db.Model(&model.AdvertisingProject{}).Where("project_id = ? AND revision = ?", projectID, expected).Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrAdvertisingRevisionConflict
	}
	return nil
}
func (r *Repository) RenameAdvertisingProject(userID, id, name string, at time.Time) error {
	err := r.db.Model(&model.Project{}).Where("id = ? AND user_id = ? AND type = ?", id, userID, "advertising").Updates(map[string]any{"name": name, "updated_at": at, "revision": gorm.Expr("revision + 1")}).Error
	var sqliteErr sqlite3.Error
	var pgErr *pgconn.PgError
	if (errors.As(err, &sqliteErr) && sqliteErr.ExtendedCode == sqlite3.ErrConstraintUnique) || (errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "idx_projects_user_name") {
		return ErrProjectNameConflict
	}
	return err
}
func (r *Repository) ReplaceAdvertisingReferences(projectID, scopeKey string, ids []string) error {
	if err := r.db.Where("project_id = ? AND scope_key = ?", projectID, scopeKey).Delete(&model.AdvertisingResourceReference{}).Error; err != nil {
		return err
	}
	for _, id := range ids {
		if err := r.db.Omit(clause.Associations).Create(&model.AdvertisingResourceReference{ProjectID: projectID, ScopeKey: scopeKey, ResourceID: id}).Error; err != nil {
			return err
		}
	}
	return nil
}
func (r *Repository) CreateAdvertisingRevision(row *model.AdvertisingRevision) error {
	return r.db.Omit(clause.Associations).Create(row).Error
}
func (r *Repository) AdvertisingRevisionForSource(projectID string, source int64) (*model.AdvertisingRevision, error) {
	var row model.AdvertisingRevision
	err := r.db.Where("project_id = ? AND source_revision = ?", projectID, source).First(&row).Error
	return &row, err
}
func (r *Repository) AdvertisingRevisionForUser(userID, projectID, id string) (*model.AdvertisingRevision, error) {
	var row model.AdvertisingRevision
	err := r.db.Joins("JOIN projects ON projects.id = advertising_revisions.project_id").Where("projects.user_id = ? AND projects.type = ? AND advertising_revisions.project_id = ? AND advertising_revisions.id = ?", userID, "advertising", projectID, id).First(&row).Error
	return &row, err
}

type AdvertisingProjectSummary struct {
	ID                        string    `json:"id"`
	Name                      string    `json:"name"`
	Revision                  int64     `json:"revision"`
	LatestConfirmedRevisionID *string   `json:"latestConfirmedRevisionId,omitempty"`
	UpdatedAt                 time.Time `json:"updatedAt"`
	DraftJSON                 string    `json:"-"`
}

func (r *Repository) AdvertisingProjects(userID string, page, size int) ([]AdvertisingProjectSummary, int64, error) {
	q := r.db.Table("advertising_projects").Joins("JOIN projects ON projects.id = advertising_projects.project_id").Where("projects.user_id = ? AND projects.type = ?", userID, "advertising")
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	rows := []AdvertisingProjectSummary{}
	err := q.Select("projects.id, projects.name, advertising_projects.revision, advertising_projects.latest_confirmed_revision_id, advertising_projects.updated_at, advertising_projects.draft_json").Order("advertising_projects.updated_at DESC, advertising_projects.project_id DESC").Offset((page - 1) * size).Limit(size).Scan(&rows).Error
	return rows, total, err
}
func (r *Repository) AdvertisingRevisions(projectID string, page, size int) ([]model.AdvertisingRevision, int64, error) {
	q := r.db.Model(&model.AdvertisingRevision{}).Where("project_id = ?", projectID)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	rows := []model.AdvertisingRevision{}
	err := q.Select("id, project_id, source_revision, created_at").Order("source_revision DESC").Offset((page - 1) * size).Limit(size).Find(&rows).Error
	return rows, total, err
}
