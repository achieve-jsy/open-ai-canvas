package app

import (
	"encoding/json"
	"strings"
	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
)

// Only frozen render inputs belong here, never task input or storage URLs.
// ponytail: storage relocation invalidates the existing resource signature; content hashes only if relocation-preserving review becomes required.
type AdvertisingSourceSettings struct {
	Selections      map[string]AdvertisingSelection `json:"selections"`
	MusicResourceID string                          `json:"musicResourceId"`
	MusicVolume     float64                         `json:"musicVolume"`
	Fit             string                          `json:"fit"`
}
type AdvertisingCutSource struct {
	Key        string `json:"key"`
	TaskID     string `json:"taskId,omitempty"`
	ResourceID string `json:"resourceId"`
	Signature  string `json:"signature"`
	DurationMs int64  `json:"durationMs"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
}
type AdvertisingCutProvenance struct {
	SourceSettings AdvertisingSourceSettings `json:"sourceSettings"`
	Sources        []AdvertisingCutSource    `json:"sources"`
	Fingerprint    string                    `json:"fingerprint"`
}

func advertisingCutSettings(snapshot AdvertisingRevisionDetail, settings AdvertisingProductionSettings, fit string) AdvertisingSourceSettings {
	source := AdvertisingSourceSettings{Selections: map[string]AdvertisingSelection{}, MusicResourceID: settings.MusicResourceID, Fit: fit}
	if source.Fit == "" {
		source.Fit = "pad"
	}
	if source.MusicResourceID != "" {
		source.MusicVolume = .15
		if settings.MusicVolume != nil {
			source.MusicVolume = *settings.MusicVolume
		}
	}
	for _, shot := range snapshot.Snapshot.Draft.Shots {
		selection := settings.Selections[shot.ID]
		if strings.TrimSpace(shot.Voiceover) == "" {
			selection.AudioSubmissionID = ""
		}
		source.Selections[shot.ID] = selection
	}
	return source
}
func advertisingCutProvenance(snapshot AdvertisingRevisionDetail, settings AdvertisingProductionSettings, media map[string]AdvertisingProbedMedia, fit string) (AdvertisingCutProvenance, error) {
	p := AdvertisingCutProvenance{SourceSettings: advertisingCutSettings(snapshot, settings, fit)}
	timeline, err := buildAdvertisingTimeline(snapshot, settings, media, fit)
	if err != nil {
		return p, err
	}
	keys := []string{}
	for _, shot := range snapshot.Snapshot.Draft.Shots {
		selection := p.SourceSettings.Selections[shot.ID]
		keys = append(keys, selection.VideoSubmissionID)
		if selection.AudioSubmissionID != "" {
			keys = append(keys, selection.AudioSubmissionID)
		}
	}
	if settings.MusicResourceID != "" {
		keys = append(keys, settings.MusicResourceID)
	}
	if id := snapshot.Snapshot.Draft.Brand.LogoResourceID; id != "" {
		keys = append(keys, id)
	}
	for _, key := range keys {
		m := media[key]
		p.Sources = append(p.Sources, AdvertisingCutSource{Key: key, TaskID: m.TaskID, ResourceID: m.Resource.ID, Signature: cloudAgentResourceSignature(&m.Resource), DurationMs: m.DurationMs, Width: m.Width, Height: m.Height})
	}
	p.Fingerprint = creationHash(map[string]any{"settings": p.SourceSettings, "sources": p.Sources, "timeline": timeline})
	return p, nil
}
func (s *Service) advertisingCutCurrent(repo *repository.Repository, user string, current AdvertisingProductionDetail, row *model.AdvertisingAssembly) (bool, string, error) {
	if row.ProvenanceJSON == nil || row.SourceFingerprint == nil {
		return false, "历史组装来源不完整，当前性未知", nil
	}
	var frozen AdvertisingCutProvenance
	if err := json.Unmarshal([]byte(*row.ProvenanceJSON), &frozen); err != nil {
		return false, "", err
	}
	source := advertisingCutSettings(current.ConfirmedSnapshot, current.Settings, current.Settings.Fit)
	if creationHash(source) != creationHash(frozen.SourceSettings) {
		return false, "选用、裁剪、音乐或画幅适配已变化", nil
	}
	media := map[string]AdvertisingProbedMedia{}
	for _, source := range frozen.Sources {
		resource, err := repo.ResourceForUser(user, source.ResourceID)
		if err != nil || resource.Status != model.ResourceStatusReady || cloudAgentResourceSignature(resource) != source.Signature {
			return false, "来源素材已变化或不可用", nil
		}
		if err := s.advertisingResourceReadable(user, resource); err != nil {
			return false, "来源素材当前不可读取", nil
		}
		if source.TaskID != "" {
			sub, err := repo.AdvertisingSubmission(current.Production.ID, source.Key)
			if err != nil || sub.TaskID == nil || *sub.TaskID != source.TaskID {
				return false, "采用的任务已变化", nil
			}
			selected, err := s.getAdvertisingCandidateResource(repo, user, current.Production.ID, sub.ShotID, sub.Kind, sub.ID)
			if err != nil || selected.ID != source.ResourceID {
				return false, "采用的任务产物已变化或不可用", nil
			}
		}
		media[source.Key] = AdvertisingProbedMedia{Resource: *resource, TaskID: source.TaskID, DurationMs: source.DurationMs, Width: source.Width, Height: source.Height}
	}
	next, err := advertisingCutProvenance(current.ConfirmedSnapshot, current.Settings, media, current.Settings.Fit)
	if err != nil {
		return false, "当前制作不满足组装条件", nil
	}
	if next.Fingerprint != *row.SourceFingerprint {
		return false, "实际成片依赖已变化", nil
	}
	return true, "", nil
}
