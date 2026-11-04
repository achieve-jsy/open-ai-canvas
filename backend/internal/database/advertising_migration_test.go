package database

import (
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
	"yingce/backend/internal/model"
)

func TestAdvertisingMigration(t *testing.T) {
	for _, upgrade := range []bool{false, true} {
		t.Run(fmt.Sprint(upgrade), func(t *testing.T) {
			db, err := Open(Config{Driver: "sqlite", DataDir: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			sqlDB, _ := db.DB()
			t.Cleanup(func() { _ = sqlDB.Close() })
			var before []schemaMigration
			if upgrade {
				if err := db.AutoMigrate(&schemaMigration{}); err != nil {
					t.Fatal(err)
				}
				for _, m := range schemaMigrations {
					if m.version > 78 {
						continue
					}
					if err := m.apply(db); err != nil {
						t.Fatal(err)
					}
					if err := db.Create(&schemaMigration{Version: m.version, Name: m.name, Checksum: m.checksum, AppliedAt: time.Now().UTC()}).Error; err != nil {
						t.Fatal(err)
					}
				}
				// The historical baseline uses today's Models list. Remove only empty new
				// tables in this temporary fixture to reproduce an actual pre-v79 database.
				for _, table := range []string{"advertising_resource_references", "advertising_revisions", "advertising_projects"} {
					if db.Migrator().HasTable(table) {
						if err := db.Migrator().DropTable(table); err != nil {
							t.Fatal(err)
						}
					}
				}
				if err := db.Order("version").Find(&before).Error; err != nil {
					t.Fatal(err)
				}
				if err := db.Create(&model.Project{ID: "old-project", UserID: "owner", Name: "Keep", Revision: 8}).Error; err != nil {
					t.Fatal(err)
				}
			}
			checkAdvertisingMigration(t, db)
			if upgrade {
				var after []schemaMigration
				if err := db.Where("version <= ?", 78).Order("version").Find(&after).Error; err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(before, after) {
					t.Fatal("historical migration records changed")
				}
				var old model.Project
				if err := db.First(&old, "id = ?", "old-project").Error; err != nil || old.Name != "Keep" || old.Revision != 8 {
					t.Fatalf("old project changed: %+v %v", old, err)
				}
			}
		})
	}
}

func checkAdvertisingMigration(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := MigrateSchema(db); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"advertising_projects", "advertising_revisions", "advertising_resource_references"} {
		if !db.Migrator().HasTable(table) {
			t.Fatalf("missing table %s", table)
		}
	}
	if !db.Migrator().HasIndex("advertising_revisions", "idx_advertising_source_revision") || !db.Migrator().HasIndex("advertising_resource_references", "idx_advertising_resource") {
		t.Fatal("missing advertising lookup indexes")
	}
	for _, sql := range []string{
		"INSERT INTO projects (id,user_id,name,type,revision) VALUES ('ad-project','owner','Ad','advertising',1)",
		"INSERT INTO resources (id,user_id) VALUES ('ad-resource','owner')",
		"INSERT INTO advertising_projects (project_id,revision,draft_json) VALUES ('ad-project',1,'{}')",
		"INSERT INTO advertising_revisions (id,project_id,source_revision,snapshot_json) VALUES ('ad-rev','ad-project',1,'{}')",
		"INSERT INTO advertising_resource_references (project_id,scope_key,resource_id) VALUES ('ad-project','ad-rev','ad-resource')",
	} {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	// Savepoint rollback keeps PostgreSQL usable after each expected constraint error.
	for i, sql := range []string{
		"DELETE FROM resources WHERE id = 'ad-resource'",
		"INSERT INTO advertising_revisions (id,project_id,source_revision,snapshot_json) VALUES ('dup','ad-project',1,'{}')",
		"INSERT INTO advertising_resource_references (project_id,scope_key,resource_id) VALUES ('ad-project','ad-rev','ad-resource')",
		"INSERT INTO advertising_resource_references (project_id,scope_key,resource_id) VALUES ('ad-project','draft','missing')",
		"DELETE FROM advertising_projects WHERE project_id = 'ad-project'",
	} {
		if err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Exec(sql).Error; err == nil {
				t.Errorf("constraint %d not enforced", i)
			}
			return fmt.Errorf("rollback expected failure")
		}); err == nil {
			t.Fatal("expected rollback")
		}
	}
	if err := MigrateSchema(db); err != nil {
		t.Fatal("repeat migration", err)
	}
	if err := db.Exec("DELETE FROM advertising_resource_references WHERE resource_id = 'ad-resource'").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("DELETE FROM resources WHERE id = 'ad-resource'").Error; err != nil {
		t.Fatal(err)
	}
	if err := RequireSchemaVersion(db); err != nil {
		t.Fatal(err)
	}
}

func TestAdvertisingPostgresMigration(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("CANVAS_TEST_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("CANVAS_TEST_POSTGRES_DSN is not configured")
	}
	base, err := Open(Config{Driver: "postgres", DSN: dsn})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := base.DB()
	defer sqlDB.Close()
	schema := fmt.Sprintf("advertising_migration_%d", time.Now().UnixNano())
	if err := base.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatal(err)
	}
	defer base.Exec("DROP SCHEMA " + schema + " CASCADE")
	scoped, err := postgresDSNWithSearchPath(dsn, schema)
	if err != nil {
		t.Fatal(err)
	}
	db, err := Open(Config{Driver: "postgres", DSN: scoped})
	if err != nil {
		t.Fatal(err)
	}
	scopedSQL, _ := db.DB()
	defer scopedSQL.Close()
	checkAdvertisingMigration(t, db)
	checkAdvertisingGenerationTables(t, db)
	var width int
	if err := db.Raw("SELECT character_maximum_length FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = ? AND column_name = ?", "advertising_resource_references", "scope_key").Scan(&width).Error; err != nil || width != 64 {
		t.Fatalf("scope key width=%d err=%v", width, err)
	}
}
