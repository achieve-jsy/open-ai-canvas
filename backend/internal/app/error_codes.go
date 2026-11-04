package app

import "yingce/backend/internal/kernel"

// 错误码和原因已迁到 kernel；此处保留兼容别名。
const (
	CodeOK                  = kernel.CodeOK
	CodeInvalidArgument     = kernel.CodeInvalidArgument
	CodeUnauthorized        = kernel.CodeUnauthorized
	CodeForbidden           = kernel.CodeForbidden
	CodeNotFound            = kernel.CodeNotFound
	CodeConflict            = kernel.CodeConflict
	CodeTooManyRequests     = kernel.CodeTooManyRequests
	CodeInternal            = kernel.CodeInternal
	CodeBadGateway          = kernel.CodeBadGateway
	CodeUnavailable         = kernel.CodeUnavailable
	CodeTimeout             = kernel.CodeTimeout
	CodeQuotaExceeded       = kernel.CodeQuotaExceeded
	CodeStorageReadOnly     = kernel.CodeStorageReadOnly
	CodeIdempotencyConflict = kernel.CodeIdempotencyConflict
	CodeRateLimited         = kernel.CodeRateLimited
)

type ErrorReason = kernel.ErrorReason

const (
	ReasonAdvertisingRevisionConflict     = kernel.ReasonAdvertisingRevisionConflict
	ReasonAdvertisingInvalidDocument      = kernel.ReasonAdvertisingInvalidDocument
	ReasonAdvertisingResourceUnavailable  = kernel.ReasonAdvertisingResourceUnavailable
	ReasonAdvertisingResourceInUse        = kernel.ReasonAdvertisingResourceInUse
	ReasonAdvertisingActiveTasks          = kernel.ReasonAdvertisingActiveTasks
	ReasonAdvertisingProductionConflict   = kernel.ReasonAdvertisingProductionConflict
	ReasonAdvertisingAssemblyChanged      = kernel.ReasonAdvertisingAssemblyChanged
	ReasonAdvertisingApprovalRequired     = kernel.ReasonAdvertisingApprovalRequired
	ReasonAdvertisingRenderRetryRequired  = kernel.ReasonAdvertisingRenderRetryRequired
	ReasonAdvertisingSubmissionMismatch   = kernel.ReasonAdvertisingSubmissionMismatch
	ReasonAdvertisingQuoteChanged         = kernel.ReasonAdvertisingQuoteChanged
	ReasonAdvertisingQuoteExpired         = kernel.ReasonAdvertisingQuoteExpired
	ReasonAdvertisingDurationInvalid      = kernel.ReasonAdvertisingDurationInvalid
	ReasonAdvertisingCandidateUnavailable = kernel.ReasonAdvertisingCandidateUnavailable
	ReasonAdvertisingOperationConflict    = kernel.ReasonAdvertisingOperationConflict
	ReasonAdvertisingMediaProbeFailed     = kernel.ReasonAdvertisingMediaProbeFailed
	ReasonAdvertisingResourceChanged      = kernel.ReasonAdvertisingResourceChanged
	ReasonInvalidArgument                 = kernel.ReasonInvalidArgument
	ReasonUnauthorized                    = kernel.ReasonUnauthorized
	ReasonForbidden                       = kernel.ReasonForbidden
	ReasonNotFound                        = kernel.ReasonNotFound
	ReasonConflict                        = kernel.ReasonConflict
	ReasonProjectNameConflict             = kernel.ReasonProjectNameConflict
	ReasonFailedPrecondition              = kernel.ReasonFailedPrecondition
	ReasonQuotaExceeded                   = kernel.ReasonQuotaExceeded
	ReasonStorageCapacity                 = kernel.ReasonStorageCapacityExceeded
	ReasonStorageReadOnly                 = kernel.ReasonStorageReadOnly
	ReasonDramaBudget                     = kernel.ReasonDramaBudgetExhausted
	ReasonDramaSegmentChanged             = kernel.ReasonDramaSegmentChanged
	ReasonAgentInterjectionRunFinished    = kernel.ReasonAgentInterjectionRunFinished
	ReasonAgentInterjectionDelivered      = kernel.ReasonAgentInterjectionDelivered
	ReasonAgentRunStateBusy               = kernel.ReasonAgentRunStateBusy
	ReasonRateLimited                     = kernel.ReasonRateLimited
	ReasonUnavailable                     = kernel.ReasonUnavailable
	ReasonTimeout                         = kernel.ReasonTimeout
	ReasonInternal                        = kernel.ReasonInternal
	ReasonBadGateway                      = kernel.ReasonBadGateway
	ReasonUpstreamDNSFailed               = kernel.ReasonUpstreamDNSFailed
)

func ReasonForStatus(status int) ErrorReason {
	return kernel.ReasonForStatus(status)
}
