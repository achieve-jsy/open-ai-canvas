package app

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"yingce/backend/internal/model"
)

func TestAdvertisingAssemblyProbeStreams(t *testing.T) {
	info, err := parseMediaProbeOutput([]byte(`{"streams":[{"codec_type":"audio"},{"codec_type":"video","width":720,"height":1280}],"format":{"duration":"5"}}`))
	if err != nil || !info.HasAudio || info.Width != 720 || info.Height != 1280 {
		t.Fatalf("stream probe: %+v %v", info, err)
	}
}

func TestAdvertisingAssemblyDuration(t *testing.T) {
	snapshot := AdvertisingRevisionDetail{Snapshot: AdvertisingSnapshot{Draft: advertisingTestDraft()}}
	zero := 0.0
	settings := AdvertisingProductionSettings{Selections: map[string]AdvertisingSelection{}, MusicResourceID: "music", MusicVolume: &zero}
	media := map[string]AdvertisingProbedMedia{"music": {Resource: model.Resource{ID: "music", MimeType: "audio/mpeg"}, DurationMs: 1000}}
	for i, shot := range snapshot.Snapshot.Draft.Shots {
		settings.Selections[shot.ID] = AdvertisingSelection{VideoSubmissionID: shot.ID, VideoStartMs: 1000}
		media[shot.ID] = AdvertisingProbedMedia{Resource: model.Resource{ID: shot.ID, MimeType: "video/mp4"}, DurationMs: 6000, Width: 720, Height: 1280}
		snapshot.Snapshot.Draft.Shots[i].OnScreenText = "商品细节"
	}
	snapshot.Snapshot.Draft.Shots[0].Voiceover = "轻便随行"
	selection := settings.Selections[snapshot.Snapshot.Draft.Shots[0].ID]
	selection.AudioSubmissionID = "audio"
	settings.Selections[snapshot.Snapshot.Draft.Shots[0].ID] = selection
	media["audio"] = AdvertisingProbedMedia{Resource: model.Resource{ID: "audio", MimeType: "audio/wav"}, DurationMs: 5000}
	snapshot.Snapshot.Draft.Brand.LogoResourceID = "logo"
	media["logo"] = AdvertisingProbedMedia{Resource: model.Resource{ID: "logo", MimeType: "image/png"}, Width: 300, Height: 100}
	project, err := buildAdvertisingTimeline(snapshot, settings, media, "pad")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(project)
	var parsed renderProject
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatal(err)
	}
	if err := validateEnhancedRenderProject(parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.DurationMs != 15000 || parsed.Output.Width != 1080 || parsed.Output.Height != 1920 {
		t.Fatalf("output: %+v", parsed)
	}
	videos, audio, music, logos, texts := 0, 0, 0, 0, 0
	for _, clip := range parsed.Clips {
		switch clip.Kind {
		case "image":
			logos++
			if clip.Overlay == nil || clip.DurationMs != 15000 {
				t.Fatalf("logo: %+v", clip)
			}
		case "text":
			texts++
			if clip.Overlay == nil || clip.Text != "商品细节" {
				t.Fatalf("text: %+v", clip)
			}
		case "video":
			if clip.Volume != 0 || clip.StartMs != int64(videos)*5000 || clip.DurationMs != 5000 || clip.SourceStartMs != 1000 {
				t.Fatalf("video: %+v", clip)
			}
			videos++
		case "audio":
			if clip.DirectMedia.StorageKey == "resource:music" {
				if clip.Volume != 0 || clip.DurationMs != 1000 {
					t.Fatalf("music loop/volume: %+v", clip)
				}
				music++
			} else {
				audio++
				if clip.Volume != 1 || clip.DurationMs != 5000 {
					t.Fatalf("voice: %+v", clip)
				}
			}
		}
	}
	if logos != 1 || texts != 3 {
		t.Fatalf("overlays: %d %d", logos, texts)
	}
	if videos != 3 || audio != 1 || music != 1 {
		t.Fatalf("tracks: %d %d %d", videos, audio, music)
	}
	for _, duration := range []int64{5001, 0} {
		m := media["audio"]
		m.DurationMs = duration
		media["audio"] = m
		if _, err := buildAdvertisingTimeline(snapshot, settings, media, "pad"); err == nil {
			t.Fatalf("accepted audio %d", duration)
		}
	}
	m := media["audio"]
	m.DurationMs = 5000
	media["audio"] = m
	first := snapshot.Snapshot.Draft.Shots[0].ID
	m = media[first]
	m.DurationMs = 5999
	media[first] = m
	if _, err := buildAdvertisingTimeline(snapshot, settings, media, "pad"); err == nil {
		t.Fatal("accepted video too short after trim")
	}
	m.DurationMs = 6000
	media[first] = m
	selection.AudioSubmissionID = ""
	settings.Selections[first] = selection
	if _, err := buildAdvertisingTimeline(snapshot, settings, media, "pad"); err == nil {
		t.Fatal("missing voice accepted")
	}
	snapshot.Snapshot.Draft.Shots[0].Voiceover = ""
	if _, err := buildAdvertisingTimeline(snapshot, settings, media, "crop"); err != nil {
		t.Fatal(err)
	}
}

