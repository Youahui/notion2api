package model

import "time"

type APIKeyUsage struct {
	ID               uint      `json:"id"                gorm:"primaryKey"`
	APIKeyID         uint      `json:"api_key_id"        gorm:"uniqueIndex:idx_api_key_usage_day_model;not null"`
	Date             string    `json:"date"              gorm:"size:10;uniqueIndex:idx_api_key_usage_day_model;not null"`
	Model            string    `json:"model"             gorm:"size:120;uniqueIndex:idx_api_key_usage_day_model;not null"`
	RequestCount     int64     `json:"request_count"     gorm:"not null;default:0"`
	PromptTokens     int64     `json:"prompt_tokens"     gorm:"not null;default:0"`
	CompletionTokens int64     `json:"completion_tokens" gorm:"not null;default:0"`
	TotalTokens      int64     `json:"total_tokens"      gorm:"not null;default:0"`
	EstimatedTokens  int64     `json:"estimated_tokens"  gorm:"not null;default:0"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}
