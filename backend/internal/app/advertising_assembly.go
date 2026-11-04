package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"math"
	"sort"
	"strings"
	"time"
	"yingce/backend/internal/assets"
	"yingce/backend/internal/canvas"
	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
)

type AdvertisingProbedMedia struct {
	Resource      model.Resource
	DurationMs    int64
	Width, Height int
}

func buildAdvertisingTimeline(snapshot AdvertisingRevisionDetail, settings AdvertisingProductionSettings, media map[string]AdvertisingProbedMedia, fit string) (map[string]any, error) {
	if fit == "" {
		fit = "pad"
	}
	if fit != "pad" && fit != "crop" {
		return nil, BadAuthRequest("画幅适配仅支持pad或crop")
	}
	volume := 0.15
	if settings.MusicVolume != nil {
		volume = *settings.MusicVolume
	}
	if math.IsNaN(volume) || math.IsInf(volume, 0) || volume < 0 || volume > 1 {
		return nil, BadAuthRequest("背景音乐音量必须为0至1")
	}
	tracks := []map[string]any{{"id": "video-1", "kind": "video", "label": "视频", "order": 0}}
	clips := []map[string]any{}
	addTrack := func(id, kind, label string) {
		tracks = append(tracks, map[string]any{"id": id, "kind": kind, "label": label, "order": len(tracks)})
	}
	addMedia := func(id, kind, track, title string, start, duration, sourceStart int64, volume float64, m AdvertisingProbedMedia) {
		clips = append(clips, map[string]any{"id": id, "nodeId": "media:" + id, "kind": kind, "trackId": track, "title": title, "startMs": start, "durationMs": duration, "sourceStartMs": sourceStart, "sourceDurationMs": m.DurationMs, "volume": volume, "directMedia": map[string]any{"id": m.Resource.ID, "kind": kind, "title": title, "storageKey": "resource:" + m.Resource.ID, "durationMs": m.DurationMs, "width": m.Width, "height": m.Height, "mimeType": m.Resource.MimeType, "bytes": m.Resource.Size}})
	}
	durationError := func(index int, kind string, actual, target int64) error {
		return advertisingConflict("duration_invalid", fmt.Sprintf("镜头%d%s实际%dms，目标%dms，请重新生成或修改脚本", index+1, kind, actual, target))
	}
	var cursor int64
	voiceTrack, textTrack := false, false
	for i, shot := range snapshot.Snapshot.Draft.Shots {
		if shot.DurationMs <= 0 || shot.DurationMs > 30000 || cursor > 30000-shot.DurationMs {
			return nil, BadAuthRequest("广告镜头时长无效")
		}
		selection := settings.Selections[shot.ID]
		video, ok := media[selection.VideoSubmissionID]
		if !ok || video.Resource.ID == "" || !strings.HasPrefix(video.Resource.MimeType, "video/") || video.Width <= 0 || video.Height <= 0 {
			return nil, advertisingCandidateUnavailable()
		}
		if selection.VideoStartMs < 0 || video.DurationMs <= 0 || selection.VideoStartMs > video.DurationMs || video.DurationMs-selection.VideoStartMs < shot.DurationMs {
			return nil, durationError(i, "视频", video.DurationMs, selection.VideoStartMs+shot.DurationMs)
		}
		addMedia("video-"+shot.ID, "video", "video-1", fmt.Sprintf("镜头%d", i+1), cursor, shot.DurationMs, selection.VideoStartMs, 0, video)
		if strings.TrimSpace(shot.Voiceover) != "" {
			audio, ok := media[selection.AudioSubmissionID]
			if !ok || audio.Resource.ID == "" || !strings.HasPrefix(audio.Resource.MimeType, "audio/") {
				return nil, advertisingConflict("candidate_unavailable", fmt.Sprintf("镜头%d尚未采用旁白", i+1))
			}
			if audio.DurationMs <= 0 || audio.DurationMs > shot.DurationMs {
				return nil, durationError(i, "旁白", audio.DurationMs, shot.DurationMs)
			}
			if !voiceTrack {
				addTrack("audio-1", "audio", "旁白")
				voiceTrack = true
			}
			addMedia("audio-"+shot.ID, "audio", "audio-1", shot.Voiceover, cursor, audio.DurationMs, 0, 1, audio)
		}
		if strings.TrimSpace(shot.OnScreenText) != "" {
			if !textTrack {
				addTrack("text-1", "text", "屏幕文字")
				textTrack = true
			}
			clips = append(clips, map[string]any{"id": "text-" + shot.ID, "nodeId": "text-" + shot.ID, "trackId": "text-1", "kind": "text", "text": shot.OnScreenText, "title": shot.OnScreenText, "startMs": cursor, "durationMs": shot.DurationMs, "overlay": map[string]any{"x": 0.08, "y": 0.76, "width": 0.84, "height": 0.16, "fontSize": 0.035, "color": "#FFFFFF", "background": "#000000", "align": "center"}})
		}
		cursor += shot.DurationMs
	}
	if settings.MusicResourceID != "" {
		music, ok := media[settings.MusicResourceID]
		if !ok || music.Resource.ID == "" || !strings.HasPrefix(music.Resource.MimeType, "audio/") || music.DurationMs <= 0 {
			return nil, advertisingConflict("duration_invalid", "背景音乐没有可读取的音轨或有效时长")
		}
		addTrack("music-1", "audio", "背景音乐")
		addMedia("music", "audio", "music-1", "背景音乐", 0, min(cursor, music.DurationMs), 0, volume, music)
	}
	if id := snapshot.Snapshot.Draft.Brand.LogoResourceID; id != "" {
		logo, ok := media[id]
		if !ok || logo.Resource.ID == "" || !strings.HasPrefix(logo.Resource.MimeType, "image/") || logo.Width <= 0 || logo.Height <= 0 {
			return nil, advertisingCandidateUnavailable()
		}
		addTrack("logo-1", "image", "品牌Logo")
		addMedia("logo", "image", "logo-1", "品牌Logo", 0, cursor, 0, 0, logo)
		width := math.Min(0.2, 0.12*1920/1080*float64(logo.Width)/float64(logo.Height))
		height := width * 1080 / 1920 * float64(logo.Height) / float64(logo.Width)
		clips[len(clips)-1]["overlay"] = map[string]any{"x": 0.06, "y": 0.05, "width": width, "height": height}
	}
	project := map[string]any{"version": 2, "output": map[string]any{"width": 1080, "height": 1920, "fit": fit, "burnSubtitles": true}, "tracks": tracks, "clips": clips, "durationMs": cursor}
	raw, err := json.Marshal(project)
	if err != nil {
		return nil, err
	}
	var render renderProject
	if err := json.Unmarshal(raw, &render); err != nil {
		return nil, err
	}
	if err := validateEnhancedRenderProject(render); err != nil {
		return nil, err
	}
	return project, nil
}

