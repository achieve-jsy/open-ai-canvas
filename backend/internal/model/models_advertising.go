package model

import "time"

// AdvertisingProject stores the editable draft; confirmed versions live separately.
type AdvertisingProject struct {
	ProjectID                 string    `json:"projectId" gorm:"primaryKey;size:36"`
	Revision                  int64     `json:"revision" gorm:"not null"`
	DraftJSON                 string    `json:"draftJson" gorm:"type:text;not null"`
	LatestConfirmedRevisionID *string   `json:"latestConfirmedRevisionId,omitempty" gorm:"size:36"`
	CreatedAt                 time.Time `json:"createdAt"`
	UpdatedAt                 time.Time `json:"updatedAt"`
	Project                   *Project  `json:"-" gorm:"foreignKey:ProjectID;references:ID;constraint:OnDelete:RESTRICT"`
}

type AdvertisingRevision struct {
	ID             string              `json:"id" gorm:"primaryKey;size:36"`
	ProjectID      string              `json:"projectId" gorm:"size:36;not null;uniqueIndex:idx_advertising_source_revision,priority:1"`
	SourceRevision int64               `json:"sourceRevision" gorm:"not null;uniqueIndex:idx_advertising_source_revision,priority:2"`
	SnapshotJSON   string              `json:"snapshotJson" gorm:"type:text;not null"`
	CreatedAt      time.Time           `json:"createdAt"`
	Project        *AdvertisingProject `json:"-" gorm:"belongsTo:Project;foreignKey:ProjectID;references:ProjectID;constraint:OnDelete:RESTRICT"`
}

// Explicit references protect both drafts and immutable snapshots from resource deletion.
type AdvertisingResourceReference struct {
	ProjectID  string              `json:"projectId" gorm:"primaryKey;size:36"`
	ScopeKey   string              `json:"scopeKey" gorm:"primaryKey;size:36"`
	ResourceID string              `json:"resourceId" gorm:"primaryKey;size:36;index:idx_advertising_resource"`
	Project    *AdvertisingProject `json:"-" gorm:"belongsTo:Project;foreignKey:ProjectID;references:ProjectID;constraint:OnDelete:RESTRICT"`
	Resource   *Resource           `json:"-" gorm:"foreignKey:ResourceID;references:ID;constraint:OnDelete:RESTRICT"`
}
