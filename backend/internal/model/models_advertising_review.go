package model

import "time"

// Events and reviews are append-only business records; only issue resolution is mutable.
type AdvertisingEvent struct {
	ID                 string              `json:"id" gorm:"primaryKey;size:36"`
	ProjectID          string              `json:"projectId" gorm:"size:36;not null;uniqueIndex:idx_ad_event_key,priority:1;index"`
	EventKey           string              `json:"-" gorm:"size:160;not null;uniqueIndex:idx_ad_event_key,priority:2"`
	ProductionID       string              `json:"productionId,omitempty" gorm:"size:36;index"`
	ShotID             string              `json:"shotId,omitempty" gorm:"size:36;index"`
	Kind               string              `json:"kind" gorm:"size:40;not null;index"`
	ActorID            string              `json:"actorId" gorm:"size:36;not null"`
	SourceRevision     int64               `json:"sourceRevision"`
	ProductionRevision int64               `json:"productionRevision"`
	SubmissionID       string              `json:"submissionId,omitempty" gorm:"size:36"`
	AssemblyID         string              `json:"assemblyId,omitempty" gorm:"size:36"`
	RenderAttemptID    string              `json:"renderAttemptId,omitempty" gorm:"size:36"`
	DetailsJSON        string              `json:"-" gorm:"type:text;not null"`
	CreatedAt          time.Time           `json:"createdAt" gorm:"index"`
	Project            *AdvertisingProject `json:"-" gorm:"belongsTo:Project;foreignKey:ProjectID;references:ProjectID;constraint:OnDelete:RESTRICT"`
}

type AdvertisingReview struct {
	AcceptedReviewID         *string                `json:"acceptedReviewId,omitempty" gorm:"size:36"`
	ID                       string                 `json:"id" gorm:"primaryKey;size:36"`
	ProductionID             string                 `json:"productionId" gorm:"size:36;not null;uniqueIndex:idx_ad_review_operation,priority:1"`
	ClientOperationID        string                 `json:"clientOperationId" gorm:"size:36;not null;uniqueIndex:idx_ad_review_operation,priority:2"`
	RequestHash              string                 `json:"-" gorm:"size:64;not null"`
	Checkpoint               string                 `json:"checkpoint" gorm:"size:12;not null"`
	SubmissionID             string                 `json:"submissionId,omitempty" gorm:"size:36"`
	AssemblyID               string                 `json:"assemblyId,omitempty" gorm:"size:36"`
	RenderAttemptID          string                 `json:"renderAttemptId,omitempty" gorm:"size:36"`
	ResourceID               string                 `json:"resourceId,omitempty" gorm:"size:36"`
	SourceRevision           int64                  `json:"sourceRevision"`
	SourceProductionRevision int64                  `json:"sourceProductionRevision"`
	SelectionHash            string                 `json:"selectionHash,omitempty" gorm:"size:64"`
	Decision                 string                 `json:"decision" gorm:"size:8;not null"`
	Reason                   string                 `json:"reason" gorm:"size:2000;not null"`
	ClearSelected            bool                   `json:"clearSelected"`
	ActorID                  string                 `json:"actorId" gorm:"size:36;not null"`
	ProductionJSON           string                 `json:"-" gorm:"type:text;not null"`
	CreatedAt                time.Time              `json:"createdAt"`
	Production               *AdvertisingProduction `json:"-" gorm:"foreignKey:ProductionID;references:ID;constraint:OnDelete:RESTRICT"`
}

type AdvertisingIssue struct {
	ID                string                 `json:"id" gorm:"primaryKey;size:36"`
	ProductionID      string                 `json:"productionId" gorm:"size:36;not null;uniqueIndex:idx_ad_issue_operation,priority:1"`
	ClientOperationID string                 `json:"clientOperationId" gorm:"size:36;not null;uniqueIndex:idx_ad_issue_operation,priority:2"`
	RequestHash       string                 `json:"-" gorm:"size:64;not null"`
	SubmissionID      string                 `json:"submissionId,omitempty" gorm:"size:36"`
	AssemblyID        string                 `json:"assemblyId,omitempty" gorm:"size:36"`
	RenderAttemptID   string                 `json:"renderAttemptId,omitempty" gorm:"size:36"`
	AtMs              *int64                 `json:"atMs,omitempty"`
	Category          string                 `json:"category" gorm:"size:24;not null"`
	Note              string                 `json:"note" gorm:"size:2000;not null"`
	ActorID           string                 `json:"actorId" gorm:"size:36;not null"`
	Revision          int64                  `json:"revision" gorm:"not null"`
	Status            string                 `json:"status" gorm:"size:12;not null"`
	ResolutionNote    string                 `json:"resolutionNote" gorm:"size:2000"`
	ResolvedBy        string                 `json:"resolvedBy,omitempty" gorm:"size:36"`
	CreatedAt         time.Time              `json:"createdAt"`
	UpdatedAt         time.Time              `json:"updatedAt"`
	Production        *AdvertisingProduction `json:"-" gorm:"foreignKey:ProductionID;references:ID;constraint:OnDelete:RESTRICT"`
}
