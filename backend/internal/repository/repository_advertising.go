package repository

import (
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/mattn/go-sqlite3"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"yingce/backend/internal/model"
)

const AdvertisingResourceReferenceKind = "广告项目"

var ErrAdvertisingResourceReferenced = errors.New("资源仍被广告草稿或确认版本引用")

// Advertising IDs reserve ad_ so deleted server projects cannot be mistaken for
// arbitrary local-only canvas scopes. This runs before billing in the admission transaction.
func (r *Repository) requireAdvertisingTaskScope(task *model.Task) error {
	if !strings.HasPrefix(task.ProjectID, "ad_") {
		return nil
	}
	if r.Dialect() == "postgres" {
		// Match the INSERT lock before reading scope; a waiting admission must re-read
		// after deletion commits, rather than validate first and insert a stale scope later.
		if err := r.db.Exec("LOCK TABLE tasks IN ROW EXCLUSIVE MODE").Error; err != nil {
			return err
		}
	} else if r.Dialect() == "sqlite" {
		if err := r.db.Exec("UPDATE tasks SET project_id = project_id WHERE 1 = 0").Error; err != nil {
			return err
		}
	}
	_, err := r.AdvertisingProjectForUser(task.UserID, task.ProjectID)
	return err
}

// GuardAdvertisingDeletion runs inside the advertising transaction before removing references.
func (r *Repository) GuardAdvertisingDeletion(userID, id string) (err error) {
	defer func() {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "55P03" {
			err = ErrProjectHasActiveTasks
		}
	}()
	if r.Dialect() == "postgres" {
		// ponytail: rare deletes briefly exclude all task writes; use per-scope admission
		// locks when deletion throughput matters. NOWAIT avoids deadlocks with nested canvas writes.
		if err := r.db.Exec("LOCK TABLE tasks IN SHARE ROW EXCLUSIVE MODE NOWAIT").Error; err != nil {
			return err
		}
		var project model.Project
		if err := r.db.Where("id = ? AND user_id = ?", id, userID).Clauses(clause.Locking{Strength: "UPDATE", Options: "NOWAIT"}).First(&project).Error; err != nil {
			return err
		}
	}
	var canvases []model.CanvasProject
	query := r.db.Select("id").Where("user_id = ? AND project_id = ?", userID, id).Order("id")
	if r.Dialect() == "postgres" {
		query = query.Clauses(clause.Locking{Strength: "UPDATE", Options: "NOWAIT"})
	}
	if err := query.Find(&canvases).Error; err != nil {
		return err
	}
	scopes := []string{id}
	for _, canvas := range canvases {
		scopes = append(scopes, canvas.ID)
	}
	for _, scope := range scopes {
		activity, err := r.CanvasAgentActivity(userID, scope)
		if err != nil {
			return err
		}
		if activity != nil {
			return ErrProjectHasActiveTasks
		}
	}
	return nil
}

func (r *Repository) DeleteAdvertisingContent(id string) error {
	for _, row := range []any{&model.AdvertisingResourceReference{}, &model.AdvertisingRevision{}, &model.AdvertisingProject{}} {
		if err := r.db.Where("project_id = ?", id).Delete(row).Error; err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) AdvertisingResourceReferences(userID string, ids []string) ([]ResourceDirectReference, error) {
	result := []ResourceDirectReference{}
	// Match the existing reference-query batch ceiling for SQLite parameter limits.
	for start := 0; start < len(ids); start += 400 {
		var rows []struct{ ProjectID, ScopeKey, ResourceID, Name string }
		err := r.db.Table("advertising_resource_references AS refs").Select("refs.project_id, refs.scope_key, refs.resource_id, projects.name").Joins("JOIN projects ON projects.id = refs.project_id").Where("projects.user_id = ? AND refs.resource_id IN ?", userID, ids[start:min(start+400, len(ids))]).Scan(&rows).Error
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			label := row.Name + "（确认版本）"
			if row.ScopeKey == "draft" {
				label = row.Name + "（草稿）"
			}
			result = append(result, ResourceDirectReference{Kind: AdvertisingResourceReferenceKind, ID: row.ProjectID + "/" + row.ScopeKey, Title: label, ResourceID: row.ResourceID})
		}
	}
	return result, nil
}

// Called inside deletion transactions; the RESTRICT FK closes a binding racing this read.
func (r *Repository) RequireNoAdvertisingReferences(ids []string) error {
	// Lock resources before checking: FK insertion takes a conflicting key-share
	// lock on PostgreSQL, so a late binding is observed rather than a raw FK error.
	if len(ids) > 0 && r.Dialect() == "postgres" {
		var rows []model.Resource
		if err := r.db.Select("id").Where("id IN ?", ids).Order("id").Clauses(clause.Locking{Strength: "UPDATE"}).Find(&rows).Error; err != nil {
			return err
		}
	}
	for start := 0; start < len(ids); start += 400 {
		var count int64
		if err := r.db.Model(&model.AdvertisingResourceReference{}).Where("resource_id IN ?", ids[start:min(start+400, len(ids))]).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return ErrAdvertisingResourceReferenced
		}
	}
	return nil
}

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
