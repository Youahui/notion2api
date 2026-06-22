package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"notion2api/backend/internal/model"

	"github.com/google/uuid"
)

const maxAssistantAttachmentBytes = 20 * 1024 * 1024

type notionUploadTarget struct {
	UploadURL     string
	Fields        map[string]string
	AttachmentURL string
}

func (g *NotionGateway) UploadAssistantChatAttachments(ctx context.Context, account model.Account, threadID string, createThread bool, attachments []NotionAttachmentUpload) ([]NotionUploadedAttachment, error) {
	if len(attachments) == 0 {
		return nil, nil
	}
	out := make([]NotionUploadedAttachment, 0, len(attachments))
	for index, attachment := range attachments {
		normalized, err := normalizeNotionAttachmentUpload(attachment)
		if err != nil {
			return nil, err
		}
		target, err := g.createAssistantAttachmentUploadTarget(ctx, account, threadID, createThread && index == 0, normalized)
		if err != nil {
			return nil, err
		}
		if err := g.uploadAssistantAttachmentToS3(ctx, account, target, normalized); err != nil {
			return nil, err
		}
		uploaded := NotionUploadedAttachment{
			Name:          normalized.Name,
			ContentType:   normalized.ContentType,
			AttachmentURL: firstNonEmpty(target.AttachmentURL, attachmentURLFromS3Key(target.Fields["key"])),
		}
		if uploaded.AttachmentURL == "" {
			return nil, fmt.Errorf("notion upload did not return an attachment url for %s", normalized.Name)
		}
		taskIDs, err := g.enqueueAssistantAttachmentProcessing(ctx, account, threadID, uploaded.AttachmentURL)
		if err != nil {
			return nil, err
		}
		uploaded.TaskIDs = taskIDs
		if len(taskIDs) > 0 {
			if err := g.waitForAssistantAttachmentTasks(ctx, account, threadID, taskIDs); err != nil {
				slog.Warn("notion attachment task polling failed", "thread_id", threadID, "task_ids", taskIDs, "error", err)
			}
		}
		if signedURL, err := g.getAssistantAttachmentSignedURL(ctx, account, threadID, uploaded); err == nil {
			uploaded.SignedURL = signedURL
		} else {
			slog.Warn("notion attachment signed url lookup failed", "thread_id", threadID, "url", uploaded.AttachmentURL, "error", err)
		}
		out = append(out, uploaded)
	}
	return out, nil
}

func normalizeNotionAttachmentUpload(attachment NotionAttachmentUpload) (NotionAttachmentUpload, error) {
	attachment.Name = strings.TrimSpace(attachment.Name)
	attachment.ContentType = strings.TrimSpace(attachment.ContentType)
	if attachment.ContentType == "" {
		attachment.ContentType = "application/octet-stream"
	}
	if attachment.Name == "" {
		attachment.Name = uuid.NewString() + extensionForContentType(attachment.ContentType)
	}
	if len(attachment.Data) == 0 {
		return NotionAttachmentUpload{}, fmt.Errorf("attachment %s has empty data", attachment.Name)
	}
	if len(attachment.Data) > maxAssistantAttachmentBytes {
		return NotionAttachmentUpload{}, fmt.Errorf("attachment %s is too large: %d bytes", attachment.Name, len(attachment.Data))
	}
	return attachment, nil
}

func (g *NotionGateway) createAssistantAttachmentUploadTarget(ctx context.Context, account model.Account, threadID string, createThread bool, attachment NotionAttachmentUpload) (notionUploadTarget, error) {
	payload := map[string]any{
		"name":          attachment.Name,
		"contentType":   attachment.ContentType,
		"contentLength": len(attachment.Data),
		"createThread":  createThread,
		"assistantChatTranscriptSessionPointer": map[string]any{
			"spaceId": account.SpaceID,
			"table":   "thread",
			"id":      threadID,
		},
	}
	resp, err := g.postNotionJSON(ctx, account, "getUploadFileUrlForAssistantChatTranscriptUpload", payload, strings.TrimRight(firstNonEmpty(g.cfg.Upstream.Origin, g.cfg.Upstream.BaseURL, "https://www.notion.so"), "/")+"/ai")
	if err != nil {
		return notionUploadTarget{}, err
	}
	target := parseNotionUploadTarget(resp)
	if target.UploadURL == "" || len(target.Fields) == 0 {
		encoded, _ := json.Marshal(resp)
		return notionUploadTarget{}, fmt.Errorf("notion upload target response missing upload url or fields: %s", truncateRunes(string(encoded), 500))
	}
	return target, nil
}

func parseNotionUploadTarget(value any) notionUploadTarget {
	target := notionUploadTarget{Fields: map[string]string{}}
	walkUploadTarget(value, &target)
	return target
}

