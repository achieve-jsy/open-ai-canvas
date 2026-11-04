package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"
	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
)

func checkAdvertisingDeleteAdmissionLock(t *testing.T, svc *Service, db *gorm.DB) {
	ad, err := svc.CreateAdvertisingProject("owner", AdvertisingCreateRequest{Name: "Ad"})
	if err != nil {
		t.Fatal(err)
	}
	held, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		done <- svc.repo.AdvertisingTransaction("owner", ad.Project.ID, func(repo *repository.Repository) error {
			if err := repo.GuardAdvertisingDeletion("owner", ad.Project.ID); err != nil {
				return err
			}
			close(held)
			<-release
			if err := repo.DeleteAdvertisingContent(ad.Project.ID); err != nil {
				return err
			}
			return deleteProjectWithRepository(repo, "owner", ad.Project.ID)
		})
	}()
	select {
	case <-held:
	case err := <-done:
		t.Fatal(err)
	case <-time.After(5 * time.Second):
		close(release)
		<-done
		t.Fatal("delete lock timed out")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	admitted := make(chan error, 1)
	go func() {
		admitted <- repository.New(db.WithContext(ctx)).CreateTaskWithActiveLimit(&model.Task{ID: "late", UserID: "owner", ProjectID: ad.Project.ID, Status: model.TaskStatusQueued}, 100)
	}()
	select {
	case err := <-admitted:
		close(release)
		<-done
		t.Fatalf("admission did not wait: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := <-admitted; !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("admission after deletion: %v", err)
	}
	var count int64
	if err := db.Model(&model.Task{}).Where("id = ?", "late").Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("orphan task persisted: %d %v", count, err)
	}
}

func TestAdvertisingAdmissionAfterDelete(t *testing.T) {
	svc, db := advertisingTestService(t)
	ad, err := svc.CreateAdvertisingProject("owner", AdvertisingCreateRequest{Name: "Deleted ad"})
	if err != nil {
		t.Fatal(err)
	}
	task := model.Task{ID: "retry-ad", UserID: "owner", ProjectID: ad.Project.ID, Status: model.TaskStatusFailed}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteAdvertisingProject("owner", ad.Project.ID, 1); err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.CreateTaskWithActiveLimit(&model.Task{ID: "late-free", UserID: "owner", ProjectID: ad.Project.ID, Status: model.TaskStatusQueued}, 100); err == nil {
		t.Error("deleted ad admitted free task")
	}
	if err := svc.repo.CreateTaskWithCreditReservation(&model.Task{ID: "late-paid", UserID: "owner", ProjectID: ad.Project.ID, Status: model.TaskStatusQueued}, &model.BillingOrder{ID: "late-order", UserID: "owner", TaskID: "late-paid"}, 100); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Errorf("paid task did not reject deleted scope before billing: %v", err)
	}
	if _, err := svc.repo.RetryTaskWithBilling("owner", &task, nil, 100); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Errorf("retry accepted stale scope: %v", err)
	}
	if err := svc.repo.CreateTaskWithActiveLimit(&model.Task{ID: "local-task", UserID: "owner", ProjectID: "local-only-canvas", Status: model.TaskStatusQueued}, 100); err != nil {
		t.Fatal("local canvas compatibility", err)
	}
}

func TestAdvertisingBindDeleteRace(t *testing.T) {
	svc, db := advertisingTestService(t)
	checkAdvertisingBindDeleteRace(t, svc, db)
}
func checkAdvertisingBindDeleteRace(t *testing.T, svc *Service, db *gorm.DB) {
	if err := db.Create(&model.Resource{ID: "race-resource", UserID: "owner", Status: model.ResourceStatusReady, MimeType: "image/png"}).Error; err != nil {
		t.Fatal(err)
	}
	ad, err := svc.CreateAdvertisingProject("owner", AdvertisingCreateRequest{Name: "Race"})
	if err != nil {
		t.Fatal(err)
	}
	d := ad.Draft
	d.ProductResourceIDs = []string{"race-resource"}
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	var saveErr, deleteErr error
	go func() {
		defer wg.Done()
		<-start
		_, saveErr = svc.SaveAdvertisingProject("owner", ad.Project.ID, AdvertisingSaveRequest{Name: "Race", ExpectedRevision: 1, Draft: d})
	}()
	go func() { defer wg.Done(); <-start; deleteErr = svc.repo.DeleteResource("owner", "race-resource") }()
	close(start)
	wg.Wait()
	if saveErr == nil && deleteErr == nil {
		t.Fatal("binding and deletion both succeeded")
	}
	var refs, resources int64
	if err := db.Model(&model.AdvertisingResourceReference{}).Where("resource_id = ?", "race-resource").Count(&refs).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.Resource{}).Where("id = ?", "race-resource").Count(&resources).Error; err != nil {
		t.Fatal(err)
	}
	if refs > 0 && resources != 1 {
		t.Fatal("committed reference lost its resource")
	}
	if saveErr == nil && refs != 1 {
		t.Fatal("successful save has no reference")
	}
}

