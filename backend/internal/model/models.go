package model

import (
	"time"

	"gorm.io/gorm"
)

type AdminUser struct {
	ID           uint           `json:"id" gorm:"primaryKey"`
	Username     string         `json:"username" gorm:"size:64;uniqueIndex;not null"`
	PasswordHash string         `json:"-" gorm:"size:255;not null"`
	Role         string         `json:"role" gorm:"size:32;not null;default:admin"`
	Enabled      bool           `json:"enabled" gorm:"not null;default:true"`
	LastLoginAt  *time.Time     `json:"last_login_at"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	DeletedAt    gorm.DeletedAt `json:"-" gorm:"index"`
}

type AdminToken struct {
	ID          uint       `json:"id" gorm:"primaryKey"`
	AdminUserID uint       `json:"admin_user_id" gorm:"index;not null"`
	TokenHash   string     `json:"-" gorm:"size:128;uniqueIndex;not null"`
	ExpiresAt   time.Time  `json:"expires_at" gorm:"index;not null"`
	RevokedAt   *time.Time `json:"revoked_at"`
	UserAgent   string     `json:"user_agent" gorm:"size:255"`
	ClientIP    string     `json:"client_ip" gorm:"size:64"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type APIKey struct {
	ID              uint           `json:"id" gorm:"primaryKey"`
	Name            string         `json:"name" gorm:"size:120;not null"`
	KeyHash         string         `json:"-" gorm:"size:128;uniqueIndex;not null"`
	APIKey          string         `json:"api_key" gorm:"type:text"`
	KeyPrefix       string         `json:"key_prefix" gorm:"size:32;index;not null"`
	KeySuffix       string         `json:"key_suffix" gorm:"size:16;not null"`
	Enabled         bool           `json:"enabled" gorm:"not null;default:true"`
	OwnerAdminID    *uint          `json:"owner_admin_id" gorm:"index"`
	ExpiresAt       *time.Time     `json:"expires_at"`
	LastUsedAt      *time.Time     `json:"last_used_at"`
	TokenQuotaTotal int64          `json:"token_quota_total" gorm:"not null;default:0"`
	TokenQuotaDaily int64          `json:"token_quota_daily" gorm:"not null;default:0"`
	TotalRequests   int64          `json:"total_requests" gorm:"not null;default:0"`
	TotalTokens     int64          `json:"total_tokens" gorm:"not null;default:0"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
	DeletedAt       gorm.DeletedAt `json:"-" gorm:"index"`
}

type APIKeyUsage struct {
	ID               uint      `json:"id" gorm:"primaryKey"`
	APIKeyID         uint      `json:"api_key_id" gorm:"uniqueIndex:idx_api_key_usage_day_model;not null"`
	Date             string    `json:"date" gorm:"size:10;uniqueIndex:idx_api_key_usage_day_model;not null"`
	Model            string    `json:"model" gorm:"size:120;uniqueIndex:idx_api_key_usage_day_model;not null"`
	RequestCount     int64     `json:"request_count" gorm:"not null;default:0"`
	PromptTokens     int64     `json:"prompt_tokens" gorm:"not null;default:0"`
	CompletionTokens int64     `json:"completion_tokens" gorm:"not null;default:0"`
	TotalTokens      int64     `json:"total_tokens" gorm:"not null;default:0"`
	EstimatedTokens  int64     `json:"estimated_tokens" gorm:"not null;default:0"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type Account struct {
	ID                  uint           `json:"id" gorm:"primaryKey"`
	Email               string         `json:"email" gorm:"size:255;uniqueIndex;not null"`
	Enabled             bool           `json:"enabled" gorm:"not null;default:true"`
	Active              bool           `json:"active" gorm:"not null;default:false;index"`
	UserID              string         `json:"user_id" gorm:"size:120"`
	UserName            string         `json:"user_name" gorm:"size:120"`
	SpaceID             string         `json:"space_id" gorm:"size:120"`
	SpaceViewID         string         `json:"space_view_id" gorm:"size:120"`
	SpaceName           string         `json:"space_name" gorm:"size:255"`
	ClientVersion       string         `json:"client_version" gorm:"size:80"`
	PlanType            string         `json:"plan_type" gorm:"size:80"`
	Status              string         `json:"status" gorm:"size:32;index;not null;default:inactive"`
	Priority            int            `json:"priority" gorm:"not null;default:0"`
	HourlyQuota         int            `json:"hourly_quota" gorm:"not null;default:0"`
	WindowStartedAt     *time.Time     `json:"window_started_at"`
	WindowRequestCount  int            `json:"window_request_count" gorm:"not null;default:0"`
	CooldownUntil       *time.Time     `json:"cooldown_until"`
	MaxConcurrency      int            `json:"max_concurrency" gorm:"not null;default:1"`
	ProxyMode           string         `json:"proxy_mode" gorm:"size:32"`
	ProxyURL            string         `json:"proxy_url" gorm:"size:512"`
	ProxyHTTPURL        string         `json:"proxy_http_url" gorm:"size:512"`
	ProxyHTTPSURL       string         `json:"proxy_https_url" gorm:"size:512"`
	AuthToken           string         `json:"-" gorm:"type:text"`
	Cookie              string         `json:"-" gorm:"type:text"`
	ProbeJSON           string         `json:"probe_json" gorm:"type:text"`
	ProfileDir          string         `json:"profile_dir" gorm:"type:text"`
	StorageStatePath    string         `json:"storage_state_path" gorm:"type:text"`
	PendingStatePath    string         `json:"pending_state_path" gorm:"type:text"`
	LoginMessage        string         `json:"login_message" gorm:"size:512"`
	LastError           string         `json:"last_error" gorm:"size:1024"`
	LastLoginAt         *time.Time     `json:"last_login_at"`
	LastUsedAt          *time.Time     `json:"last_used_at"`
	LastSuccessAt       *time.Time     `json:"last_success_at"`
	LastRefreshAt       *time.Time     `json:"last_refresh_at"`
	ConsecutiveFailures int            `json:"consecutive_failures" gorm:"not null;default:0"`
	TotalSuccesses      int64          `json:"total_successes" gorm:"not null;default:0"`
	TotalFailures       int64          `json:"total_failures" gorm:"not null;default:0"`
	CreatedAt           time.Time      `json:"created_at"`
	UpdatedAt           time.Time      `json:"updated_at"`
	DeletedAt           gorm.DeletedAt `json:"-" gorm:"index"`
}

type AccountLoginSession struct {
	ID                uint       `json:"id" gorm:"primaryKey"`
	Email             string     `json:"email" gorm:"size:255;uniqueIndex;not null"`
	Status            string     `json:"status" gorm:"size:32;index;not null"`
	Success           bool       `json:"success" gorm:"not null;default:false"`
	Message           string     `json:"message" gorm:"size:512"`
	Error             string     `json:"error" gorm:"size:1024"`
	ClientVersion     string     `json:"client_version" gorm:"size:80"`
	LoginOptionsToken string     `json:"-" gorm:"type:text"`
	CSRFState         string     `json:"-" gorm:"type:text"`
	DeviceID          string     `json:"device_id" gorm:"size:120"`
	CookiesJSON       string     `json:"-" gorm:"type:text"`
	ProfileDir        string     `json:"profile_dir" gorm:"type:text"`
	StorageStatePath  string     `json:"storage_state_path" gorm:"type:text"`
	PendingStatePath  string     `json:"pending_state_path" gorm:"type:text"`
	ProbePath         string     `json:"probe_path" gorm:"type:text"`
	CurrentURL        string     `json:"current_url" gorm:"type:text"`
	FinalURL          string     `json:"final_url" gorm:"type:text"`
	Title             string     `json:"title" gorm:"size:255"`
	LastLoginAt       *time.Time `json:"last_login_at"`
	ExpiresAt         *time.Time `json:"expires_at" gorm:"index"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

type Model struct {
	ID          uint           `json:"id" gorm:"primaryKey"`
	PublicID    string         `json:"public_id" gorm:"size:120;uniqueIndex;not null"`
	Name        string         `json:"name" gorm:"size:120;not null"`
	NotionModel string         `json:"notion_model" gorm:"size:120"`
	Family      string         `json:"family" gorm:"size:80"`
	Group       string         `json:"group" gorm:"size:80"`
	Beta        bool           `json:"beta" gorm:"not null;default:false"`
	Enabled     bool           `json:"enabled" gorm:"not null;default:true"`
	Aliases     string         `json:"aliases" gorm:"size:512"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `json:"-" gorm:"index"`
}

type Conversation struct {
	ID         uint           `json:"id" gorm:"primaryKey"`
	ExternalID string         `json:"external_id" gorm:"size:120;uniqueIndex;not null"`
	APIKeyID   *uint          `json:"api_key_id" gorm:"index"`
	AccountID  *uint          `json:"account_id" gorm:"index"`
	Model      string         `json:"model" gorm:"size:120"`
	Title      string         `json:"title" gorm:"size:255"`
	Status     string         `json:"status" gorm:"size:32;index;not null;default:active"`
	CreatedAt  time.Time      `json:"created_at"`
	UpdatedAt  time.Time      `json:"updated_at"`
	DeletedAt  gorm.DeletedAt `json:"-" gorm:"index"`
}

type Message struct {
	ID             uint      `json:"id" gorm:"primaryKey"`
	ConversationID uint      `json:"conversation_id" gorm:"index;not null"`
	Role           string    `json:"role" gorm:"size:32;not null"`
	Content        string    `json:"content" gorm:"type:text"`
	AttachmentJSON string    `json:"attachment_json" gorm:"type:text"`
	PromptTokens   int64     `json:"prompt_tokens" gorm:"not null;default:0"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type Response struct {
	ID             uint           `json:"id" gorm:"primaryKey"`
	ExternalID     string         `json:"external_id" gorm:"size:120;uniqueIndex;not null"`
	APIKeyID       *uint          `json:"api_key_id" gorm:"index"`
	ConversationID *uint          `json:"conversation_id" gorm:"index"`
	Model          string         `json:"model" gorm:"size:120"`
	PayloadJSON    string         `json:"payload_json" gorm:"type:text"`
	ExpiresAt      *time.Time     `json:"expires_at" gorm:"index"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
	DeletedAt      gorm.DeletedAt `json:"-" gorm:"index"`
}

type Setting struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	Key       string    `json:"key" gorm:"size:120;uniqueIndex;not null"`
	Value     string    `json:"value" gorm:"type:text"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type RequestLog struct {
	ID               uint      `json:"id" gorm:"primaryKey"`
	APIKeyID         *uint     `json:"api_key_id" gorm:"index"`
	Method           string    `json:"method" gorm:"size:16"`
	Route            string    `json:"route" gorm:"size:160;index"`
	StatusCode       int       `json:"status_code" gorm:"index"`
	LatencyMs        int64     `json:"latency_ms"`
	Model            string    `json:"model" gorm:"size:120;index"`
	PromptTokens     int64     `json:"prompt_tokens"`
	CompletionTokens int64     `json:"completion_tokens"`
	TotalTokens      int64     `json:"total_tokens"`
	Estimated        bool      `json:"estimated"`
	ErrorSummary     string    `json:"error_summary" gorm:"size:512"`
	CreatedAt        time.Time `json:"created_at" gorm:"index"`
}
