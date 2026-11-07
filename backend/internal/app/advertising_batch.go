package app

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
)

type AdvertisingBatchRequest struct {
	AdvertisingSubmissionRequest
	Count      int    `json:"count"`
	Adjustment string `json:"adjustment"`
}
type AdvertisingBatchApproval struct {
	SubmissionID string `json:"submissionId"`
	QuoteHash    string `json:"quoteHash"`
}
type AdvertisingBatchDetail struct {
	Batch model.AdvertisingCandidateBatch `json:"batch"`
	Items []AdvertisingSubmissionDetail   `json:"items"`
}
type AdvertisingActivity struct {
	Batches []model.AdvertisingCandidateBatch `json:"batches"`
	Items   []AdvertisingSubmissionDetail     `json:"items"`
}

func advertisingBatchDetail(repo *repository.Repository, userID string, batch *model.AdvertisingCandidateBatch) (AdvertisingBatchDetail, error) {
	result := AdvertisingBatchDetail{Batch: *batch, Items: []AdvertisingSubmissionDetail{}}
	rows, err := repo.AdvertisingBatchMembers(batch.ProductionID, batch.ID)
	if err != nil {
		return result, err
	}
	for _, row := range rows {
		item, e := advertisingSubmissionDetail(repo, userID, &row)
		if e != nil {
			return result, e
		}
		result.Items = append(result.Items, item)
	}
	return result, nil
}
func (s *Service) PrepareAdvertisingBatch(userID, projectID, productionID string, req AdvertisingBatchRequest) (AdvertisingBatchDetail, error) {
	var result AdvertisingBatchDetail
	production, err := s.GetAdvertisingProduction(userID, projectID, productionID)
	if err != nil {
		return result, err
	}
	if req.Count < 1 || req.Count > 3 || !utf8.ValidString(req.Adjustment) || utf8.RuneCountInString(req.Adjustment) > 1000 {
		return result, BadAuthRequest("候选数量必须为1–3，修改内容最多1000字符")
	}
	req.ClientOperationID = strings.ToLower(req.ClientOperationID)
	if _, err = uuid.Parse(req.ClientOperationID); err != nil || len(req.ClientOperationID) != 36 {
		return result, BadAuthRequest("操作键必须为UUID")
	}
	if req.Kind == "audio" && req.Format == "" {
		req.Format = "mp3"
	}
	hash := creationHash(req)
	replay := func(repo *repository.Repository, row *model.AdvertisingCandidateBatch) (AdvertisingBatchDetail, error) {
		if row.RequestHash != hash {
			return result, advertisingConflict("batch_mismatch", "操作键已绑定其他批次请求")
		}
		return advertisingBatchDetail(repo, userID, row)
	}
	if row, e := s.repo.AdvertisingBatchForOperation(productionID, req.ClientOperationID); e == nil {
		return replay(s.repo, row)
	} else if !errors.Is(e, gorm.ErrRecordNotFound) {
		return result, e
	}
	batch := model.AdvertisingCandidateBatch{ID: newID(), ProductionID: productionID, ClientOperationID: req.ClientOperationID, ShotID: req.ShotID, Kind: req.Kind, Count: req.Count, Adjustment: req.Adjustment, RequestHash: hash}
	rows := []*model.AdvertisingSubmission{}
	for i := 0; i < req.Count; i++ {
		selection := req.AdvertisingSubmissionRequest
		selection.ClientOperationID = uuid.NewString()
		taskReq, e := s.advertisingTaskRequest(userID, projectID, production.ConfirmedSnapshot.Snapshot, selection)
		if e != nil {
			return result, e
		}
		if req.Adjustment != "" {
			taskReq.Prompt += "\n本次候选调整：" + req.Adjustment
			taskReq.Input["prompt"] = taskReq.Prompt
		}
		row, e := s.freezeAdvertisingSubmission(userID, productionID, selection, taskReq)
		if e != nil {
			return result, e
		}
		index := i + 1
		row.BatchID = &batch.ID
		row.CandidateIndex = &index
		rows = append(rows, row)
	}
	err = s.repo.AdvertisingProductionTransaction(userID, projectID, productionID, func(repo *repository.Repository) error {
		if fresh, e := repo.AdvertisingBatchForOperation(productionID, req.ClientOperationID); e == nil {
			result, e = replay(repo, fresh)
			return e
		} else if !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		if e := repo.CreateAdvertisingBatch(&batch); e != nil {
			return e
		}
		for _, row := range rows {
			if e := repo.CreateAdvertisingSubmission(row); e != nil {
				return e
			}
		}
		var e error
		result, e = advertisingBatchDetail(repo, userID, &batch)
		return e
	})
	return result, advertisingProductionFailure(err)
}
func (s *Service) GetAdvertisingBatch(userID, projectID, productionID, id string) (AdvertisingBatchDetail, error) {
	if _, err := s.GetAdvertisingProduction(userID, projectID, productionID); err != nil {
		return AdvertisingBatchDetail{}, err
	}
	row, err := s.repo.AdvertisingBatch(productionID, id)
	if err != nil {
		return AdvertisingBatchDetail{}, advertisingFailure(err)
	}
	return advertisingBatchDetail(s.repo, userID, row)
}
func (s *Service) GetAdvertisingBatchForOperation(userID, projectID, productionID, key string) (AdvertisingBatchDetail, error) {
	if _, err := s.GetAdvertisingProduction(userID, projectID, productionID); err != nil {
		return AdvertisingBatchDetail{}, err
	}
	row, err := s.repo.AdvertisingBatchForOperation(productionID, strings.ToLower(key))
	if err != nil {
		return AdvertisingBatchDetail{}, advertisingFailure(err)
	}
	return advertisingBatchDetail(s.repo, userID, row)
}
func (s *Service) ListAdvertisingBatches(userID, projectID, productionID string, page, size int) (AdvertisingPage[AdvertisingBatchDetail], error) {
	result := AdvertisingPage[AdvertisingBatchDetail]{Items: []AdvertisingBatchDetail{}, Page: page, PageSize: size}
	if err := advertisingPagination(page, size); err != nil {
		return result, err
	}
	if _, err := s.GetAdvertisingProduction(userID, projectID, productionID); err != nil {
		return result, err
	}
	rows, total, err := s.repo.AdvertisingBatches(productionID, page, size)
	if err != nil {
		return result, err
	}
	result.Total = total
	for _, row := range rows {
		detail, e := advertisingBatchDetail(s.repo, userID, &row)
		if e != nil {
			return result, e
		}
		result.Items = append(result.Items, detail)
	}
	return result, nil
}

