package model

import (
	"time"

	"gorm.io/gorm"
)

type APIKey struct {
	ID              uint           `json:"id"                gorm:"primaryKey"`
	Name            string         `json:"name"              gorm:"size:120;not null"`
	KeyHash         string         `json:"-"                 gorm:"size:128;uniqueIndex;not null"`
	APIKey          string         `json:"api_key"           gorm:"type:text"`
	KeyPrefix       string         `json:"key_prefix"        gorm:"size:32;index;not null"`
	KeySuffix       string         `json:"key_suffix"        gorm:"size:16;not null"`
	Enabled         bool           `json:"enabled"           gorm:"not null;default:true"`
	OwnerAdminID    *uint          `json:"owner_admin_id"    gorm:"index"`
	GroupID         *uint          `json:"group_id"          gorm:"index"`
	ExpiresAt       *time.Time     `json:"expires_at"`
	LastUsedAt      *time.Time     `json:"last_used_at"`
	TokenQuotaTotal int64          `json:"token_quota_total" gorm:"not null;default:0"`
	TokenQuotaDaily int64          `json:"token_quota_daily" gorm:"not null;default:0"`
	TotalRequests   int64          `json:"total_requests"    gorm:"not null;default:0"`
	TotalTokens     int64          `json:"total_tokens"      gorm:"not null;default:0"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
	DeletedAt       gorm.DeletedAt `json:"-"                 gorm:"index"`
}
