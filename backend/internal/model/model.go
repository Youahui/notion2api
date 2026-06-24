package model

import (
	"time"

	"gorm.io/gorm"
)

type Model struct {
	ID          uint           `json:"id"           gorm:"primaryKey"`
	PublicID    string         `json:"public_id"    gorm:"size:120;uniqueIndex;not null"`
	Name        string         `json:"name"         gorm:"size:120;not null"`
	NotionModel string         `json:"notion_model" gorm:"size:120"`
	Family      string         `json:"family"       gorm:"size:80"`
	Group       string         `json:"group"        gorm:"size:80"`
	Beta        bool           `json:"beta"         gorm:"not null;default:false"`
	Enabled     bool           `json:"enabled"      gorm:"not null;default:true"`
	Aliases     string         `json:"aliases"      gorm:"size:512"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `json:"-"            gorm:"index"`
}
