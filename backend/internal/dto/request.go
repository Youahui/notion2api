package dto

import "time"

type RequestLogDTO struct {
	ID               uint       `json:"id"`
	APIKeyID         *uint      `json:"api_key_id,omitempty"`
	APIKeyName       string     `json:"api_key_name,omitempty"`
	APIKeyMasked     string     `json:"api_key_masked,omitempty"`
	Method           string     `json:"method"`
	Route            string     `json:"route"`
	StatusCode       int        `json:"status_code"`
	LatencyMs        int64      `json:"latency_ms"`
	Model            string     `json:"model"`
	PromptTokens     int64      `json:"prompt_tokens"`
	CompletionTokens int64      `json:"completion_tokens"`
	TotalTokens      int64      `json:"total_tokens"`
	Estimated        bool       `json:"estimated"`
	ErrorSummary     string     `json:"error_summary"`
	CreatedAt        *time.Time `json:"created_at,omitempty"`
}
