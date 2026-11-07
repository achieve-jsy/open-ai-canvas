package app

import (
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"yingce/backend/internal/kernel"
	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
)

type AdvertisingSubmissionRequest struct {
	ClientOperationID string `json:"clientOperationId"`
	ShotID            string `json:"shotId"`
	Kind              string `json:"kind"`
	ModelID           string `json:"modelId"`
	VoiceID           string `json:"voiceId"`
	Format            string `json:"format"`
}
type advertisingSubmissionInput struct {
	Selection AdvertisingSubmissionRequest `json:"selection"`
	Request   CreateTaskRequest            `json:"request"`
}
type AdvertisingSubmissionDetail struct {
	BatchID        *string        `json:"batchId,omitempty"`
	CandidateIndex *int           `json:"candidateIndex,omitempty"`
	ID             string         `json:"id"`
	ProductionID   string         `json:"productionId"`
	ShotID         string         `json:"shotId"`
	Kind           string         `json:"kind"`
	RequestHash    string         `json:"requestHash"`
	PublicRequest  map[string]any `json:"publicRequest"`
	Quote          CreationQuote  `json:"quote"`
	Task           *model.Task    `json:"task,omitempty"`
}
type AdvertisingPage[T any] struct {
	Items    []T   `json:"items"`
	Page     int   `json:"page"`
	PageSize int   `json:"pageSize"`
	Total    int64 `json:"total"`
}

