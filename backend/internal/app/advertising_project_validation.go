package app

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

type AdvertisingBrief struct {
	ProductName   string   `json:"productName"`
	Description   string   `json:"description"`
	Audience      string   `json:"audience"`
	SellingPoints []string `json:"sellingPoints"`
	CTA           string   `json:"cta"`
	DurationMs    int64    `json:"durationMs"`
}
type AdvertisingBrand struct {
	Name           string `json:"name"`
	PrimaryColor   string `json:"primaryColor"`
	LogoResourceID string `json:"logoResourceId"`
}
type AdvertisingShot struct {
	ID                   string   `json:"id"`
	DurationMs           int64    `json:"durationMs"`
	Visual               string   `json:"visual"`
	Voiceover            string   `json:"voiceover"`
	OnScreenText         string   `json:"onScreenText"`
	ReferenceResourceIDs []string `json:"referenceResourceIds"`
}
type AdvertisingDraft struct {
	SchemaVersion         int               `json:"schemaVersion"`
	Brief                 AdvertisingBrief  `json:"brief"`
	Brand                 AdvertisingBrand  `json:"brand"`
	ProductResourceIDs    []string          `json:"productResourceIds"`
	AdditionalResourceIDs []string          `json:"additionalResourceIds"`
	Shots                 []AdvertisingShot `json:"shots"`
}

var advertisingID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,36}$`)
var advertisingShotID = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var advertisingColor = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

func normalizeAdvertisingDraft(d AdvertisingDraft) AdvertisingDraft {
	if d.Brief.SellingPoints == nil {
		d.Brief.SellingPoints = []string{}
	}
	d.ProductResourceIDs = advertisingUniqueIDs(d.ProductResourceIDs)
	d.AdditionalResourceIDs = advertisingUniqueIDs(d.AdditionalResourceIDs)
	d.Shots = slices.Clone(d.Shots)
	if d.Shots == nil {
		d.Shots = []AdvertisingShot{}
	}
	for i := range d.Shots {
		d.Shots[i].ReferenceResourceIDs = advertisingUniqueIDs(d.Shots[i].ReferenceResourceIDs)
	}
	return d
}
func advertisingUniqueIDs(ids []string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}
func advertisingText(field, value string, max int, required bool) error {
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) > max || strings.ContainsRune(value, 0) || (required && strings.TrimSpace(value) == "") {
		return fmt.Errorf("%s: 必填内容或长度不符合要求（最多%d字）", field, max)
	}
	return nil
}
func validateAdvertisingDraft(d AdvertisingDraft, confirm bool) error {
	if d.SchemaVersion != 1 {
		return fmt.Errorf("schemaVersion: 仅支持1")
	}
	for _, f := range []struct {
		name, value string
		max         int
		required    bool
	}{
		{"brief.productName", d.Brief.ProductName, 120, confirm}, {"brief.description", d.Brief.Description, 2000, false}, {"brief.audience", d.Brief.Audience, 300, confirm}, {"brief.cta", d.Brief.CTA, 80, confirm}, {"brand.name", d.Brand.Name, 120, false},
	} {
		if err := advertisingText(f.name, f.value, f.max, f.required); err != nil {
			return err
		}
	}
	if d.Brief.DurationMs < 15000 || d.Brief.DurationMs > 30000 {
		return fmt.Errorf("brief.durationMs: 必须为15000–30000毫秒")
	}
	if len(d.Brief.SellingPoints) > 5 || (confirm && len(d.Brief.SellingPoints) == 0) {
		return fmt.Errorf("brief.sellingPoints: 确认时需要1–5条")
	}
	for _, point := range d.Brief.SellingPoints {
		if err := advertisingText("brief.sellingPoints", point, 120, true); err != nil {
			return err
		}
	}
	if d.Brand.PrimaryColor != "" && !advertisingColor.MatchString(d.Brand.PrimaryColor) {
		return fmt.Errorf("brand.primaryColor: 必须为#RRGGBB")
	}
	if d.Brand.LogoResourceID != "" && !advertisingID.MatchString(d.Brand.LogoResourceID) {
		return fmt.Errorf("brand.logoResourceId: 无效资源ID")
	}
	if len(d.ProductResourceIDs) > 10 || len(d.AdditionalResourceIDs) > 20 || (confirm && len(d.ProductResourceIDs) == 0) {
		return fmt.Errorf("productResourceIds/additionalResourceIds: 超出数量限制或缺少商品图")
	}
	pool := map[string]bool{}
	for _, ids := range [][]string{d.ProductResourceIDs, d.AdditionalResourceIDs} {
		for _, id := range ids {
			if !advertisingID.MatchString(id) {
				return fmt.Errorf("resourceIds: 无效资源ID")
			}
			pool[id] = true
		}
	}
	if len(d.Shots) > 5 || (confirm && len(d.Shots) < 3) {
		return fmt.Errorf("shots: 确认时需要3–5镜头")
	}
	seen := map[string]bool{}
	var duration int64
	for i, shot := range d.Shots {
		path := fmt.Sprintf("shots[%d]", i)
		if !advertisingShotID.MatchString(shot.ID) || seen[strings.ToLower(shot.ID)] {
			return fmt.Errorf("%s.id: 需要唯一UUID", path)
		}
		seen[strings.ToLower(shot.ID)] = true
		if shot.DurationMs < 1000 || shot.DurationMs > 15000 {
			return fmt.Errorf("%s.durationMs: 必须为1000–15000毫秒", path)
		}
		duration += shot.DurationMs
		for _, f := range []struct {
			name, value string
			max         int
			required    bool
		}{{"visual", shot.Visual, 1000, confirm}, {"voiceover", shot.Voiceover, 500, false}, {"onScreenText", shot.OnScreenText, 200, false}} {
			if err := advertisingText(path+"."+f.name, f.value, f.max, f.required); err != nil {
				return err
			}
		}
		if strings.Count(strings.ReplaceAll(shot.OnScreenText, "\r\n", "\n"), "\n") > 1 || strings.Contains(strings.ReplaceAll(shot.OnScreenText, "\r\n", ""), "\r") {
			return fmt.Errorf("%s.onScreenText: 最多两行", path)
		}
		if len(shot.ReferenceResourceIDs) > 5 {
			return fmt.Errorf("%s.referenceResourceIds: 最多5项", path)
		}
		for _, id := range shot.ReferenceResourceIDs {
			if !pool[id] {
				return fmt.Errorf("%s.referenceResourceIds: 不在本稿素材池", path)
			}
		}
	}
	if confirm && duration != d.Brief.DurationMs {
		return fmt.Errorf("shots.durationMs: 镜头总时长须等于目标时长")
	}
	return nil
}
func advertisingResourceIDs(d AdvertisingDraft) []string {
	ids := append(slices.Clone(d.ProductResourceIDs), d.AdditionalResourceIDs...)
	if d.Brand.LogoResourceID != "" {
		ids = append(ids, d.Brand.LogoResourceID)
	}
	return advertisingUniqueIDs(ids)
}
