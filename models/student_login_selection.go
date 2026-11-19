package models

import (
	"time"

	"gorm.io/datatypes"
)

// StudentLoginSelection records a verified identity while the browser chooses a roster record.
type StudentLoginSelection struct {
	ID        string         `gorm:"primaryKey;size:64"`
	Email     string         `gorm:"size:255;not null"`
	Provider  string         `gorm:"size:20;not null"`
	Metadata  datatypes.JSON `gorm:"type:jsonb"`
	ExpiresAt time.Time      `gorm:"not null;index"`
}
