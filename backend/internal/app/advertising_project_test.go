package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"gorm.io/driver/postgres"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"
	"yingce/backend/internal/database"
	"yingce/backend/internal/kernel"
	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
)

func advertisingTestService(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	dir := t.TempDir()
	db, err := database.Open(database.Config{Driver: "sqlite", DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := database.MigrateSchema(db); err != nil {
		t.Fatal(err)
	}
	return &Service{repo: repository.New(db), dataDir: dir}, db
}
func advertisingTestDraft() AdvertisingDraft {
	return AdvertisingDraft{SchemaVersion: 1, Brief: AdvertisingBrief{ProductName: "商品", Audience: "通勤用户", SellingPoints: []string{"轻巧"}, CTA: "立即选购", DurationMs: 15000}, ProductResourceIDs: []string{"resource"}, Shots: []AdvertisingShot{
		{ID: "10000000-0000-4000-8000-000000000001", DurationMs: 5000, Visual: "商品全景", ReferenceResourceIDs: []string{"resource"}},
		{ID: "10000000-0000-4000-8000-000000000002", DurationMs: 5000, Visual: "细节"},
		{ID: "10000000-0000-4000-8000-000000000003", DurationMs: 5000, Visual: "结束"}}}
}
func TestAdvertisingProjectPostgres(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("CANVAS_TEST_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("CANVAS_TEST_POSTGRES_DSN is not configured")
	}
	for _, check := range []struct {
		name string
		run  func(*testing.T, *Service, *gorm.DB)
	}{{"lifecycle", checkAdvertisingProjectLifecycle}, {"concurrent", checkAdvertisingProjectConcurrentSave}, {"delete-admission-lock", checkAdvertisingDeleteAdmissionLock}, {"bind-delete-race", checkAdvertisingBindDeleteRace}} {
		t.Run(check.name, func(t *testing.T) {
			admin, err := database.Open(database.Config{Driver: "postgres", DSN: dsn})
			if err != nil {
				t.Fatal(err)
			}
			adminSQL, _ := admin.DB()
			t.Cleanup(func() { _ = adminSQL.Close() })
			schema := fmt.Sprintf("advertising_project_%d", time.Now().UnixNano())
			if err := admin.Exec("CREATE SCHEMA " + schema).Error; err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := admin.Exec("DROP SCHEMA " + schema + " CASCADE").Error; err != nil {
					t.Error(err)
				}
			})
			config, err := pgx.ParseConfig(dsn)
			if err != nil {
				t.Fatal(err)
			}
			config.RuntimeParams["search_path"] = schema
			// A registered connection config supports both URL and keyword PostgreSQL DSNs.
			scopedDSN := stdlib.RegisterConnConfig(config)
			t.Cleanup(func() { stdlib.UnregisterConnConfig(scopedDSN) })
			db, err := gorm.Open(postgres.New(postgres.Config{DriverName: "pgx", DSN: scopedDSN}), &gorm.Config{})
			if err != nil {
				t.Fatal(err)
			}
			sqlDB, _ := db.DB()
			t.Cleanup(func() { _ = sqlDB.Close() })
			if err := database.MigrateSchema(db); err != nil {
				t.Fatal(err)
			}
			check.run(t, &Service{repo: repository.New(db), dataDir: t.TempDir()}, db)
		})
	}
}

