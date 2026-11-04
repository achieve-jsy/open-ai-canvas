package app

import (
	"context"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const renderFontFamily = "Noto Sans CJK SC"

func renderOverlayCount(p renderProject) int {
	count := 0
	for _, c := range p.Clips {
		if renderClipVisible(p, c) && (c.Kind == "image" && c.Overlay != nil || c.Kind == "text" && strings.TrimSpace(c.Text) != "") {
			count++
		}
	}
	return count
}

func renderClipVisible(p renderProject, c renderClip) bool {
	for _, tr := range p.Tracks {
		if tr.ID == c.TrackID {
			return tr.Visible == nil || *tr.Visible
		}
	}
	return false
}

func renderTextOverlay(c renderClip) renderOverlay {
	if c.Overlay != nil {
		return *c.Overlay
	}
	return renderOverlay{X: .1, Y: .1, Width: .8, Height: .2}
}

func renderASSColor(color string) string {
	if color == "" {
		color = "#FFFFFF"
	}
	return "&H" + color[5:7] + color[3:5] + color[1:3] + "&"
}

// Break literal backslash commands with a zero-width word joiner, escape braces.
func renderASSLiteral(text string) string {
	return strings.NewReplacer("\\", "\\\u2060", "{", "\\{", "}", "\\}", "\r", "", "\n", "\\N").Replace(text)
}

func renderASSTime(ms int64) string {
	cs := (ms + 5) / 10
	return fmt.Sprintf("%d:%02d:%02d.%02d", cs/360000, cs/6000%60, cs/100%60, cs%100)
}

func buildRenderASS(p renderProject) (string, error) {
	if err := validateEnhancedRenderProject(p); err != nil {
		return "", err
	}
	plan := buildRenderPlan(p)
	width, height := plan.size()
	var clips []renderClip
	orders := map[string]int{}
	for _, tr := range p.Tracks {
		orders[tr.ID] = tr.Order
	}
	for _, c := range p.Clips {
		if !renderClipVisible(p, c) || strings.TrimSpace(c.Text) == "" {
			continue
		}
		if c.Kind == "text" || c.Kind == "subtitle" && p.Output != nil && p.Output.BurnSubtitles {
			clips = append(clips, c)
		}
	}
	if len(clips) == 0 {
		return "", nil
	}
	sort.SliceStable(clips, func(i, j int) bool {
		a, b := clips[i], clips[j]
		if (a.Kind == "subtitle") != (b.Kind == "subtitle") {
			return b.Kind == "subtitle"
		}
		if orders[a.TrackID] != orders[b.TrackID] {
			return orders[a.TrackID] < orders[b.TrackID]
		}
		return a.ID < b.ID
	})
	var b strings.Builder
	fmt.Fprintf(&b, "[Script Info]\nScriptType: v4.00+\nPlayResX: %d\nPlayResY: %d\nWrapStyle: 2\nScaledBorderAndShadow: yes\n\n[V4+ Styles]\nFormat: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding\nStyle: Default,%s,%.0f,&H00FFFFFF,&H00FFFFFF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,0,0,7,0,0,0,1\n\n[Events]\nFormat: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\n", width, height, renderFontFamily, math.Round(float64(height)*.03))
	for i, c := range clips {
		o := renderTextOverlay(c)
		font := .03
		opacity := 1.
		if o.FontSize != nil {
			font = *o.FontSize
		}
		if o.Opacity != nil {
			opacity = *o.Opacity
		}
		x, y := math.Round(o.X*float64(width)), math.Round(o.Y*float64(height))
		w, h := math.Round(o.Width*float64(width)), math.Round(o.Height*float64(height))
		align := 8
		switch o.Align {
		case "left":
			align = 7
		case "right":
			align = 9
		}
		if align == 8 {
			x += w / 2
		}
		if align == 9 {
			x += w
		}
		if c.Kind == "subtitle" {
			align = 2
			x = float64(width) / 2
			y = math.Round(float64(height) * .94)
			font = .03
			o.Color = "#FFFFFF"
			o.Background = ""
			opacity = 1
		}
		alpha := int(math.Round((1 - opacity) * 255))
		if o.Background != "" {
			fmt.Fprintf(&b, "Dialogue: %d,%s,%s,Default,,0,0,0,,{\\an7\\pos(%.0f,%.0f)\\bord0\\shad0\\1c%s\\alpha&H%02X&\\p1}m 0 0 l %.0f 0 %.0f %.0f 0 %.0f{\\p0}\n", i*2, renderASSTime(c.StartMs), renderASSTime(c.StartMs+c.DurationMs), math.Round(o.X*float64(width)), y, renderASSColor(o.Background), alpha, w, w, h, h)
		}
		fmt.Fprintf(&b, "Dialogue: %d,%s,%s,Default,,0,0,0,,{\\an%d\\pos(%.0f,%.0f)\\fs%.0f\\1c%s\\alpha&H%02X&\\q2}%s\n", i*2+1, renderASSTime(c.StartMs), renderASSTime(c.StartMs+c.DurationMs), align, x, y, math.Round(font*float64(height)), renderASSColor(o.Color), alpha, renderASSLiteral(c.Text))
	}
	return b.String(), nil
}

func prepareRenderOverlays(ctx context.Context, p renderProject, plan *renderPlan, workDir string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	ass, err := buildRenderASS(p)
	if err != nil {
		return err
	}
	if ass == "" && len(plan.ImageOverlays) == 0 {
		return nil
	}
	ffmpeg, err := renderFfmpegBinary()
	if err != nil {
		return err
	}
	filters, err := exec.CommandContext(ctx, ffmpeg, "-hide_banner", "-filters").CombinedOutput()
	if err != nil {
		return fmt.Errorf("无法检查FFmpeg滤镜: %w", err)
	}
	has := func(name string) bool {
		for _, line := range strings.Split(string(filters), "\n") {
			fields := strings.Fields(line)
			if len(fields) > 1 && fields[1] == name {
				return true
			}
		}
		return false
	}
	if len(plan.ImageOverlays) > 0 && !has("overlay") {
		return fmt.Errorf("FFmpeg缺少overlay滤镜")
	}
	if ass == "" {
		return nil
	}
	if !has("ass") {
		return fmt.Errorf("FFmpeg缺少libass字幕滤镜")
	}
	family, err := exec.CommandContext(ctx, "fc-match", "-f", "%{family}", renderFontFamily).Output()
	if err != nil || !strings.Contains(string(family), renderFontFamily) {
		return fmt.Errorf("未安装中文字体%s或fontconfig", renderFontFamily)
	}
	if err := os.WriteFile(filepath.Join(workDir, "overlay.ass"), []byte(ass), 0600); err != nil {
		return err
	}
	plan.ASSPath = "overlay.ass"
	return nil
}
