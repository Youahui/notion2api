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
	APIKey          string     `json:"api_key"`
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
	Role         string `json:"role"`
	Content      any    `json:"content"`
	Name         string `json:"name,omitempty"`
	ToolCallID   string `json:"tool_call_id,omitempty"`
	ToolCalls    any    `json:"tool_calls,omitempty"`
	FunctionCall any    `json:"function_call,omitempty"`
}

type ChatCompletionRequest struct {
	Model              string        `json:"model"`
	Messages           []ChatMessage `json:"messages"`
	Stream             bool          `json:"stream"`
	ConversationID     string        `json:"conversation_id"`
	Conversation       string        `json:"conversation"`
	PromptCacheKey     string        `json:"prompt_cache_key"`
	ThreadID           string        `json:"thread_id"`
	Thread             string        `json:"thread"`
	NotionThreadID     string        `json:"notion_thread_id"`
	User               string        `json:"user"`
	AccountEmail       string        `json:"account_email"`
	NotionAccountEmail string        `json:"notion_account_email"`
	UseWebSearch       *bool         `json:"use_web_search"`
	ResponseFormat     any           `json:"response_format"`
	Metadata           any           `json:"metadata"`
	Tools              any           `json:"tools"`
	ToolChoice         any           `json:"tool_choice"`
	ParallelToolCalls  *bool         `json:"parallel_tool_calls"`
	Attachments        any           `json:"attachments"`
}