type AdvertisingAssemblyDetail struct {
	model.AdvertisingAssembly
	Timeline map[string]any `json:"timeline"`
}

func advertisingAssemblyDetail(row *model.AdvertisingAssembly) (AdvertisingAssemblyDetail, error) {
	result := AdvertisingAssemblyDetail{AdvertisingAssembly: *row}
	err := json.Unmarshal([]byte(row.TimelineJSON), &result.Timeline)
	return result, err
}

func replayAdvertisingAssembly(row *model.AdvertisingAssembly, revision int64, fit string) (AdvertisingAssemblyDetail, error) {
	result, err := advertisingAssemblyDetail(row)
	if err != nil {
		return result, err
	}
	var project renderProject
	if err := json.Unmarshal([]byte(row.TimelineJSON), &project); err != nil {
		return result, err
	}
	if row.SourceProductionRevision != revision || project.Output == nil || project.Output.Fit != fit {
		return AdvertisingAssemblyDetail{}, advertisingConflict("operation_conflict", "同一组装操作键不能更改版本或适配方式")
	}
	return result, nil
}

func (s *Service) GetAdvertisingAssembly(userID, projectID, productionID, id string) (AdvertisingAssemblyDetail, error) {
	if _, err := s.GetAdvertisingProduction(userID, projectID, productionID); err != nil {
		return AdvertisingAssemblyDetail{}, err
	}
	row, err := s.repo.AdvertisingAssembly(productionID, id)
	if err != nil {
		return AdvertisingAssemblyDetail{}, advertisingProductionFailure(err)
	}
	return advertisingAssemblyDetail(row)
}

func (s *Service) ListAdvertisingAssemblies(userID, projectID, productionID string, page, size int) (AdvertisingPage[AdvertisingAssemblyDetail], error) {
	result := AdvertisingPage[AdvertisingAssemblyDetail]{Items: []AdvertisingAssemblyDetail{}, Page: page, PageSize: size}
	if err := advertisingPagination(page, size); err != nil {
		return result, err
	}
	if _, err := s.GetAdvertisingProduction(userID, projectID, productionID); err != nil {
		return result, err
	}
	rows, total, err := s.repo.AdvertisingAssemblies(productionID, page, size)
	if err != nil {
		return result, err
	}
	result.Total = total
	for _, row := range rows {
		item, err := advertisingAssemblyDetail(&row)
		if err != nil {
			return result, err
		}
		result.Items = append(result.Items, item)
	}
	return result, nil
}

