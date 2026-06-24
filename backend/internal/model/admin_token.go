package model

import "time"

type AdminToken struct {
	ID          uint       `json:"id"            gorm:"primaryKey"`
	AdminUserID uint       `json:"admin_user_id" gorm:"index;not null"`
	TokenHash   string     `json:"-"             gorm:"size:128;uniqueIndex;not null"`
	ExpiresAt   time.Time  `json:"expires_at"    gorm:"index;not null"`
	RevokedAt   *time.Time `json:"revoked_at"`
	UserAgent   string     `json:"user_agent"    gorm:"size:255"`
	ClientIP    string     `json:"client_ip"     gorm:"size:64"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}
