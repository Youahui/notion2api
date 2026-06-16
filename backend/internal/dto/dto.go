package dto

import "time"

type LoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type LoginResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
	Admin     AdminDTO  `json:"admin"`
}

type AdminDTO struct {
	ID          uint       `json:"id"`
	Username    string     `json:"username"`
	Role        string     `json:"role"`
	Enabled     bool       `json:"enabled"`
	LastLoginAt *time.Time `json:"last_login_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type CreateAdminRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
	Role     string `json:"role"`
	Enabled  *bool  `json:"enabled"`
}

type UpdateAdminRequest struct {
	Password *string `json:"password"`
	Role     *string `json:"role"`
	Enabled  *bool   `json:"enabled"`
}

type APIKeyDTO struct {
	ID              uint       `json:"id"`
	Name            string     `json:"name"`
	KeyPrefix       string     `json:"key_prefix"`
	KeySuffix       string     `json:"key_suffix"`
	Enabled         bool       `json:"enabled"`
	OwnerAdminID    *uint      `json:"owner_admin_id"`
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
	ExpiresAt       *time.Time `json:"expires_at"`
	TokenQuotaTotal *int64     `json:"token_quota_total"`
	TokenQuotaDaily *int64     `json:"token_quota_daily"`
}

type ChatMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type ChatCompletionRequest struct {
	Model              string        `json:"model"`
	Messages           []ChatMessage `json:"messages"`
	Stream             bool          `json:"stream"`
	ConversationID     string        `json:"conversation_id"`
	Conversation       string        `json:"conversation"`
	ThreadID           string        `json:"thread_id"`
	Thread             string        `json:"thread"`
	NotionThreadID     string        `json:"notion_thread_id"`
	AccountEmail       string        `json:"account_email"`
	NotionAccountEmail string        `json:"notion_account_email"`
	UseWebSearch       *bool         `json:"use_web_search"`
	Metadata           any           `json:"metadata"`
	Tools              any           `json:"tools"`
	Attachments        any           `json:"attachments"`
}

type UsageDTO struct {
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	TotalTokens      int64 `json:"total_tokens"`
	Estimated        bool  `json:"estimated"`
}

type ModelDTO struct {
	ID          string `json:"id"`
	Object      string `json:"object"`
	Created     int64  `json:"created"`
	OwnedBy     string `json:"owned_by"`
	Name        string `json:"name"`
	Family      string `json:"family"`
	Group       string `json:"group"`
	Beta        bool   `json:"beta"`
	NotionModel string `json:"notion_model"`
}

type AccountDTO struct {
	ID                  uint       `json:"id"`
	Email               string     `json:"email"`
	Enabled             bool       `json:"enabled"`
	Active              bool       `json:"active"`
	UserID              string     `json:"user_id"`
	UserName            string     `json:"user_name"`
	SpaceID             string     `json:"space_id"`
	SpaceViewID         string     `json:"space_view_id"`
	SpaceName           string     `json:"space_name"`
	ClientVersion       string     `json:"client_version"`
	Status              string     `json:"status"`
	Priority            int        `json:"priority"`
	MaxConcurrency      int        `json:"max_concurrency"`
	ProxyMode           string     `json:"proxy_mode"`
	ProxyURL            string     `json:"proxy_url"`
	ProxyHTTPURL        string     `json:"proxy_http_url"`
	ProxyHTTPSURL       string     `json:"proxy_https_url"`
	ProbeJSON           string     `json:"probe_json"`
	HasAuthToken        bool       `json:"has_auth_token"`
	HasCookie           bool       `json:"has_cookie"`
	LastError           string     `json:"last_error"`
	LastLoginAt         *time.Time `json:"last_login_at"`
	LastUsedAt          *time.Time `json:"last_used_at"`
	LastSuccessAt       *time.Time `json:"last_success_at"`
	LastRefreshAt       *time.Time `json:"last_refresh_at"`
	ConsecutiveFailures int        `json:"consecutive_failures"`
	TotalSuccesses      int64      `json:"total_successes"`
	TotalFailures       int64      `json:"total_failures"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

type UpsertAccountRequest struct {
	Email          string `json:"email" binding:"required"`
	Enabled        *bool  `json:"enabled"`
	Active         *bool  `json:"active"`
	UserID         string `json:"user_id"`
	UserName       string `json:"user_name"`
	SpaceID        string `json:"space_id"`
	SpaceViewID    string `json:"space_view_id"`
	SpaceName      string `json:"space_name"`
	ClientVersion  string `json:"client_version"`
	Status         string `json:"status"`
	Priority       int    `json:"priority"`
	MaxConcurrency int    `json:"max_concurrency"`
	ProxyMode      string `json:"proxy_mode"`
	ProxyURL       string `json:"proxy_url"`
	ProxyHTTPURL   string `json:"proxy_http_url"`
	ProxyHTTPSURL  string `json:"proxy_https_url"`
	AuthToken      string `json:"auth_token"`
	Cookie         string `json:"cookie"`
	ProbeJSON      string `json:"probe_json"`
	ClearAuthToken bool   `json:"clear_auth_token"`
	ClearCookie    bool   `json:"clear_cookie"`
}

type AccountTestRequest struct {
	Prompt string `json:"prompt"`
	Model  string `json:"model"`
}

type AccountTestResponse struct {
	Success       bool           `json:"success"`
	Account       AccountDTO     `json:"account"`
	NotionRequest map[string]any `json:"notion_request"`
	Text          string         `json:"text"`
}