func walkUploadTarget(value any, target *notionUploadTarget) {
	switch typed := value.(type) {
	case map[string]any:
		if looksLikeS3Fields(typed) {
			for key, raw := range typed {
				if text := stringFromAny(raw); text != "" {
					target.Fields[key] = text
				}
			}
		}
		for key, raw := range typed {
			lowerKey := strings.ToLower(strings.TrimSpace(key))
			if text := stringFromAny(raw); text != "" {
				switch {
				case strings.HasPrefix(text, "attachment:") && target.AttachmentURL == "":
					target.AttachmentURL = text
				case isS3UploadURL(text) && target.UploadURL == "":
					target.UploadURL = text
				case strings.Contains(lowerKey, "attachment") && strings.HasPrefix(text, "attachment:"):
					target.AttachmentURL = text
				case (lowerKey == "url" || strings.Contains(lowerKey, "upload")) && isS3UploadURL(text):
					target.UploadURL = text
				}
			}
			walkUploadTarget(raw, target)
		}
	case []any:
		for _, item := range typed {
			walkUploadTarget(item, target)
		}
	}
}

func looksLikeS3Fields(value map[string]any) bool {
	if len(value) == 0 {
		return false
	}
	keys := 0
	for _, key := range []string{"key", "Policy", "policy", "X-Amz-Signature", "x-amz-signature", "X-Amz-Credential", "bucket"} {
		if _, ok := value[key]; ok {
			keys++
		}
	}
	return keys >= 2
}

func isS3UploadURL(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	if (parsed.Scheme == "http" || parsed.Scheme == "https") && (host == "127.0.0.1" || host == "localhost" || host == "::1") {
		return true
	}
	return parsed.Scheme == "https" && (strings.Contains(host, "amazonaws.com") || strings.Contains(host, "s3"))
}

func (g *NotionGateway) uploadAssistantAttachmentToS3(ctx context.Context, account model.Account, target notionUploadTarget, attachment NotionAttachmentUpload) error {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for _, key := range preferredS3FieldOrder(target.Fields) {
		if err := writer.WriteField(key, target.Fields[key]); err != nil {
			return err
		}
	}
	part, err := writer.CreateFormFile("file", attachment.Name)
	if err != nil {
		return err
	}
	if _, err := part.Write(attachment.Data); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target.UploadURL, &body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Origin", strings.TrimRight(firstNonEmpty(g.cfg.Upstream.Origin, g.cfg.Upstream.BaseURL, "https://www.notion.so"), "/"))
	req.Header.Set("Referer", strings.TrimRight(firstNonEmpty(g.cfg.Upstream.Origin, g.cfg.Upstream.BaseURL, "https://www.notion.so"), "/")+"/")

	client, err := g.clientForAccount(account)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &NotionAPIError{StatusCode: resp.StatusCode, Body: strings.TrimSpace(string(raw)), Message: "s3 upload failed"}
	}
	return nil
}

func preferredS3FieldOrder(fields map[string]string) []string {
	preferred := []string{
		"Content-Type",
		"x-amz-storage-class",
		"tagging",
		"bucket",
		"X-Amz-Algorithm",
		"X-Amz-Credential",
		"X-Amz-Date",
		"X-Amz-Security-Token",
		"key",
		"Policy",
		"X-Amz-Signature",
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(fields))
	for _, key := range preferred {
		if _, ok := fields[key]; ok {
			seen[key] = struct{}{}
			out = append(out, key)
		}
	}
	for key := range fields {
		if _, ok := seen[key]; ok {
			continue
		}
		out = append(out, key)
	}
	return out
}

func (g *NotionGateway) enqueueAssistantAttachmentProcessing(ctx context.Context, account model.Account, threadID string, attachmentURL string) ([]string, error) {
	payload := map[string]any{
		"task": map[string]any{
			"eventName": "processAgentAttachment",
			"request": map[string]any{
				"url":           attachmentURL,
				"spaceId":       account.SpaceID,
				"source":        "user_upload",
				"clientVersion": notionClientVersion(account.ClientVersion),
				"aiSessionPointer": map[string]any{
					"spaceId": account.SpaceID,
					"table":   "thread",
					"id":      threadID,
				},
			},
			"cellRouting": map[string]any{
				"spaceIds": []string{account.SpaceID},
			},
		},
	}
	resp, err := g.postNotionJSON(ctx, account, "enqueueTask", payload, strings.TrimRight(firstNonEmpty(g.cfg.Upstream.Origin, g.cfg.Upstream.BaseURL, "https://www.notion.so"), "/")+"/ai")
	if err != nil {
		return nil, err
	}
	return extractTaskIDs(resp), nil
}

func extractTaskIDs(value any) []string {
	seen := map[string]struct{}{}
	out := []string{}
	var walk func(any)
	walk = func(value any) {
		switch typed := value.(type) {
		case map[string]any:
			for key, raw := range typed {
				lowerKey := strings.ToLower(strings.TrimSpace(key))
				if strings.Contains(lowerKey, "task") {
					if text := stringFromAny(raw); text != "" {
						if _, ok := seen[text]; !ok {
							seen[text] = struct{}{}
							out = append(out, text)
						}
					}
				}
				walk(raw)
			}
		case []any:
			for _, item := range typed {
				walk(item)
			}
		}
	}
	walk(value)
	return out
}

