package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"yingce/backend/internal/model"
)

// renderTestProject 构造一个可渲染的 v2 快照：一条可见视频轨 + 一个
// 经 directMedia.storageKey（resource:<id>）引用后端资源的片段。
func renderTestProject(storageKey string) renderProject {
	visible := true
	return renderProject{
		Version:    2,
		DurationMs: 2000,
		Tracks:     []renderTrack{{ID: "track-v1", Kind: "video", Visible: &visible}},
		Clips: []renderClip{{
			ID:            "clip-v1",
			Kind:          "video",
			TrackID:       "track-v1",
			StartMs:       0,
			DurationMs:    2000,
			SourceStartMs: 0,
			Volume:        1,
			DirectMedia: &struct {
				ID         string `json:"id"`
				Kind       string `json:"kind"`
				StorageKey string `json:"storageKey"`
			}{ID: "asset-v1", Kind: "video", StorageKey: storageKey},
		}},
	}
}

func renderInputJSON(t *testing.T, project renderProject) string {
	t.Helper()
	raw, err := json.Marshal(timelineRenderInput{Timeline: project})
	if err != nil {
		t.Fatalf("marshal render input: %v", err)
	}
	return string(raw)
}

func seedRunningRenderTask(t *testing.T, db *gorm.DB, inputJSON string) *model.Task {
	t.Helper()
	task := &model.Task{
		ID:        fmt.Sprintf("tsk-render-%d", time.Now().UnixNano()),
		UserID:    "usr-render-test",
		Type:      model.TaskTypeTimelineRender,
		Status:    model.TaskStatusRunning,
		Stage:     "已领取",
		InputJSON: inputJSON,
	}
	if err := db.Create(task).Error; err != nil {
		t.Fatalf("seed task: %v", err)
	}
	return task
}

func TestRenderImageMIMEIsNotAPath(t *testing.T) {
	for mime, want := range map[string]string{"image/png": ".png", "image/jpeg": ".jpg", "image/webp": ".webp", "image/png/invalid": ".bin", "image/../../escape": ".bin"} {
		if got := extForMime(mime); got != want {
			t.Fatalf("extension %q: %q want %q", mime, got, want)
		}
	}
	svc, _ := newTimelineTaskTestService(t)
	svc.dataDir = t.TempDir()
	resource, _, err := svc.storeResource("usr-render-test", "media", "image.png", "image/png/invalid", 3, 1, 1, 0, strings.NewReader("bad"), nil, false)
	if err != nil {
		t.Fatal(err)
	}
	p := renderTestProject("resource:" + resource.ID)
	p.Clips[0].Kind = "image"
	plan := buildRenderPlan(p)
	_, cleanup, err := materializeRenderSources(context.Background(), svc, "usr-render-test", &plan)
	if cleanup != nil {
		defer cleanup()
	}
	if err == nil || !strings.Contains(err.Error(), "不支持的图片类型") {
		t.Fatalf("want rejected MIME, got %v", err)
	}
}

