package model

import (
	"time"

	"gorm.io/gorm"
)

type Response struct {
	ID             uint           `json:"id"              gorm:"primaryKey"`
	ExternalID     string         `json:"external_id"     gorm:"size:120;uniqueIndex;not null"`
	APIKeyID       *uint          `json:"api_key_id"      gorm:"index"`
	ConversationID *uint          `json:"conversation_id" gorm:"index"`
	Model          string         `json:"model"           gorm:"size:120"`
	PayloadJSON    string         `json:"payload_json"    gorm:"type:text"`
	ExpiresAt      *time.Time     `json:"expires_at"      gorm:"index"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
	DeletedAt      gorm.DeletedAt `json:"-"               gorm:"index"`
}
