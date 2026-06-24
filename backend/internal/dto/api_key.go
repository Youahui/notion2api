package dto

import "time"

type APIKeyDTO struct {
	ID              uint       `json:"id"`
	Name            string     `json:"name"`
	APIKey          string     `json:"api_key"`
	APIKeyMasked    string     `json:"api_key_masked,omitempty"`
	KeyPrefix       string     `json:"key_prefix"`
	KeySuffix       string     `json:"key_suffix"`
	Enabled         bool       `json:"enabled"`
	OwnerAdminID    *uint      `json:"owner_admin_id"`
	GroupID         *uint      `json:"group_id"`
	GroupName       string     `json:"group_name,omitempty"`
	ExpiresAt       *time.Time `json:"expires_at"`
	LastUsedAt      *time.Time `json:"last_used_at"`
	TokenQuotaTotal int64      `json:"token_quota_total"`
	TokenQuotaDaily int64      `json:"token_quota_daily"`
	TotalRequests   int64      `json:"total_requests"`
	TotalTokens     int64      `json:"total_tokens"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type CreateAPIKeyRequest struct {
	Name            string     `json:"name" binding:"required"`
	Enabled         *bool      `json:"enabled"`
	GroupID         *uint      `json:"group_id"`
	ExpiresAt       *time.Time `json:"expires_at"`
	TokenQuotaTotal int64      `json:"token_quota_total"`
	TokenQuotaDaily int64      `json:"token_quota_daily"`
}

type CreateAPIKeyResponse struct {
	APIKeyDTO
	Secret string `json:"secret"`
}

type UpdateAPIKeyRequest struct {
	Name            *string    `json:"name"`
	GroupID         *uint      `json:"group_id"`
	ExpiresAt       *time.Time `json:"expires_at"`
	TokenQuotaTotal *int64     `json:"token_quota_total"`
	TokenQuotaDaily *int64     `json:"token_quota_daily"`
}

type APIKeyUsageDTO struct {
	ID               uint      `json:"id"`
	APIKeyID         uint      `json:"api_key_id"`
	APIKeyName       string    `json:"api_key_name,omitempty"`
	APIKey           string    `json:"api_key"`
	APIKeyMasked     string    `json:"api_key_masked,omitempty"`
	Date             string    `json:"date"`
	Model            string    `json:"model"`
	RequestCount     int64     `json:"request_count"`
	PromptTokens     int64     `json:"prompt_tokens"`
	CompletionTokens int64     `json:"completion_tokens"`
	TotalTokens      int64     `json:"total_tokens"`
	EstimatedTokens  int64     `json:"estimated_tokens"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}
