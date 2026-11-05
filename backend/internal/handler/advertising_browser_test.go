package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"yingce/backend/internal/database"
	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
	"yingce/backend/internal/service"
)

// Only compiled into tests. No fixture routes or fake provider switch in the product.
func TestAdvertisingBrowserServer(t *testing.T) {
	dir := os.Getenv("ADVERTISING_BROWSER_TEST_DIR")
	if dir == "" {
		t.Skip("run web/scripts/advertising-generation-chrome-e2e.mjs")
	}
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")
	gin.SetMode(gin.TestMode)
	data := t.TempDir()
	media := filepath.Join(data, "resources")
	if err := os.MkdirAll(media, 0700); err != nil {
		t.Fatal(err)
	}
	ffmpeg := func(args ...string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, "ffmpeg", append([]string{"-v", "error", "-y"}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("media: %v %s", err, out)
		}
	}
	ffmpeg("-f", "lavfi", "-i", "testsrc2=size=160x284:rate=30", "-f", "lavfi", "-i", "sine=frequency=900:sample_rate=48000", "-t", "5", "-vf", "drawtext=text=TEST:x=10:y=10:fontsize=22:fontcolor=white", "-c:v", "libx264", "-c:a", "aac", "-pix_fmt", "yuv420p", "-movflags", "+faststart", filepath.Join(media, "test.mp4"))
	ffmpeg("-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000", "-t", "2", filepath.Join(media, "voice.mp3"))
	ffmpeg("-f", "lavfi", "-i", "sine=frequency=220:sample_rate=48000", "-t", "3", filepath.Join(media, "music.wav"))
	ffmpeg("-f", "lavfi", "-i", "testsrc2=size=160x160", "-frames:v", "1", filepath.Join(media, "product.png"))
	db, err := database.Open(database.Config{Driver: "sqlite", DataDir: data})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()
	if err := database.MigrateSchema(db); err != nil {
		t.Fatal(err)
	}
	svc := service.New(repository.New(db), data)
	previousRuntime := runtimeService
	ConfigureRuntime(svc)
	defer ConfigureRuntime(previousRuntime)
	var videoCalls, audioCalls atomic.Int64
	var upstream *httptest.Server
	upstream = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/test.mp4":
			http.ServeFile(w, r, filepath.Join(media, "test.mp4"))
		case strings.HasSuffix(r.URL.Path, "/audio/speech"):
			audioCalls.Add(1)
			w.Header().Set("Content-Type", "audio/mpeg")
			http.ServeFile(w, r, filepath.Join(media, "voice.mp3"))
		case strings.HasSuffix(r.URL.Path, "/contents/generations/tasks") && r.Method == "POST":
			n := videoCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"id":"test-%d","status":"succeeded","content":{"video_url":%q},"duration":5}`, n, upstream.URL+"/test.mp4")
		case strings.Contains(r.URL.Path, "/contents/generations/tasks/"):
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"id":"test","status":"succeeded","content":{"video_url":%q},"duration":5}`, upstream.URL+"/test.mp4")
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	capability := service.DefaultModelCapabilityConfigForModel(string(model.ChannelInterfaceVolcengineArkVideo), "seedance-test")
	raw, _ := json.Marshal(capability)
	for _, row := range []any{
		&model.ModelChannel{ID: "test-channel", Scope: model.ChannelScopeSystem, Enabled: true, Name: "TEST ONLY", BaseURL: upstream.URL, APIKey: "test-not-a-secret", APIFormat: "openai", ModelsJSON: "[]"},
		&model.ChannelModel{ID: "test-video", ChannelID: "test-channel", ModelKey: "seedance-test", DisplayName: "TEST video", Capability: "video", Protocol: model.ChannelInterfaceVolcengineArkVideo, CapabilityConfigJSON: string(raw), BillingMode: "per_second", PriceConfigured: true, Enabled: true},
		&model.ChannelModel{ID: "test-audio", ChannelID: "test-channel", ModelKey: "tts-1", DisplayName: "TEST TTS", Capability: "audio", Protocol: model.ChannelInterfaceOpenAIAudio, BillingMode: "fixed_request", PriceConfigured: true, Enabled: true},
		&model.ChannelModelPriceTier{ID: "test-video-price", ChannelModelID: "test-video", SelectorKey: "{}", SelectorJSON: "{}", BillingMode: "per_second", UnitPriceMicrocredits: 1, PriceConfigured: true, Enabled: true},
		&model.ChannelModelPriceTier{ID: "test-audio-price", ChannelModelID: "test-audio", SelectorKey: "{}", SelectorJSON: "{}", BillingMode: "fixed_request", UnitPriceMicrocredits: 10, PriceConfigured: true, Enabled: true},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	router := gin.New()
	router.Use(gin.Recovery())
	RegisterCanvasAPI(router.Group("/api"), svc)
	router.POST("/api/test-fixture", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		if err := db.Model(&model.CreditAccount{}).Where("user_id = ?", user.ID).Update("available_microcredits", 10000).Error; err != nil {
			failService(c, err)
			return
		}
		for _, spec := range []struct {
			id, file, mime, kind string
			duration             int64
		}{{"test-product", "product.png", "image/png", "image", 0}, {"test-music", "music.wav", "audio/wav", "audio", 3000}} {
			stat, err := os.Stat(filepath.Join(media, spec.file))
			if err != nil {
				failService(c, err)
				return
			}
			resource := model.Resource{ID: spec.id, UserID: user.ID, Status: model.ResourceStatusReady, Provider: "local", ObjectKey: spec.file, MimeType: spec.mime, Kind: spec.kind, Size: stat.Size(), Width: 160, Height: 160, DurationMs: spec.duration}
			if err := db.Create(&resource).Error; err != nil {
				failService(c, err)
				return
			}
		}
		ok(c, gin.H{"productId": "test-product", "musicId": "test-music"})
	})
	router.POST("/api/test-expire-quote/:id", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		owned := db.Table("advertising_productions").Select("advertising_productions.id").Joins("JOIN projects ON projects.id = advertising_productions.project_id").Where("projects.user_id = ?", user.ID)
		result := db.Model(&model.AdvertisingSubmission{}).Where("id = ? AND production_id IN (?) AND task_id IS NULL", c.Param("id"), owned).Update("expires_at", time.Now().Add(-time.Minute))
		if result.Error != nil {
			t.Error(result.Error)
			c.Status(500)
			return
		}
		ok(c, gin.H{"expired": result.RowsAffected == 1})
	})
	router.GET("/api/test-evidence", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		var account model.CreditAccount
		db.First(&account, "user_id = ?", user.ID)
		var tasks []model.Task
		db.Where("user_id = ?", user.ID).Find(&tasks)
		ok(c, gin.H{"videoCalls": videoCalls.Load(), "audioCalls": audioCalls.Load(), "account": account, "tasks": tasks})
	})
	dist, err := filepath.Abs("../../../web/dist")
	if err != nil {
		t.Fatal(err)
	}
	router.NoRoute(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/api/") {
			c.Status(404)
			return
		}
		path := filepath.Join(dist, filepath.Clean("/"+c.Request.URL.Path))
		if st, err := os.Stat(path); err != nil || st.IsDir() {
			path = filepath.Join(dist, "index.html")
		}
		c.File(path)
	})
	server := httptest.NewServer(router)
	defer server.Close()
	t.Setenv("CANVAS_PUBLIC_BASE_URL", server.URL)
	svc.StartWorker()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := svc.StopWorker(ctx); err != nil {
			t.Error(err)
		}
	}()
	if err := os.WriteFile(filepath.Join(dir, "server-url"), []byte(server.URL), 0600); err != nil {
		t.Fatal(err)
	}
	deadline := time.NewTimer(8 * time.Minute)
	defer deadline.Stop()
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-deadline.C:
			t.Fatal("browser test exceeded eight minutes")
		case <-tick.C:
			if _, err := os.Stat(filepath.Join(dir, "stop")); err == nil {
				return
			}
		}
	}
}
