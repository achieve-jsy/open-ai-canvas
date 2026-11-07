package database

import (
	"reflect"
	"testing"
	"time"
	"yingce/backend/internal/model"
)

func TestAdvertisingReviewMigration(t *testing.T) {
	for _, upgrade := range []bool{false, true} {
		t.Run(map[bool]string{false: "empty", true: "v82"}[upgrade], func(t *testing.T) {
			db, err := Open(Config{Driver: "sqlite", DataDir: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			sqlDB, _ := db.DB()
			defer sqlDB.Close()
			before := map[string][]map[string]any{}
			if upgrade {
				if err := db.AutoMigrate(&schemaMigration{}); err != nil {
					t.Fatal(err)
				}
				for _, m := range schemaMigrations {
					if m.version > 82 {
						continue
					}
					if err := m.apply(db); err != nil {
						t.Fatal(err)
					}
					if err := db.Create(&schemaMigration{Version: m.version, Name: m.name, Checksum: m.checksum, AppliedAt: time.Now().UTC()}).Error; err != nil {
						t.Fatal(err)
					}
				}
				// Baseline follows current Models: remove only empty v83 tables, then populate the v82 graph.
				for _, table := range []string{"advertising_events", "advertising_reviews", "advertising_issues"} {
					if err := db.Migrator().DropTable(table); err != nil {
						t.Fatal(err)
					}
				}
				taskID, batchID, index := "task", "batch", 1
				for _, row := range []any{
					&model.Project{ID: "ad_old", UserID: "owner", Name: "Old", Type: "advertising"},
					&model.AdvertisingProject{ProjectID: "ad_old", Revision: 2, DraftJSON: "{}"},
					&model.AdvertisingRevision{ID: "revision", ProjectID: "ad_old", SourceRevision: 1, SnapshotJSON: "{}"},
					&model.AdvertisingProduction{ID: "production", ProjectID: "ad_old", ConfirmedRevisionID: "revision", Revision: 1, SettingsJSON: "{}"},
					&model.Resource{ID: "media", UserID: "owner", MimeType: "video/mp4", DurationMs: 5000},
					&model.Task{ID: taskID, UserID: "owner", ProjectID: "ad_old", Status: model.TaskStatusSucceeded, ResultJSON: "{}"},
					&model.AdvertisingCandidateBatch{ID: batchID, ProductionID: "production", ClientOperationID: "batch-op", ShotID: "shot", Kind: "video", Count: 1, RequestHash: "batch-hash"},
					&model.AdvertisingSubmission{ID: "submission", ProductionID: "production", ClientOperationID: "sub-op", TaskID: &taskID, BatchID: &batchID, CandidateIndex: &index, ShotID: "shot", Kind: "video", RequestJSON: "{}", QuoteJSON: "{}"},
					&model.AdvertisingAssembly{ID: "assembly", ProductionID: "production", ClientOperationID: "assembly-op", SourceProductionRevision: 1, SelectionHash: "selection", TimelineJSON: "{}"},
					&model.AdvertisingRenderAttempt{ID: "render", AssemblyID: "assembly", ClientOperationID: "render-op", TaskID: taskID},
					&model.AdvertisingResourceReference{ProjectID: "ad_old", ScopeKey: "candidate:submission", ResourceID: "media"},
				} {
					if err := db.Create(row).Error; err != nil {
						t.Fatal(err)
					}
				}
				for _, table := range []string{"projects", "advertising_projects", "advertising_revisions", "advertising_productions", "advertising_candidate_batches", "advertising_submissions", "advertising_assemblies", "advertising_render_attempts", "advertising_resource_references", "tasks", "resources", "schema_migrations"} {
					var rows []map[string]any
					if err := db.Table(table).Order("1").Find(&rows).Error; err != nil {
						t.Fatal(err)
					}
					before[table] = rows
				}
			}
			for range 2 {
				if err := MigrateSchema(db); err != nil {
					t.Fatal(err)
				}
				if err := RequireSchemaVersion(db); err != nil {
					t.Fatal(err)
				}
				for _, table := range []string{"advertising_events", "advertising_reviews", "advertising_issues"} {
					if !db.Migrator().HasTable(table) {
						t.Fatal("missing", table)
					}
					var n int64
					if err := db.Table(table).Count(&n).Error; err != nil || n != 0 {
						t.Fatal("migration invented human history", table, n, err)
					}
				}
				for table, rows := range before {
					var got []map[string]any
					q := db.Table(table).Order("1")
					if table == "schema_migrations" {
						q = q.Where("version <= ?", 82)
					}
					if err := q.Find(&got).Error; err != nil {
						t.Fatal(err)
					}
					if !reflect.DeepEqual(rows, got) {
						t.Fatalf("changed historical %s", table)
					}
				}
			}
			if !upgrade {
				return
			}
			for _, row := range []any{
				&model.AdvertisingEvent{ID: "event", ProjectID: "ad_old", EventKey: "decision", Kind: "candidate_reviewed", ActorID: "owner", DetailsJSON: "{}"},
				&model.AdvertisingReview{ID: "review", ProductionID: "production", ClientOperationID: "review-op", RequestHash: "hash", Checkpoint: "candidate", SubmissionID: "submission", Decision: "approve", Reason: "checked", ActorID: "owner", ProductionJSON: "{}"},
				&model.AdvertisingIssue{ID: "issue", ProductionID: "production", ClientOperationID: "issue-op", RequestHash: "hash", SubmissionID: "submission", Category: "product", Note: "check", ActorID: "owner", Revision: 1, Status: "open"},
			} {
				if err := db.Create(row).Error; err != nil {
					t.Fatal(err)
				}
			}
			for _, q := range []string{
				"INSERT INTO advertising_events (id,project_id,event_key,kind,actor_id,details_json) VALUES ('duplicate','ad_old','decision','candidate_reviewed','owner','{}')",
				"INSERT INTO advertising_reviews (id,production_id,client_operation_id,request_hash,checkpoint,decision,reason,actor_id,production_json) VALUES ('duplicate','production','review-op','h','candidate','reject','x','owner','{}')",
				"INSERT INTO advertising_issues (id,production_id,client_operation_id,request_hash,category,note,actor_id,revision,status) VALUES ('duplicate','production','issue-op','h','product','x','owner',1,'open')",
				"DELETE FROM advertising_productions WHERE id='production'",
				"DELETE FROM resources WHERE id='media'",
				"DELETE FROM tasks WHERE id='task'",
			} {
				if err := db.Exec(q).Error; err == nil {
					t.Fatal("constraint missing", q)
				}
			}
		})
	}
}
