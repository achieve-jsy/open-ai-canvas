package model

import "time"

// Production selection is mutable; its confirmed script and assemblies are not.
type AdvertisingProduction struct {
	ID                  string               `json:"id" gorm:"primaryKey;size:36"`
	ProjectID           string               `json:"projectId" gorm:"size:36;not null;index"`
	ConfirmedRevisionID string               `json:"confirmedRevisionId" gorm:"size:36;not null;uniqueIndex"`
	Revision            int64                `json:"revision" gorm:"not null"`
	SettingsJSON        string               `json:"-" gorm:"type:text;not null"`
	CreatedAt           time.Time            `json:"createdAt"`
	UpdatedAt           time.Time            `json:"updatedAt"`
	Project             *AdvertisingProject  `json:"-" gorm:"belongsTo:Project;foreignKey:ProjectID;references:ProjectID;constraint:OnDelete:RESTRICT"`
	ConfirmedRevision   *AdvertisingRevision `json:"-" gorm:"foreignKey:ConfirmedRevisionID;references:ID;constraint:OnDelete:RESTRICT"`
}

type AdvertisingSubmission struct {
	ID                string                 `json:"id" gorm:"primaryKey;size:36"`
	ProductionID      string                 `json:"productionId" gorm:"size:36;not null;uniqueIndex:idx_ad_submission_operation,priority:1"`
	ShotID            string                 `json:"shotId" gorm:"size:36;not null"`
	Kind              string                 `json:"kind" gorm:"size:8;not null"`
	ClientOperationID string                 `json:"clientOperationId" gorm:"size:36;not null;uniqueIndex:idx_ad_submission_operation,priority:2"`
	RequestHash       string                 `json:"requestHash" gorm:"size:64;not null"`
	RequestJSON       string                 `json:"-" gorm:"type:text;not null"`
	QuoteJSON         string                 `json:"-" gorm:"type:text;not null"`
	PriceSignature    string                 `json:"-" gorm:"type:text;not null"`
	ExpiresAt         time.Time              `json:"expiresAt"`
	ApprovedAt        *time.Time             `json:"approvedAt,omitempty"`
	TaskID            *string                `json:"taskId,omitempty" gorm:"size:36;uniqueIndex"`
	CreatedAt         time.Time              `json:"createdAt"`
	UpdatedAt         time.Time              `json:"updatedAt"`
	Production        *AdvertisingProduction `json:"-" gorm:"foreignKey:ProductionID;references:ID;constraint:OnDelete:RESTRICT"`
	Task              *Task                  `json:"-" gorm:"foreignKey:TaskID;references:ID;constraint:OnDelete:RESTRICT"`
}

type AdvertisingAssembly struct {
	ID                       string                 `json:"id" gorm:"primaryKey;size:36"`
	ProductionID             string                 `json:"productionId" gorm:"size:36;not null;uniqueIndex:idx_ad_assembly_operation,priority:1"`
	ClientOperationID        string                 `json:"clientOperationId" gorm:"size:36;not null;uniqueIndex:idx_ad_assembly_operation,priority:2"`
	SourceProductionRevision int64                  `json:"sourceProductionRevision" gorm:"not null"`
	SelectionHash            string                 `json:"selectionHash" gorm:"size:64;not null"`
	TimelineJSON             string                 `json:"-" gorm:"type:text;not null"`
	CanvasID                 *string                `json:"canvasId,omitempty" gorm:"size:36"`
	CreatedAt                time.Time              `json:"createdAt"`
	UpdatedAt                time.Time              `json:"updatedAt"`
	Production               *AdvertisingProduction `json:"-" gorm:"foreignKey:ProductionID;references:ID;constraint:OnDelete:RESTRICT"`
	Canvas                   *CanvasProject         `json:"-" gorm:"foreignKey:CanvasID;references:ID;constraint:OnDelete:SET NULL"`
}

type AdvertisingRenderAttempt struct {
	ID                string               `json:"id" gorm:"primaryKey;size:36"`
	AssemblyID        string               `json:"assemblyId" gorm:"size:36;not null;uniqueIndex:idx_ad_render_operation,priority:1"`
	ClientOperationID string               `json:"clientOperationId" gorm:"size:36;not null;uniqueIndex:idx_ad_render_operation,priority:2"`
	TaskID            string               `json:"taskId" gorm:"size:36;not null;uniqueIndex"`
	CreatedAt         time.Time            `json:"createdAt"`
	Assembly          *AdvertisingAssembly `json:"-" gorm:"foreignKey:AssemblyID;references:ID;constraint:OnDelete:RESTRICT"`
	Task              *Task                `json:"-" gorm:"foreignKey:TaskID;references:ID;constraint:OnDelete:RESTRICT"`
}
