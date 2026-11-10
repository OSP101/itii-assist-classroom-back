package models

import "time"

// Only a hash of the bearer challenge is persisted. A challenge proves that
// password/OAuth verification succeeded and is consumed atomically with 2FA.
type LoginChallenge struct {
	ID             string    `gorm:"primaryKey;size:64"`
	UserID         uint      `gorm:"not null;index"`
	Method         string    `gorm:"size:20;not null"`
	ExpiresAt      time.Time `gorm:"not null;index"`
	Attempts       int
	UsedBackupCode bool
	LastSentAt     *time.Time
	VerifiedAt     *time.Time
	ConsumedAt     *time.Time
	ProofHash      string `gorm:"size:64;index"`
	CreatedAt      time.Time
}
