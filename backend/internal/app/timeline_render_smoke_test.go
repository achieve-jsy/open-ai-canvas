package app

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"yingce/backend/internal/model"
)

func TestTimelineRenderSmoke(t *testing.T) {
	for _, tool := range []string{"ffmpeg", "ffprobe", "fc-match"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatal(err)
		}
	}
	svc, db := newTimelineTaskTestService(t)
	svc.dataDir = t.TempDir()
	user := "usr-render-test"
	src := filepath.Join(t.TempDir(), "source.mp4")
	args := []string{"-y", "-f", "lavfi", "-i", "color=c=black:s=320x568:r=30", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=44100", "-t", "3", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", src}
	if out, err := exec.Command("ffmpeg", args...).CombinedOutput(); err != nil {
		t.Fatalf("source: %v %s", err, out)
	}
	f, err := os.Open(src)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	stat, _ := f.Stat()
	video, _, err := svc.storeResource(user, "media", "source.mp4", "video/mp4", stat.Size(), 320, 568, 3000, f, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewNRGBA(image.Rect(0, 0, 40, 40))
	for y := 8; y < 32; y++ {
		for x := 8; x < 32; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: 255, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	logo, _, err := svc.storeResource(user, "media", "logo.png", "image/png", int64(buf.Len()), 40, 40, 0, bytes.NewReader(buf.Bytes()), nil, false)
	if err != nil {
		t.Fatal(err)
	}
	p := enhancedTestProject()
	p.DurationMs = 3000
	p.Clips[0].DurationMs = 3000
	p.Clips[0].DirectMedia.StorageKey = "resource:" + video.ID
	p.Clips[1].DurationMs = 3000
	p.Clips[1].Text = "价格 ¥99: 50%" + string(rune(10)) + `{\fs100}\N`
	p.Clips[1].Overlay.X = .05
	p.Clips[1].Overlay.Width = .9
	p.Tracks = append(p.Tracks, renderTrack{ID: "logo", Kind: "image"}, renderTrack{ID: "sub", Kind: "subtitle"})
	lc := renderClipFixture("logo", "image", "logo", 1000, 1000, "resource:"+logo.ID)
	lc.Overlay = &renderOverlay{X: .4, Y: .4, Width: .2, Height: .2}
	p.Clips = append(p.Clips, lc, renderClip{ID: "sub", Kind: "subtitle", TrackID: "sub", DurationMs: 3000, Text: "立即购买"})
	task := seedRunningRenderTask(t, db, renderInputJSON(t, p))
	if err := newTaskWorkerCoordinator(svc).processTimelineRender(task, context.Background()); err != nil {
		t.Fatal(err)
	}
	var stored model.Task
	if err := db.First(&stored, "id = ?", task.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != model.TaskStatusSucceeded {
		t.Fatalf("render: %s", stored.Error)
	}
	var result timelineRenderOutput
	if err := json.Unmarshal([]byte(stored.ResultJSON), &result); err != nil {
		t.Fatal(err)
	}
	if !result.SubtitlesBurned || result.OverlayCount != 2 || len(result.Sources) != 2 {
		t.Fatalf("result: %+v", result)
	}
	resource, err := svc.repo.ResourceForUser(user, result.ResourceID)
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(svc.dataDir, "resources", filepath.FromSlash(resource.ObjectKey))
	info, err := probeMediaFile(context.Background(), output)
	if err != nil {
		t.Fatal(err)
	}
	if info.Width != 320 || info.Height != 568 || math.Abs(float64(info.DurationMs-3000)) > 100 {
		t.Fatalf("probe: %+v", info)
	}
	audio, err := probeHasAudioStream(context.Background(), output)
	if err != nil || !audio {
		t.Fatal("missing audio", err)
	}
	if mediaProbeTag(info.Tags, "AIGC") == "" {
		t.Fatal("missing AIGC")
	}
	for _, at := range []string{"0.5", "1.5", "2.5"} {
		frame, err := exec.Command("ffmpeg", "-v", "error", "-ss", at, "-i", output, "-frames:v", "1", "-f", "image2pipe", "-vcodec", "png", "pipe:1").Output()
		if err != nil {
			t.Fatal(err)
		}
		im, err := png.Decode(bytes.NewReader(frame))
		if err != nil {
			t.Fatal(err)
		}
		r, g, b, _ := im.At(160, 284).RGBA()
		red := r > 30000 && g < 10000 && b < 10000
		if red != (at == "1.5") {
			t.Fatalf("logo interval %s: %d %d %d", at, r, g, b)
		}
		visible := func(rect image.Rectangle) bool {
			for y := rect.Min.Y; y < rect.Max.Y; y++ {
				for x := rect.Min.X; x < rect.Max.X; x++ {
					r, g, b, _ := im.At(x, y).RGBA()
					if r > 10000 && g > 10000 && b > 10000 {
						return true
					}
				}
			}
			return false
		}
		if !visible(image.Rect(0, 45, 320, 115)) || !visible(image.Rect(0, 480, 320, 550)) {
			t.Fatal("missing text/subtitle pixels")
		}
		if dest := os.Getenv("CANVAS_RENDER_SMOKE_OUTPUT"); dest != "" && at == "1.5" {
			if err := os.MkdirAll(dest, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dest, "frame.png"), frame, 0600); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dest, "render.mp4"), data, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
}
