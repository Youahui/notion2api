package model

import (
	"time"

	"gorm.io/gorm"
)

type AccountGroup struct {
	ID          uint           `json:"id"          gorm:"primaryKey"`
	Name        string         `json:"name"        gorm:"size:120;uniqueIndex;not null"`
	Description string         `json:"description" gorm:"size:512"`
	Enabled     bool           `json:"enabled"     gorm:"not null;default:true"`
	IsDefault   bool           `json:"is_default"  gorm:"not null;default:false;index"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `json:"-"           gorm:"index"`
}

type AccountGroupMember struct {
	ID        uint      `json:"id"         gorm:"primaryKey"`
	GroupID   uint      `json:"group_id"   gorm:"uniqueIndex:idx_account_group_member;not null"`
	AccountID uint      `json:"account_id" gorm:"uniqueIndex:idx_account_group_member;index;not null"`
	Enabled   bool      `json:"enabled"    gorm:"not null;default:true"`
	Priority  int       `json:"priority"   gorm:"not null;default:0"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
