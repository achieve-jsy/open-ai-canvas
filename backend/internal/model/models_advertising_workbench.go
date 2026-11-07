package model

import "time"

// A batch freezes one shot's independently billed candidates, not a multi-output Task.
type AdvertisingCandidateBatch struct {
	ID                string                 `json:"id" gorm:"primaryKey;size:36"`
	ProductionID      string                 `json:"productionId" gorm:"size:36;not null;uniqueIndex:idx_ad_batch_operation,priority:1"`
	ClientOperationID string                 `json:"clientOperationId" gorm:"size:36;not null;uniqueIndex:idx_ad_batch_operation,priority:2"`
	ShotID            string                 `json:"shotId" gorm:"size:36;not null"`
	Kind              string                 `json:"kind" gorm:"size:8;not null"`
	Count             int                    `json:"count" gorm:"not null;check:ad_batch_count,count >= 1 AND count <= 3"`
	Adjustment        string                 `json:"adjustment" gorm:"size:1000;not null"`
	RequestHash       string                 `json:"requestHash" gorm:"size:64;not null"`
	ApprovedAt        *time.Time             `json:"approvedAt,omitempty"`
	ApprovalHash      string                 `json:"approvalHash,omitempty" gorm:"size:64"`
	ClosedAt          *time.Time             `json:"closedAt,omitempty"`
	CreatedAt         time.Time              `json:"createdAt"`
	UpdatedAt         time.Time              `json:"updatedAt"`
	Production        *AdvertisingProduction `json:"-" gorm:"foreignKey:ProductionID;references:ID;constraint:OnDelete:RESTRICT"`
}
