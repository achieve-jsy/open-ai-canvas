package app

import (
	"encoding/json"
	"errors"
	"gorm.io/gorm"
	"math"
	"slices"
	"yingce/backend/internal/kernel"
	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
)

type AdvertisingSelection struct {
	VideoSubmissionID string `json:"videoSubmissionId"`
	AudioSubmissionID string `json:"audioSubmissionId"`
	VideoStartMs      int64  `json:"videoStartMs"`
}
type AdvertisingProductionSettings struct {
	TTSModelID      string                          `json:"ttsModelId"`
	VoiceID         string                          `json:"voiceId"`
	Selections      map[string]AdvertisingSelection `json:"selections"`
	MusicResourceID string                          `json:"musicResourceId"`
	MusicVolume     *float64                        `json:"musicVolume"`
}
type AdvertisingProductionDetail struct {
	Production        model.AdvertisingProduction   `json:"production"`
	Settings          AdvertisingProductionSettings `json:"settings"`
	ConfirmedSnapshot AdvertisingRevisionDetail     `json:"confirmedSnapshot"`
}

func advertisingProductionFailure(err error) error {
	if errors.Is(err, repository.ErrAdvertisingProductionConflict) {
		return advertisingError(409, kernel.ErrorReason("advertising_production_conflict"), "制作状态已更新，请查看服务端版本后重试")
	}
	return advertisingFailure(err)
}
func advertisingProductionDetail(repo *repository.Repository, userID, projectID, id string) (AdvertisingProductionDetail, error) {
	var result AdvertisingProductionDetail
	row, err := repo.AdvertisingProductionForUser(userID, projectID, id)
	if err != nil {
		return result, err
	}
	result.Production = *row
	if err := json.Unmarshal([]byte(row.SettingsJSON), &result.Settings); err != nil {
		return result, err
	}
	confirmed, err := repo.AdvertisingRevisionForUser(userID, projectID, row.ConfirmedRevisionID)
	if err != nil {
		return result, err
	}
	result.ConfirmedSnapshot, err = advertisingRevisionDetail(confirmed)
	return result, err
}
func (s *Service) CreateAdvertisingProduction(userID, projectID, confirmedRevisionID string) (AdvertisingProductionDetail, error) {
	var result AdvertisingProductionDetail
	if err := advertisingAccess(userID, projectID); err != nil {
		return result, err
	}
	err := s.repo.AdvertisingTransaction(userID, projectID, func(repo *repository.Repository) error {
		if _, err := repo.AdvertisingRevisionForUser(userID, projectID, confirmedRevisionID); err != nil {
			return err
		}
		row, err := repo.AdvertisingProductionForRevision(projectID, confirmedRevisionID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			volume := 0.15
			settings, err := json.Marshal(AdvertisingProductionSettings{Selections: map[string]AdvertisingSelection{}, MusicVolume: &volume})
			if err != nil {
				return err
			}
			row = &model.AdvertisingProduction{ID: newID(), ProjectID: projectID, ConfirmedRevisionID: confirmedRevisionID, Revision: 1, SettingsJSON: string(settings)}
			if err := repo.CreateAdvertisingProduction(row); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		result, err = advertisingProductionDetail(repo, userID, projectID, row.ID)
		return err
	})
	return result, advertisingProductionFailure(err)
}
func (s *Service) GetAdvertisingProduction(userID, projectID, id string) (AdvertisingProductionDetail, error) {
	if err := advertisingAccess(userID, projectID); err != nil {
		return AdvertisingProductionDetail{}, err
	}
	result, err := advertisingProductionDetail(s.repo, userID, projectID, id)
	return result, advertisingProductionFailure(err)
}
func validateAdvertisingProductionSettings(repo *repository.Repository, userID string, snapshot AdvertisingSnapshot, settings *AdvertisingProductionSettings) error {
	for name, value := range map[string]string{"ttsModelId": settings.TTSModelID, "voiceId": settings.VoiceID} {
		if err := advertisingText(name, value, 120, false); err != nil {
			return BadAuthRequest(err.Error())
		}
	}
	if settings.MusicVolume == nil {
		volume := 0.15
		settings.MusicVolume = &volume
	}
	volume := *settings.MusicVolume
	if math.IsNaN(volume) || math.IsInf(volume, 0) || volume < 0 || volume > 1 {
		return BadAuthRequest("背景音乐音量必须为0至1")
	}
	if settings.Selections == nil {
		settings.Selections = map[string]AdvertisingSelection{}
	}
	for id, selection := range settings.Selections {
		if !slices.ContainsFunc(snapshot.Draft.Shots, func(shot AdvertisingShot) bool { return shot.ID == id }) || selection.VideoStartMs < 0 {
			return BadAuthRequest("镜头或裁剪起点不合法")
		}
		// No candidate can be adopted until its task has completed and registered a resource.
		if selection.VideoSubmissionID != "" || selection.AudioSubmissionID != "" {
			return advertisingError(409, kernel.ErrorReason("advertising_candidate_unavailable"), "候选尚未就绪")
		}
	}
	if settings.MusicResourceID != "" {
		row, err := repo.ResourceForUser(userID, settings.MusicResourceID)
		if err != nil {
			return err
		}
		if row.Status != model.ResourceStatusReady || !slices.Contains([]string{"audio/mpeg", "audio/mp4", "audio/wav", "audio/x-wav"}, row.MimeType) {
			return advertisingError(400, kernel.ReasonAdvertisingResourceUnavailable, "背景音乐未就绪或类型不符")
		}
	}
	return nil
}
func (s *Service) SaveAdvertisingProductionSettings(userID, projectID, id string, expectedRevision int64, settings AdvertisingProductionSettings) (AdvertisingProductionDetail, error) {
	var result AdvertisingProductionDetail
	if err := advertisingAccess(userID, projectID); err != nil {
		return result, err
	}
	if expectedRevision < 1 {
		return result, BadAuthRequest("制作修订必须为正整数")
	}
	err := s.repo.AdvertisingProductionTransaction(userID, projectID, id, func(repo *repository.Repository) error {
		current, err := advertisingProductionDetail(repo, userID, projectID, id)
		if err != nil {
			return err
		}
		if current.Production.Revision != expectedRevision {
			return repository.ErrAdvertisingProductionConflict
		}
		if err := validateAdvertisingProductionSettings(repo, userID, current.ConfirmedSnapshot.Snapshot, &settings); err != nil {
			return err
		}
		data, err := json.Marshal(settings)
		if err != nil {
			return err
		}
		if err := repo.CASAdvertisingProduction(id, expectedRevision, string(data)); err != nil {
			return err
		}
		ids := []string{}
		if settings.MusicResourceID != "" {
			ids = append(ids, settings.MusicResourceID)
		}
		if err := repo.ReplaceAdvertisingReferences(projectID, "production:"+id, ids); err != nil {
			return err
		}
		result, err = advertisingProductionDetail(repo, userID, projectID, id)
		return err
	})
	return result, advertisingProductionFailure(err)
}