func TestAdvertisingResourceProtection(t *testing.T) {
	svc, db := advertisingTestService(t)
	seedResource(t, db, "resource", "owner", "image/png")
	objectKey := "ad/product.png"
	path := filepath.Join(svc.dataDir, "resources", objectKey)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("product"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.Resource{}).Where("id = ?", "resource").Update("object_key", objectKey).Error; err != nil {
		t.Fatal(err)
	}
	d := advertisingTestDraft()
	a, err := svc.CreateAdvertisingProject("owner", AdvertisingCreateRequest{Name: "广告A", Draft: &d})
	if err != nil {
		t.Fatal(err)
	}
	b, err := svc.CreateAdvertisingProject("owner", AdvertisingCreateRequest{Name: "广告B", Draft: &d})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ConfirmAdvertisingProject("owner", a.Project.ID, 1); err != nil {
		t.Fatal(err)
	}
	d.ProductResourceIDs = []string{}
	d.Shots = []AdvertisingShot{}
	if _, err := svc.SaveAdvertisingProject("owner", a.Project.ID, AdvertisingSaveRequest{Name: "广告A", Draft: d, ExpectedRevision: 2}); err != nil {
		t.Fatal(err)
	}
	asset := model.Asset{ID: "asset", UserID: "owner", Title: "商品", PayloadJSON: `{"data":{"storageKey":"resource:resource"}}`}
	if err := db.Create(&asset).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.deleteUserAssetWithResources("owner", "asset", false); err == nil || !strings.Contains(err.Error(), "广告") {
		t.Fatalf("missing ad usage: %v", err)
	}
	if err := svc.deleteUserAssetWithResources("owner", "asset", true); err != nil {
		t.Fatalf("purge entry must preserve media: %v", err)
	}
	if err := svc.cleanupDetachedUserResources("owner", []model.Resource{{ID: "resource", UserID: "owner", Provider: "local", ObjectKey: objectKey}}); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteAdvertisingProject("owner", b.Project.ID, 1); err != nil {
		t.Fatal(err)
	}
	if err := svc.cleanupDetachedUserResources("owner", []model.Resource{{ID: "resource", UserID: "owner", Provider: "local", ObjectKey: objectKey}}); err != nil {
		t.Fatal(err)
	}
	var jobs int64
	if err := db.Model(&model.ResourceDeletionJob{}).Count(&jobs).Error; err != nil || jobs != 0 {
		t.Fatalf("referenced file queued: %d %v", jobs, err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("referenced file missing", err)
	}
	if err := svc.DeleteAdvertisingProject("owner", a.Project.ID, 3); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.repo.ResourceForUser("owner", "resource"); err != nil {
		t.Fatal("project delete removed independent resource", err)
	}
}

func TestAdvertisingDeleteRevisionAndTasks(t *testing.T) {
	svc, db := advertisingTestService(t)
	created, err := svc.CreateAdvertisingProject("owner", AdvertisingCreateRequest{Name: "Ad"})
	if err != nil {
		t.Fatal(err)
	}
	id := created.Project.ID
	canvas := model.CanvasProject{ID: "canvas", UserID: "owner", ProjectID: id, Revision: 1, PayloadJSON: `{"id":"canvas","projectId":"` + id + `","nodes":[],"connections":[]}`}
	if err := db.Create(&canvas).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteAdvertisingProject("owner", id, 2); err == nil {
		t.Fatal("stale delete accepted")
	}
	if err := svc.DeleteAdvertisingProject("other", id, 1); err == nil {
		t.Fatal("foreign delete accepted")
	}
	task := model.Task{ID: "active", UserID: "owner", ProjectID: "canvas", Status: model.TaskStatusRunning}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteAdvertisingProject("owner", id, 1); err == nil {
		t.Fatal("active canvas task ignored")
	}
	if err := db.Delete(&task).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteAdvertisingProject("owner", id, 1); err != nil {
		t.Fatal(err)
	}
	var got model.CanvasProject
	if err := db.First(&got, "id = ?", "canvas").Error; err != nil || got.ProjectID != "" || strings.Contains(got.PayloadJSON, id) {
		t.Fatalf("canvas not detached: %+v %v", got, err)
	}
	if _, err := svc.GetAdvertisingProject("owner", id); err == nil {
		t.Fatal("deleted project readable")
	}
}

func TestAdvertisingDeleteRejectsActiveAgentReplay(t *testing.T) {
	svc, db := advertisingTestService(t)
	ad, err := svc.CreateAdvertisingProject("owner", AdvertisingCreateRequest{Name: "Ad"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.CanvasProject{ID: "canvas", UserID: "owner", ProjectID: ad.Project.ID, PayloadJSON: "{}"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Task{ID: "agent", UserID: "owner", ProjectID: "canvas", Operation: "cloud_agent", Status: model.TaskStatusTextReplay}).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteAdvertisingProject("owner", ad.Project.ID, 1); err == nil {
		t.Fatal("active Agent replay ignored")
	}
}

func TestAdvertisingDeleteRechecksActiveTaskAndRollsBack(t *testing.T) {
	svc, db := advertisingTestService(t)
	d := advertisingTestDraft()
	seedResource(t, db, "resource", "owner", "image/png")
	ad, err := svc.CreateAdvertisingProject("owner", AdvertisingCreateRequest{Name: "Ad", Draft: &d})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ConfirmAdvertisingProject("owner", ad.Project.ID, 1); err != nil {
		t.Fatal(err)
	}
	// Inject on the second count: the service preflight has passed, but repository must recheck.
	counts := 0
	if err := db.Callback().Query().After("gorm:query").Register("test:late_ad_task", func(tx *gorm.DB) {
		if tx.Statement.Table == "tasks" {
			counts++
			if counts == 2 {
				if p, ok := tx.Statement.Dest.(*int64); ok {
					*p = 1
				}
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteAdvertisingProject("owner", ad.Project.ID, 2); err == nil {
		t.Fatal("late active task ignored")
	}
	after, err := svc.GetAdvertisingProject("owner", ad.Project.ID)
	if err != nil || after.Revision != 2 || after.LatestConfirmedRevisionID == nil {
		t.Fatalf("delete partially committed: %+v %v", after, err)
	}
	var refs int64
	if err := db.Model(&model.AdvertisingResourceReference{}).Where("project_id = ?", ad.Project.ID).Count(&refs).Error; err != nil || refs != 2 {
		t.Fatalf("references lost: %d %v", refs, err)
	}
}

func TestAdvertisingResourceFreshAnnouncementCleanup(t *testing.T) {
	svc, db := advertisingTestService(t)
	seedResource(t, db, "resource", "owner", "image/png")
	key := "ad/fresh.png"
	path := filepath.Join(svc.dataDir, "resources", key)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("product"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.Resource{}).Where("id = ?", "resource").Update("object_key", key).Error; err != nil {
		t.Fatal(err)
	}
	d := advertisingTestDraft()
	if _, err := svc.CreateAdvertisingProject("owner", AdvertisingCreateRequest{Name: "Ad", Draft: &d}); err != nil {
		t.Fatal(err)
	}
	resource, err := svc.repo.ResourceForUser("owner", "resource")
	if err != nil {
		t.Fatal(err)
	}
	_ = svc.deleteFreshAnnouncementImageResource(resource)
	if _, err := os.Stat(path); err != nil {
		t.Fatal("cleanup deleted advertising media before checking references", err)
	}
}

func TestAdvertisingResourceTransactionalGuards(t *testing.T) {
	svc, db := advertisingTestService(t)
	seedResource(t, db, "resource", "owner", "image/png")
	d := advertisingTestDraft()
	if _, err := svc.CreateAdvertisingProject("owner", AdvertisingCreateRequest{Name: "Ad", Draft: &d}); err != nil {
		t.Fatal(err)
	}
	resource, err := svc.repo.ResourceForUser("owner", "resource")
	if err != nil {
		t.Fatal(err)
	}
	job := model.ResourceDeletionJob{ID: "job", ResourceID: "resource", UserID: "owner"}
	if err := svc.repo.DeleteDetachedResources([]model.Resource{*resource}, []model.ResourceDeletionJob{job}); !errors.Is(err, repository.ErrResourceCleanupStillReferenced) {
		t.Fatalf("late binding not classified: %v", err)
	}
	asset := model.Asset{ID: "asset", UserID: "owner", PayloadJSON: `{"data":{"storageKey":"resource:resource"}}`}
	if err := db.Create(&asset).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.DeleteAssetsAndResources("owner", []string{"asset"}, []string{"resource"}, []model.ResourceDeletionJob{job}, true); err == nil {
		t.Fatal("late ad binding deleted")
	}
	var count int64
	if err := db.Model(&model.ResourceDeletionJob{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("failed delete queued physical file: %d %v", count, err)
	}
	seedResource(t, db, "removed", "owner", "image/png")
	if err := svc.repo.DeleteResource("owner", "removed"); err != nil {
		t.Fatal(err)
	}
	d.ProductResourceIDs = []string{"removed"}
	d.Shots[0].ReferenceResourceIDs = []string{"removed"}
	if _, err := svc.CreateAdvertisingProject("owner", AdvertisingCreateRequest{Name: "Missing", Draft: &d}); err == nil {
		t.Fatal("delete-first binding succeeded")
	}
	var lost model.Resource
	if err := db.First(&lost, "id = ?", "removed").Error; !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatal(err)
	}
}