func TestAdvertisingDraftValidation(t *testing.T) {
	if err := validateAdvertisingDraft(advertisingTestDraft(), true); err != nil {
		t.Fatal(err)
	}
	empty := normalizeAdvertisingDraft(AdvertisingDraft{SchemaVersion: 1, Brief: AdvertisingBrief{DurationMs: 15000}})
	if err := validateAdvertisingDraft(empty, false); err != nil {
		t.Fatal(err)
	}
	if err := validateAdvertisingDraft(empty, true); err == nil {
		t.Fatal("empty draft confirmed")
	}
	cases := map[string]func(*AdvertisingDraft){
		"uuid case duplicate": func(d *AdvertisingDraft) {
			d.Shots[0].ID = "aaaaaaaa-0000-4000-8000-000000000001"
			d.Shots[1].ID = strings.ToUpper(d.Shots[0].ID)
		},
		"schema":        func(d *AdvertisingDraft) { d.SchemaVersion = 2 },
		"unicode":       func(d *AdvertisingDraft) { d.Brief.ProductName = strings.Repeat("字", 121) },
		"duration":      func(d *AdvertisingDraft) { d.Brief.DurationMs = 30001 },
		"sum":           func(d *AdvertisingDraft) { d.Shots[0].DurationMs = 4000 },
		"shot duration": func(d *AdvertisingDraft) { d.Shots[0].DurationMs = 999 },
		"duplicate":     func(d *AdvertisingDraft) { d.Shots[1].ID = d.Shots[0].ID },
		"id":            func(d *AdvertisingDraft) { d.Shots[0].ID = "bad" },
		"reference":     func(d *AdvertisingDraft) { d.Shots[0].ReferenceResourceIDs = []string{"missing"} },
		"url":           func(d *AdvertisingDraft) { d.ProductResourceIDs = []string{"https://example.com/file"} },
		"lines":         func(d *AdvertisingDraft) { d.Shots[0].OnScreenText = "a\nb\nc" },
		"color":         func(d *AdvertisingDraft) { d.Brand.PrimaryColor = "red" },
		"audience":      func(d *AdvertisingDraft) { d.Brief.Audience = " " },
		"points":        func(d *AdvertisingDraft) { d.Brief.SellingPoints = []string{} },
		"shots":         func(d *AdvertisingDraft) { d.Shots = d.Shots[:2] },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			d := advertisingTestDraft()
			mutate(&d)
			if err := validateAdvertisingDraft(d, true); err == nil {
				t.Fatal("invalid draft accepted")
			}
		})
	}

	for _, field := range []struct {
		name  string
		limit int
		set   func(*AdvertisingDraft, string)
	}{
		{"description", 2000, func(d *AdvertisingDraft, v string) { d.Brief.Description = v }},
		{"productName", 120, func(d *AdvertisingDraft, v string) { d.Brief.ProductName = v }},
		{"audience", 300, func(d *AdvertisingDraft, v string) { d.Brief.Audience = v }},
		{"cta", 80, func(d *AdvertisingDraft, v string) { d.Brief.CTA = v }},
		{"brand", 120, func(d *AdvertisingDraft, v string) { d.Brand.Name = v }},
		{"sellingPoint", 120, func(d *AdvertisingDraft, v string) { d.Brief.SellingPoints = []string{v} }},
		{"visual", 1000, func(d *AdvertisingDraft, v string) { d.Shots[0].Visual = v }},
		{"voiceover", 500, func(d *AdvertisingDraft, v string) { d.Shots[0].Voiceover = v }},
		{"text", 200, func(d *AdvertisingDraft, v string) { d.Shots[0].OnScreenText = v }},
	} {
		t.Run(field.name+" boundary", func(t *testing.T) {
			d := advertisingTestDraft()
			field.set(&d, strings.Repeat("字", field.limit))
			if err := validateAdvertisingDraft(d, true); err != nil {
				t.Fatal(err)
			}
			field.set(&d, strings.Repeat("字", field.limit+1))
			if err := validateAdvertisingDraft(d, false); err == nil {
				t.Fatal("overlong draft accepted")
			}
		})
	}
	for _, duration := range []int64{14999, 30001} {
		d := advertisingTestDraft()
		d.Brief.DurationMs = duration
		if err := validateAdvertisingDraft(d, false); err == nil {
			t.Fatal("invalid target duration")
		}
	}
	for _, duration := range []int64{15000, 30000} {
		d := advertisingTestDraft()
		d.Brief.DurationMs = duration
		if err := validateAdvertisingDraft(d, false); err != nil {
			t.Fatal(err)
		}
	}
	for _, duration := range []int64{999, 15001} {
		d := advertisingTestDraft()
		d.Shots[0].DurationMs = duration
		if err := validateAdvertisingDraft(d, false); err == nil {
			t.Fatal("invalid shot duration")
		}
	}
	for _, duration := range []int64{1000, 15000} {
		d := advertisingTestDraft()
		d.Shots[0].DurationMs = duration
		if err := validateAdvertisingDraft(d, false); err != nil {
			t.Fatal(err)
		}
	}
	d := advertisingTestDraft()
	d.Brief.ProductName = strings.Repeat("字", 120)
	if err := validateAdvertisingDraft(d, true); err != nil {
		t.Fatal(err)
	}
}
func TestAdvertisingProjectLifecycle(t *testing.T) {
	svc, db := advertisingTestService(t)
	checkAdvertisingProjectLifecycle(t, svc, db)
}

