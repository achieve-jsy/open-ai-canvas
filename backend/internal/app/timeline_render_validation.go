package app

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"yingce/backend/internal/model"
)

func enhancedRenderProject(p renderProject) bool {
	if p.Output != nil {
		return true
	}
	for _, c := range p.Clips {
		if c.Overlay != nil || c.Kind == "text" {
			return true
		}
	}
	return false
}

var renderHexColor = regexp.MustCompile("^#[0-9a-fA-F]{6}$")

func validateEnhancedRenderProject(p renderProject) error {
	if !enhancedRenderProject(p) {
		return nil
	}
	invalid := func(s string) error { return BadAuthRequest("时间线参数无效：" + s) }
	if p.Version != 2 || p.DurationMs <= 0 || p.DurationMs > 60000 {
		return invalid("版本或时长超出范围")
	}
	if o := p.Output; o != nil {
		if o.Width < 16 || o.Height < 16 || o.Width > 1920 || o.Height > 1920 || o.Width%2 != 0 || o.Height%2 != 0 {
			return invalid("输出尺寸须为16–1920的偶数")
		}
		if o.Fit != "auto" && o.Fit != "pad" && o.Fit != "crop" {
			return invalid("画面适配模式")
		}
		if p.Width != 0 && p.Width != o.Width || p.Height != 0 && p.Height != o.Height || p.Fit != "" && p.Fit != o.Fit {
			return invalid("输出配置冲突")
		}
	}
	tracks := map[string]renderTrack{}
	for _, tr := range p.Tracks {
		if tr.ID == "" {
			return invalid("轨道ID为空")
		}
		if _, ok := tracks[tr.ID]; ok {
			return invalid("重复轨道")
		}
		tracks[tr.ID] = tr
	}
	seen := map[string]bool{}
	count, layers := 0, 0
	var main []renderClip
	finite := func(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
	for _, c := range p.Clips {
		if c.ID == "" || seen[c.ID] {
			return invalid("重复或空片段ID")
		}
		seen[c.ID] = true
		tr, ok := tracks[c.TrackID]
		if !ok {
			return invalid("未知轨道")
		}
		if tr.Visible != nil && !*tr.Visible {
			continue
		}
		count++
		if c.StartMs < 0 || c.DurationMs <= 0 || c.StartMs > p.DurationMs || c.DurationMs > p.DurationMs-c.StartMs || c.SourceStartMs < 0 || c.SourceStartMs > math.MaxInt64-c.DurationMs {
			return invalid("片段时间")
		}
		if !finite(c.Volume) || c.Volume > 10 || c.Volume < -1 {
			return invalid("音量")
		}
		switch c.Kind {
		case "video", "audio", "image", "text", "subtitle":
		default:
			return invalid("未知片段类型")
		}
		if c.Kind == "text" || c.Kind == "subtitle" {
			if utf8.RuneCountInString(c.Text) > 200 || strings.Count(c.Text, "\n") > 1 || strings.ContainsRune(c.Text, 0) {
				return invalid("文本长度或行数")
			}
		}
		if c.Overlay != nil && c.Kind != "image" && c.Kind != "text" {
			return invalid("叠加仅支持图片或文字")
		}
		if c.Kind == "text" || c.Overlay != nil {
			layers++
		}
		if o := c.Overlay; o != nil {
			for _, v := range []float64{o.X, o.Y, o.Width, o.Height} {
				if !finite(v) {
					return invalid("叠加位置")
				}
			}
			if o.X < 0 || o.Y < 0 || o.Width <= 0 || o.Height <= 0 || o.X+o.Width > 1 || o.Y+o.Height > 1 {
				return invalid("叠加超出画面")
			}
			if o.Opacity != nil && (!finite(*o.Opacity) || *o.Opacity < 0 || *o.Opacity > 1) {
				return invalid("透明度")
			}
			if o.FontSize != nil && (!finite(*o.FontSize) || *o.FontSize < .01 || *o.FontSize > .1) {
				return invalid("字号")
			}
			if o.Color != "" && !renderHexColor.MatchString(o.Color) || o.Background != "" && !renderHexColor.MatchString(o.Background) {
				return invalid("颜色")
			}
			if o.Align != "" && o.Align != "left" && o.Align != "center" && o.Align != "right" {
				return invalid("对齐")
			}
		}
		if c.Kind == "video" || c.Kind == "image" && c.Overlay == nil {
			main = append(main, c)
		}
	}
	if count > 32 || layers > 16 {
		return invalid("片段或叠加数量超限")
	}
	if len(main) == 0 {
		return BadAuthRequest("时间线没有可渲染的媒体片段")
	}
	sort.Slice(main, func(i, j int) bool { return main[i].StartMs < main[j].StartMs })
	var end int64
	for _, c := range main {
		if c.StartMs < end {
			return invalid("主画面片段不能重叠")
		}
		end = c.StartMs + c.DurationMs
	}
	return nil
}

func (s *Service) validateTimelineRenderAccess(userID, projectID string, p renderProject) error {
	if strings.TrimSpace(userID) == "" {
		return BadAuthRequest("缺少当前用户")
	}
	if projectID = strings.TrimSpace(projectID); projectID != "" {
		if _, err := s.repo.ProjectForUser(userID, projectID); err != nil {
			return BadAuthRequest("无法读取时间线所属项目")
		}
	}
	tracks := map[string]renderTrack{}
	for _, tr := range p.Tracks {
		tracks[tr.ID] = tr
	}
	for _, c := range p.Clips {
		tr, ok := tracks[c.TrackID]
		if !ok {
			return BadAuthRequest("时间线引用了不存在的轨道")
		}
		if tr.Visible != nil && !*tr.Visible {
			continue
		}
		if c.Kind != "video" && c.Kind != "audio" && c.Kind != "image" {
			continue
		}
		id, ok := mediaResourceID(c)
		if !ok {
			return BadAuthRequest("请先上传时间线引用的媒体")
		}
		resource, err := s.repo.ResourceForUser(userID, id)
		if err != nil || resource.Status != model.ResourceStatusReady {
			return BadAuthRequest("无法读取时间线引用的媒体")
		}
		if !strings.HasPrefix(strings.ToLower(resource.MimeType), c.Kind+"/") {
			return BadAuthRequest(fmt.Sprintf("时间线引用的媒体类型与%s片段不匹配", c.Kind))
		}
	}
	return nil
}
