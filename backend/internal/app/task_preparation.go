package app

import (
	"encoding/json"
	"strings"
	"yingce/backend/internal/model"
)

type advertisingTaskAdmission struct {
	SubmissionID string
	MaxCharge    int64
}

// prepareManagedTask resolves a server-managed request without queueing or charging.
func (s *Service) prepareManagedTask(userID string, req CreateTaskRequest) (*model.Task, *model.BillingOrder, string, error) {
	config, _ := req.Input["config"].(map[string]any)
	if req.LogicalModelID == "" && strings.TrimSpace(stringValue(config["channelId"])) == "" {
		return nil, nil, "", BadAuthRequest("智能创作目前仅支持后端受管模型，请在原入口使用其他渠道")
	}
	if taskInputUsesWorkflowProvider(req.Input) || isTextReplayTaskRequest(req.Input) {
		return nil, nil, "", BadAuthRequest("智能创作不支持本机、工作流或文本回放任务")
	}
	// System selectors are the only authority; discard frontend sentinel credentials.
	safeConfig := map[string]any{}
	for _, key := range []string{"channelId", "channelModelKey", "model", "priceTierId", "apiFormat", "interfaceType", "size", "quality", "transparentBackground", "count", "videoSeconds", "vquality", "videoGenerateAudio", "videoWatermark", "videoArkPrivateAssetUpload", "systemPrompt", "audioVoice", "audioFormat", "audioSpeed", "audioInstructions"} {
		if value, ok := config[key]; ok {
			safeConfig[key] = value
		}
	}
	config = safeConfig
	req.Input["config"] = safeConfig
	if err := validateCreationJSON(req); err != nil {
		return nil, nil, "", err
	}
	if req.Type != "canvas_text" && req.Type != "text" && req.Type != "canvas_image" && req.Type != "canvas_video" && req.Type != "canvas_audio" {
		return nil, nil, "", BadAuthRequest("智能创作任务类型不受支持")
	}
	expectedMode := map[string]string{"text": "text", "canvas_text": "text", "canvas_image": "image", "canvas_video": "video", "canvas_audio": "audio"}[req.Type]
	if stringValue(req.Input["mode"]) != expectedMode || strings.TrimSpace(stringValue(req.Input["prompt"])) != strings.TrimSpace(req.Prompt) {
		return nil, nil, "", BadAuthRequest("任务类型、模式和实际提示词必须一致")
	}
	if expectedMode != "text" && req.Input["agentRequests"] != nil {
		return nil, nil, "", BadAuthRequest("媒体任务不允许携带独立模型协议请求")
	}
	if count := stringValue(config["count"]); count != "" && count != "1" {
		return nil, nil, "", BadAuthRequest("每个获批执行项只能生成一个产物")
	}
	if req.Input["mask"] != nil {
		return nil, nil, "", BadAuthRequest("本期智能创作暂不支持蒙版任务")
	}
	for _, name := range []string{"referenceImages", "referenceVideos", "referenceAudios"} {
		if list, ok := req.Input[name].([]any); ok {
			for _, raw := range list {
				media, ok := raw.(map[string]any)
				if !ok {
					return nil, nil, "", BadAuthRequest("素材引用格式无效")
				}
				key := stringValue(media["storageKey"])
				if !strings.HasPrefix(key, "resource:") {
					return nil, nil, "", BadAuthRequest("请先将参考素材保存到当前账号资源库")
				}
				resource, err := s.repo.ResourceForUser(userID, strings.TrimPrefix(key, "resource:"))
				if err != nil {
					return nil, nil, "", creationError(err)
				}
				if resource.Status != "ready" {
					return nil, nil, "", BadAuthRequest("参考素材尚未就绪")
				}
				delete(media, "url")
				delete(media, "dataUrl")
				media["bytes"] = resource.Size
				media["durationMs"] = resource.DurationMs
			}
		}
	}
	prepared := &creationTaskPreparation{}
	req.creationPrepare = prepared
	task, err := s.CreateTask(userID, req)
	if err != nil {
		return nil, nil, "", err
	}
	var input map[string]any
	_ = json.Unmarshal([]byte(task.InputJSON), &input)
	resolved, _ := input["config"].(map[string]any)
	for _, key := range []string{"size", "videoSeconds", "vquality", "quality", "count"} {
		if requested := stringValue(config[key]); requested != "" && !strings.EqualFold(requested, stringValue(resolved[key])) {
			return nil, nil, "", creationConflict("模型解析后的生成规格与请求不同，请调整方案后重新报价")
		}
	}
	channel, channelErr := s.repo.SystemChannel(stringValue(resolved["channelId"]))
	if channelErr != nil {
		return nil, nil, "", channelErr
	}
	if expectedMode == "text" {
		var typed canvasGenerationInput
		if err = json.Unmarshal([]byte(task.InputJSON), &typed); err != nil {
			return nil, nil, "", err
		}
		if len(typed.ReferenceImages) > 0 {
			cm, e := s.repo.ChannelModelByKey(channel.ID, stringValue(resolved["model"]))
			if e != nil {
				return nil, nil, "", e
			}
			profile, e := DecodeModelCapabilityConfig(cm.CapabilityConfigJSON)
			if e != nil || profile == nil || profile.Text == nil || profile.Text.References.MaxImages < len(typed.ReferenceImages) {
				return nil, nil, "", BadAuthRequest("当前文本模型未配置足够的图片理解能力")
			}
		}
		if err = validateAgentResourcePlaceholders(typed); err != nil {
			return nil, nil, "", err
		}
	}
	sig, err := s.repo.CreationPriceSignature(task, stringValue(resolved["channelId"]), stringValue(resolved["model"]))
	return task, prepared.Order, sig, err
}