func TestAdvertisingAssemblyReplay(t *testing.T) {
	requireFFmpegTools(t)
	s, db := advertisingTestService(t)
	seedResource(t, db, "resource", "owner", "image/png")
	draft := advertisingTestDraft()
	draft.Shots[0].Voiceover = "旁白测试"
	ad, err := s.CreateAdvertisingProject("owner", AdvertisingCreateRequest{Name: "Assembly", Draft: &draft})
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
	dir := filepath.Join(s.dataDir, "resources")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	videoPath := filepath.Join(dir, "source.mp4")
	if output, err := exec.Command("ffmpeg", "-v", "error", "-y", "-f", "lavfi", "-i", "testsrc2=size=160x284:rate=30", "-t", "5", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-movflags", "+faststart", videoPath).CombinedOutput(); err != nil {
		t.Fatalf("fixture: %v %s", err, output)
	}
	stat, err := os.Stat(videoPath)
	if err != nil {
		t.Fatal(err)
	}
	resource := model.Resource{ID: "video", UserID: "owner", Status: model.ResourceStatusReady, Provider: "local", ObjectKey: "source.mp4", MimeType: "video/mp4", Size: stat.Size(), Width: 160, Height: 284, DurationMs: 999999}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for _, shot := range draft.Shots {
		task := model.Task{ID: "task-" + shot.ID, UserID: "owner", ProjectID: ad.Project.ID, Type: "canvas_video", Status: model.TaskStatusSucceeded, ResultJSON: "{\"storageKey\":\"resource:video\"}"}
		if err := db.Create(&task).Error; err != nil {
			t.Fatal(err)
		}
		submission := model.AdvertisingSubmission{ID: shot.ID, ProductionID: p.Production.ID, ShotID: shot.ID, Kind: "video", ClientOperationID: shot.ID, TaskID: &task.ID, ApprovedAt: &now}
		if err := s.repo.CreateAdvertisingSubmission(&submission); err != nil {
			t.Fatal(err)
		}
		if err := s.repo.ReplaceAdvertisingReferences(ad.Project.ID, "candidate:"+shot.ID, []string{resource.ID}); err != nil {
			t.Fatal(err)
		}
		p.Settings.Selections[shot.ID] = AdvertisingSelection{VideoSubmissionID: shot.ID}
	}
	audioPath := filepath.Join(dir, "voice.wav")
	if output, err := exec.Command("ffmpeg", "-v", "error", "-y", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000", "-t", "1", audioPath).CombinedOutput(); err != nil {
		t.Fatalf("audio fixture: %v %s", err, output)
	}
	audioStat, err := os.Stat(audioPath)
	if err != nil {
		t.Fatal(err)
	}
	audioResource := model.Resource{ID: "voice", UserID: "owner", Status: model.ResourceStatusReady, Provider: "local", ObjectKey: "voice.wav", MimeType: "audio/wav", Size: audioStat.Size(), DurationMs: 4000}
	voiceTask := model.Task{ID: "voice-task", UserID: "owner", ProjectID: ad.Project.ID, Type: "canvas_audio", Status: model.TaskStatusSucceeded, ResultJSON: `{"storageKey":"resource:voice"}`}
	voice := model.AdvertisingSubmission{ID: "voice-submission", ProductionID: p.Production.ID, ShotID: draft.Shots[0].ID, Kind: "audio", ClientOperationID: "voice-operation", TaskID: &voiceTask.ID, ApprovedAt: &now}
	for _, row := range []any{&audioResource, &voiceTask, &voice} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := s.repo.ReplaceAdvertisingReferences(ad.Project.ID, "candidate:"+voice.ID, []string{audioResource.ID}); err != nil {
		t.Fatal(err)
	}
	selection := p.Settings.Selections[draft.Shots[0].ID]
	selection.AudioSubmissionID = voice.ID
	p.Settings.Selections[draft.Shots[0].ID] = selection
	p, err = s.SaveAdvertisingProductionSettings("owner", ad.Project.ID, p.Production.ID, 1, p.Settings)
	if err != nil {
		t.Fatal(err)
	}
	key := "20000000-0000-4000-8000-000000000001"
	var wg sync.WaitGroup
	rows := make(chan AdvertisingAssemblyDetail, 2)
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			row, err := s.CreateAdvertisingAssembly("owner", ad.Project.ID, p.Production.ID, p.Production.Revision, key, "pad")
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
	var assembled AdvertisingAssemblyDetail
	for row := range rows {
		if assembled.ID != "" && assembled.ID != row.ID {
			t.Fatal("duplicate assembly")
		}
		assembled = row
	}
	if assembled.CanvasID == nil {
		t.Fatal("missing canvas")
	}
	canvas, err := s.repo.CanvasProjectForUser("owner", *assembled.CanvasID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.validateCanvasMediaAssets("owner", []byte(canvas.PayloadJSON)); err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(canvas.PayloadJSON), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["timeline"] == nil {
		t.Fatal("canvas missing timeline")
	}
	t.Run("frontend-restoration", func(t *testing.T) {
		if _, err := exec.LookPath("bun"); err != nil {
			t.Skip("bun unavailable; frontend restoration not verified")
		}
		raw, err := s.UserCanvasProject("owner", canvas.ID)
		if err != nil {
			t.Fatal(err)
		}
		script := `import assert from "node:assert/strict";
import { parseCanvasStorageDocument } from "./src/lib/canvas/canvas-storage-revision";
import { resolveTimelineRenderSnapshot } from "./src/lib/timeline/timeline-render-snapshot";
const input = JSON.parse(await Bun.stdin.text());
const project = parseCanvasStorageDocument(JSON.stringify({state:{projects:[input]}})).state.projects[0];
assert.equal(project.timeline.durationMs, 15000);
assert.deepEqual(project.nodes, []);
const result = resolveTimelineRenderSnapshot(project.timeline, () => { throw Error("missing direct media"); });
assert.equal(result.clips.length, 4);
assert(result.clips.filter(c => c.kind === "video").every(c => c.volume === 0 && c.sourceDurationMs === 5000 && c.directMedia.storageKey === "resource:video"));
assert.equal(result.clips.find(c => c.kind === "audio").durationMs, 1000);
assert(project.timeline.clips.every(c => c.directMedia.assetId));
`
		cmd := exec.Command("bun", "--eval", script)
		cmd.Dir = "../../../web"
		cmd.Stdin = strings.NewReader(string(raw))
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("frontend snapshot: %v %s", err, output)
		}
	})
	var parsed renderProject
	timeline, _ := json.Marshal(assembled.Timeline)
	if err := json.Unmarshal(timeline, &parsed); err != nil {
		t.Fatal(err)
	}
	for _, clip := range parsed.Clips {
		if clip.Kind == "audio" && (clip.DurationMs != 1000 || clip.SourceDurationMs != 1000) {
			t.Fatalf("trusted audio metadata: %+v", clip)
		}
		if clip.Kind == "video" && clip.SourceDurationMs != 5000 {
			t.Fatalf("trusted stored duration: %+v", clip)
		}
	}
	payload["title"] = "手改画布"
	edited, _ := json.Marshal(payload)
	if err := db.Model(&model.CanvasProject{}).Where("id = ?", canvas.ID).Updates(map[string]any{"title": "手改画布", "payload_json": string(edited)}).Error; err != nil {
		t.Fatal(err)
	}
	replay, err := s.CreateAdvertisingAssembly("owner", ad.Project.ID, p.Production.ID, p.Production.Revision, key, "pad")
	if err != nil || replay.ID != assembled.ID {
		t.Fatalf("replay: %+v %v", replay, err)
	}
	canvas, _ = s.repo.CanvasProjectForUser("owner", canvas.ID)
	if canvas.Title != "手改画布" {
		t.Fatal("replay overwrote edits")
	}
	if _, err := s.CreateAdvertisingAssembly("owner", ad.Project.ID, p.Production.ID, p.Production.Revision, key, "crop"); err == nil {
		t.Fatal("reused key changed input")
	}
	if _, err := s.GetAdvertisingAssembly("other", ad.Project.ID, p.Production.ID, assembled.ID); err == nil {
		t.Fatal("cross user")
	}
	next, err := s.CreateAdvertisingAssembly("owner", ad.Project.ID, p.Production.ID, p.Production.Revision, "20000000-0000-4000-8000-000000000002", "crop")
	if err != nil || next.CanvasID == nil || *next.CanvasID == *assembled.CanvasID {
		t.Fatalf("new assembly: %+v %v", next, err)
	}
	list, err := s.ListAdvertisingAssemblies("owner", ad.Project.ID, p.Production.ID, 1, 20)
	if err != nil || list.Total != 2 {
		t.Fatalf("list: %+v %v", list, err)
	}
	if err := db.Exec("CREATE TRIGGER fail_assembly BEFORE INSERT ON advertising_assemblies BEGIN SELECT RAISE(ABORT, 'assembly registration failed'); END").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateAdvertisingAssembly("owner", ad.Project.ID, p.Production.ID, p.Production.Revision, "20000000-0000-4000-8000-000000000004", "pad"); err == nil {
		t.Fatal("expected assembly insert failure")
	}
	var canvasCount int64
	if err := db.Model(&model.CanvasProject{}).Where("project_id = ?", ad.Project.ID).Count(&canvasCount).Error; err != nil || canvasCount != 2 {
		t.Fatalf("partial canvas committed: %d %v", canvasCount, err)
	}
	if err := db.Exec("DROP TRIGGER fail_assembly").Error; err != nil {
		t.Fatal(err)
	}
	// Container duration must not let a one-second video pass as a five-second shot.
	if output, err := exec.Command("ffmpeg", "-v", "error", "-y", "-f", "lavfi", "-i", "testsrc2=size=160x284:rate=30:duration=1", "-f", "lavfi", "-i", "sine=duration=5", "-c:v", "libx264", "-c:a", "aac", "-movflags", "+faststart", videoPath).CombinedOutput(); err != nil {
		t.Fatalf("mismatched streams: %v %s", err, output)
	}
	if _, err := s.CreateAdvertisingAssembly("owner", ad.Project.ID, p.Production.ID, p.Production.Revision, "20000000-0000-4000-8000-000000000005", "pad"); err == nil {
		t.Fatal("short video accepted using longer audio/container duration")
	}
	if err := db.Model(&model.Resource{}).Where("id = ?", resource.ID).Update("object_key", "missing.mp4").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateAdvertisingAssembly("owner", ad.Project.ID, p.Production.ID, p.Production.Revision, "20000000-0000-4000-8000-000000000003", "pad"); err == nil {
		t.Fatal("assembled unreadable media")
	}
}
