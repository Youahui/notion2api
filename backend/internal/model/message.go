package model

import "time"

type Message struct {
	ID             uint      `json:"id"              gorm:"primaryKey"`
	ConversationID uint      `json:"conversation_id" gorm:"index;not null"`
	Role           string    `json:"role"            gorm:"size:32;not null"`
	Content        string    `json:"content"         gorm:"type:text"`
	AttachmentJSON string    `json:"attachment_json" gorm:"type:text"`
	PromptTokens   int64     `json:"prompt_tokens"   gorm:"not null;default:0"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}