type advertisingExecution struct {
	task                 *model.Task
	order                *model.BillingOrder
	signature, quoteHash string
}

func (s *Service) prepareAdvertisingExecution(userID string, row *model.AdvertisingSubmission, quoteHash string) (*advertisingExecution, error) {
	detail, err := advertisingSubmissionDetail(s.repo, userID, row)
	if err != nil {
		return nil, err
	}
	if quoteHash != detail.Quote.QuoteHash {
		return nil, advertisingConflict("quote_changed", "请确认当前报价")
	}
	prepared := &advertisingExecution{quoteHash: quoteHash}
	if row.TaskID != nil {
		return prepared, nil
	}
	if !row.ExpiresAt.After(time.Now()) {
		return nil, advertisingConflict("quote_expired", "报价已过期，请重新获取并确认")
	}
	var stored advertisingSubmissionInput
	if err = json.Unmarshal([]byte(row.RequestJSON), &stored); err != nil {
		return nil, err
	}
	stored.Request.advertisingAdmission = &advertisingTaskAdmission{SubmissionID: row.ID, MaxCharge: detail.Quote.AmountMicrocredits}
	task, order, sig, err := s.prepareManagedTask(userID, stored.Request)
	if err != nil {
		return nil, err
	}
	if advertisingQuote(task, order, sig, row.RequestHash, row.ExpiresAt).QuoteHash != quoteHash {
		return nil, advertisingConflict("quote_changed", "报价已变化，请重新确认")
	}
	var input map[string]any
	if err = json.Unmarshal([]byte(task.InputJSON), &input); err != nil {
		return nil, err
	}
	if err = s.protectTaskSecrets(input); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	task.InputJSON = string(encoded)
	task.Operation = "advertising_generation"
	if order != nil {
		task.BillingOrderID = order.ID
	}
	prepared.task, prepared.order, prepared.signature = task, order, sig
	return prepared, nil
}
func validateAdvertisingExecution(repo *repository.Repository, row *model.AdvertisingSubmission, prepared *advertisingExecution) error {
	if !row.ExpiresAt.After(time.Now()) {
		return advertisingConflict("quote_expired", "报价已过期")
	}
	var quote CreationQuote
	if err := json.Unmarshal([]byte(row.QuoteJSON), &quote); err != nil {
		return err
	}
	if prepared.task == nil || quote.QuoteHash != prepared.quoteHash {
		return advertisingConflict("quote_changed", "请重新确认报价")
	}
	if err := checkCreationPriceSignature(repo, prepared.task, prepared.signature); err != nil {
		return advertisingConflict("quote_changed", "报价已变化，请重新确认")
	}
	return nil
}
func (s *Service) ExecuteAdvertisingBatch(userID, projectID, productionID, id string, approvals []AdvertisingBatchApproval) (AdvertisingBatchDetail, error) {
	result, err := s.GetAdvertisingBatch(userID, projectID, productionID, id)
	if err != nil {
		return result, err
	}
	if result.Batch.ClosedAt != nil {
		return result, advertisingConflict("batch_closed", "批次已关闭")
	}
	if len(approvals) != result.Batch.Count || len(result.Items) != result.Batch.Count {
		return result, advertisingConflict("batch_approval_required", "必须明确批准全部冻结成员")
	}
	hashes := map[string]string{}
	for _, approval := range approvals {
		if _, ok := hashes[approval.SubmissionID]; ok {
			return result, advertisingConflict("batch_approval_required", "重复批准成员")
		}
		hashes[approval.SubmissionID] = approval.QuoteHash
	}
	prepared := map[string]*advertisingExecution{}
	canonical := []AdvertisingBatchApproval{}
	for _, item := range result.Items {
		hash, ok := hashes[item.ID]
		if !ok || hash != item.Quote.QuoteHash {
			return result, advertisingConflict("quote_changed", "必须批准全部当前报价")
		}
		row, e := s.repo.AdvertisingSubmission(productionID, item.ID)
		if e != nil {
			return result, e
		}
		prepared[item.ID], e = s.prepareAdvertisingExecution(userID, row, hash)
		if e != nil {
			return result, e
		}
		canonical = append(canonical, AdvertisingBatchApproval{SubmissionID: item.ID, QuoteHash: hash})
	}
	approvalHash := creationHash(canonical)
	policy, err := s.RuntimePolicy()
	if err != nil {
		return result, err
	}
	s.storageMu.Lock()
	defer s.storageMu.Unlock()
	err = s.repo.AdvertisingProductionTransaction(userID, projectID, productionID, func(repo *repository.Repository) error {
		batch, e := repo.AdvertisingBatch(productionID, id)
		if e != nil {
			return e
		}
		if batch.ClosedAt != nil {
			return advertisingConflict("batch_closed", "批次已关闭")
		}
		if batch.ApprovedAt != nil {
			if batch.ApprovalHash != approvalHash {
				return advertisingConflict("batch_mismatch", "批准内容不匹配")
			}
			result, e = advertisingBatchDetail(repo, userID, batch)
			return e
		}
		rows, e := repo.AdvertisingBatchMembers(productionID, id)
		if e != nil {
			return e
		}
		if len(rows) != batch.Count {
			return advertisingConflict("batch_mismatch", "批次成员不完整")
		}
		if e = s.guardAdvertisingActivity(repo, userID, productionID, batch.ShotID, batch.Kind); e != nil {
			return e
		}
		for _, row := range rows {
			p, ok := prepared[row.ID]
			if !ok || row.TaskID != nil {
				return advertisingConflict("batch_mismatch", "批次成员已变化")
			}
			if e = validateAdvertisingExecution(repo, &row, p); e != nil {
				return e
			}
		}
		now := time.Now().UTC()
		for _, row := range rows {
			p := prepared[row.ID]
			if e = createTaskWithStorageQuotaRepository(repo, p.task, p.order, policy); e != nil {
				return e
			}
			if e = repo.ApproveAdvertisingSubmission(row.ID, p.task.ID, now); e != nil {
				return e
			}
			if e = advertisingProductionEvent(repo, userID, projectID, productionID, "admitted:"+row.ID, "submission_admitted", row.ShotID, row.ID, "", "", AdvertisingEventDetails{TaskID: p.task.ID}); e != nil {
				return e
			}
		}
		if e = repo.ApproveAdvertisingBatch(id, approvalHash, now); e != nil {
			return e
		}
		batch.ApprovedAt = &now
		batch.ApprovalHash = approvalHash
		result, e = advertisingBatchDetail(repo, userID, batch)
		return e
	})
	return result, advertisingProductionFailure(err)
}
func (s *Service) CloseAdvertisingBatch(userID, projectID, productionID, id string) (AdvertisingBatchDetail, error) {
	var result AdvertisingBatchDetail
	err := s.repo.AdvertisingProductionTransaction(userID, projectID, productionID, func(repo *repository.Repository) error {
		row, e := repo.AdvertisingBatch(productionID, id)
		if e != nil {
			return e
		}
		if row.ApprovedAt != nil {
			return advertisingConflict("batch_active", "已接纳任务不能通过关闭批次取消")
		}
		if row.ClosedAt == nil {
			now := time.Now().UTC()
			if e = repo.CloseAdvertisingBatch(id, now); e != nil {
				return e
			}
			row.ClosedAt = &now
		}
		result, e = advertisingBatchDetail(repo, userID, row)
		return e
	})
	return result, advertisingProductionFailure(err)
}
func advertisingTaskUnresolved(repo *repository.Repository, task *model.Task) (bool, error) {
	if task.Status == model.TaskStatusQueued || task.Status == model.TaskStatusRunning {
		return true, nil
	}
	if task.ProviderCancelStatus == model.ProviderCancelStatusRequested || task.ProviderCancelStatus == model.ProviderCancelStatusUncertain {
		return true, nil
	}
	if task.MediaRecoveryJSON != "" && task.MediaStage != "completed" {
		return true, nil
	}
	if task.BillingOrderID != "" {
		order, err := repo.BillingOrder(task.BillingOrderID)
		if err != nil {
			return false, err
		}
		if order.Status == model.BillingStatusUncertain {
			return true, nil
		}
	}
	// Cancellation is durable before its asynchronous provider/billing reconciliation.
	// Confirmed cancellation is authoritative even when the old poll/diagnostic remains.
	if task.Status == model.TaskStatusCancelled && task.ProviderCancelStatus == model.ProviderCancelStatusConfirmed {
		return false, nil
	}
	if task.Status == model.TaskStatusFailed || task.Status == model.TaskStatusCancelled {
		var diagnostic model.TaskExecutionDiagnostic
		if task.ExecutionDiagnosticJSON != "" {
			if err := json.Unmarshal([]byte(task.ExecutionDiagnosticJSON), &diagnostic); err != nil {
				return true, nil
			}
		}
		if diagnostic.SubmissionOutcome == "unknown" || (task.Status == model.TaskStatusCancelled && (diagnostic.TaskSubmitted || diagnostic.SubmissionOutcome == "accepted" || diagnostic.SubmissionOutcome == "submitted")) {
			return true, nil
		}
		if task.ProviderRequestID != "" && task.PollStage != "failed" && task.PollStage != "cancelled" {
			return true, nil
		}
	}
	return false, nil
}
func (s *Service) guardAdvertisingActivity(repo *repository.Repository, userID, productionID, shotID, kind string) error {
	rows, err := repo.AdvertisingTaskSubmissions(productionID)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if row.ShotID != shotID || row.Kind != kind {
			continue
		}
		task, e := repo.TaskForUser(userID, *row.TaskID)
		if e != nil {
			return e
		}
		active, e := advertisingTaskUnresolved(repo, task)
		if e != nil {
			return e
		}
		if active {
			return advertisingConflict("batch_active", "该镜头已有活动或待核对任务，请查询原任务")
		}
	}
	return nil
}
func (s *Service) GetAdvertisingActivity(userID, projectID, productionID string) (AdvertisingActivity, error) {
	result := AdvertisingActivity{Items: []AdvertisingSubmissionDetail{}, Batches: []model.AdvertisingCandidateBatch{}}
	if _, err := s.GetAdvertisingProduction(userID, projectID, productionID); err != nil {
		return result, err
	}
	rows, err := s.repo.AdvertisingTaskSubmissions(productionID)
	if err != nil {
		return result, err
	}
	seen := map[string]bool{}
	for _, row := range rows {
		task, e := s.repo.TaskForUser(userID, *row.TaskID)
		if e != nil {
			return result, e
		}
		active, e := advertisingTaskUnresolved(s.repo, task)
		if e != nil {
			return result, e
		}
		if !active {
			continue
		}
		item, e := advertisingSubmissionDetail(s.repo, userID, &row)
		if e != nil {
			return result, e
		}
		result.Items = append(result.Items, item)
		if row.BatchID != nil && !seen[*row.BatchID] {
			batch, e := s.repo.AdvertisingBatch(productionID, *row.BatchID)
			if e != nil {
				return result, e
			}
			result.Batches = append(result.Batches, *batch)
			seen[*row.BatchID] = true
		}
	}
	return result, nil
}
