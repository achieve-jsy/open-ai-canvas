package app

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"yingce/backend/internal/database"
	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
)

func TestAdvertisingBatchPostgres(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("CANVAS_TEST_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("CANVAS_TEST_POSTGRES_DSN is not configured")
	}
	admin, err := database.Open(database.Config{Driver: "postgres", DSN: dsn})
	if err != nil {
		t.Fatal(err)
	}
	adminSQL, _ := admin.DB()
	defer adminSQL.Close()
	schema := fmt.Sprintf("advertising_batch_%d", time.Now().UnixNano())
	if err = admin.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := admin.Exec("DROP SCHEMA " + schema + " CASCADE").Error; err != nil {
			t.Error(err)
		}
	}()
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.RuntimeParams["search_path"] = schema
	scoped := stdlib.RegisterConnConfig(config)
	defer stdlib.UnregisterConnConfig(scoped)
	db, err := gorm.Open(postgres.New(postgres.Config{DriverName: "pgx", DSN: scoped}), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()
	if err = database.MigrateSchema(db); err != nil {
		t.Fatal(err)
	}
	if err = database.MigrateSchema(db); err != nil {
		t.Fatal(err)
	}
	// Reuse the real managed-task fixture's deterministic catalog, not a provider call.
	_, source, project, production, req := advertisingBatchFixture(t)
	for _, rows := range []any{&[]model.User{}, &[]model.ModelChannel{}, &[]model.ChannelModel{}, &[]model.ChannelModelPriceTier{}, &[]model.CreditAccount{}, &[]model.Resource{}, &[]model.Project{}, &[]model.AdvertisingProject{}, &[]model.AdvertisingRevision{}, &[]model.AdvertisingProduction{}} {
		if err = source.Find(rows).Error; err != nil {
			t.Fatal(err)
		}
		if err = db.Omit("LatestConfirmedRevision").Create(rows).Error; err != nil {
			t.Fatal(err)
		}
	}
	s := &Service{repo: repository.New(db), dataDir: t.TempDir()}
	checkAdvertisingBatchTwoServices(t, s, db, project, production, req)
}
