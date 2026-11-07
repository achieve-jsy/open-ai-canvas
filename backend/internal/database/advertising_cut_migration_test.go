package database

import (
	"testing"
	"yingce/backend/internal/model"
)

func TestAdvertisingCutMigration(t *testing.T) {
	if CurrentSchemaVersion != 84 {
		t.Fatalf("need native migration84, got %d", CurrentSchemaVersion)
	}
	db, err := Open(Config{Driver: "sqlite", DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	sql, _ := db.DB()
	defer sql.Close()
	if err = MigrateSchema(db); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"ProvenanceJSON", "SourceFingerprint"} {
		if !db.Migrator().HasColumn(&model.AdvertisingAssembly{}, field) {
			t.Fatal(field)
		}
	}
	if !db.Migrator().HasColumn(&model.AdvertisingReview{}, "AcceptedReviewID") {
		t.Fatal("accepted review identity missing")
	}
	if err = MigrateSchema(db); err != nil {
		t.Fatal(err)
	}
	// Simulate the populated v83 tables, then invoke only the appended migration.
	if err = db.Exec("PRAGMA foreign_keys = OFF").Error; err != nil {
		t.Fatal(err)
	}
	if err = db.Exec("INSERT INTO advertising_assemblies (id,production_id,client_operation_id,source_production_revision,selection_hash,timeline_json) VALUES ('old','p','op',1,'legacy','{}')").Error; err != nil {
		t.Fatal(err)
	}
	if err = db.Exec("INSERT INTO advertising_reviews (id,production_id,client_operation_id,request_hash,checkpoint,decision,reason,actor_id,production_json) VALUES ('old-review','p','review','h','assembly','approve','old','user','{}')").Error; err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"ProvenanceJSON", "SourceFingerprint"} {
		if err = db.Migrator().DropColumn(&model.AdvertisingAssembly{}, field); err != nil {
			t.Fatal(err)
		}
	}
	if err = db.Migrator().DropColumn(&model.AdvertisingReview{}, "AcceptedReviewID"); err != nil {
		t.Fatal(err)
	}
	if err = schemaMigrations[len(schemaMigrations)-1].apply(db); err != nil {
		t.Fatal(err)
	}
	var old model.AdvertisingAssembly
	if err = db.First(&old, "id = ?", "old").Error; err != nil {
		t.Fatal(err)
	}
	if old.SelectionHash != "legacy" || old.TimelineJSON != "{}" || old.ProvenanceJSON != nil || old.SourceFingerprint != nil {
		t.Fatal("fabricated or mutated old provenance")
	}
	var review model.AdvertisingReview
	if err = db.First(&review, "id = ?", "old-review").Error; err != nil {
		t.Fatal(err)
	}
	if review.AcceptedReviewID != nil {
		t.Fatal("fabricated old delivery identity")
	}
}
