package dto

import "time"

type AccountDTO struct {
	ID                  uint       `json:"id"`
	Email               string     `json:"email"`
	Groups              []GroupDTO `json:"groups,omitempty"`
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
	GroupIDs         []uint `json:"group_ids"`
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

type GroupDTO struct {
	ID           uint      `json:"id"`
	Name         string    `json:"name"`
	Description  string    `json:"description"`
	Enabled      bool      `json:"enabled"`
	IsDefault    bool      `json:"is_default"`
	AccountCount int64     `json:"account_count"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type GroupMemberDTO struct {
	AccountID uint       `json:"account_id"`
	Email     string     `json:"email"`
	Enabled   bool       `json:"enabled"`
	Priority  int        `json:"priority"`
	Status    string     `json:"status"`
	LastUsed  *time.Time `json:"last_used_at"`
}

type CreateGroupRequest struct {
	Name        string `json:"name" binding:"required"`
	Description string `json:"description"`
	Enabled     *bool  `json:"enabled"`
	IsDefault   *bool  `json:"is_default"`
}

type UpdateGroupRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	Enabled     *bool   `json:"enabled"`
	IsDefault   *bool   `json:"is_default"`
}

type UpdateGroupMembersRequest struct {
	Members []UpdateGroupMemberRequest `json:"members"`
}

type UpdateGroupMemberRequest struct {
	AccountID uint `json:"account_id" binding:"required"`
	Enabled   bool `json:"enabled"`
	Priority  int  `json:"priority"`
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
