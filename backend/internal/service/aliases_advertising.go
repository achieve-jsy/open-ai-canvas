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
type AdvertisingDraft = app.AdvertisingDraft
type AdvertisingBrief = app.AdvertisingBrief
type AdvertisingBrand = app.AdvertisingBrand
type AdvertisingShot = app.AdvertisingShot
type AdvertisingDetail = app.AdvertisingDetail
type AdvertisingConfirmation = app.AdvertisingConfirmation
type AdvertisingRevisionDetail = app.AdvertisingRevisionDetail
