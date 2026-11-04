package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"yingce/backend/internal/auth"
	"yingce/backend/internal/database"
	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
	"yingce/backend/internal/service"
)

func TestAdvertisingRoutes(t *testing.T) {
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
	for _, row := range []any{&model.User{ID: "owner", Username: "owner", Status: model.UserStatusActive}, &model.AuthSession{ID: "owner", UserID: "owner", TokenHash: auth.HashToken("token"), ExpiresAt: time.Now().Add(time.Hour)}, &model.SystemSetting{Key: "feature_availability", ValueJSON: `{"shortDramaEnabled":false}`}, &model.Resource{ID: "resource", UserID: "owner", Status: model.ResourceStatusReady, MimeType: "image/png"}} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	svc := service.New(repository.New(db), t.TempDir())
	router := gin.New()
	RegisterCanvasAPI(router.Group("/api"), svc)
	call := func(method, path, body string, authenticated bool, want int) json.RawMessage {
		t.Helper()
		req := httptest.NewRequest(method, "/api"+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if authenticated {
			req.AddCookie(&http.Cookie{Name: service.SessionCookieName, Value: "owner.token"})
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != want {
			t.Fatalf("%s %s got %d want %d: %s", method, path, w.Code, want, w.Body.String())
		}
		var envelope struct {
			Data json.RawMessage `json:"data"`
			Code int             `json:"code"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		if want >= 400 && envelope.Code == 0 {
			t.Fatal("error reported as success")
		}
		return envelope.Data
	}
	for _, route := range []struct{ method, path string }{{"GET", "/ad-projects"}, {"POST", "/ad-projects"}, {"GET", "/ad-projects/x"}, {"PUT", "/ad-projects/x"}, {"POST", "/ad-projects/x/confirm"}, {"GET", "/ad-projects/x/revisions"}, {"GET", "/ad-projects/x/revisions/y"}, {"DELETE", "/ad-projects/x?expectedRevision=1"}} {
		call(route.method, route.path, "{}", false, 401)
	}
	draft := `{"schemaVersion":1,"brief":{"productName":"商品","audience":"通勤","sellingPoints":["轻巧"],"cta":"立即选购","durationMs":15000},"productResourceIds":["resource"],"shots":[{"id":"10000000-0000-4000-8000-000000000001","durationMs":5000,"visual":"全景"},{"id":"10000000-0000-4000-8000-000000000002","durationMs":5000,"visual":"细节"},{"id":"10000000-0000-4000-8000-000000000003","durationMs":5000,"visual":"结束"}]}`
	raw := call("POST", "/ad-projects", `{"name":"广告","draft":`+draft+"}", true, 200)
	var detail struct {
		Project  struct{ ID string }
		Revision int64
	}
	if err := json.Unmarshal(raw, &detail); err != nil || detail.Project.ID == "" {
		t.Fatalf("create %s %v", raw, err)
	}
	path := "/ad-projects/" + detail.Project.ID
	call("GET", "/ad-projects", "", true, 200)
	call("GET", path, "", true, 200)
	call("GET", "/ad-projects/missing", "", true, 404)
	call("GET", "/ad-projects?pageSize=101", "", true, 400)
	call("PUT", path, `{"expectedRevision":1,"name":"广告修改","draft":`+draft+"}", true, 200)
	call("PUT", path, `{"expectedRevision":1,"name":"冲突","draft":`+draft+"}", true, 409)
	confirmation := call("POST", path+"/confirm", `{"expectedRevision":2}`, true, 200)
	var version struct {
		Snapshot        struct{ ID string }
		CurrentRevision int64
	}
	if err := json.Unmarshal(confirmation, &version); err != nil || version.Snapshot.ID == "" || version.CurrentRevision != 3 {
		t.Fatalf("confirm %s %v", confirmation, err)
	}
	call("GET", path+"/revisions", "", true, 200)
	call("GET", path+"/revisions/"+version.Snapshot.ID, "", true, 200)
	for name, body := range map[string]string{"unknown": `{"name":"x","surprise":1}`, "duplicate": `{"name":"x","name":"y"}`, "nested duplicate": `{"name":"x","draft":{"schemaVersion":1,"brief":{"durationMs":15000,"durationMs":15001}}}`, "trailing": `{"name":"x"}{}`, "null": "null", "oversize": fmt.Sprintf(`{"name":"%s"}`, strings.Repeat("x", 256<<10)), "case alias": `{"name":"x","Name":"y"}`} {
		t.Run(name, func(t *testing.T) { call("POST", "/ad-projects", body, true, 400) })
	}
	call("POST", path+"/confirm", `{"expectedRevision":0}`, true, 400)
	call("POST", path+"/confirm", `{"expectedRevision":3,"draft":{}}`, true, 400)
	call("DELETE", path+"?expectedRevision=2", "", true, 409)
	call("DELETE", path+"?expectedRevision=3", "", true, 200)
	call("GET", path, "", true, 404)
}
