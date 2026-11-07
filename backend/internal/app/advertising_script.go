package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
)

const advertisingPlanTimeout = 35 * time.Second

// AdvertisingScriptPlanRequest carries no provider, model or pricing parameters.
type AdvertisingScriptPlanRequest struct {
	ExpectedRevision  int64  `json:"expectedRevision"`
	ClientOperationID string `json:"clientOperationId"`
}

type AdvertisingScriptPlanEvidence struct {
	Claim  string `json:"claim"`
	Source string `json:"source"`
}

type AdvertisingScriptPlanResult struct {
	OperationID    string                          `json:"operationId"`
	SourceRevision int64                           `json:"sourceRevision"`
	Status         string                          `json:"status"`
	Draft          *AdvertisingDraft               `json:"draft,omitempty"`
	Evidence       []AdvertisingScriptPlanEvidence `json:"evidence,omitempty"`
	Uncertainties  []string                        `json:"uncertainties,omitempty"`
	Error          string                          `json:"error,omitempty"`
}

type adPlanModelOutput struct {
	Shots []struct {
		Visual string `json:"visual"`
	} `json:"shots"`
	Evidence      []AdvertisingScriptPlanEvidence `json:"evidence"`
	Uncertainties []string                        `json:"uncertainties"`
}

var adPlanOperationID = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var adPlanUnverifiedClaim = regexp.MustCompile(`(?i)(https?://|www\.|[0-9０-９]+|[%％¥￥]|零糖|无糖|半价|折扣|免费|包邮|保证|治愈|销量|第一|认证|官方|权威|领先|环保|安全|纯天然|功效|购买|下单|领取|点击|扫码)`)

