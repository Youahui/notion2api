package model

import (
	"time"

	"gorm.io/gorm"
)

type AdminUser struct {
	ID           uint           `json:"id"            gorm:"primaryKey"`
	Username     string         `json:"username"      gorm:"size:64;uniqueIndex;not null"`
	PasswordHash string         `json:"-"             gorm:"size:255;not null"`
	Role         string         `json:"role"          gorm:"size:32;not null;default:admin"`
	Enabled      bool           `json:"enabled"       gorm:"not null;default:true"`
	LastLoginAt  *time.Time     `json:"last_login_at"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	DeletedAt    gorm.DeletedAt `json:"-"             gorm:"index"`
}