func advertisingConflict(reason, message string) error {
	return advertisingError(409, kernel.ErrorReason("advertising_"+reason), message)
}
func advertisingSubmissionDetail(repo *repository.Repository, userID string, row *model.AdvertisingSubmission) (AdvertisingSubmissionDetail, error) {
	detail := AdvertisingSubmissionDetail{ID: row.ID, ProductionID: row.ProductionID, ShotID: row.ShotID, Kind: row.Kind, RequestHash: row.RequestHash, BatchID: row.BatchID, CandidateIndex: row.CandidateIndex}
	var stored advertisingSubmissionInput
	if err := json.Unmarshal([]byte(row.RequestJSON), &stored); err != nil {
		return detail, err
	}
	input := stored.Request.Input
	detail.PublicRequest = map[string]any{"prompt": input["prompt"], "modelId": stored.Selection.ModelID}
	if stored.Selection.Kind == "audio" {
		detail.PublicRequest["voiceId"] = stored.Selection.VoiceID
		detail.PublicRequest["format"] = stored.Selection.Format
		detail.PublicRequest["speed"] = 1
	} else {
		config, _ := input["config"].(map[string]any)
		detail.PublicRequest["durationSeconds"] = config["videoSeconds"]
		detail.PublicRequest["size"] = config["size"]
		detail.PublicRequest["resolution"] = config["vquality"]
		var ids []string
		if refs, ok := input["referenceImages"].([]any); ok {
			for _, ref := range refs {
				if fields, ok := ref.(map[string]any); ok {
					if id, ok := fields["id"].(string); ok {
						ids = append(ids, id)
					}
				}
			}
		}
		detail.PublicRequest["referenceResourceIds"] = ids
	}
	if err := json.Unmarshal([]byte(row.QuoteJSON), &detail.Quote); err != nil {
		return detail, err
	}
	if row.TaskID != nil {
		task, err := repo.TaskForUser(userID, *row.TaskID)
		if err != nil {
			return detail, err
		}
		detail.Task = taskForOutput(*task)
	}
	return detail, nil
}
func advertisingQuote(task *model.Task, order *model.BillingOrder, signature, requestHash string, expires time.Time) CreationQuote {
	quote := creationQuoteFor(task, order, signature, expires)
	quote.QuoteHash = creationHash([]any{requestHash, quote.QuoteHash})
	return quote
}
func (s *Service) advertisingModel(userID, id, kind string) (PublicChannelModel, string, error) {
	access, err := s.userGroupAccess(userID)
	if err != nil {
		return PublicChannelModel{}, "", err
	}
	catalog, err := s.publicSystemChannelCatalog(access, nil)
	if err != nil {
		return PublicChannelModel{}, "", err
	}
	for _, channel := range catalog {
		for _, cm := range channel.Models {
			if cm.ID == id && cm.Capability == kind && cm.Available {
				return cm, channel.ID, nil
			}
		}
	}
	return PublicChannelModel{}, "", BadAuthRequest("模型不可见、不可用或类型不符")
}
func (s *Service) advertisingTaskRequest(userID, projectID string, snapshot AdvertisingSnapshot, selection AdvertisingSubmissionRequest) (CreateTaskRequest, error) {
	var req CreateTaskRequest
	if _, err := uuid.Parse(selection.ClientOperationID); err != nil || len(selection.ClientOperationID) != 36 {
		return req, BadAuthRequest("操作键必须为UUID")
	}
	if selection.Kind != "video" && selection.Kind != "audio" {
		return req, BadAuthRequest("仅支持video或audio")
	}
	if err := advertisingText("modelId", selection.ModelID, 120, true); err != nil {
		return req, BadAuthRequest(err.Error())
	}
	if err := advertisingText("voiceId", selection.VoiceID, 120, false); err != nil {
		return req, BadAuthRequest(err.Error())
	}
	index := slices.IndexFunc(snapshot.Draft.Shots, func(shot AdvertisingShot) bool { return shot.ID == selection.ShotID })
	if index < 0 {
		return req, BadAuthRequest("镜头不属于当前确认版本")
	}
	shot := snapshot.Draft.Shots[index]
	cm, channelID, err := s.advertisingModel(userID, selection.ModelID, selection.Kind)
	if err != nil {
		return req, err
	}
	config := map[string]any{"channelId": channelID, "model": cm.ModelKey, "count": "1"}
	input := map[string]any{"mode": selection.Kind, "config": config}
	prompt := shot.Voiceover
	if selection.Kind == "audio" {
		if strings.TrimSpace(prompt) == "" {
			return req, BadAuthRequest("空旁白不生成音频")
		}
		metadata, ok := s.protocolMetadata(string(cm.Protocol))
		if !ok {
			return req, BadAuthRequest("音频协议未安装")
		}
		voiceOK, formatOK := false, false
		format := selection.Format
		if format == "" {
			format = "mp3"
		}
		if format != "mp3" && format != "wav" {
			return req, BadAuthRequest("首版旁白仅支持mp3或wav")
		}
		for _, param := range metadata.Parameters {
			if param.Name == "audioVoice" || param.Name == "voice" {
				voiceOK = slices.Contains(param.Values, selection.VoiceID)
			}
			if param.Name == "audioFormat" || param.Name == "format" || param.Name == "response_format" {
				formatOK = slices.Contains(param.Values, format)
			}
		}
		if !voiceOK || !formatOK {
			return req, BadAuthRequest("音频协议未声明所选音色或格式，不能生成")
		}
		config["audioVoice"], config["audioFormat"], config["audioSpeed"] = selection.VoiceID, format, "1"
	} else {
		if selection.VoiceID != "" || selection.Format != "" {
			return req, BadAuthRequest("视频请求不得指定旁白音色或格式")
		}
		raw, err := json.Marshal(cm.CapabilityConfig)
		if err != nil {
			return req, err
		}
		capability, err := DecodeModelCapabilityConfig(string(raw))
		if err != nil || capability == nil || capability.Video == nil {
			return req, BadAuthRequest("视频模型缺少能力配置")
		}
		video := capability.Video
		if video.Processing != nil || !slices.Contains(video.Ratios, "9:16") {
			return req, BadAuthRequest("视频模型不支持竖屏生成")
		}
		target := int((shot.DurationMs + 999) / 1000)
		duration := 0
		if video.Duration.Selection == "range" {
			step := max(video.Duration.Step, 1)
			duration = max(target, video.Duration.Min)
			duration = video.Duration.Min + ((duration-video.Duration.Min+step-1)/step)*step
			if duration > video.Duration.Max {
				duration = 0
			}
		} else {
			for _, value := range video.Duration.Values {
				if value >= target && (duration == 0 || value < duration) {
					duration = value
				}
			}
		}
		if duration == 0 {
			return req, BadAuthRequest("视频模型没有覆盖镜头时长的档位")
		}
		config["size"], config["videoSeconds"], config["vquality"] = "9:16", strconv.Itoa(duration), video.DefaultResolution
		config["videoGenerateAudio"] = strconv.FormatBool(video.GenerateAudio.Default)
		parts := []string{"广告分镜模板v1"}
		for _, item := range []struct{ label, value string }{
			{"商品", snapshot.Draft.Brief.ProductName},
			{"受众", snapshot.Draft.Brief.Audience},
			{"卖点", strings.Join(snapshot.Draft.Brief.SellingPoints, "；")},
			{"行动号召", snapshot.Draft.Brief.CTA},
		} {
			if strings.TrimSpace(item.value) != "" {
				parts = append(parts, item.label+"："+item.value)
			}
		}
		parts = append(parts, "画面："+shot.Visual, "保持参考商品一致，不添加未经确认的功效、优惠、字幕或文字")
		prompt = strings.Join(parts, "。") + "。"
		ids := shot.ReferenceResourceIDs
		if len(ids) == 0 && len(snapshot.Draft.ProductResourceIDs) > 0 {
			ids = snapshot.Draft.ProductResourceIDs[:1]
		}
		if len(ids) < video.References.MinImages || len(ids) > video.References.MaxImages {
			return req, BadAuthRequest("参考图数量不符合所选模型能力")
		}
		refs := []any{}
		for _, id := range ids {
			resource, err := s.repo.ResourceForUser(userID, id)
			if err != nil {
				return req, err
			}
			if resource.Status != model.ResourceStatusReady || !slices.Contains([]string{"image/png", "image/jpeg", "image/webp"}, resource.MimeType) {
				return req, BadAuthRequest("首版镜头参考只支持已就绪图片，请修改脚本后重新确认")
			}
			refs = append(refs, map[string]any{"id": id, "storageKey": "resource:" + id, "mimeType": resource.MimeType, "width": resource.Width, "height": resource.Height})
		}
		input["referenceImages"] = refs
	}
	input["prompt"] = prompt
	req = CreateTaskRequest{ProjectID: projectID, Type: "canvas_" + selection.Kind, Prompt: prompt, Model: cm.ModelKey, Input: input}
	return req, nil
}
func (s *Service) PrepareAdvertisingSubmission(userID, projectID, productionID string, selection AdvertisingSubmissionRequest) (AdvertisingSubmissionDetail, error) {
	var result AdvertisingSubmissionDetail
	production, err := s.GetAdvertisingProduction(userID, projectID, productionID)
	if err != nil {
		return result, err
	}
	selection.ClientOperationID = strings.ToLower(selection.ClientOperationID)
	if selection.Kind == "audio" && selection.Format == "" {
		selection.Format = "mp3"
	}
	existing, err := s.repo.AdvertisingSubmissionForOperation(productionID, selection.ClientOperationID)
	if err == nil {
		return s.replayAdvertisingPreparation(userID, existing, selection)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return result, err
	}
	req, err := s.advertisingTaskRequest(userID, projectID, production.ConfirmedSnapshot.Snapshot, selection)
	if err != nil {
		return result, err
	}
	row, err := s.freezeAdvertisingSubmission(userID, productionID, selection, req)
	if err != nil {
		return result, err
	}
	err = s.repo.AdvertisingProductionTransaction(userID, projectID, productionID, func(repo *repository.Repository) error {
		fresh, e := repo.AdvertisingSubmissionForOperation(productionID, selection.ClientOperationID)
		if e == nil {
			result, e = s.replayAdvertisingPreparation(userID, fresh, selection)
			return e
		}
		if !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		if e = repo.CreateAdvertisingSubmission(row); e != nil {
			return e
		}
		result, e = advertisingSubmissionDetail(repo, userID, row)
		return e
	})
	return result, advertisingProductionFailure(err)
}
func (s *Service) freezeAdvertisingSubmission(userID, productionID string, selection AdvertisingSubmissionRequest, req CreateTaskRequest) (*model.AdvertisingSubmission, error) {
	req.advertisingAdmission = &advertisingTaskAdmission{MaxCharge: 1 << 62}
	task, order, sig, err := s.prepareManagedTask(userID, req)
	if err != nil {
		return nil, err
	}
	requestHash := creationHash(req)
	quote := advertisingQuote(task, order, sig, requestHash, time.Now().UTC().Add(5*time.Minute))
	requestJSON, err := json.Marshal(advertisingSubmissionInput{Selection: selection, Request: req})
	if err != nil {
		return nil, err
	}
	quoteJSON, err := json.Marshal(quote)
	if err != nil {
		return nil, err
	}
	row := model.AdvertisingSubmission{ID: newID(), ProductionID: productionID, ShotID: selection.ShotID, Kind: selection.Kind, ClientOperationID: selection.ClientOperationID, RequestHash: requestHash, RequestJSON: string(requestJSON), QuoteJSON: string(quoteJSON), PriceSignature: sig, ExpiresAt: quote.ExpiresAt}
	return &row, nil
}
func (s *Service) replayAdvertisingPreparation(userID string, row *model.AdvertisingSubmission, selection AdvertisingSubmissionRequest) (AdvertisingSubmissionDetail, error) {
	var stored advertisingSubmissionInput
	if err := json.Unmarshal([]byte(row.RequestJSON), &stored); err != nil {
		return AdvertisingSubmissionDetail{}, err
	}
	if stored.Selection != selection {
		return AdvertisingSubmissionDetail{}, advertisingConflict("submission_mismatch", "操作键已绑定其他请求")
	}
	return advertisingSubmissionDetail(s.repo, userID, row)
}
func (s *Service) ExecuteAdvertisingSubmission(userID, projectID, productionID, id, quoteHash string) (AdvertisingSubmissionDetail, error) {
	var result AdvertisingSubmissionDetail
	if _, err := s.GetAdvertisingProduction(userID, projectID, productionID); err != nil {
		return result, err
	}
	row, err := s.repo.AdvertisingSubmission(productionID, id)
	if err != nil {
		return result, advertisingFailure(err)
	}
	if row.BatchID != nil {
		return result, advertisingConflict("batch_approval_required", "请明确批准该批全部候选")
	}
	prepared, err := s.prepareAdvertisingExecution(userID, row, quoteHash)
	if err != nil {
		return result, err
	}
	policy, err := s.RuntimePolicy()
	if err != nil {
		return result, err
	}
	s.storageMu.Lock()
	defer s.storageMu.Unlock()
	err = s.repo.AdvertisingProductionTransaction(userID, projectID, productionID, func(repo *repository.Repository) error {
		fresh, e := repo.AdvertisingSubmission(productionID, id)
		if e != nil {
			return e
		}
		if fresh.TaskID == nil {
			if e = s.guardAdvertisingActivity(repo, userID, productionID, fresh.ShotID, fresh.Kind); e != nil {
				return e
			}
			if e = validateAdvertisingExecution(repo, fresh, prepared); e != nil {
				return e
			}
			if e = createTaskWithStorageQuotaRepository(repo, prepared.task, prepared.order, policy); e != nil {
				return e
			}
			now := time.Now().UTC()
			if e = repo.ApproveAdvertisingSubmission(id, prepared.task.ID, now); e != nil {
				return e
			}
			if e = advertisingProductionEvent(repo, userID, projectID, productionID, "admitted:"+id, "submission_admitted", fresh.ShotID, id, "", "", AdvertisingEventDetails{TaskID: prepared.task.ID}); e != nil {
				return e
			}
			fresh.TaskID = &prepared.task.ID
			fresh.ApprovedAt = &now
		}
		result, e = advertisingSubmissionDetail(repo, userID, fresh)
		return e
	})
	return result, advertisingProductionFailure(err)
}

func (s *Service) ListAdvertisingSubmissions(userID, projectID, productionID string, page, size int) (AdvertisingPage[AdvertisingSubmissionDetail], error) {
	result := AdvertisingPage[AdvertisingSubmissionDetail]{Items: []AdvertisingSubmissionDetail{}, Page: page, PageSize: size}
	if err := advertisingPagination(page, size); err != nil {
		return result, err
	}
	if _, err := s.GetAdvertisingProduction(userID, projectID, productionID); err != nil {
		return result, err
	}
	rows, total, err := s.repo.AdvertisingSubmissions(productionID, page, size)
	if err != nil {
		return result, err
	}
	result.Total = total
	for _, row := range rows {
		detail, err := advertisingSubmissionDetail(s.repo, userID, &row)
		if err != nil {
			return result, err
		}
		result.Items = append(result.Items, detail)
	}
	return result, nil
}
