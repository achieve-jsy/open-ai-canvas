package model

import "time"

// AdvertisingScriptPlan is operator-funded and never enters the billable task queue.
type AdvertisingScriptPlan struct {
	ID             string    `gorm:"primaryKey;size:36"`
	UserID         string    `gorm:"size:36;not null;index:idx_ad_script_user_created,priority:1"`
	ProjectID      string    `gorm:"size:80;not null;uniqueIndex:idx_ad_script_operation,priority:1;index"`
	OperationID    string    `gorm:"size:36;not null;uniqueIndex:idx_ad_script_operation,priority:2"`
	SourceRevision int64     `gorm:"not null"`
	SnapshotJSON   string    `gorm:"type:text;not null"`
	ResultJSON     string    `gorm:"type:text"`
	Status         string    `gorm:"size:16;not null;index"`
	Error          string    `gorm:"size:240"`
	CreatedAt      time.Time `gorm:"index:idx_ad_script_user_created,priority:2"`
	UpdatedAt      time.Time
}