func TestCreateTimelineRenderTaskQueues(t *testing.T) {
	svc, db := newTimelineTaskTestService(t)
	seedResource(t, db, "res-1", "usr-render-test", "video/mp4")
	if err := db.Create(&model.Project{ID: "prj-render", UserID: "usr-render-test", Name: "render"}).Error; err != nil {
		t.Fatal(err)
	}

	task, err := svc.CreateTimelineRenderTask("usr-render-test", TimelineRenderCreateRequest{
		ProjectID: "prj-render",
		Timeline:  renderTestProject("resource:res-1"),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if task.Type != model.TaskTypeTimelineRender || task.Status != model.TaskStatusQueued {
		t.Fatalf("task = %s/%s, want %s/queued", task.Type, task.Status, model.TaskTypeTimelineRender)
	}
	if task.Provider != "local" || task.Model != "ffmpeg" {
		t.Fatalf("provider/model = %s/%s, want local/ffmpeg", task.Provider, task.Model)
	}

	var stored model.Task
	if err := db.First(&stored, "id = ?", task.ID).Error; err != nil {
		t.Fatalf("load task: %v", err)
	}
	if stored.ProjectID != "prj-render" || stored.UserID != "usr-render-test" {
		t.Fatalf("stored owner/project mismatch: %s/%s", stored.UserID, stored.ProjectID)
	}
}

func TestCreateTimelineRenderTaskRejectsNoMedia(t *testing.T) {
	svc, _ := newTimelineTaskTestService(t)

	visible := true
	textOnly := renderProject{
		Version:    2,
		DurationMs: 1000,
		Tracks:     []renderTrack{{ID: "track-t1", Kind: "text", Visible: &visible}},
		Clips: []renderClip{{
			ID: "clip-t1", Kind: "text", TrackID: "track-t1", StartMs: 0,
			DurationMs: 1000, Text: "字幕",
		}},
	}
	_, err := svc.CreateTimelineRenderTask("usr-render-test", TimelineRenderCreateRequest{Timeline: textOnly})
	if err == nil || !strings.Contains(err.Error(), "可渲染") {
		t.Fatalf("err = %v, want mention of 没有可渲染的媒体片段", err)
	}
}

// Exercise worker terminal writes, not just prepareRenderOverlays errors.
func TestTimelineRenderFailureTerminals(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"resource-owner-changed", "source-file-missing", "missing-font", "cancelled-context", "cancelled-task"} {
		t.Run(scenario, func(t *testing.T) {
			svc, db := newTimelineTaskTestService(t)
			svc.dataDir = t.TempDir()
			source := seedLocalVideoResource(t, svc, "usr-render-test", 32, 32, nil)
			project := enhancedTestProject()
			project.Clips[0].DirectMedia.StorageKey = "resource:" + source.ID
			queued, err := svc.CreateTimelineRenderTask("usr-render-test", TimelineRenderCreateRequest{Timeline: project})
			if err != nil {
				t.Fatal(err)
			}
			if err := db.Model(&model.Task{}).Where("id = ?", queued.ID).Updates(map[string]any{"status": model.TaskStatusRunning, "stage": "准备媒体"}).Error; err != nil {
				t.Fatal(err)
			}
			task, err := svc.repo.Task(queued.ID)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			wantStatus := model.TaskStatusFailed
			wantError := ""
			switch scenario {
			case "source-file-missing":
				// Point only this temporary fixture at an absent object; never delete user files.
				if err := db.Model(&model.Resource{}).Where("id = ?", source.ID).Update("object_key", "missing.mp4").Error; err != nil {
					t.Fatal(err)
				}
				wantError = "无法读取时间线引用的媒体"
			case "resource-owner-changed":
				if err := db.Model(&model.Resource{}).Where("id = ?", source.ID).Update("user_id", "other-user").Error; err != nil {
					t.Fatal(err)
				}
				wantError = "时间线引用的媒体"
			case "missing-font":
				bin := t.TempDir()
				if err := os.WriteFile(filepath.Join(bin, "fc-match"), []byte("#!/bin/sh\nprintf 'Wrong Font'\n"), 0700); err != nil {
					t.Fatal(err)
				}
				t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
				t.Setenv(renderFfmpegEnv, ffmpeg)
				wantError = "未安装中文字体"
			case "cancelled-context":
				cancel()
				wantError = "context canceled"
			case "cancelled-task":
				if _, err := svc.CancelTask(ctx, task.UserID, task.ID); err != nil {
					t.Fatal(err)
				}
				cancel()
				wantStatus = model.TaskStatusCancelled
				wantError = "任务已取消"
			}
			workerErr := newTaskWorkerCoordinator(svc).processTimelineRender(task, ctx)
			if workerErr != nil && scenario != "cancelled-task" {
				t.Fatal(workerErr)
			}
			stored, err := svc.repo.Task(task.ID)
			if err != nil {
				t.Fatal(err)
			}
			if stored.Status != wantStatus || stored.CompletedAt == nil || stored.ResultJSON != "" || !strings.Contains(stored.Error, wantError) {
				t.Fatalf("terminal: status=%s completed=%v result=%q error=%q worker=%v", stored.Status, stored.CompletedAt, stored.ResultJSON, stored.Error, workerErr)
			}
			var resources int64
			if err := db.Model(&model.Resource{}).Count(&resources).Error; err != nil {
				t.Fatal(err)
			}
			if resources != 1 {
				t.Fatalf("failed worker created output resources: %d", resources)
			}
		})
	}
}

func TestTimelineRenderCancellationAfterOutputStored(t *testing.T) {
	requireFFmpegTools(t)
	svc, db := newTimelineTaskTestService(t)
	svc.dataDir = t.TempDir()
	source := seedLocalVideoResource(t, svc, "usr-render-test", 32, 32, nil)
	project := renderTestProject("resource:" + source.ID)
	queued, err := svc.CreateTimelineRenderTask("usr-render-test", TimelineRenderCreateRequest{Timeline: project})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.Task{}).Where("id = ?", queued.ID).Updates(map[string]any{"status": model.TaskStatusRunning, "stage": "后端接管任务"}).Error; err != nil {
		t.Fatal(err)
	}
	task, err := svc.repo.Task(queued.ID)
	if err != nil {
		t.Fatal(err)
	}
	cancelled := false
	if err := db.Callback().Update().Before("gorm:update").Register("test:cancel_before_render_completion", func(tx *gorm.DB) {
		completion, ok := tx.Statement.Dest.(*model.Task)
		if !ok || completion.ID != task.ID || completion.Status != model.TaskStatusSucceeded || cancelled {
			return
		}
		cancelled = true
		var count int64
		if err := db.Model(&model.Resource{}).Count(&count).Error; err != nil {
			tx.AddError(err)
			return
		}
		if count != 2 {
			tx.AddError(fmt.Errorf("expected persisted output before cancellation, got %d resources", count))
			return
		}
		if _, err := svc.CancelTask(context.Background(), task.UserID, task.ID); err != nil {
			tx.AddError(err)
		}
	}); err != nil {
		t.Fatal(err)
	}
	err = newTaskWorkerCoordinator(svc).processTimelineRender(task, context.Background())
	if !cancelled || err == nil {
		t.Fatalf("expected lost completion after cancellation, fired=%v err=%v", cancelled, err)
	}
	stored, loadErr := svc.repo.Task(task.ID)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if stored.Status != model.TaskStatusCancelled || stored.ResultJSON != "" {
		t.Fatalf("cancel overwritten: %+v", stored)
	}
	var resources []model.Resource
	if err := db.Find(&resources).Error; err != nil {
		t.Fatal(err)
	}
	if len(resources) != 1 || resources[0].ID != source.ID {
		t.Fatalf("orphan output remains: %+v", resources)
	}
	svc.drainResourceDeletionJobs(10)
	var videos []string
	if err := filepath.WalkDir(filepath.Join(svc.dataDir, "resources"), func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.HasSuffix(path, ".mp4") {
			videos = append(videos, path)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(videos) != 1 {
		t.Fatalf("orphan physical output remains: %v", videos)
	}
}

func TestTimelineRenderFailsFastWhenSourceUnreadable(t *testing.T) {
	svc, db := newTimelineTaskTestService(t)
	// 指向不存在的资源；ffmpeg 路径设为无效值不影响断言——本测试在
	// 素材落盘（materialize）阶段即失败，且该失败早于任何外部命令执行。
	task := seedRunningRenderTask(t, db, renderInputJSON(t, renderTestProject("resource:res-missing")))

	t.Setenv(renderFfmpegEnv, "/no/such/ffmpeg")
	w := newTaskWorkerCoordinator(svc)
	if err := w.processTimelineRender(task, context.Background()); err != nil {
		t.Fatalf("process: want nil (task terminal handled internally), got %v", err)
	}

	var stored model.Task
	if err := db.First(&stored, "id = ?", task.ID).Error; err != nil {
		t.Fatalf("load task: %v", err)
	}
	if stored.Status != model.TaskStatusFailed {
		t.Fatalf("status = %q, want failed", stored.Status)
	}
	if !strings.Contains(stored.Error, "时间线引用的媒体") {
		t.Fatalf("error = %q, want mention of 无法读取时间线引用的媒体", stored.Error)
	}
}

// 真实 ffmpeg 端到端：近似画幅的来源裁切铺满，成片写入 AIGC 隐式标识，结果记录尺寸、适配方式与来源标识。
func TestTimelineRenderCropsNearAspectAndWritesProvenance(t *testing.T) {
	requireFFmpegTools(t)
	svc, db := newTimelineTaskTestService(t)
	svc.dataDir = t.TempDir()
	sourceTag := `{"Label":"1","ContentProducer":"upstream-co","ProduceID":"p-1"}`
	source := seedLocalVideoResource(t, svc, "usr-render-test", 768, 1344, map[string]string{"AIGC": sourceTag, "comment": "upstream comment"})
	project := renderTestProject("resource:" + source.ID)
	project.Width, project.Height, project.DurationMs = 1080, 1920, 1000
	project.Clips[0].DurationMs = 1000
	task := seedRunningRenderTask(t, db, renderInputJSON(t, project))

	if err := newTaskWorkerCoordinator(svc).processTimelineRender(task, context.Background()); err != nil {
		t.Fatalf("process: %v", err)
	}
	var stored model.Task
	if err := db.First(&stored, "id = ?", task.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != model.TaskStatusSucceeded {
		t.Fatalf("status = %s, error = %s", stored.Status, stored.Error)
	}
	var result timelineRenderOutput
	if err := json.Unmarshal([]byte(stored.ResultJSON), &result); err != nil {
		t.Fatal(err)
	}
	if result.Width != 1080 || result.Height != 1920 || result.Fit != renderFitAuto || len(result.Sources) != 1 {
		t.Fatalf("result = %+v", result)
	}
	if record := result.Sources[0]; record.ResourceID != source.ID || record.Fit != renderFitCrop || record.Width != 768 || record.Height != 1344 || record.AIGC != sourceTag {
		t.Fatalf("source record = %+v", record)
	}
	var label aigcImplicitLabel
	if err := json.Unmarshal([]byte(result.AIGC), &label); err != nil {
		t.Fatalf("result aigc: %v", err)
	}
	if label.Label != "1" || label.ContentProducer != "upstream-co" || label.ProduceID != "p-1" || label.PropagateID != task.ID || label.ContentPropagator != AIGCPlatformPropagator {
		t.Fatalf("label = %+v", label)
	}

	output, err := svc.repo.ResourceForUser("usr-render-test", result.ResourceID)
	if err != nil {
		t.Fatal(err)
	}
	info, err := probeMediaFile(context.Background(), filepath.Join(svc.dataDir, "resources", filepath.FromSlash(output.ObjectKey)))
	if err != nil {
		t.Fatal(err)
	}
	if info.Width != 1080 || info.Height != 1920 {
		t.Fatalf("output size = %dx%d", info.Width, info.Height)
	}
	if got := mediaProbeTag(info.Tags, "AIGC"); got != result.AIGC {
		t.Fatalf("output AIGC tag = %q, want %q", got, result.AIGC)
	}
	if got := mediaProbeTag(info.Tags, "comment"); got != renderAIGCComment {
		t.Fatalf("output comment = %q: source metadata leaked or label missing", got)
	}
}
