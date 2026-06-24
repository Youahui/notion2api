package model

import (
	"time"

	"gorm.io/gorm"
)

type Account struct {
	ID                  uint           `json:"id"                   gorm:"primaryKey"`
	Email               string         `json:"email"                gorm:"size:255;uniqueIndex;not null"`
	Enabled             bool           `json:"enabled"              gorm:"not null;default:true"`
	Active              bool           `json:"active"               gorm:"not null;default:false;index"`
	UserID              string         `json:"user_id"              gorm:"size:120"`
	UserName            string         `json:"user_name"            gorm:"size:120"`
	SpaceID             string         `json:"space_id"             gorm:"size:120"`
	SpaceViewID         string         `json:"space_view_id"        gorm:"size:120"`
	SpaceName           string         `json:"space_name"           gorm:"size:255"`
	ClientVersion       string         `json:"client_version"       gorm:"size:80"`
	PlanType            string         `json:"plan_type"            gorm:"size:80"`
	Status              string         `json:"status"               gorm:"size:32;index;not null;default:inactive"`
	Priority            int            `json:"priority"             gorm:"not null;default:0"`
	HourlyQuota         int            `json:"hourly_quota"         gorm:"not null;default:0"`
	WindowStartedAt     *time.Time     `json:"window_started_at"`
	WindowRequestCount  int            `json:"window_request_count" gorm:"not null;default:0"`
	CooldownUntil       *time.Time     `json:"cooldown_until"`
	MaxConcurrency      int            `json:"max_concurrency"      gorm:"not null;default:1"`
	ProxyMode           string         `json:"proxy_mode"           gorm:"size:32"`
	ProxyURL            string         `json:"proxy_url"            gorm:"size:512"`
	ProxyHTTPURL        string         `json:"proxy_http_url"       gorm:"size:512"`
	ProxyHTTPSURL       string         `json:"proxy_https_url"      gorm:"size:512"`
	AuthToken           string         `json:"-"                    gorm:"type:text"`
	Cookie              string         `json:"-"                    gorm:"type:text"`
	ProbeJSON           string         `json:"probe_json"           gorm:"type:text"`
	ProfileDir          string         `json:"profile_dir"          gorm:"type:text"`
	StorageStatePath    string         `json:"storage_state_path"   gorm:"type:text"`
	PendingStatePath    string         `json:"pending_state_path"   gorm:"type:text"`
	LoginMessage        string         `json:"login_message"        gorm:"size:512"`
	LastError           string         `json:"last_error"           gorm:"size:1024"`
	LastLoginAt         *time.Time     `json:"last_login_at"`
	LastUsedAt          *time.Time     `json:"last_used_at"`
	LastSuccessAt       *time.Time     `json:"last_success_at"`
	LastRefreshAt       *time.Time     `json:"last_refresh_at"`
	ConsecutiveFailures int            `json:"consecutive_failures" gorm:"not null;default:0"`
	TotalSuccesses      int64          `json:"total_successes"      gorm:"not null;default:0"`
	TotalFailures       int64          `json:"total_failures"       gorm:"not null;default:0"`
	CreatedAt           time.Time      `json:"created_at"`
	UpdatedAt           time.Time      `json:"updated_at"`
	DeletedAt           gorm.DeletedAt `json:"-"                    gorm:"index"`
}
