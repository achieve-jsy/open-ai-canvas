package handler

import (
	"encoding/json"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"yingce/backend/internal/auth"
	"yingce/backend/internal/database"
	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
	"yingce/backend/internal/service"
)

func TestAdvertisingGenerationRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := database.Open(database.Config{Driver: "sqlite", DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := database.MigrateSchema(db); err != nil {
		t.Fatal(err)
	}
	for _, row := range []any{&model.User{ID: "owner", Username: "owner", Status: model.UserStatusActive}, &model.User{ID: "other", Username: "other", Status: model.UserStatusActive}, &model.AuthSession{ID: "owner", UserID: "owner", TokenHash: auth.HashToken("token"), ExpiresAt: time.Now().Add(time.Hour)}, &model.AuthSession{ID: "other", UserID: "other", TokenHash: auth.HashToken("token"), ExpiresAt: time.Now().Add(time.Hour)}, &model.SystemSetting{Key: "feature_availability", ValueJSON: `{"shortDramaEnabled":false}`}, &model.Resource{ID: "resource", UserID: "owner", Status: model.ResourceStatusReady, MimeType: "image/png"}} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	svc := service.New(repository.New(db), t.TempDir())
	router := gin.New()
	RegisterCanvasAPI(router.Group("/api"), svc)
	call := func(method, path, body, user string, want int) json.RawMessage {
		t.Helper()
		req := httptest.NewRequest(method, "/api"+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if user != "" {
			req.AddCookie(&http.Cookie{Name: service.SessionCookieName, Value: user + ".token"})
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != want {
			t.Fatalf("%s %s got %d want %d: %s", method, path, w.Code, want, w.Body.String())
		}
		var envelope struct {
			Data json.RawMessage
			Code int
		}
		if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		if want >= 400 && envelope.Code == 0 {
			t.Fatal("error reported as success")
		}
		return envelope.Data
	}
	draft := service.AdvertisingDraft{SchemaVersion: 1, Brief: service.AdvertisingBrief{ProductName: "Test", Audience: "Test", SellingPoints: []string{"Test"}, CTA: "Test", DurationMs: 15000}, ProductResourceIDs: []string{"resource"}, Shots: []service.AdvertisingShot{{ID: "10000000-0000-4000-8000-000000000001", DurationMs: 5000, Visual: "Test"}, {ID: "10000000-0000-4000-8000-000000000002", DurationMs: 5000, Visual: "Test"}, {ID: "10000000-0000-4000-8000-000000000003", DurationMs: 5000, Visual: "Test"}}}
	ad, err := svc.CreateAdvertisingProject("owner", service.AdvertisingCreateRequest{Name: "Test", Draft: &draft})
	if err != nil {
		t.Fatal(err)
	}
	confirmed, err := svc.ConfirmAdvertisingProject("owner", ad.Project.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	base := "/ad-projects/" + ad.Project.ID + "/productions"
	raw := call("POST", base, `{"confirmedRevisionId":"`+confirmed.Snapshot.ID+`"}`, "owner", 200)
	var production struct {
		Production struct {
			ID       string
			Revision int64
		}
	}
	if err := json.Unmarshal(raw, &production); err != nil || production.Production.ID == "" {
		t.Fatalf("production: %s %v", raw, err)
	}
	p := base + "/" + production.Production.ID
	routes := []struct{ method, path, body string }{
		{"POST", base, `{"confirmedRevisionId":"` + confirmed.Snapshot.ID + `"}`},
		{"GET", p, ""}, {"PATCH", p, `{"expectedRevision":1,"settings":{"selections":{}}}`},
		{"GET", p + "/submissions", ""}, {"POST", p + "/submissions", `{"clientOperationId":"20000000-0000-4000-8000-000000000001","shotId":"10000000-0000-4000-8000-000000000001","kind":"video","modelId":"missing"}`},
		{"POST", p + "/submissions/missing/execute", `{"quoteHash":"missing"}`},
		{"GET", p + "/assemblies", ""}, {"POST", p + "/assemblies", `{"expectedRevision":1,"clientOperationId":"20000000-0000-4000-8000-000000000001","fit":"pad"}`},
		{"GET", p + "/assemblies/missing", ""}, {"GET", p + "/assemblies/missing/renders", ""},
		{"POST", p + "/assemblies/missing/renders", `{"clientOperationId":"20000000-0000-4000-8000-000000000001"}`},
	}
	for _, route := range routes {
		call(route.method, route.path, route.body, "", 401)
		call(route.method, route.path, route.body, "other", 404)
	}
	otherAd, err := svc.CreateAdvertisingProject("owner", service.AdvertisingCreateRequest{Name: "Other", Draft: &draft})
	if err != nil {
		t.Fatal(err)
	}
	otherConfirmed, err := svc.ConfirmAdvertisingProject("owner", otherAd.Project.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	otherProduction, err := svc.CreateAdvertisingProduction("owner", otherAd.Project.ID, otherConfirmed.Snapshot.ID)
	if err != nil {
		t.Fatal(err)
	}
	quote := model.AdvertisingSubmission{ID: "quote", ProductionID: production.Production.ID, ShotID: draft.Shots[0].ID, Kind: "video", ClientOperationID: "quote", RequestJSON: `{"selection":{"modelId":"model"},"request":{"prompt":"TEST"}}`, QuoteJSON: `{"quoteHash":"correct"}`}
	if err := db.Create(&quote).Error; err != nil {
		t.Fatal(err)
	}
	call("POST", p+"/submissions/quote/execute", `{"quoteHash":"wrong"}`, "owner", 409)
	call("POST", "/ad-projects/"+otherAd.Project.ID+"/productions/"+otherProduction.Production.ID+"/submissions/quote/execute", `{"quoteHash":"correct"}`, "owner", 404)
	call("GET", base+"/"+otherProduction.Production.ID, "", "owner", 404)
	call("GET", p, "", "owner", 200)
	for _, suffix := range []string{"/submissions", "/assemblies"} {
		call("GET", p+suffix, "", "owner", 200)
		call("GET", p+suffix+"?pageSize=101", "", "owner", 400)
	}
	call("PATCH", p, `{"expectedRevision":1,"settings":{"selections":{}}}`, "owner", 200)
	call("PATCH", p, `{"expectedRevision":1,"settings":{"selections":{}}}`, "owner", 409)
	call("PATCH", p, `{"expectedRevision":0,"settings":{}}`, "owner", 400)
	for _, route := range routes {
		if route.method == "GET" {
			continue
		}
		for _, body := range []string{`{"unexpected":1}`, `{"quoteHash":"a","quoteHash":"b"}`, `{"x":"` + strings.Repeat("x", 256<<10) + `"}`} {
			call(route.method, route.path, body, "owner", 400)
		}
	}
	call("POST", p+"/assemblies", `{"expectedRevision":2,"clientOperationId":"20000000-0000-4000-8000-000000000001","fit":"pad"}`, "owner", 409)
}
