package dto

type ModelDTO struct {
	ID          string `json:"id"`
	Object      string `json:"object"`
	Created     int64  `json:"created"`
	OwnedBy     string `json:"owned_by"`
	Name        string `json:"name"`
	Family      string `json:"family"`
	Group       string `json:"group"`
	Beta        bool   `json:"beta"`
	NotionModel string `json:"notion_model"`
}
