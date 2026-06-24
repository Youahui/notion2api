package dto

import "time"

type NotionConversationAttachmentDTO struct {
	Name        string `json:"name,omitempty"`
	ContentType string `json:"content_type,omitempty"`
	Source      string `json:"source,omitempty"`
	URL         string `json:"url,omitempty"`
}

type NotionConversationMessageDTO struct {
	ID          string                            `json:"id"`
	Role        string                            `json:"role"`
	Status      string                            `json:"status"`
	Content     string                            `json:"content"`
	CreatedAt   *time.Time                        `json:"created_at,omitempty"`
	UpdatedAt   *time.Time                        `json:"updated_at,omitempty"`
	Attachments []NotionConversationAttachmentDTO `json:"attachments,omitempty"`
}

type NotionConversationSummaryDTO struct {
	ID                   string     `json:"id"`
	Title                string     `json:"title"`
	Origin               string     `json:"origin"`
	RemoteOnly           bool       `json:"remote_only"`
	Preview              string     `json:"preview,omitempty"`
	Status               string     `json:"status"`
	Source               string     `json:"source"`
	Transport            string     `json:"transport"`
	AccountEmail         string     `json:"account_email"`
	SpaceID              string     `json:"space_id"`
	ThreadID             string     `json:"thread_id"`
	CreatedByDisplayName string     `json:"created_by_display_name,omitempty"`
	CreatedAt            *time.Time `json:"created_at,omitempty"`
	UpdatedAt            *time.Time `json:"updated_at,omitempty"`
	MessageCount         int        `json:"message_count,omitempty"`
}

type NotionConversationDetailDTO struct {
	NotionConversationSummaryDTO
	Messages []NotionConversationMessageDTO `json:"messages"`
}

type NotionConversationPageDTO struct {
	Data     []NotionConversationSummaryDTO `json:"data"`
	Page     int                            `json:"page"`
	PageSize int                            `json:"page_size"`
	Total    int                            `json:"total"`
	HasMore  bool                           `json:"has_more"`
}