func advertisingPlanOutput(raw string, source AdvertisingDraft) (AdvertisingScriptPlanResult, error) {
	var output adPlanModelOutput
	decoder := json.NewDecoder(strings.NewReader(strings.TrimSpace(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&output); err != nil {
		return AdvertisingScriptPlanResult{}, fmt.Errorf("模型输出格式无效: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return AdvertisingScriptPlanResult{}, errors.New("模型输出包含多余内容")
	}
	if len(output.Shots) != 3 || source.Brief.DurationMs != 15000 || len(source.ProductResourceIDs) != 1 {
		return AdvertisingScriptPlanResult{}, errors.New("需要15秒、三个镜头和一张商品图")
	}
	if len(output.Evidence) > 12 || len(output.Uncertainties) > 12 {
		return AdvertisingScriptPlanResult{}, errors.New("证据数量超限")
	}
	known := strings.Join(append([]string{source.Brief.ProductName, source.Brief.Description, source.Brief.Audience, source.Brand.Name}, source.Brief.SellingPoints...), " ")
	for _, e := range output.Evidence {
		if e.Source != "user" || strings.TrimSpace(e.Claim) == "" || len(e.Claim) > 240 || !strings.Contains(known, e.Claim) {
			return AdvertisingScriptPlanResult{}, errors.New("证据来源无法核实")
		}
	}
	for _, u := range output.Uncertainties {
		if len(u) > 240 || adPlanUnverifiedClaim.MatchString(u) && strings.Contains(strings.ToLower(u), "http") {
			return AdvertisingScriptPlanResult{}, errors.New("不允许外部链接")
		}
	}
	draft := normalizeAdvertisingDraft(source)
	if draft.Brief.Goal == "conversion" {
		return AdvertisingScriptPlanResult{}, errors.New("转化目标需要用户提供核实的落地页")
	}
	draft.Brief.Goal = "awareness"
	draft.Brief.CTA = ""
	draft.Shots = nil
	for i, shot := range output.Shots {
		visual := strings.TrimSpace(shot.Visual)
		if visual == "" || len([]rune(visual)) > 900 || adPlanUnverifiedClaim.MatchString(visual) {
			return AdvertisingScriptPlanResult{}, errors.New("镜头含未核实的事实、行动或链接")
		}
		draft.Shots = append(draft.Shots, AdvertisingShot{ID: uuid.NewString(), DurationMs: 5000, Visual: visual, ReferenceResourceIDs: []string{source.ProductResourceIDs[i%len(source.ProductResourceIDs)]}})
	}
	if err := validateAdvertisingDraft(draft, true); err != nil {
		return AdvertisingScriptPlanResult{}, err
	}
	return AdvertisingScriptPlanResult{Status: "ready", Draft: &draft, Evidence: output.Evidence, Uncertainties: append(output.Uncertainties, "未核实的宣传文案及落地页信息未生成，旁白与屏幕文字留空")}, nil
}

func advertisingPlanView(row *model.AdvertisingScriptPlan) (AdvertisingScriptPlanResult, error) {
	result := AdvertisingScriptPlanResult{OperationID: row.OperationID, SourceRevision: row.SourceRevision, Status: row.Status, Error: row.Error}
	if row.Status == "ready" {
		if err := json.Unmarshal([]byte(row.ResultJSON), &result); err != nil {
			return AdvertisingScriptPlanResult{}, err
		}
		result.OperationID = row.OperationID
		result.SourceRevision = row.SourceRevision
	}
	return result, nil
}

func (s *Service) GetAdvertisingScriptPlan(userID, projectID, operationID string) (*AdvertisingScriptPlanResult, error) {
	if err := advertisingAccess(userID, projectID); err != nil {
		return nil, err
	}
	if _, err := s.repo.AdvertisingProjectForUser(userID, projectID); err != nil {
		return nil, advertisingFailure(err)
	}
	if operationID == "" {
		return nil, BadAuthRequest("operationId不能为空")
	}
	row, err := s.repo.AdvertisingPlan(projectID, operationID)
	if err != nil {
		return nil, advertisingFailure(err)
	}
	if row.Status == "running" && time.Since(row.CreatedAt) > advertisingPlanTimeout+10*time.Second {
		_ = s.repo.FailStaleAdvertisingPlans(projectID, time.Now().Add(-advertisingPlanTimeout-10*time.Second))
		row, err = s.repo.AdvertisingPlan(projectID, operationID)
		if err != nil {
			return nil, err
		}
	}
	result, err := advertisingPlanView(row)
	if err != nil {
		return nil, err
	}
	return s.advertisingPlanFresh(userID, projectID, &result), nil
}

func (s *Service) advertisingPlanFresh(userID, projectID string, result *AdvertisingScriptPlanResult) *AdvertisingScriptPlanResult {
	if result.Status != "ready" || result.Draft == nil {
		return result
	}
	detail, err := advertisingDetail(s.repo, userID, projectID)
	if err == nil && detail.Revision == result.SourceRevision {
		_, err = advertisingResources(s.repo, userID, *result.Draft, true)
	}
	if err != nil || detail.Revision != result.SourceRevision {
		result.Status = "blocked"
		result.Draft = nil
		result.Error = "草稿或商品图片已变化，请重新规划"
	}
	return result
}

func (s *Service) LatestAdvertisingScriptPlan(userID, projectID string) (*AdvertisingScriptPlanResult, error) {
	if err := advertisingAccess(userID, projectID); err != nil {
		return nil, err
	}
	if _, err := s.repo.AdvertisingProjectForUser(userID, projectID); err != nil {
		return nil, advertisingFailure(err)
	}
	row, err := s.repo.LatestAdvertisingPlan(projectID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return s.GetAdvertisingScriptPlan(userID, projectID, row.OperationID)
}

func (s *Service) CreateAdvertisingScriptPlan(ctx context.Context, userID, projectID string, req AdvertisingScriptPlanRequest) (result *AdvertisingScriptPlanResult, err error) {
	if err := advertisingAccess(userID, projectID); err != nil {
		return nil, err
	}
	if !adPlanOperationID.MatchString(req.ClientOperationID) || req.ExpectedRevision < 1 {
		return nil, BadAuthRequest("需要有效的clientOperationId和expectedRevision")
	}
	if prior, lookupErr := s.repo.AdvertisingPlan(projectID, req.ClientOperationID); lookupErr == nil {
		if prior.UserID != userID {
			return nil, BadAuthRequest("操作不属于当前账号")
		}
		return s.GetAdvertisingScriptPlan(userID, projectID, req.ClientOperationID)
	} else if !errors.Is(lookupErr, gorm.ErrRecordNotFound) {
		return nil, lookupErr
	}
	channelID, modelKey := strings.TrimSpace(os.Getenv("CANVAS_AD_SCRIPT_CHANNEL_ID")), strings.TrimSpace(os.Getenv("CANVAS_AD_SCRIPT_MODEL_KEY"))
	if channelID == "" || modelKey == "" {
		return nil, BadAuthRequest("广告规划模型尚未配置")
	}
	config, err := s.resolveProviderConfig(providerConfig{ChannelID: channelID, ChannelModelKey: modelKey, Model: modelKey})
	if err != nil {
		return nil, err
	}
	if config.InterfaceType != "chat-completion" && config.InterfaceType != "openai-response" && config.InterfaceType != string(model.ChannelInterfaceClaudeAPI) {
		return nil, BadAuthRequest("广告规划模型不支持图片输入")
	}
	var row *model.AdvertisingScriptPlan
	var existing bool
	err = s.repo.AdvertisingTransaction(userID, projectID, func(repo *repository.Repository) error {
		prior, err := repo.AdvertisingPlan(projectID, req.ClientOperationID)
		if err == nil {
			row = prior
			existing = true
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err = repo.LockAdvertisingPlanBudget(); err != nil {
			return err
		}
		if err = repo.LockAdvertisingPlanUser(userID); err != nil {
			return err
		}
		now := time.Now()
		if err = repo.FailStaleAdvertisingPlans(projectID, now.Add(-advertisingPlanTimeout-10*time.Second)); err != nil {
			return err
		}
		day := now.UTC().Truncate(24 * time.Hour)
		perUser, err := repo.CountAdvertisingPlansSince(userID, day)
		if err != nil {
			return err
		}
		global, err := repo.CountAllAdvertisingPlansSince(day)
		if err != nil {
			return err
		}
		active, err := repo.ActiveAdvertisingPlans(projectID, now.Add(-advertisingPlanTimeout-10*time.Second))
		if err != nil {
			return err
		}
		activeUser, err := repo.ActiveUserAdvertisingPlans(userID, now.Add(-advertisingPlanTimeout-10*time.Second))
		if err != nil {
			return err
		}
		if perUser >= 3 || global >= 200 || active > 0 || activeUser > 0 {
			return BadAuthRequest("广告规划调用额度已用尽或正在运行")
		}
		detail, err := advertisingDetail(repo, userID, projectID)
		if err != nil {
			return err
		}
		if detail.Revision != req.ExpectedRevision {
			return repository.ErrAdvertisingRevisionConflict
		}
		if detail.Draft.Brief.Goal == "conversion" {
			return BadAuthRequest("转化目标需要经过核实的落地页信息，当前仅支持品牌认知规划")
		}
		if detail.Draft.Brief.DurationMs != 15000 || len(detail.Draft.ProductResourceIDs) != 1 {
			return BadAuthRequest("规划需要15秒目标和一张商品图")
		}
		resources, err := advertisingResources(repo, userID, detail.Draft, true)
		if err != nil {
			return err
		}
		for _, resource := range resources {
			if !strings.HasPrefix(resource.MimeType, "image/") {
				return BadAuthRequest("规划只支持图片素材")
			}
		}
		snapshot, err := json.Marshal(AdvertisingSnapshot{Name: detail.Project.Name, Draft: detail.Draft, Resources: resources})
		if err != nil {
			return err
		}
		row = &model.AdvertisingScriptPlan{ID: newID(), UserID: userID, ProjectID: projectID, OperationID: req.ClientOperationID, SourceRevision: detail.Revision, SnapshotJSON: string(snapshot), Status: "running", CreatedAt: now, UpdatedAt: now}
		if err = repo.CreateAdvertisingPlan(row); err != nil {
			return err
		}
		return repo.ReplaceAdvertisingReferences(projectID, "plan:"+row.OperationID, advertisingResourceIDs(detail.Draft))
	})
	if err != nil {
		return nil, advertisingFailure(err)
	}
	if existing {
		return s.GetAdvertisingScriptPlan(userID, projectID, req.ClientOperationID)
	}
	// ponytail: synchronous upstream call, capped at 35s; use a leased worker if proxy limits or throughput require it.
	callCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), advertisingPlanTimeout)
	defer cancel()
	status, problem, resultJSON := "failed", "模型规划失败", ""
	defer func() {
		if finishErr := s.repo.FinishAdvertisingPlan(projectID, req.ClientOperationID, status, resultJSON, problem); finishErr != nil {
			result, err = nil, finishErr
		}
	}()
	var snapshot AdvertisingSnapshot
	if err = json.Unmarshal([]byte(row.SnapshotJSON), &snapshot); err != nil {
		return nil, err
	}
	input := canvasGenerationInput{Mode: "text", Config: config, Metadata: map[string]interface{}{"edit": "layer-planning"}, Prompt: "Return ONLY JSON {\"shots\":[{\"visual\":string},{\"visual\":string},{\"visual\":string}],\"evidence\":[],\"uncertainties\":[]}. Three visual descriptions for a 15-second portrait advertisement. No numeric claims, guarantees, discounts, URLs, labels, text overlays or calls to action. Use only supplied product photos and user-authored facts. Never infer unseen product properties. User brief: " + string(row.SnapshotJSON), MaxOutputTokens: 1200}
	for _, id := range snapshot.Draft.ProductResourceIDs {
		input.ReferenceImages = append(input.ReferenceImages, providerMedia{StorageKey: "resource:" + id})
	}
	if err = s.validateTextTaskReferences(input); err != nil {
		return nil, err
	}
	if err = s.hydrateGenerationMedia(userID, &input, providerMediaHydrationPolicy{imageOnly: true, maxBytes: 8 << 20}); err != nil {
		return nil, err
	}
	if len(input.ReferenceImages) != 1 || input.ReferenceImages[0].Bytes <= 0 || input.ReferenceImages[0].Bytes > 8<<20 {
		return nil, BadAuthRequest("商品图片不得超过8MiB")
	}
	output, err := runTextTask(callCtx, input)
	if err != nil {
		return nil, err
	}
	text, _ := output["text"].(string)
	candidate, err := advertisingPlanOutput(text, snapshot.Draft)
	if err != nil {
		status = "blocked"
		problem = err.Error()
		return &AdvertisingScriptPlanResult{OperationID: req.ClientOperationID, SourceRevision: row.SourceRevision, Status: status, Error: problem}, nil
	}
	candidate.OperationID = req.ClientOperationID
	candidate.SourceRevision = row.SourceRevision
	checked := s.advertisingPlanFresh(userID, projectID, &candidate)
	if checked.Status == "blocked" {
		status = "blocked"
		problem = checked.Error
		return checked, nil
	}
	data, err := json.Marshal(candidate)
	if err != nil {
		return nil, err
	}
	status = "ready"
	problem = ""
	resultJSON = string(data)
	candidate.OperationID = req.ClientOperationID
	candidate.SourceRevision = row.SourceRevision
	return &candidate, nil
}