func (s *Service) CreateAdvertisingAssembly(userID, projectID, productionID string, expectedRevision int64, operationID, fit string) (AdvertisingAssemblyDetail, error) {
	var result AdvertisingAssemblyDetail
	if _, err := uuid.Parse(operationID); err != nil || len(operationID) != 36 || expectedRevision < 1 {
		return result, BadAuthRequest("操作键必须为UUID且版本为正整数")
	}
	if fit == "" {
		fit = "pad"
	}
	if fit != "pad" && fit != "crop" {
		return result, BadAuthRequest("画幅适配仅支持pad或crop")
	}
	production, err := s.GetAdvertisingProduction(userID, projectID, productionID)
	if err != nil {
		return result, err
	}
	prior, err := s.repo.AdvertisingAssemblyForOperation(productionID, operationID)
	if err == nil {
		return replayAdvertisingAssembly(prior, expectedRevision, fit)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return result, err
	}
	if production.Production.Revision != expectedRevision {
		return result, advertisingProductionFailure(repository.ErrAdvertisingProductionConflict)
	}
	media := map[string]AdvertisingProbedMedia{}
	resources := map[string]model.Resource{}
	probed := map[string]AdvertisingProbedMedia{}
	probe := func(key, kind string, resource *model.Resource) error {
		if resource.UserID != userID || resource.Status != model.ResourceStatusReady || !strings.HasPrefix(resource.MimeType, kind+"/") {
			return advertisingCandidateUnavailable()
		}
		m, ok := probed[resource.ID]
		if !ok {
			info, err := s.probeStoredResourceMedia(context.Background(), resource)
			if err != nil {
				return advertisingConflict("media_probe_failed", fmt.Sprintf("无法探测素材%s：%v", resource.ID, err))
			}
			if kind == "audio" && !info.HasAudio || kind != "audio" && (info.Width <= 0 || info.Height <= 0) {
				return advertisingConflict("media_probe_failed", "素材缺少所需的真实音视频流")
			}
			m = AdvertisingProbedMedia{Resource: *resource, DurationMs: info.DurationMs, Width: info.Width, Height: info.Height}
			probed[resource.ID] = m
		}
		media[key], resources[resource.ID] = m, *resource
		return nil
	}
	for _, shot := range production.ConfirmedSnapshot.Snapshot.Draft.Shots {
		selection := production.Settings.Selections[shot.ID]
		for _, item := range []struct{ kind, id string }{{"video", selection.VideoSubmissionID}, {"audio", selection.AudioSubmissionID}} {
			if item.kind == "audio" && strings.TrimSpace(shot.Voiceover) == "" {
				continue
			}
			resource, err := s.getAdvertisingCandidateResource(s.repo, userID, productionID, shot.ID, item.kind, item.id)
			if err != nil {
				return result, err
			}
			if err := probe(item.id, item.kind, resource); err != nil {
				return result, err
			}
		}
	}
	for _, item := range []struct{ kind, id string }{{"audio", production.Settings.MusicResourceID}, {"image", production.ConfirmedSnapshot.Snapshot.Draft.Brand.LogoResourceID}} {
		if item.id == "" {
			continue
		}
		resource, err := s.repo.ResourceForUser(userID, item.id)
		if err != nil {
			return result, advertisingCandidateUnavailable()
		}
		if err := probe(item.id, item.kind, resource); err != nil {
			return result, err
		}
	}
	timeline, err := buildAdvertisingTimeline(production.ConfirmedSnapshot, production.Settings, media, fit)
	if err != nil {
		return result, err
	}
	signature := creationHash(map[string]any{"revision": expectedRevision, "settings": production.Settings, "media": media, "fit": fit})
	policy, err := s.RuntimePolicy()
	if err != nil {
		return result, err
	}
	s.storageMu.Lock()
	defer s.storageMu.Unlock()
	err = s.repo.AdvertisingProductionTransaction(userID, projectID, productionID, func(repo *repository.Repository) error {
		prior, err := repo.AdvertisingAssemblyForOperation(productionID, operationID)
		if err == nil {
			result, err = replayAdvertisingAssembly(prior, expectedRevision, fit)
			return err
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		current, err := repo.AdvertisingProductionForUser(userID, projectID, productionID)
		if err != nil {
			return err
		}
		if current.Revision != expectedRevision {
			return repository.ErrAdvertisingProductionConflict
		}
		ids := make([]string, 0, len(resources))
		for id, before := range resources {
			after, err := repo.ResourceForUser(userID, id)
			if err != nil || after.Status != model.ResourceStatusReady {
				return advertisingCandidateUnavailable()
			}
			if cloudAgentResourceSignature(&before) != cloudAgentResourceSignature(after) {
				return advertisingConflict("resource_changed", "媒体已变化，请重新组装")
			}
			ids = append(ids, id)
		}
		sort.Strings(ids)
		// ponytail: one account-wide asset scan per assembly; index resource-to-asset mappings if large libraries make this slow.
		existing, err := repo.Assets(userID)
		if err != nil {
			return err
		}
		assetIDs := map[string]string{}
		for _, asset := range existing {
			refs, err := assets.CollectDocumentResourceReferences(asset.PayloadJSON)
			if err != nil {
				return err
			}
			for _, ref := range refs {
				if _, ok := resources[ref.ResourceID]; ok && assetIDs[ref.ResourceID] == "" {
					assetIDs[ref.ResourceID] = asset.ID
				}
			}
		}
		now := time.Now().UTC()
		for _, id := range ids {
			if assetIDs[id] != "" {
				continue
			}
			m := probed[id]
			kind := strings.SplitN(m.Resource.MimeType, "/", 2)[0]
			raw, err := json.Marshal(map[string]any{"id": newID(), "kind": kind, "category": "material", "status": "confirmed", "title": "广告组装素材", "coverUrl": resourceFileURL(id), "source": "广告组装", "tags": []string{}, "createdAt": now, "updatedAt": now, "data": map[string]any{"storageKey": "resource:" + id, "url": resourceFileURL(id), "dataUrl": resourceFileURL(id), "durationMs": m.DurationMs, "width": m.Width, "height": m.Height, "mimeType": m.Resource.MimeType, "bytes": m.Resource.Size}})
			if err != nil {
				return err
			}
			asset, err := canvas.AssetFromJSON(userID, raw)
			if err != nil {
				return err
			}
			usage, err := repo.UserStructuredStorageUsage(userID)
			if err != nil {
				return err
			}
			if err := validateStructuredStorageQuotaWithPolicy(usage, "asset", true, int64(len(raw)), policy.Resource); err != nil {
				return err
			}
			if err := repo.UpsertAsset(&asset); err != nil {
				return err
			}
			assetIDs[id] = asset.ID
		}
		for _, clip := range timeline["clips"].([]map[string]any) {
			if direct, ok := clip["directMedia"].(map[string]any); ok {
				direct["assetId"] = assetIDs[direct["id"].(string)]
			}
		}
		raw, err := json.Marshal(timeline)
		if err != nil {
			return err
		}
		canvasID := newID()
		patch := map[string]json.RawMessage{}
		for k, v := range map[string]any{"title": production.ConfirmedSnapshot.Snapshot.Name + " · 广告组装", "projectId": projectID, "chatSessions": []any{}, "activeChatId": nil, "backgroundMode": "dots", "showImageInfo": true, "previsScenes": []any{}, "timeline": timeline} {
			encoded, err := json.Marshal(v)
			if err != nil {
				return err
			}
			patch[k] = encoded
		}
		canvasRaw, err := json.Marshal(patch)
		if err != nil {
			return err
		}
		usage, err := repo.UserStructuredStorageUsage(userID)
		if err != nil {
			return err
		}
		if err := validateStructuredStorageQuotaWithPolicy(usage, "canvas", true, int64(len(canvasRaw)+len(raw)), policy.Resource); err != nil {
			return err
		}
		_, err = canvas.New(repo, nil).ApplyUserCanvasOperations(userID, canvasID, canvas.CanvasOperationsRequest{MutationID: "advertising-assembly:" + productionID + ":" + operationID, Operations: []canvas.CanvasOperation{{Type: "create_canvas", ID: canvasID, Patch: patch}}})
		if err != nil {
			return err
		}
		row := model.AdvertisingAssembly{ID: newID(), ProductionID: productionID, ClientOperationID: operationID, SourceProductionRevision: expectedRevision, SelectionHash: signature, TimelineJSON: string(raw), CanvasID: &canvasID, CreatedAt: now, UpdatedAt: now}
		if err := repo.CreateAdvertisingAssembly(&row); err != nil {
			return err
		}
		if err := repo.ReplaceAdvertisingReferences(projectID, "assembly:"+row.ID, ids); err != nil {
			return err
		}
		result, err = advertisingAssemblyDetail(&row)
		return err
	})
	return result, advertisingProductionFailure(err)
}
