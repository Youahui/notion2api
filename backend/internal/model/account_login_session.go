package model

import "time"

type AccountLoginSession struct {
	ID                uint       `json:"id"                 gorm:"primaryKey"`
	Email             string     `json:"email"              gorm:"size:255;uniqueIndex;not null"`
	Status            string     `json:"status"             gorm:"size:32;index;not null"`
	Success           bool       `json:"success"            gorm:"not null;default:false"`
	Message           string     `json:"message"            gorm:"size:512"`
	Error             string     `json:"error"              gorm:"size:1024"`
	ClientVersion     string     `json:"client_version"     gorm:"size:80"`
	LoginOptionsToken string     `json:"-"                  gorm:"type:text"`
	CSRFState         string     `json:"-"                  gorm:"type:text"`
	DeviceID          string     `json:"device_id"          gorm:"size:120"`
	CookiesJSON       string     `json:"-"                  gorm:"type:text"`
	ProfileDir        string     `json:"profile_dir"        gorm:"type:text"`
	StorageStatePath  string     `json:"storage_state_path" gorm:"type:text"`
	PendingStatePath  string     `json:"pending_state_path" gorm:"type:text"`
	ProbePath         string     `json:"probe_path"         gorm:"type:text"`
	CurrentURL        string     `json:"current_url"        gorm:"type:text"`
	FinalURL          string     `json:"final_url"          gorm:"type:text"`
	Title             string     `json:"title"              gorm:"size:255"`
	LastLoginAt       *time.Time `json:"last_login_at"`
	ExpiresAt         *time.Time `json:"expires_at"         gorm:"index"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}