type UsageDTO struct {
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	TotalTokens      int64 `json:"total_tokens"`
	Estimated        bool  `json:"estimated"`
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
	PlanType            string     `json:"plan_type"`
	Status              string     `json:"status"`
	Priority            int        `json:"priority"`
	HourlyQuota         int        `json:"hourly_quota"`
	WindowStartedAt     *time.Time `json:"window_started_at"`
	WindowRequestCount  int        `json:"window_request_count"`
	CooldownUntil       *time.Time `json:"cooldown_until"`
	MaxConcurrency      int        `json:"max_concurrency"`
	ProxyMode           string     `json:"proxy_mode"`
	ProxyURL            string     `json:"proxy_url"`
	ProxyHTTPURL        string     `json:"proxy_http_url"`
	ProxyHTTPSURL       string     `json:"proxy_https_url"`
	ProbeJSON           string     `json:"probe_json"`
	ProfileDir          string     `json:"profile_dir"`
	StorageStatePath    string     `json:"storage_state_path"`
	PendingStatePath    string     `json:"pending_state_path"`
	LoginMessage        string     `json:"login_message"`
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

type AccountSpaceDTO struct {
	ID             string `json:"id"`
	SpaceID        string `json:"space_id"`
	SpaceViewID    string `json:"space_view_id"`
	Name           string `json:"name"`
	PlanType       string `json:"plan_type"`
	MembershipType string `json:"membership_type"`
	Role           string `json:"role"`
	Domain         string `json:"domain"`
	Region         string `json:"region"`
	Current        bool   `json:"current"`
}

type UpsertAccountRequest struct {
	Email            string `json:"email" binding:"required"`
	Enabled          *bool  `json:"enabled"`
	Active           *bool  `json:"active"`
	UserID           string `json:"user_id"`
	UserName         string `json:"user_name"`
	SpaceID          string `json:"space_id"`
	SpaceViewID      string `json:"space_view_id"`
	SpaceName        string `json:"space_name"`
	ClientVersion    string `json:"client_version"`
	PlanType         string `json:"plan_type"`
	Status           string `json:"status"`
	Priority         int    `json:"priority"`
	HourlyQuota      int    `json:"hourly_quota"`
	MaxConcurrency   int    `json:"max_concurrency"`
	ProxyMode        string `json:"proxy_mode"`
	ProxyURL         string `json:"proxy_url"`
	ProxyHTTPURL     string `json:"proxy_http_url"`
	ProxyHTTPSURL    string `json:"proxy_https_url"`
	ProfileDir       string `json:"profile_dir"`
	StorageStatePath string `json:"storage_state_path"`
	PendingStatePath string `json:"pending_state_path"`
	AuthToken        string `json:"auth_token"`
	Cookie           string `json:"cookie"`
	ProbeJSON        string `json:"probe_json"`
	ClearAuthToken   bool   `json:"clear_auth_token"`
	ClearCookie      bool   `json:"clear_cookie"`
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

type ManualImportAccountRequest struct {
	Email          string `json:"email"`
	UserID         string `json:"user_id"`
	UserName       string `json:"user_name"`
	SpaceID        string `json:"space_id"`
	SpaceViewID    string `json:"space_view_id"`
	SpaceName      string `json:"space_name"`
	ClientVersion  string `json:"client_version"`
	CookieHeader   string `json:"cookie_header"`
	TokenV2        string `json:"token_v2"`
	ProbeJSONText  string `json:"probe_json_text"`
	Active         bool   `json:"active"`
	Priority       int    `json:"priority"`
	HourlyQuota    int    `json:"hourly_quota"`
	MaxConcurrency int    `json:"max_concurrency"`
}

type AccountLoginStartRequest struct {
	Email string `json:"email" binding:"required"`
}

type AccountLoginVerifyRequest struct {
	Email string `json:"email" binding:"required"`
	Code  string `json:"code" binding:"required"`
}

type AccountLoginStatusDTO struct {
	ID               uint       `json:"id"`
	Email            string     `json:"email"`
	Status           string     `json:"status"`
	Success          bool       `json:"success"`
	Message          string     `json:"message"`
	Error            string     `json:"error"`
	ClientVersion    string     `json:"client_version"`
	DeviceID         string     `json:"device_id"`
	ProfileDir       string     `json:"profile_dir"`
	StorageStatePath string     `json:"storage_state_path"`
	PendingStatePath string     `json:"pending_state_path"`
	ProbePath        string     `json:"probe_path"`
	CurrentURL       string     `json:"current_url"`
	FinalURL         string     `json:"final_url"`
	Title            string     `json:"title"`
	LastLoginAt      *time.Time `json:"last_login_at"`
	ExpiresAt        *time.Time `json:"expires_at"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

type AccountLoginResponse struct {
	Success bool                  `json:"success"`
	Account AccountDTO            `json:"account"`
	Status  AccountLoginStatusDTO `json:"status"`
}

type NotionConversationAttachmentDTO struct {
	Name        string `json:"name,omitempty"`
	ContentType string `json:"content_type,omitempty"`
	Source      string `json:"source,omitempty"`
	URL         string `json:"url,omitempty"`
}

type NotionConversationMessageDTO struct {
	ID          string                            `json:"id"`
	Role        string                            `json:"role"`
	Status      string                            `json:"status"`
	Content     string                            `json:"content"`
	CreatedAt   *time.Time                        `json:"created_at,omitempty"`
	UpdatedAt   *time.Time                        `json:"updated_at,omitempty"`
	Attachments []NotionConversationAttachmentDTO `json:"attachments,omitempty"`
}

type NotionConversationSummaryDTO struct {
	ID                   string     `json:"id"`
	Title                string     `json:"title"`
	Origin               string     `json:"origin"`
	RemoteOnly           bool       `json:"remote_only"`
	Preview              string     `json:"preview,omitempty"`
	Status               string     `json:"status"`
	Source               string     `json:"source"`
	Transport            string     `json:"transport"`
	AccountEmail         string     `json:"account_email"`
	SpaceID              string     `json:"space_id"`
	ThreadID             string     `json:"thread_id"`
	CreatedByDisplayName string     `json:"created_by_display_name,omitempty"`
	CreatedAt            *time.Time `json:"created_at,omitempty"`
	UpdatedAt            *time.Time `json:"updated_at,omitempty"`
	MessageCount         int        `json:"message_count,omitempty"`
}

type NotionConversationDetailDTO struct {
	NotionConversationSummaryDTO
	Messages []NotionConversationMessageDTO `json:"messages"`
}
