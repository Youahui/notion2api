package model

import "time"

type RequestLog struct {
	ID               uint      `json:"id"                gorm:"primaryKey"`
	APIKeyID         *uint     `json:"api_key_id"        gorm:"index"`
	Method           string    `json:"method"            gorm:"size:16"`
	Route            string    `json:"route"             gorm:"size:160;index"`
	StatusCode       int       `json:"status_code"       gorm:"index"`
	LatencyMs        int64     `json:"latency_ms"`
	Model            string    `json:"model"             gorm:"size:120;index"`
	PromptTokens     int64     `json:"prompt_tokens"`
	CompletionTokens int64     `json:"completion_tokens"`
	TotalTokens      int64     `json:"total_tokens"`
	Estimated        bool      `json:"estimated"`
	ErrorSummary     string    `json:"error_summary"     gorm:"size:512"`
	CreatedAt        time.Time `json:"created_at"        gorm:"index"`
}
