package service

import (
	"encoding/base64"
	"fmt"
	"mime"
	"net/url"
	"path/filepath"
	"strings"

	"notaion2api/backend/internal/dto"

	"github.com/google/uuid"
)

func chatCompletionAttachmentUploads(req dto.ChatCompletionRequest) []NotionAttachmentUpload {
	uploads := []NotionAttachmentUpload{}
	uploads = append(uploads, openAIAttachmentUploadsFromAny(req.Messages)...)
	uploads = append(uploads, openAIAttachmentUploadsFromAny(req.Attachments)...)
	return dedupeAttachmentUploads(uploads)
}

func responsesAttachmentUploadsFromBody(body map[string]any) []NotionAttachmentUpload {
	if body == nil {
		return nil
	}
	uploads := []NotionAttachmentUpload{}
	uploads = append(uploads, openAIAttachmentUploadsFromAny(body["input"])...)
	uploads = append(uploads, openAIAttachmentUploadsFromAny(body["attachments"])...)
	return dedupeAttachmentUploads(uploads)
}

func openAIAttachmentUploadsFromAny(value any) []NotionAttachmentUpload {
	uploads := []NotionAttachmentUpload{}
	collectOpenAIAttachmentUploads(value, &uploads)
	return uploads
}

func collectOpenAIAttachmentUploads(value any, uploads *[]NotionAttachmentUpload) {
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			collectOpenAIAttachmentUploads(item, uploads)
		}
	case []map[string]any:
		for _, item := range typed {
			collectOpenAIAttachmentUploads(item, uploads)
		}
	case []dto.ChatMessage:
		for _, message := range typed {
			collectOpenAIAttachmentUploads(message.Content, uploads)
		}
	case map[string]any:
		if upload, ok := uploadFromOpenAIContentItem(typed); ok {
			*uploads = append(*uploads, upload)
		}
		for _, key := range []string{"content", "attachments"} {
			if child, exists := typed[key]; exists {
				collectOpenAIAttachmentUploads(child, uploads)
			}
		}
	}
}

func uploadFromOpenAIContentItem(item map[string]any) (NotionAttachmentUpload, bool) {
	if item == nil {
		return NotionAttachmentUpload{}, false
	}
	itemType := strings.ToLower(strings.TrimSpace(stringFromAny(item["type"])))
	isImage := itemType == "input_image" || itemType == "image_url"
	isFile := itemType == "input_file" || itemType == "file" || itemType == "attachment"
	if !isImage && !isFile && !hasInlineAttachmentData(item) {
		return NotionAttachmentUpload{}, false
	}
	name := firstNonEmpty(
		stringFromAny(item["filename"]),
		stringFromAny(item["file_name"]),
		stringFromAny(item["name"]),
		stringFromAny(item["title"]),
	)
	contentType := firstNonEmpty(
		stringFromAny(item["content_type"]),
		stringFromAny(item["contentType"]),
		stringFromAny(item["mime_type"]),
		stringFromAny(item["mimeType"]),
	)
	rawData := firstNonEmpty(stringFromAny(item["file_data"]), stringFromAny(item["data"]))
	if rawData == "" {
		if imageURL := mapFromAny(item["image_url"]); imageURL != nil {
			rawData = firstNonEmpty(stringFromAny(imageURL["url"]), stringFromAny(imageURL["value"]))
		} else if imageURL := stringFromAny(item["image_url"]); imageURL != "" {
			rawData = imageURL
		}
	}
	data, detectedContentType, ok := decodeOpenAIInlineAttachmentData(rawData)
	if !ok {
		return NotionAttachmentUpload{}, false
	}
	contentType = firstNonEmpty(contentType, detectedContentType)
	if contentType == "" {
		if isImage {
			contentType = "image/jpeg"
		} else {
			contentType = "application/octet-stream"
		}
	}
	if name == "" {
		name = uuid.NewString() + extensionForContentType(contentType)
	}
	return NotionAttachmentUpload{
		Name:        sanitizeAttachmentFileName(name, contentType),
		ContentType: contentType,
		Data:        data,
		Source:      "openai_request",
	}, true
}

func hasInlineAttachmentData(item map[string]any) bool {
	if strings.TrimSpace(stringFromAny(item["file_data"])) != "" || strings.TrimSpace(stringFromAny(item["data"])) != "" {
		return true
	}
	if imageURL := mapFromAny(item["image_url"]); imageURL != nil {
		ref := firstNonEmpty(stringFromAny(imageURL["url"]), stringFromAny(imageURL["value"]))
		return strings.HasPrefix(strings.TrimSpace(ref), "data:")
	}
	return strings.HasPrefix(strings.TrimSpace(stringFromAny(item["image_url"])), "data:")
}

func decodeOpenAIInlineAttachmentData(raw string) ([]byte, string, bool) {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return nil, "", false
	}
	if strings.HasPrefix(clean, "data:") {
		comma := strings.Index(clean, ",")
		if comma < 0 {
			return nil, "", false
		}
		meta := clean[:comma]
		data := clean[comma+1:]
		contentType := strings.TrimSpace(strings.TrimPrefix(strings.Split(meta, ";")[0], "data:"))
		if strings.Contains(strings.ToLower(meta), ";base64") {
			decoded, err := base64.StdEncoding.DecodeString(data)
			if err != nil {
				return nil, "", false
			}
			return decoded, contentType, true
		}
		decoded, err := url.QueryUnescape(data)
		if err != nil {
			return nil, "", false
		}
		return []byte(decoded), contentType, true
	}
	decoded, err := base64.StdEncoding.DecodeString(clean)
	if err != nil {
		return nil, "", false
	}
	return decoded, "", true
}

func extensionForContentType(contentType string) string {
	contentType = strings.TrimSpace(strings.Split(contentType, ";")[0])
	if contentType == "" {
		return ""
	}
	extensions, err := mime.ExtensionsByType(contentType)
	if err == nil && len(extensions) > 0 {
		return extensions[0]
	}
	switch strings.ToLower(contentType) {
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "application/pdf":
		return ".pdf"
	case "text/plain":
		return ".txt"
	default:
		return ""
	}
}

func sanitizeAttachmentFileName(name string, contentType string) string {
	clean := filepath.Base(strings.TrimSpace(name))
	clean = strings.ReplaceAll(clean, "\x00", "")
	if clean == "" || clean == "." || clean == "/" {
		clean = uuid.NewString() + extensionForContentType(contentType)
	}
	if filepath.Ext(clean) == "" {
		clean += extensionForContentType(contentType)
	}
	return clean
}

func dedupeAttachmentUploads(uploads []NotionAttachmentUpload) []NotionAttachmentUpload {
	if len(uploads) <= 1 {
		return uploads
	}
	seen := map[string]struct{}{}
	out := make([]NotionAttachmentUpload, 0, len(uploads))
	for _, upload := range uploads {
		key := fmt.Sprintf("%s\x00%s\x00%d", upload.Name, upload.ContentType, len(upload.Data))
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, upload)
	}
	return out
}
