package app

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
)

func TestAdvertisingRenderReplay(t *testing.T) {
	s, db := advertisingTestService(t)
	seedResource(t, db, "resource", "owner", "image/png")
	seedResource(t, db, "video", "owner", "video/mp4")
	if err := os.MkdirAll(filepath.Join(s.dataDir, "resources"), 0700); err != nil {
		t.Fatal(err)
	}
	// Readability-only fixture: this test does not execute FFmpeg.
	if err := os.WriteFile(filepath.Join(s.dataDir, "resources", "video.mp4"), []byte("readability fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.Resource{}).Where("id = ?", "video").Updates(map[string]any{"provider": "local", "object_key": "video.mp4"}).Error; err != nil {
		t.Fatal(err)
	}
	draft := advertisingTestDraft()
	ad, err := s.CreateAdvertisingProject("owner", AdvertisingCreateRequest{Name: "Render", Draft: &draft})
	if err != nil {
		t.Fatal(err)
	}
	confirmed, err := s.ConfirmAdvertisingProject("owner", ad.Project.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.CreateAdvertisingProduction("owner", ad.Project.ID, confirmed.Snapshot.ID)
	if err != nil {
		t.Fatal(err)
	}
	timeline, _ := json.Marshal(renderTestProject("resource:video"))
	assembly := model.AdvertisingAssembly{ID: "assembly", ProductionID: p.Production.ID, ClientOperationID: "assembly-op", SourceProductionRevision: 1, TimelineJSON: string(timeline)}
	if err := s.repo.CreateAdvertisingAssembly(&assembly); err != nil {
		t.Fatal(err)
	}
	key := "30000000-0000-4000-8000-000000000001"
	var wg sync.WaitGroup
	rows := make(chan AdvertisingRenderAttemptDetail, 2)
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			row, err := s.CreateAdvertisingRender("owner", ad.Project.ID, p.Production.ID, assembly.ID, key)
			rows <- row
			errs <- err
		}()
	}
	wg.Wait()
	close(rows)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var first AdvertisingRenderAttemptDetail
	for row := range rows {
		if first.ID != "" && first.Task.ID != row.Task.ID {
			t.Fatal("duplicate task")
		}
		first = row
	}
	if err := db.Model(&model.Task{}).Where("id = ?", first.Task.ID).Updates(map[string]any{"status": model.TaskStatusFailed, "result_json": "original"}).Error; err != nil {
		t.Fatal(err)
	}
	next, err := s.CreateAdvertisingRender("owner", ad.Project.ID, p.Production.ID, assembly.ID, "30000000-0000-4000-8000-000000000002")
	if err != nil || next.Task.ID == first.Task.ID {
		t.Fatalf("next: %+v %v", next, err)
	}
	replay, err := s.CreateAdvertisingRender("owner", ad.Project.ID, p.Production.ID, assembly.ID, key)
	if err != nil || replay.Task.ID != first.Task.ID || replay.Task.ResultJSON != "original" {
		t.Fatalf("lost replay: %+v %v", replay, err)
	}
	if _, err := s.RetryTask("owner", first.Task.ID); err == nil {
		t.Fatal("generic retry mutated immutable render attempt")
	}
	if err := db.Exec("CREATE TRIGGER fail_attempt BEFORE INSERT ON advertising_render_attempts BEGIN SELECT RAISE(ABORT, 'attempt failed'); END").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateAdvertisingRender("owner", ad.Project.ID, p.Production.ID, assembly.ID, "30000000-0000-4000-8000-000000000003"); err == nil {
		t.Fatal("attempt insert should fail")
	}
	var count int64
	if err := db.Model(&model.Task{}).Count(&count).Error; err != nil || count != 2 {
		t.Fatalf("orphan task: %d %v", count, err)
	}
	list, err := s.ListAdvertisingRenderAttempts("owner", ad.Project.ID, p.Production.ID, assembly.ID, 1, 20)
	if err != nil || list.Total != 2 {
		t.Fatalf("list: %+v %v", list, err)
	}
	seedResource(t, db, "render-output", "owner", "video/mp4")
	if err := db.Model(&model.Resource{}).Where("id = ?", "render-output").Update("duration_ms", 2000).Error; err != nil {
		t.Fatal(err)
	}
	task, err := s.repo.TaskForUser("owner", next.Task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.Task{}).Where("id = ?", task.ID).Update("status", model.TaskStatusRunning).Error; err != nil {
		t.Fatal(err)
	}
	task.Status = model.TaskStatusSucceeded
	task.ResultJSON = `{"resourceId":"render-output"}`
	if err := db.Exec("CREATE TRIGGER fail_render_ref BEFORE INSERT ON advertising_resource_references WHEN NEW.scope_key LIKE 'render:%' BEGIN SELECT RAISE(ABORT, 'render reference failed'); END").Error; err != nil {
		t.Fatal(err)
	}
	if err := s.repo.SaveTaskCompletionWithRegistration(task, model.TaskStatusRunning, nil, func(repo *repository.Repository) error { return s.registerAdvertisingRenderOutput(repo, *task) }); err == nil {
		t.Fatal("partial completion committed")
	}
	stored, err := s.repo.TaskForUser("owner", task.ID)
	if err != nil || stored.Status != model.TaskStatusRunning {
		t.Fatalf("completion rollback: %+v %v", stored, err)
	}
	if err := db.Exec("DROP TRIGGER fail_render_ref").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.CancelTask(context.Background(), "owner", task.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.repo.SaveTaskCompletionWithRegistration(task, model.TaskStatusRunning, nil, func(repo *repository.Repository) error { return s.registerAdvertisingRenderOutput(repo, *task) }); err == nil {
		t.Fatal("late completion after cancellation")
	}
	refs, err := s.repo.AdvertisingResourceReferences("owner", []string{"render-output"})
	if err != nil || len(refs) != 0 {
		t.Fatalf("late output reference: %+v %v", refs, err)
	}
	if _, err := s.CreateAdvertisingRender("other", ad.Project.ID, p.Production.ID, assembly.ID, key); err == nil {
		t.Fatal("cross user render")
	}
}

func TestAdvertisingRenderOutput(t *testing.T) {
	requireFFmpegTools(t)
	s, db := advertisingTestService(t)
	seedResource(t, db, "resource", "owner", "image/png")
	store := func(name, mime string, duration int64, data []byte) model.Resource {
		t.Helper()
		resource, _, err := s.storeResource("owner", "media", name, mime, int64(len(data)), 160, 284, duration, bytes.NewReader(data), nil, false)
		if err != nil {
			t.Fatal(err)
		}
		return *resource
	}
	generate := func(name string, args ...string) []byte {
		t.Helper()
		path := filepath.Join(t.TempDir(), name)
		command := append([]string{"-v", "error", "-y"}, args...)
		command = append(command, path)
		if output, err := exec.Command("ffmpeg", command...).CombinedOutput(); err != nil {
			t.Fatalf("fixture: %v %s", err, output)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	draft := advertisingTestDraft()
	settings := AdvertisingProductionSettings{Selections: map[string]AdvertisingSelection{}}
	media := map[string]AdvertisingProbedMedia{}
	for i := range draft.Shots {
		shot := &draft.Shots[i]
		shot.OnScreenText = fmt.Sprintf("TEST SHOT %d", i+1)
		video := store(fmt.Sprintf("shot%d.mp4", i), "video/mp4", 5000, generate("source.mp4", "-f", "lavfi", "-i", "testsrc2=size=160x284:rate=30", "-f", "lavfi", "-i", "sine=frequency=1000:sample_rate=48000", "-vf", fmt.Sprintf("hue=h=%d", i*100), "-t", "5", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", "-movflags", "+faststart"))
		settings.Selections[shot.ID] = AdvertisingSelection{VideoSubmissionID: shot.ID}
		media[shot.ID] = AdvertisingProbedMedia{Resource: video, DurationMs: 5000, Width: 160, Height: 284}
	}
	voice := store("voice.wav", "audio/wav", 2000, generate("voice.wav", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000", "-t", "2"))
	music := store("music.wav", "audio/wav", 3000, generate("music.wav", "-f", "lavfi", "-i", "sine=frequency=220:sample_rate=48000", "-t", "3"))
	imageData := image.NewNRGBA(image.Rect(0, 0, 60, 30))
	for y := 0; y < 30; y++ {
		for x := 0; x < 60; x++ {
			imageData.SetNRGBA(x, y, color.NRGBA{R: 255, G: 255, A: 255})
		}
	}
	var imageBytes bytes.Buffer
	if err := png.Encode(&imageBytes, imageData); err != nil {
		t.Fatal(err)
	}
	logo := store("logo.png", "image/png", 0, imageBytes.Bytes())
	draft.Brand.LogoResourceID = logo.ID
	draft.Shots[0].Voiceover = "TEST VOICE"
	selection := settings.Selections[draft.Shots[0].ID]
	selection.AudioSubmissionID = "voice"
	settings.Selections[draft.Shots[0].ID] = selection
	settings.MusicResourceID = music.ID
	media["voice"] = AdvertisingProbedMedia{Resource: voice, DurationMs: 2000}
	media[music.ID] = AdvertisingProbedMedia{Resource: music, DurationMs: 3000}
	media[logo.ID] = AdvertisingProbedMedia{Resource: logo, Width: 60, Height: 30}
	ad, err := s.CreateAdvertisingProject("owner", AdvertisingCreateRequest{Name: "Synthetic render fixture", Draft: &draft})
	if err != nil {
		t.Fatal(err)
	}
	confirmed, err := s.ConfirmAdvertisingProject("owner", ad.Project.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.CreateAdvertisingProduction("owner", ad.Project.ID, confirmed.Snapshot.ID)
	if err != nil {
		t.Fatal(err)
	}
	timeline, err := buildAdvertisingTimeline(confirmed.Snapshot, settings, media, "pad")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(timeline)
	if err != nil {
		t.Fatal(err)
	}
	assembly := model.AdvertisingAssembly{ID: "render-assembly", ProductionID: p.Production.ID, ClientOperationID: "assembly", SourceProductionRevision: 1, TimelineJSON: string(raw)}
	if err := s.repo.CreateAdvertisingAssembly(&assembly); err != nil {
		t.Fatal(err)
	}
	key := "30000000-0000-4000-8000-000000000010"
	attempt, err := s.CreateAdvertisingRender("owner", ad.Project.ID, p.Production.ID, assembly.ID, key)
	if err != nil {
		t.Fatal(err)
	}
	if attempt.Available {
		t.Fatal("queued render is downloadable")
	}
	task, err := s.repo.TaskForUser("owner", attempt.Task.ID)
	if err != nil {
		t.Fatal(err)
	}
	task.Status = model.TaskStatusRunning
	if err := db.Model(&model.Task{}).Where("id = ?", task.ID).Update("status", model.TaskStatusRunning).Error; err != nil {
		t.Fatal(err)
	}
	if err := newTaskWorkerCoordinator(s).processTimelineRender(task, context.Background()); err != nil {
		t.Fatal(err)
	}
	result, err := s.CreateAdvertisingRender("owner", ad.Project.ID, p.Production.ID, assembly.ID, key)
	if err != nil || !result.Available {
		t.Fatalf("output: %+v %v", result, err)
	}
	resource, err := s.repo.ResourceForUser("owner", result.ResourceID)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.dataDir, "resources", filepath.FromSlash(resource.ObjectKey))
	info, err := probeMediaFile(context.Background(), path)
	if err != nil || !info.HasAudio || info.Width != 1080 || info.Height != 1920 || math.Abs(float64(info.DurationMs-15000)) > 34 {
		t.Fatalf("output probe: %+v %v", info, err)
	}
	var output timelineRenderOutput
	if err := json.Unmarshal([]byte(result.Task.ResultJSON), &output); err != nil {
		t.Fatal(err)
	}
	if len(output.Sources) != 6 || output.OverlayCount != 4 {
		t.Fatalf("provenance: %+v", output)
	}
	frames := map[string]bool{}
	for _, at := range []string{"1", "6", "11"} {
		frame, err := exec.Command("ffmpeg", "-v", "error", "-ss", at, "-i", path, "-frames:v", "1", "-vf", "scale=32:32", "-f", "rawvideo", "-pix_fmt", "rgb24", "pipe:1").Output()
		if err != nil || len(frame) != 32*32*3 {
			t.Fatalf("decode frame %s: %v", at, err)
		}
		var sum int
		for _, v := range frame {
			sum += int(v)
		}
		if sum < 10000 {
			t.Fatal("black test substitute")
		}
		frames[string(frame)] = true
	}
	if len(frames) != 3 {
		t.Fatal("shots do not change")
	}
	for _, interval := range []struct {
		at      string
		audible bool
	}{{"0.5", true}, {"2.3", true}, {"7", false}} {
		pcm, err := exec.Command("ffmpeg", "-v", "error", "-ss", interval.at, "-i", path, "-t", "0.4", "-vn", "-ac", "1", "-ar", "8000", "-f", "s16le", "pipe:1").Output()
		if err != nil || len(pcm) == 0 {
			t.Fatalf("audio decode: %v", err)
		}
		var energy float64
		for i := 0; i+1 < len(pcm); i += 2 {
			v := float64(int16(binary.LittleEndian.Uint16(pcm[i : i+2])))
			energy += v * v
		}
		rms := math.Sqrt(energy / float64(len(pcm)/2))
		if interval.audible && rms < 20 || !interval.audible && rms > 2 {
			t.Fatalf("audio at %s: rms=%f", interval.at, rms)
		}
	}
	refs, err := s.repo.AdvertisingResourceReferences("owner", []string{resource.ID})
	if err != nil || len(refs) != 1 {
		t.Fatalf("output reference: %+v %v", refs, err)
	}
	if err := s.repo.DeleteResource("owner", resource.ID); err == nil {
		t.Fatal("deleted referenced output")
	}
	if _, err := s.CancelTask(context.Background(), "owner", task.ID); err == nil {
		t.Fatal("cancelled completed render")
	}
	// Simulate missing storage without deleting a user file outside this temporary fixture.
	if err := db.Model(&model.Resource{}).Where("id = ?", resource.ID).Update("object_key", "missing.mp4").Error; err != nil {
		t.Fatal(err)
	}
	result, err = s.CreateAdvertisingRender("owner", ad.Project.ID, p.Production.ID, assembly.ID, key)
	if err != nil || result.Available {
		t.Fatalf("missing output available: %+v %v", result, err)
	}
	if err := db.Model(&model.Resource{}).Where("id = ?", voice.ID).Update("object_key", "missing.wav").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateAdvertisingRender("owner", ad.Project.ID, p.Production.ID, assembly.ID, "30000000-0000-4000-8000-000000000011"); err == nil {
		t.Fatal("queued missing source")
	}
	if err := s.DeleteAdvertisingProject("owner", ad.Project.ID, confirmed.CurrentRevision); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateAdvertisingRender("owner", ad.Project.ID, p.Production.ID, assembly.ID, key); err == nil {
		t.Fatal("replayed deleted project")
	}
}