func (g *NotionGateway) waitForAssistantAttachmentTasks(ctx context.Context, account model.Account, threadID string, taskIDs []string) error {
	if len(taskIDs) == 0 {
		return nil
	}
	interval := time.Duration(g.cfg.Upstream.PollIntervalSeconds * float64(time.Second))
	if interval <= 0 || interval > 2*time.Second {
		interval = 500 * time.Millisecond
	}
	rounds := g.cfg.Upstream.PollMaxRounds
	if rounds <= 0 || rounds > 20 {
		rounds = 20
	}
	payload := map[string]any{"taskIds": taskIDs}
	for attempt := 0; attempt < rounds; attempt++ {
		resp, err := g.postNotionJSON(ctx, account, "getTasks", payload, strings.TrimRight(firstNonEmpty(g.cfg.Upstream.Origin, g.cfg.Upstream.BaseURL, "https://www.notion.so"), "/")+"/ai")
		if err != nil {
			return err
		}
		if notionTasksTerminal(resp) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
	}
	return fmt.Errorf("notion attachment tasks did not finish for thread %s: %v", threadID, taskIDs)
}

func notionTasksTerminal(value any) bool {
	raw, _ := json.Marshal(value)
	text := strings.ToLower(string(raw))
	if text == "" || text == "null" {
		return true
	}
	for _, pending := range []string{"queued", "pending", "running", "processing", "in_progress", "not_started"} {
		if strings.Contains(text, pending) {
			return false
		}
	}
	return true
}

func (g *NotionGateway) getAssistantAttachmentSignedURL(ctx context.Context, account model.Account, threadID string, attachment NotionUploadedAttachment) (string, error) {
	payload := map[string]any{
		"urls": []map[string]any{
			{
				"url":          attachment.AttachmentURL,
				"download":     false,
				"downloadName": attachment.Name,
				"permissionRecord": map[string]any{
					"table":   "thread",
					"id":      threadID,
					"spaceId": account.SpaceID,
				},
			},
		},
	}
	resp, err := g.postNotionJSON(ctx, account, "getSignedFileUrls", payload, g.chatReferer(threadID))
	if err != nil {
		return "", err
	}
	for _, item := range sliceFromAny(resp["signedUrls"]) {
		if signed := stringFromAny(item); signed != "" {
			return signed, nil
		}
	}
	return "", fmt.Errorf("notion signed url response did not include signedUrls")
}

func injectUploadedAttachmentsIntoPayload(payload map[string]any, account model.Account, attachments []NotionUploadedAttachment) {
	if len(attachments) == 0 || payload == nil {
		return
	}
	now := time.Now().Format("2006-01-02T15:04:05.000-07:00")
	steps := make([]map[string]any, 0, len(attachments))
	for _, attachment := range attachments {
		if strings.TrimSpace(attachment.AttachmentURL) == "" {
			continue
		}
		step := map[string]any{
			"id":          uuid.NewString(),
			"type":        "attachment",
			"fileName":    attachment.Name,
			"contentType": attachment.ContentType,
			"fileUrl":     attachment.AttachmentURL,
			"source":      "user_upload",
			"userId":      account.UserID,
			"createdAt":   now,
		}
		if signedURL := strings.TrimSpace(attachment.SignedURL); signedURL != "" {
			step["signedUrl"] = signedURL
		}
		steps = append(steps, step)
	}
	if len(steps) == 0 {
		return
	}
	switch transcript := payload["transcript"].(type) {
	case []map[string]any:
		payload["transcript"] = insertAttachmentStepsBeforeUser(transcript, steps)
	case []any:
		mapped := make([]map[string]any, 0, len(transcript))
		for _, item := range transcript {
			if step := mapFromAny(item); step != nil {
				mapped = append(mapped, step)
			}
		}
		payload["transcript"] = insertAttachmentStepsBeforeUser(mapped, steps)
	}
}

func insertAttachmentStepsBeforeUser(transcript []map[string]any, attachments []map[string]any) []map[string]any {
	insertAt := len(transcript)
	for index := len(transcript) - 1; index >= 0; index-- {
		if strings.EqualFold(stringFromAny(transcript[index]["type"]), "user") {
			insertAt = index
			break
		}
	}
	out := make([]map[string]any, 0, len(transcript)+len(attachments))
	out = append(out, transcript[:insertAt]...)
	out = append(out, attachments...)
	out = append(out, transcript[insertAt:]...)
	return out
}

func attachmentURLFromS3Key(key string) string {
	parts := strings.Split(strings.Trim(strings.TrimSpace(key), "/"), "/")
	if len(parts) < 3 {
		return ""
	}
	fileID := strings.TrimSpace(parts[1])
	name := path.Base(parts[len(parts)-1])
	if fileID == "" || name == "" || name == "." || name == "/" {
		return ""
	}
	return "attachment:" + fileID + ":" + name
}
