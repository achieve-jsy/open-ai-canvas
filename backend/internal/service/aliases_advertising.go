package service

import "yingce/backend/internal/app"
import "yingce/backend/internal/kernel"

const (
	ReasonAdvertisingRevisionConflict    = kernel.ReasonAdvertisingRevisionConflict
	ReasonAdvertisingInvalidDocument     = kernel.ReasonAdvertisingInvalidDocument
	ReasonAdvertisingResourceUnavailable = kernel.ReasonAdvertisingResourceUnavailable
	ReasonAdvertisingResourceInUse       = kernel.ReasonAdvertisingResourceInUse
	ReasonAdvertisingActiveTasks         = kernel.ReasonAdvertisingActiveTasks
)

type AdvertisingCreateRequest = app.AdvertisingCreateRequest
type AdvertisingSaveRequest = app.AdvertisingSaveRequest
type AdvertisingScriptPlanRequest = app.AdvertisingScriptPlanRequest
type AdvertisingScriptPlanResult = app.AdvertisingScriptPlanResult
type AdvertisingDraft = app.AdvertisingDraft
type AdvertisingBrief = app.AdvertisingBrief
type AdvertisingBrand = app.AdvertisingBrand
type AdvertisingShot = app.AdvertisingShot
type AdvertisingDetail = app.AdvertisingDetail
type AdvertisingConfirmation = app.AdvertisingConfirmation
type AdvertisingRevisionDetail = app.AdvertisingRevisionDetail
type AdvertisingProductionSettings = app.AdvertisingProductionSettings
type AdvertisingProductionDetail = app.AdvertisingProductionDetail
type AdvertisingBatchRequest = app.AdvertisingBatchRequest
type AdvertisingBatchApproval = app.AdvertisingBatchApproval
type AdvertisingBatchDetail = app.AdvertisingBatchDetail
type AdvertisingActivity = app.AdvertisingActivity
type AdvertisingSubmissionRequest = app.AdvertisingSubmissionRequest
type AdvertisingSubmissionDetail = app.AdvertisingSubmissionDetail
type AdvertisingAssemblyDetail = app.AdvertisingAssemblyDetail
type AdvertisingSourceSettings = app.AdvertisingSourceSettings
type AdvertisingCutSource = app.AdvertisingCutSource
type AdvertisingRenderAttemptDetail = app.AdvertisingRenderAttemptDetail
type AdvertisingReviewRequest = app.AdvertisingReviewRequest
type AdvertisingReviewResult = app.AdvertisingReviewResult
type AdvertisingIssueRequest = app.AdvertisingIssueRequest
type AdvertisingIssueResolution = app.AdvertisingIssueResolution
type AdvertisingEventDetail = app.AdvertisingEventDetail
