package models

import "time"

// DeferredEmail holds instructor notification emails raised outside the
// allowed sending window, until the window opens again.
type DeferredEmail struct {
	ID        uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	ToEmail   string     `gorm:"type:varchar(255);not null" json:"to_email"`
	Subject   string     `gorm:"type:text;not null" json:"subject"`
	HTML      string     `gorm:"type:text" json:"html"`
	Plain     string     `gorm:"type:text" json:"plain"`
	SendAfter time.Time  `gorm:"type:timestamptz;not null" json:"send_after"`
	Attempts  int        `json:"attempts"`
	LastError string     `gorm:"type:text" json:"last_error"`
	SentAt    *time.Time `gorm:"type:timestamptz" json:"sent_at,omitempty"`
	FailedAt  *time.Time `gorm:"type:timestamptz" json:"failed_at,omitempty"`
	CreatedAt time.Time  `gorm:"type:timestamptz" json:"created_at"`
}
