package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderASSLiteralText(t *testing.T) {
	p := enhancedTestProject()
	p.Clips[1].Text = "价格 ¥99: 50%" + string(rune(10)) + string(rune(92)) + "N{bad}"
	hidden := false
	p.Tracks = append(p.Tracks, renderTrack{ID: "sub", Kind: "subtitle", Visible: &hidden})
	p.Clips = append(p.Clips, renderClip{ID: "sub", Kind: "subtitle", TrackID: "sub", DurationMs: 1000, Text: "隐藏字幕"})
	ass, err := buildRenderASS(p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(ass, "Dialogue:") != 1 || !strings.Contains(ass, "价格 ¥99: 50%") || !strings.Contains(ass, "0:00:00.00,0:00:01.00") || strings.Contains(ass, "隐藏字幕") {
		t.Fatalf("unexpected ASS: %s", ass)
	}
	if strings.Contains(buildRenderSubtitleSRT(p), "隐藏字幕") {
		t.Fatal("hidden subtitle leaked")
	}
	p.Output.BurnSubtitles = false
	p.Tracks[2].Visible = nil
	ass, err = buildRenderASS(p)
	if err != nil || strings.Contains(ass, "隐藏字幕") {
		t.Fatal("burn-disabled subtitle rendered", err)
	}
}

func TestLogoDoesNotExtendTimeline(t *testing.T) {
	p := enhancedTestProject()
	p.DurationMs = 3000
	p.Clips[0].DurationMs = 3000
	p.Tracks = append(p.Tracks, renderTrack{ID: "logo", Kind: "image"})
	logo := renderClipFixture("logo", "image", "logo", 1000, 1000, "resource:logo")
	logo.Overlay = &renderOverlay{X: .8, Y: .05, Width: .15, Height: .1}
	p.Clips = append(p.Clips, logo)
	plan := buildRenderPlan(p)
	if planDurationMs(plan) != 3000 || len(plan.Segments) != 1 || len(plan.ImageOverlays) != 1 {
		t.Fatalf("logo changed sequence: %+v", plan)
	}
	p.Clips[0].DurationMs = 1000
	if planDurationMs(buildRenderPlan(p)) != 3000 {
		t.Fatal("late CTA truncated")
	}
	p.Clips = []renderClip{logo}
	if buildRenderPlan(p).HasMedia {
		t.Fatal("logo alone is not main media")
	}
}

func TestRenderCancelledAndMissingFont(t *testing.T) {
	p := enhancedTestProject()
	plan := buildRenderPlan(p)
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := prepareRenderOverlays(ctx, p, &plan, dir); err == nil {
		t.Fatal("cancel ignored")
	}
	t.Setenv("PATH", t.TempDir())
	if err := prepareRenderOverlays(context.Background(), p, &plan, dir); err == nil {
		t.Fatal("missing tools/font accepted")
	}
	if _, err := os.Stat(filepath.Join(dir, "overlay.ass")); !os.IsNotExist(err) {
		t.Fatal("partial ASS left behind")
	}
}