func checkAdvertisingProjectLifecycle(t *testing.T, svc *Service, db *gorm.DB) {
	t.Helper()
	seedResource(t, db, "resource", "owner", "image/png")
	d := advertisingTestDraft()
	created, err := svc.CreateAdvertisingProject("owner", AdvertisingCreateRequest{Name: "Ad", Draft: &d})
	if err != nil {
		t.Fatal(err)
	}
	id := created.Project.ID
	for _, table := range []string{"workflow_instances", "project_units"} {
		var count int64
		if err := db.Table(table).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatal("created drama rows", table)
		}
	}
	if _, err := svc.GetAdvertisingProject("other", id); err == nil {
		t.Fatal("cross-user read")
	}
	confirmed, err := svc.ConfirmAdvertisingProject("owner", id, 1)
	if err != nil {
		t.Fatal(err)
	}
	if confirmed.CurrentRevision != 2 || confirmed.Snapshot.SourceRevision != 1 {
		t.Fatalf("confirmation: %+v", confirmed)
	}
	again, err := svc.ConfirmAdvertisingProject("owner", id, 2)
	if err != nil || again.Snapshot.ID != confirmed.Snapshot.ID || again.CurrentRevision != 2 {
		t.Fatalf("no-op confirmation: %+v %v", again, err)
	}
	d.Brand.Name = "New brand"
	saved, err := svc.SaveAdvertisingProject("owner", id, AdvertisingSaveRequest{ExpectedRevision: 2, Name: "Ad changed", Draft: d})
	if err != nil || saved.Revision != 3 {
		t.Fatalf("save: %+v %v", saved, err)
	}
	if _, err := svc.SaveAdvertisingProject("owner", id, AdvertisingSaveRequest{ExpectedRevision: 2, Name: "stale", Draft: d}); err == nil {
		t.Fatal("stale save")
	}
	second, err := svc.ConfirmAdvertisingProject("owner", id, 3)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := svc.ConfirmAdvertisingProject("owner", id, 1)
	if err != nil || retry.Snapshot.ID != confirmed.Snapshot.ID || retry.CurrentRevision != 4 {
		t.Fatalf("retry: %+v %v", retry, err)
	}
	current, err := svc.GetAdvertisingProject("owner", id)
	if err != nil || current.LatestConfirmedRevisionID == nil || *current.LatestConfirmedRevisionID != second.Snapshot.ID {
		t.Fatalf("latest moved back: %+v %v", current, err)
	}
	old, err := svc.GetAdvertisingRevision("owner", id, confirmed.Snapshot.ID)
	if err != nil || old.Snapshot.Draft.Brand.Name != "" || old.Snapshot.Name != "Ad" {
		t.Fatalf("old mutated %+v %v", old, err)
	}
	other, err := svc.CreateAdvertisingProject("owner", AdvertisingCreateRequest{Name: "Other"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.GetAdvertisingRevision("owner", other.Project.ID, old.ID); err == nil {
		t.Fatal("cross-project version")
	}
	if _, err := svc.ConfirmAdvertisingProject("owner", id, 2); err == nil {
		t.Fatal("stale unconfirmed revision")
	}
	list, err := svc.ListAdvertisingProjects("owner", 1, 20)
	if err != nil || list.Total != 2 {
		t.Fatalf("list: %+v %v", list, err)
	}
	versions, err := svc.ListAdvertisingRevisions("owner", id, 1, 20)
	if err != nil || versions.Total != 2 {
		t.Fatalf("versions: %+v %v", versions, err)
	}
	d.ProductResourceIDs = []string{"foreign"}
	seedResource(t, db, "foreign", "other", "image/png")
	if _, err := svc.CreateAdvertisingProject("owner", AdvertisingCreateRequest{Name: "Forbidden", Draft: &d}); err == nil {
		t.Fatal("foreign reference")
	}
}
func TestAdvertisingCannotBypassRevision(t *testing.T) {
	svc, db := advertisingTestService(t)
	ad, err := svc.CreateAdvertisingProject("owner", AdvertisingCreateRequest{Name: "Ad"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateProject("owner", CreateProjectRequest{Name: "Bypass", Type: "advertising"}); err == nil {
		t.Error("generic create accepted advertising")
	}
	if _, err := svc.UpdateProject("owner", ad.Project.ID, UpdateProjectRequest{Name: "Updated ad"}); err == nil {
		t.Error("generic update accepted advertising")
	}
	if _, err := svc.CreateProjectUnit("owner", ad.Project.ID, CreateProjectUnitRequest{Title: "Bypass"}); err == nil {
		t.Error("unit write accepted advertising")
	}
	if _, err := svc.ProjectDetail("owner", ad.Project.ID); err == nil {
		t.Error("drama detail accepted advertising")
	}
	if _, err := svc.activeProjectForUser("owner", ad.Project.ID); err == nil {
		t.Error("drama guard accepted advertising")
	}
	if _, err := svc.CreateUnitWorkflow("owner", ad.Project.ID, "unit"); err == nil {
		t.Error("workflow accepted advertising")
	}
	if _, err := svc.CreateProjectShot("owner", ad.Project.ID, CreateProjectShotRequest{Title: "Shot"}); err == nil {
		t.Error("shot accepted advertising")
	}
	if _, _, err := svc.dramaForUser("owner", ad.Project.ID); err == nil {
		t.Error("drama accepted advertising")
	}
	if err := svc.DeleteProject("owner", ad.Project.ID); err == nil {
		t.Error("generic delete accepted advertising")
	}
	if err := db.Create(&model.Project{ID: "drama", UserID: "owner", Name: "Drama", Type: "short-drama", Revision: 1}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateProject("owner", "drama", UpdateProjectRequest{Type: "advertising"}); err == nil {
		t.Error("generic type switch accepted advertising")
	}
	projects, err := svc.repo.Projects("owner")
	if err != nil || len(projects) != 1 || projects[0].ID != "drama" {
		t.Errorf("drama list includes ad: %+v %v", projects, err)
	}
	if _, err := svc.repo.ProjectForUser("owner", ad.Project.ID); err != nil {
		t.Fatal("shared ownership lookup broken", err)
	}
}

func TestAdvertisingProjectResourceValidation(t *testing.T) {
	svc, db := advertisingTestService(t)
	seedResource(t, db, "resource", "owner", "image/png")
	d := advertisingTestDraft()
	created, err := svc.CreateAdvertisingProject("owner", AdvertisingCreateRequest{Name: "Ad", Draft: &d})
	if err != nil {
		t.Fatal(err)
	}
	for _, mime := range []string{"image/svg+xml", "video/mp4", "audio/mpeg"} {
		if err := db.Model(&model.Resource{}).Where("id = ?", "resource").Update("mime_type", mime).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := svc.ConfirmAdvertisingProject("owner", created.Project.ID, 1); err == nil {
			t.Fatal("invalid product image confirmed", mime)
		}
	}
	if err := db.Model(&model.Resource{}).Where("id = ?", "resource").Updates(map[string]any{"mime_type": "image/png", "status": "processing"}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ConfirmAdvertisingProject("owner", created.Project.ID, 1); err == nil {
		t.Fatal("unready resource confirmed")
	}
	detail, err := svc.GetAdvertisingProject("owner", created.Project.ID)
	if err != nil || detail.Revision != 1 || detail.Resources[0].Available {
		t.Fatalf("unavailable draft read: %+v %v", detail, err)
	}
}

func TestAdvertisingProjectConcurrentConfirmation(t *testing.T) {
	svc, db := advertisingTestService(t)
	seedResource(t, db, "resource", "owner", "image/png")
	d := advertisingTestDraft()
	created, err := svc.CreateAdvertisingProject("owner", AdvertisingCreateRequest{Name: "Ad", Draft: &d})
	if err != nil {
		t.Fatal(err)
	}
	type result struct {
		confirmation AdvertisingConfirmation
		err          error
	}
	results := make(chan result, 2)
	for i := 0; i < 2; i++ {
		go func() {
			c, err := svc.ConfirmAdvertisingProject("owner", created.Project.ID, 1)
			results <- result{c, err}
		}()
	}
	first, second := <-results, <-results
	if first.err != nil || second.err != nil || first.confirmation.Snapshot.ID != second.confirmation.Snapshot.ID || first.confirmation.CurrentRevision != 2 || second.confirmation.CurrentRevision != 2 {
		t.Fatalf("concurrent confirmation: %+v %+v", first, second)
	}
}

func TestAdvertisingProjectRollback(t *testing.T) {
	svc, db := advertisingTestService(t)
	seedResource(t, db, "resource", "owner", "image/png")
	d := advertisingTestDraft()
	created, err := svc.CreateAdvertisingProject("owner", AdvertisingCreateRequest{Name: "Ad", Draft: &d})
	if err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(created)
	if err := db.Callback().Create().Before("gorm:create").Register("test:fail_ad_revision", func(tx *gorm.DB) {
		if tx.Statement.Table == "advertising_revisions" {
			tx.AddError(errors.New("injected"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ConfirmAdvertisingProject("owner", created.Project.ID, 1); err == nil {
		t.Fatal("injected failure hidden")
	}
	after, err := svc.GetAdvertisingProject("owner", created.Project.ID)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(after)
	if string(before) != string(raw) {
		t.Fatalf("partial state before=%s after=%s", before, raw)
	}
}
func TestAdvertisingProjectSaveValidationAndRollback(t *testing.T) {
	svc, db := advertisingTestService(t)
	seedResource(t, db, "resource", "owner", "image/png")
	d := advertisingTestDraft()
	created, err := svc.CreateAdvertisingProject("owner", AdvertisingCreateRequest{Name: "Ad", Draft: &d})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateAdvertisingProject("owner", AdvertisingCreateRequest{Name: "Taken"}); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*AdvertisingDraft){func(d *AdvertisingDraft) { d.SchemaVersion = 0 }, func(d *AdvertisingDraft) { d.Brief.DurationMs = 0 }} {
		bad := advertisingTestDraft()
		mutate(&bad)
		if _, err := svc.SaveAdvertisingProject("owner", created.Project.ID, AdvertisingSaveRequest{ExpectedRevision: 1, Name: "Ad", Draft: bad}); err == nil {
			t.Error("invalid explicit zero accepted")
		}
	}
	_, err = svc.SaveAdvertisingProject("owner", created.Project.ID, AdvertisingSaveRequest{ExpectedRevision: 1, Name: "Taken", Draft: d})
	var appErr *kernel.AppError
	if !errors.As(err, &appErr) || appErr.Status != 409 || appErr.Reason != kernel.ReasonProjectNameConflict {
		t.Errorf("name conflict = %v", err)
	}
	before, err := svc.GetAdvertisingProject("owner", created.Project.ID)
	if err != nil || before.Revision != 1 || before.Project.Name != "Ad" {
		t.Fatalf("conflict changed state: %+v %v", before, err)
	}
	if err := db.Callback().Create().Before("gorm:create").Register("test:fail_ad_reference", func(tx *gorm.DB) {
		if tx.Statement.Table == "advertising_resource_references" {
			tx.AddError(errors.New("injected reference failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SaveAdvertisingProject("owner", created.Project.ID, AdvertisingSaveRequest{ExpectedRevision: 1, Name: "Changed", Draft: d}); err == nil {
		t.Fatal("reference failure hidden")
	}
	after, err := svc.GetAdvertisingProject("owner", created.Project.ID)
	a, _ := json.Marshal(after)
	b, _ := json.Marshal(before)
	if err != nil || string(a) != string(b) {
		t.Fatalf("reference rollback failed: %s %s %v", a, b, err)
	}
	var refs int64
	if err := db.Model(&model.AdvertisingResourceReference{}).Where("project_id = ?", created.Project.ID).Count(&refs).Error; err != nil || refs != 1 {
		t.Fatalf("references lost: %d %v", refs, err)
	}
}

func TestAdvertisingProjectConcurrentSave(t *testing.T) {
	svc, db := advertisingTestService(t)
	checkAdvertisingProjectConcurrentSave(t, svc, db)
}

func checkAdvertisingProjectConcurrentSave(t *testing.T, svc *Service, db *gorm.DB) {
	t.Helper()
	seedResource(t, db, "resource", "owner", "image/png")
	d := advertisingTestDraft()
	created, err := svc.CreateAdvertisingProject("owner", AdvertisingCreateRequest{Name: "Ad", Draft: &d})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := svc.SaveAdvertisingProject("owner", created.Project.ID, AdvertisingSaveRequest{ExpectedRevision: 1, Name: fmt.Sprint("Ad", i), Draft: d})
			results <- err
		}(i)
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		} else {
			var appErr *AppError
			if !errors.As(err, &appErr) || appErr.Status != 409 {
				t.Fatalf("loser must get revision conflict: %v", err)
			}
		}
	}
	if success != 1 {
		t.Fatalf("concurrent winners: %d", success)
	}
}
