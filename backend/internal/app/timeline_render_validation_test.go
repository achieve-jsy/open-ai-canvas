package app

import (
	"math"
	"strings"
	"testing"

	"yingce/backend/internal/model"
)

func enhancedTestProject() renderProject {
	p := renderTestProject("resource:res-own")
	p.Output = &renderOutput{Width: 320, Height: 568, Fit: "pad", BurnSubtitles: true}
	p.Tracks = append(p.Tracks, renderTrack{ID: "text", Kind: "text"})
	p.Clips = append(p.Clips, renderClip{ID: "text", Kind: "text", TrackID: "text", DurationMs: 1000, Text: "立即购买", Overlay: &renderOverlay{X: .1, Y: .1, Width: .8, Height: .2}})
	return p
}

func TestValidateEnhancedRenderProject(t *testing.T) {
	if err := validateEnhancedRenderProject(enhancedTestProject()); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*renderProject){
		"legacy size bypass": func(p *renderProject) { p.Output = nil; p.Width = 4096; p.Height = 4096 },
		"legacy fit bypass":  func(p *renderProject) { p.Output = nil; p.Fit = "bad" },
		"negative start":     func(p *renderProject) { p.Clips[0].StartMs = -1 },
		"zero duration":      func(p *renderProject) { p.Clips[0].DurationMs = 0 },
		"source offset":      func(p *renderProject) { p.Clips[0].SourceStartMs = -1 },
		"overflow duration":  func(p *renderProject) { p.Clips[0].DurationMs = math.MaxInt64 },
		"nan":                func(p *renderProject) { p.Clips[1].Overlay.X = math.NaN() },
		"infinite":           func(p *renderProject) { p.Clips[1].Overlay.Y = math.Inf(1) },
		"offscreen":          func(p *renderProject) { p.Clips[1].Overlay.X = .9 },
		"fontsize":           func(p *renderProject) { v := .2; p.Clips[1].Overlay.FontSize = &v },
		"opacity":            func(p *renderProject) { v := 2.; p.Clips[1].Overlay.Opacity = &v },
		"color":              func(p *renderProject) { p.Clips[1].Overlay.Color = "white;bad" },
		"align":              func(p *renderProject) { p.Clips[1].Overlay.Align = "bad" },
		"text length":        func(p *renderProject) { p.Clips[1].Text = strings.Repeat("中", 201) },
		"text lines":         func(p *renderProject) { p.Clips[1].Text = "a\nb\nc" },
		"duration cap":       func(p *renderProject) { p.DurationMs = 60001 },
		"clip cap": func(p *renderProject) {
			for range 32 {
				c := p.Clips[1]
				c.ID = newID()
				p.Clips = append(p.Clips, c)
			}
		},
		"overlay cap": func(p *renderProject) {
			for range 16 {
				c := p.Clips[1]
				c.ID = newID()
				p.Clips = append(p.Clips, c)
			}
		},
		"overlap":          func(p *renderProject) { c := p.Clips[0]; c.ID = "other"; p.Clips = append(p.Clips, c) },
		"duplicate clip":   func(p *renderProject) { p.Clips[1].ID = p.Clips[0].ID },
		"duplicate track":  func(p *renderProject) { p.Tracks = append(p.Tracks, p.Tracks[0]) },
		"unknown track":    func(p *renderProject) { p.Clips[0].TrackID = "missing" },
		"audio overlay":    func(p *renderProject) { p.Clips[1].Kind = "audio" },
		"odd size":         func(p *renderProject) { p.Output.Width = 321 },
		"conflicting size": func(p *renderProject) { p.Width = 640 },
		"unknown fit":      func(p *renderProject) { p.Output.Fit = "bad" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			p := enhancedTestProject()
			mutate(&p)
			if err := validateEnhancedRenderProject(p); err == nil {
				t.Fatal("accepted invalid project")
			}
		})
	}
	legacy := renderTestProject("resource:res-own")
	legacy.DurationMs = 120000
	legacy.Clips[0].DurationMs = 120000
	if err := validateEnhancedRenderProject(legacy); err != nil {
		t.Fatal("legacy duration", err)
	}
}

func TestTimelineRenderAccess(t *testing.T) {
	svc, db := newTimelineTaskTestService(t)
	seedResource(t, db, "res-own", "usr-render-test", "video/mp4")
	seedResource(t, db, "res-foreign", "other", "video/mp4")
	seedResource(t, db, "res-image", "usr-render-test", "image/png")
	if err := db.Create(&model.Project{ID: "project-foreign", UserID: "other", Name: "foreign"}).Error; err != nil {
		t.Fatal(err)
	}
	for _, req := range []TimelineRenderCreateRequest{
		{Timeline: renderTestProject("resource:res-foreign")},
		{ProjectID: "project-foreign", Timeline: renderTestProject("resource:res-own")},
		{Timeline: renderTestProject("resource:res-image")},
		{Timeline: renderTestProject("resource:missing")},
	} {
		if _, err := svc.CreateTimelineRenderTask("usr-render-test", req); err == nil {
			t.Fatal("accepted unauthorized or invalid media")
		}
	}
	var count int64
	db.Model(&model.Task{}).Count(&count)
	if count != 0 {
		t.Fatalf("created %d rejected tasks", count)
	}
	if _, err := svc.CreateTimelineRenderTask("usr-render-test", TimelineRenderCreateRequest{Timeline: renderTestProject("resource:res-own")}); err != nil {
		t.Fatal(err)
	}
}
