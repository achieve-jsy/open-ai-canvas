package main

import (
	"sync"
	"testing"

	"yingce/backend/internal/database"
	"yingce/backend/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func TestMigrationListCoversSchemaModels(t *testing.T) {
	want := make(map[string]bool, len(database.Models()))
	cache := &sync.Map{}
	for _, value := range database.Models() {
		parsed, err := schema.Parse(value, cache, schema.NamingStrategy{})
		if err != nil {
			t.Fatal(err)
		}
		want[parsed.Table] = false
	}
	for _, migration := range migrations() {
		if _, exists := want[migration.name]; !exists {
			t.Fatalf("migration list contains unknown table %q", migration.name)
		}
		if want[migration.name] {
			t.Fatalf("migration list contains duplicate table %q", migration.name)
		}
		want[migration.name] = true
	}
	for table, covered := range want {
		if !covered {
			t.Errorf("migration list is missing table %q", table)
		}
	}
}

func TestAdvertisingSnapshotMigration(t *testing.T) {
	open := func() *gorm.DB {
		db, err := database.Open(database.Config{Driver: "sqlite", DataDir: t.TempDir()})
		if err != nil {
			t.Fatal(err)
		}
		sqlDB, _ := db.DB()
		t.Cleanup(func() { _ = sqlDB.Close() })
		if err := db.AutoMigrate(&model.Project{}, &model.Resource{}, &model.AdvertisingProject{}, &model.AdvertisingRevision{}, &model.AdvertisingResourceReference{}); err != nil {
			t.Fatal(err)
		}
		return db
	}
	source, target := open(), open()
	revID := "snapshot"
	for _, row := range []any{&model.Project{ID: "project", UserID: "owner", Name: "Ad"}, &model.Resource{ID: "resource", UserID: "owner"}, &model.AdvertisingProject{ProjectID: "project", Revision: 2, DraftJSON: "{}", LatestConfirmedRevisionID: &revID}, &model.AdvertisingRevision{ID: revID, ProjectID: "project", SourceRevision: 1, SnapshotJSON: "{\"name\":\"原商品\"}"}, &model.AdvertisingResourceReference{ProjectID: "project", ScopeKey: revID, ResourceID: "resource"}} {
		if err := source.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	wanted := map[string]bool{"projects": true, "resources": true, "advertising_projects": true, "advertising_revisions": true, "advertising_resource_references": true}
	for _, m := range migrations() {
		if !wanted[m.name] {
			continue
		}
		count, err := m.run(source, target, true)
		if err != nil || count != 1 {
			t.Fatalf("copy %s count=%d err=%v", m.name, count, err)
		}
	}
	var got model.AdvertisingRevision
	if err := target.First(&got, "id = ?", revID).Error; err != nil || got.SnapshotJSON != "{\"name\":\"原商品\"}" {
		t.Fatalf("snapshot changed: %+v %v", got, err)
	}
	if err := target.Delete(&model.Resource{}, "id = ?", "resource").Error; err == nil {
		t.Fatal("copied reference did not protect resource")
	}
}

func TestAdvertisingGenerationCopy(t *testing.T) {
	open := func() *gorm.DB {
		db, err := database.Open(database.Config{Driver: "sqlite", DataDir: t.TempDir()})
		if err != nil {
			t.Fatal(err)
		}
		sqlDB, _ := db.DB()
		t.Cleanup(func() { _ = sqlDB.Close() })
		if err := database.MigrateSchema(db); err != nil {
			t.Fatal(err)
		}
		return db
	}
	source, target := open(), open()
	rows := []any{&model.Project{ID: "project", UserID: "owner", Name: "Ad"}, &model.AdvertisingProject{ProjectID: "project", Revision: 2, DraftJSON: "{}"}, &model.AdvertisingRevision{ID: "confirmed", ProjectID: "project", SourceRevision: 1, SnapshotJSON: "{}"}, &model.Task{ID: "task", UserID: "owner"}, &model.Task{ID: "render", UserID: "owner"}, &model.AdvertisingProduction{ID: "production", ProjectID: "project", ConfirmedRevisionID: "confirmed", Revision: 1, SettingsJSON: "{}"}}
	taskID := "task"
	batchID, index := "batch", 1
	rows = append(rows, &model.AdvertisingCandidateBatch{ID: batchID, ProductionID: "production", ClientOperationID: "batch-op", ShotID: "shot", Kind: "video", Count: 1, Adjustment: "frozen", RequestHash: "hash"})
	rows = append(rows, &model.AdvertisingSubmission{ID: "submission", BatchID: &batchID, CandidateIndex: &index, ProductionID: "production", ShotID: "shot", Kind: "video", ClientOperationID: "operation", TaskID: &taskID, RequestJSON: "{}", QuoteJSON: "{}"}, &model.AdvertisingAssembly{ID: "assembly", ProductionID: "production", ClientOperationID: "operation", SourceProductionRevision: 1, TimelineJSON: "{\"version\":2}"}, &model.AdvertisingRenderAttempt{ID: "attempt", AssemblyID: "assembly", ClientOperationID: "operation", TaskID: "render"})
	rows = append(rows,
		&model.AdvertisingEvent{ID: "event", ProjectID: "project", ProductionID: "production", EventKey: "review:review", Kind: "assembly_reviewed", ActorID: "owner", DetailsJSON: "{}"},
		&model.AdvertisingReview{ID: "review", ProductionID: "production", ClientOperationID: "review-op", RequestHash: "request-hash", Checkpoint: "assembly", AssemblyID: "assembly", RenderAttemptID: "attempt", ResourceID: "server-output", ActorID: "owner", Decision: "approve", Reason: "watched", ProductionJSON: "{}"},
		&model.AdvertisingIssue{ID: "issue", ProductionID: "production", ClientOperationID: "issue-op", RequestHash: "issue-hash", SubmissionID: "submission", ActorID: "owner", Category: "product", Note: "check logo", Revision: 2, Status: "resolved", ResolutionNote: "watched again", ResolvedBy: "owner"},
	)
	for _, row := range rows {
		if err := source.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	wanted := map[string]bool{"projects": true, "advertising_projects": true, "advertising_revisions": true, "tasks": true, "advertising_productions": true, "advertising_candidate_batches": true, "advertising_submissions": true, "advertising_assemblies": true, "advertising_render_attempts": true, "advertising_events": true, "advertising_reviews": true, "advertising_issues": true}
	for _, m := range migrations() {
		if !wanted[m.name] {
			continue
		}
		n, err := m.run(source, target, true)
		if err != nil || n < 1 {
			t.Fatalf("copy %s: %d %v", m.name, n, err)
		}
	}
	var review model.AdvertisingReview
	if err := target.First(&review, "id = ?", "review").Error; err != nil || review.RenderAttemptID != "attempt" || review.ResourceID != "server-output" || review.ProductionJSON != "{}" {
		t.Fatalf("copied review identity: %+v %v", review, err)
	}
	var issue model.AdvertisingIssue
	if err := target.First(&issue, "id = ?", "issue").Error; err != nil || issue.Revision != 2 || issue.Status != "resolved" || issue.ResolvedBy != "owner" {
		t.Fatalf("copied resolution: %+v %v", issue, err)
	}
	var member model.AdvertisingSubmission
	if err := target.First(&member, "id = ?", "submission").Error; err != nil || member.BatchID == nil || *member.BatchID != batchID || member.CandidateIndex == nil || *member.CandidateIndex != 1 {
		t.Fatalf("copied batch identity: %+v %v", member, err)
	}
	if err := target.Delete(&model.AdvertisingCandidateBatch{}, "id = ?", batchID).Error; err == nil {
		t.Fatal("copied member failed to protect batch")
	}
	var got model.AdvertisingAssembly
	if err := target.First(&got, "id = ?", "assembly").Error; err != nil || got.TimelineJSON != "{\"version\":2}" {
		t.Fatalf("copied snapshot %+v %v", got, err)
	}
	if err := target.Delete(&model.Task{}, "id = ?", "task").Error; err == nil {
		t.Fatal("copied submission failed to protect task")
	}
}

func TestCanvasHistoryCompositeKeyMigration(t *testing.T) {
	source, err := database.Open(database.Config{Driver: "sqlite", DSN: "file:history-migration-source?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	target, err := database.Open(database.Config{Driver: "sqlite", DSN: "file:history-migration-target?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	for _, db := range []*gorm.DB{source, target} {
		if err := db.AutoMigrate(&model.Resource{}, &model.CanvasSnapshotResource{}); err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&model.Resource{ID: "resource"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	refs := []model.CanvasSnapshotResource{{SnapshotID: "b", ResourceID: "resource"}, {SnapshotID: "a", ResourceID: "resource"}}
	if err := source.Create(&refs).Error; err != nil {
		t.Fatal(err)
	}
	count, err := migrateTable[model.CanvasSnapshotResource]("canvas_snapshot_resources").run(source, target, true)
	if err != nil || count != 2 {
		t.Fatalf("history refs not copied/verified: %d %v", count, err)
	}
}
