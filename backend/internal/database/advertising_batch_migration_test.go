package database

import (
	"reflect"
	"testing"
	"time"

	"yingce/backend/internal/model"
)

func TestAdvertisingBatchMigration(t *testing.T) {
	for _, upgrade := range []bool{false, true} {
		t.Run(map[bool]string{false: "empty", true: "v81"}[upgrade], func(t *testing.T) {
			db, err := Open(Config{Driver: "sqlite", DataDir: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			sqlDB, _ := db.DB()
			defer sqlDB.Close()
			historical := map[string][]map[string]any{}
			if upgrade {
				if err = db.AutoMigrate(&schemaMigration{}); err != nil {
					t.Fatal(err)
				}
				for _, m := range schemaMigrations {
					if m.version > 81 {
						continue
					}
					if err = m.apply(db); err != nil {
						t.Fatal(err)
					}
					if err = db.Create(&schemaMigration{Version: m.version, Name: m.name, Checksum: m.checksum, AppliedAt: time.Now()}).Error; err != nil {
						t.Fatal(err)
					}
				}
				// V1 follows current models: remove only empty new schema to simulate v81.
				if db.Migrator().HasConstraint(&model.AdvertisingSubmission{}, "Batch") {
					if err = db.Migrator().DropConstraint(&model.AdvertisingSubmission{}, "Batch"); err != nil {
						t.Fatal(err)
					}
				}
				for _, col := range []string{"batch_id", "candidate_index"} {
					if db.Migrator().HasColumn("advertising_submissions", col) {
						if err = db.Migrator().DropColumn(&model.AdvertisingSubmission{}, col); err != nil {
							t.Fatal(err)
						}
					}
				}
				if db.Migrator().HasTable("advertising_candidate_batches") {
					if err = db.Migrator().DropTable("advertising_candidate_batches"); err != nil {
						t.Fatal(err)
					}
				}
				for _, col := range []string{"batch_id", "candidate_index"} {
					if db.Migrator().HasColumn("advertising_submissions", col) {
						t.Fatal("pre82 contains", col)
					}
				}
				if db.Migrator().HasTable("advertising_candidate_batches") || db.Migrator().HasConstraint(&model.AdvertisingSubmission{}, "Batch") || db.Migrator().HasIndex("advertising_submissions", "idx_ad_batch_candidate") {
					t.Fatal("pre82 retains batch schema")
				}
				// Insert only pre82 columns: current Submission model must not silently restore new fields.
				for _, q := range []string{
					`INSERT INTO projects (id,user_id,name,type) VALUES ('ad_test','owner','Ad','advertising')`,
					`INSERT INTO advertising_projects (project_id,revision,draft_json) VALUES ('ad_test',1,'{}')`,
					`INSERT INTO advertising_revisions (id,project_id,source_revision,snapshot_json) VALUES ('rev','ad_test',1,'{}')`,
					`INSERT INTO advertising_productions (id,project_id,confirmed_revision_id,revision,settings_json) VALUES ('p','ad_test','rev',1,'{"selections":{"shot":{"videoSubmissionId":"old"}}}')`,
					`INSERT INTO resources (id,user_id,kind,status,object_key,mime_type,duration_ms,size) VALUES ('candidate-media','owner','video','ready','old/candidate.mp4','video/mp4',5000,1024),('render-media','owner','video','ready','old/render.mp4','video/mp4',5000,2048)`,
					`INSERT INTO tasks (id,user_id,project_id,type,status,result_json) VALUES ('old-task','owner','ad_test','canvas_video','succeeded','{"storageKey":"resource:candidate-media"}'),('render-task','owner','ad_test','timeline_render','succeeded','{"resourceId":"render-media","storageKey":"resource:render-media"}')`,
					`INSERT INTO advertising_submissions (id,production_id,shot_id,kind,client_operation_id,request_hash,request_json,quote_json,price_signature,task_id,approved_at) VALUES ('old','p','shot','video','old-op','hash','{"prompt":"historical"}','{"quoteHash":"old-quote"}','sig','old-task','2026-11-01 00:00:00+00:00')`,
					`INSERT INTO canvas_projects (id,user_id,project_id,title,payload_json,revision) VALUES ('canvas','owner','ad_test','Historical assembly','{"timeline":{"clips":[{"kind":"video","directMedia":{"storageKey":"resource:candidate-media"}}]}}',1)`,
					`INSERT INTO advertising_assemblies (id,production_id,client_operation_id,source_production_revision,selection_hash,timeline_json,canvas_id) VALUES ('assembly','p','assembly-op',1,'selection-hash','{"clips":[{"kind":"video","directMedia":{"storageKey":"resource:candidate-media"}}]}','canvas')`,
					`INSERT INTO advertising_render_attempts (id,assembly_id,client_operation_id,task_id) VALUES ('render','assembly','render-op','render-task')`,
					`INSERT INTO advertising_resource_references (project_id,scope_key,resource_id) VALUES ('ad_test','candidate:old','candidate-media'),('ad_test','assembly:assembly','candidate-media'),('ad_test','render:render','render-media')`,
				} {
					if err = db.Exec(q).Error; err != nil {
						t.Fatal(err)
					}
				}
				for _, table := range []string{"projects", "advertising_projects", "advertising_revisions", "advertising_productions", "advertising_submissions", "tasks", "resources", "advertising_resource_references", "advertising_assemblies", "advertising_render_attempts", "canvas_projects", "schema_migrations"} {
					var rows []map[string]any
					if err = db.Table(table).Order("1, 2, 3").Find(&rows).Error; err != nil {
						t.Fatal(err)
					}
					historical[table] = rows
				}
			}
			for range 2 {
				if err = MigrateSchema(db); err != nil {
					t.Fatal(err)
				}
				if err = RequireSchemaVersion(db); err != nil {
					t.Fatal(err)
				}
				if !db.Migrator().HasTable("advertising_candidate_batches") {
					t.Fatal("missing batch table")
				}
				for _, col := range []string{"batch_id", "candidate_index"} {
					if !db.Migrator().HasColumn("advertising_submissions", col) {
						t.Fatal("missing column", col)
					}
				}
				for table, before := range historical {
					var after []map[string]any
					query := db.Table(table).Order("1, 2, 3")
					if table == "schema_migrations" {
						query = query.Where("version <= ?", 81)
					}
					if err = query.Find(&after).Error; err != nil {
						t.Fatal(err)
					}
					if table == "advertising_submissions" {
						for _, row := range after {
							if row["batch_id"] != nil || row["candidate_index"] != nil {
								t.Fatal("legacy candidate acquired batch membership")
							}
							delete(row, "batch_id")
							delete(row, "candidate_index")
						}
					}
					if !reflect.DeepEqual(before, after) {
						t.Fatalf("historical %s changed: before=%+v after=%+v", table, before, after)
					}
				}
			}
			if !upgrade {
				return
			}
			// Exercise constraints on the upgraded old schema, not an AutoMigrate-only fixture.
			for _, q := range []string{
				"DELETE FROM tasks WHERE id = 'old-task'",
				"DELETE FROM tasks WHERE id = 'render-task'",
				"DELETE FROM resources WHERE id = 'candidate-media'",
				"DELETE FROM resources WHERE id = 'render-media'",
				"DELETE FROM advertising_assemblies WHERE id = 'assembly'",
			} {
				if err = db.Exec(q).Error; err == nil {
					t.Fatal("lost historical foreign key:", q)
				}
			}
			batch := model.AdvertisingCandidateBatch{ID: "new-batch", ProductionID: "p", ClientOperationID: "new-op", ShotID: "shot", Kind: "video", Count: 2, RequestHash: "new-hash"}
			for _, count := range []int{0, 4} {
				bad := batch
				bad.Count = count
				if err = db.Create(&bad).Error; err == nil {
					t.Fatal("batch count constraint missing", count)
				}
			}
			if err = db.Create(&batch).Error; err != nil {
				t.Fatal(err)
			}
			index := 1
			member := model.AdvertisingSubmission{ID: "new-member", ProductionID: "p", ShotID: "shot", Kind: "video", ClientOperationID: "member-op", RequestHash: "hash", RequestJSON: "{}", QuoteJSON: "{}", PriceSignature: "sig", BatchID: &batch.ID, CandidateIndex: &index}
			if err = db.Create(&member).Error; err != nil {
				t.Fatal(err)
			}
			member.ID, member.ClientOperationID = "duplicate-position", "other-op"
			if err = db.Create(&member).Error; err == nil {
				t.Fatal("duplicate member position accepted")
			}
			if err = db.Delete(&batch).Error; err == nil {
				t.Fatal("referenced batch deleted")
			}
		})
	}
}
