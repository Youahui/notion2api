package model

import (
	"time"

	"gorm.io/gorm"
)

type Conversation struct {
	ID         uint           `json:"id"          gorm:"primaryKey"`
	ExternalID string         `json:"external_id" gorm:"size:120;uniqueIndex;not null"`
	APIKeyID   *uint          `json:"api_key_id"  gorm:"index"`
	AccountID  *uint          `json:"account_id"  gorm:"index"`
	Model      string         `json:"model"       gorm:"size:120"`
	Title      string         `json:"title"       gorm:"size:255"`
	Status     string         `json:"status"      gorm:"size:32;index;not null;default:active"`
	CreatedAt  time.Time      `json:"created_at"`
	UpdatedAt  time.Time      `json:"updated_at"`
	DeletedAt  gorm.DeletedAt `json:"-"           gorm:"index"`
}
