package model

import "time"

type Setting struct {
	ID        uint      `json:"id"         gorm:"primaryKey"`
	Key       string    `json:"key"        gorm:"size:120;uniqueIndex;not null"`
	Value     string    `json:"value"      gorm:"type:text"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
