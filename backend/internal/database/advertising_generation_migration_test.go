package database

import (
	"gorm.io/gorm"
	"strings"
	"testing"
	"time"
)

func TestAdvertisingGenerationMigration(t *testing.T) {
	db, err := Open(Config{Driver: "sqlite", DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()
	if err := db.AutoMigrate(&schemaMigration{}); err != nil {
		t.Fatal(err)
	}
	for _, m := range schemaMigrations {
		if m.version > 79 {
			continue
		}
		if err := m.apply(db); err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&schemaMigration{Version: m.version, Name: m.name, Checksum: m.checksum, AppliedAt: time.Now().UTC()}).Error; err != nil {
			t.Fatal(err)
		}
	}
	// V1 uses today's model list; drop only empty C tables to simulate an actual v79 upgrade.
	for _, name := range []string{"advertising_render_attempts", "advertising_assemblies", "advertising_submissions", "advertising_productions"} {
		if db.Migrator().HasTable(name) {
			if err := db.Migrator().DropTable(name); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, q := range []string{
		"INSERT INTO projects (id,user_id,name,type,revision) VALUES ('ad-project','owner','Ad','advertising',1)",
		"INSERT INTO resources (id,user_id) VALUES ('resource','owner')",
		"INSERT INTO advertising_projects (project_id,revision,draft_json) VALUES ('ad-project',1,'{}')",
		"INSERT INTO advertising_revisions (id,project_id,source_revision,snapshot_json) VALUES ('confirmed','ad-project',1,'{}')",
		"INSERT INTO advertising_resource_references (project_id,scope_key,resource_id) VALUES ('ad-project','draft','resource')",
	} {
		if err := db.Exec(q).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := MigrateSchema(db); err != nil {
		t.Fatal(err)
	}
	checkAdvertisingGenerationTables(t, db)
	var n int64
	if err := db.Table("advertising_resource_references").Where("scope_key = ?", "draft").Count(&n).Error; err != nil || n != 1 {
		t.Fatalf("old reference lost: %d %v", n, err)
	}
	if err := db.Exec("INSERT INTO advertising_resource_references (project_id,scope_key,resource_id) VALUES (?,?,?)", "ad-project", "production:"+strings.Repeat("a", 32), "resource").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO advertising_productions (id,project_id,confirmed_revision_id,revision,settings_json) VALUES ('production','ad-project','confirmed',1,'{}')").Error; err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		"INSERT INTO advertising_productions (id,project_id,confirmed_revision_id,revision,settings_json) VALUES ('duplicate','ad-project','confirmed',1,'{}')",
		"DELETE FROM advertising_revisions WHERE id='confirmed'",
		"INSERT INTO advertising_render_attempts (id,assembly_id,client_operation_id,task_id) VALUES ('attempt','absent','operation','absent')",
	} {
		if err := db.Exec(q).Error; err == nil {
			t.Fatalf("constraint absent: %s", q)
		}
	}
	if err := MigrateSchema(db); err != nil {
		t.Fatal(err)
	}
	if err := RequireSchemaVersion(db); err != nil {
		t.Fatal(err)
	}
}
func checkAdvertisingGenerationTables(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, table := range []string{"advertising_productions", "advertising_submissions", "advertising_assemblies", "advertising_render_attempts"} {
		if !db.Migrator().HasTable(table) {
			t.Fatalf("missing C table: %s", table)
		}
	}
}
