package handler

import (
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/goccy/go-yaml"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
	dataDir := t.TempDir()
	svc := service.New(repository.New(db), dataDir)
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
	call("GET", "/ad-projects/"+ad.Project.ID+"/events", "", "owner", 200)
	call("GET", p+"/reviews", "", "owner", 200)
	call("GET", p+"/issues", "", "owner", 200)
	call("POST", p+"/reviews", `{"actorId":"forged"}`, "owner", 400)
	call("POST", p+"/issues", `{"createdAt":"forged"}`, "owner", 400)
	routes := []struct{ method, path, body string }{
		{"POST", base, `{"confirmedRevisionId":"` + confirmed.Snapshot.ID + `"}`},
		{"GET", p, ""}, {"PATCH", p, `{"expectedRevision":1,"settings":{"selections":{}}}`},
		{"GET", p + "/batches", ""}, {"GET", p + "/batches/missing", ""}, {"GET", p + "/activity", ""},
		{"POST", p + "/batches", `{"count":2}`}, {"POST", p + "/batches/missing/execute", `{"items":[]}`},
		{"POST", p + "/batches/missing/close", `{}`},
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
	if err := os.MkdirAll(filepath.Join(dataDir, "resources"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "resources", "review.mp4"), []byte("readability fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	taskID := "review-http-task"
	for _, row := range []any{
		&model.Resource{ID: "review-http-media", UserID: "owner", Status: model.ResourceStatusReady, Size: 20, MimeType: "video/mp4", DurationMs: 6000, ObjectKey: "review.mp4", Provider: "local"},
		&model.Task{ID: taskID, UserID: "owner", ProjectID: ad.Project.ID, Type: "canvas_video", Status: model.TaskStatusSucceeded, ResultJSON: `{"storageKey":"resource:review-http-media"}`},
		&model.AdvertisingSubmission{ID: "review-http-sub", ProductionID: production.Production.ID, ShotID: draft.Shots[0].ID, Kind: "video", ClientOperationID: "http-sub-op", TaskID: &taskID, ApprovedAt: &now, RequestJSON: "{}", QuoteJSON: "{}"},
		&model.AdvertisingResourceReference{ProjectID: ad.Project.ID, ScopeKey: "candidate:review-http-sub", ResourceID: "review-http-media"},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	reviewBody := `{"clientOperationId":"30000000-0000-4000-8000-000000000001","expectedProductionRevision":2,"checkpoint":"candidate","submissionId":"review-http-sub","decision":"approve","reason":"watched"}`
	issueBody := `{"clientOperationId":"30000000-0000-4000-8000-000000000002","submissionId":"review-http-sub","atMs":6000,"category":"product","note":"check logo"}`
	for _, route := range []struct{ method, path, body string }{
		{"GET", "/ad-projects/" + ad.Project.ID + "/events", ""}, {"GET", p + "/reviews", ""}, {"GET", p + "/issues", ""},
		{"POST", p + "/reviews", reviewBody}, {"POST", p + "/issues", issueBody}, {"PATCH", p + "/issues/missing", `{"expectedRevision":1,"status":"resolved","resolutionNote":"checked"}`},
	} {
		call(route.method, route.path, route.body, "", 401)
		call(route.method, route.path, route.body, "other", 404)
	}
	first := call("POST", p+"/reviews", reviewBody, "owner", 200)
	if again := call("POST", p+"/reviews", reviewBody, "owner", 200); string(first) != string(again) {
		t.Fatal("HTTP replay lost response")
	}
	call("GET", p+"/reviews?clientOperationId=30000000-0000-4000-8000-000000000001", "", "owner", 200)
	call("POST", p+"/reviews", strings.Replace(reviewBody, "watched", "different", 1), "owner", 409)
	var issue model.AdvertisingIssue
	if err := json.Unmarshal(call("POST", p+"/issues", issueBody, "owner", 200), &issue); err != nil {
		t.Fatal(err)
	}
	resolution := `{"expectedRevision":1,"status":"resolved","resolutionNote":"checked again"}`
	call("PATCH", p+"/issues/"+issue.ID, resolution, "owner", 200)
	call("PATCH", p+"/issues/"+issue.ID, resolution, "owner", 409)
	call("GET", p+"/issues?clientOperationId=30000000-0000-4000-8000-000000000002", "", "owner", 200)
	for _, suffix := range []string{"/reviews", "/issues"} {
		call("GET", p+suffix+"?pageSize=101", "", "owner", 400)
		call("GET", p+suffix+"?clientOperationId=bad", "", "owner", 400)
	}
}

func TestAdvertisingReviewOpenAPI(t *testing.T) {
	var spec map[string]any
	if err := yaml.Unmarshal(openAPISpec, &spec); err != nil {
		t.Fatal(err)
	}
	paths := spec["paths"].(map[string]any)
	for path, methods := range map[string][]string{
		"/ad-projects/{id}/events":                                      {"get"},
		"/ad-projects/{id}/productions/{productionId}/reviews":          {"get", "post"},
		"/ad-projects/{id}/productions/{productionId}/issues":           {"get", "post"},
		"/ad-projects/{id}/productions/{productionId}/issues/{issueId}": {"patch"},
	} {
		item, ok := paths[path].(map[string]any)
		if !ok {
			t.Fatal("missing path", path)
		}
		for _, method := range methods {
			if item[method] == nil {
				t.Fatal("missing operation", path, method)
			}
		}
	}
	schemas := spec["components"].(map[string]any)["schemas"].(map[string]any)
	for _, name := range []string{"AdvertisingReviewRequest", "AdvertisingReviewResult", "AdvertisingIssueRequest", "AdvertisingIssueResolution", "AdvertisingEventPage"} {
		if schemas[name] == nil {
			t.Fatal("missing schema", name)
		}
	}
	request := schemas["AdvertisingReviewRequest"].(map[string]any)
	if request["additionalProperties"] != false || request["properties"].(map[string]any)["renderAttemptId"] == nil || request["properties"].(map[string]any)["resourceId"] == nil || request["properties"].(map[string]any)["acceptedReviewId"] == nil || len(request["oneOf"].([]any)) != 3 {
		t.Fatal("missing explicit output binding schema")
	}
}
