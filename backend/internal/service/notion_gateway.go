package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"notaion2api/backend/internal/config"
	"notaion2api/backend/internal/model"

	"github.com/google/uuid"
)

type NotionGateway struct {
	cfg    config.Config
	client *http.Client
}

type NotionPromptRequest struct {
	Prompt           string
	PublicModel      string
	NotionModel      string
	UseWebSearch     bool
	ConversationID   string
	UpstreamThreadID string
	RawMessageCount  int
}

type NotionRunResult struct {
	Text          string
	ThreadID      string
	MessageID     string
	ConvertedOnly bool
	Payload       map[string]any
	RawResponse   string
}

func NewNotionGateway(cfg config.Config) *NotionGateway {
	timeout := cfg.Upstream.TimeoutSeconds
	if timeout <= 0 {
		timeout = 180
	}
	return &NotionGateway{
		cfg:    cfg,
		client: &http.Client{Timeout: time.Duration(timeout) * time.Second},
	}
}

func (g *NotionGateway) BuildPayload(account model.Account, req NotionPromptRequest) (map[string]any, string) {
	threadID := strings.TrimSpace(req.UpstreamThreadID)
	if threadID == "" {
		threadID = uuid.NewString()
	}
	now := time.Now().Format("2006-01-02T15:04:05.000-07:00")
	threadType := "workflow"
	surface := "ai_module"
	configID := uuid.NewString()
	contextID := uuid.NewString()
	userStepID := uuid.NewString()
	configValue := map[string]any{
		"type":                       threadType,
		"model":                      strings.TrimSpace(req.NotionModel),
		"useWebSearch":               req.UseWebSearch,
		"useReadOnlyMode":            false,
		"writerMode":                 false,
		"enableGenerateImage":        true,
		"enableCsvAttachmentSupport": true,
	}
	contextValue := map[string]any{
		"timezone":        "Asia/Shanghai",
		"userName":        firstNonEmpty(account.UserName, account.Email),
		"userId":          account.UserID,
		"userEmail":       account.Email,
		"spaceName":       account.SpaceName,
		"spaceId":         account.SpaceID,
		"currentDatetime": now,
		"surface":         surface,
	}
	if clean := strings.TrimSpace(account.SpaceViewID); clean != "" {
		contextValue["spaceViewId"] = clean
	}
	transcript := []map[string]any{
		{"id": configID, "type": "config", "value": configValue},
		{"id": contextID, "type": "context", "value": contextValue},
		{
			"id":        userStepID,
			"type":      "user",
			"value":     [][]string{{req.Prompt}},
			"userId":    account.UserID,
			"createdAt": now,
		},
	}
	payload := map[string]any{
		"spaceId":                       account.SpaceID,
		"threadId":                      threadID,
		"createThread":                  strings.TrimSpace(req.UpstreamThreadID) == "",
		"generateTitle":                 strings.TrimSpace(req.UpstreamThreadID) == "",
		"traceId":                       uuid.NewString(),
		"transcript":                    transcript,
		"threadType":                    threadType,
		"asPatchResponse":               true,
		"isPartialTranscript":           false,
		"saveAllThreadOperations":       strings.TrimSpace(req.UpstreamThreadID) == "",
		"setUnreadState":                true,
		"createdSource":                 surface,
		"isUserInAnySalesAssistedSpace": false,
		"isSpaceSalesAssisted":          false,
		"debugOverrides": map[string]any{
			"annotationInferences":            map[string]any{},
			"cachedInferences":                map[string]any{},
			"emitAgentSearchExtractedResults": true,
			"emitInferences":                  false,
		},
	}
	if strings.TrimSpace(req.UpstreamThreadID) == "" && strings.TrimSpace(account.SpaceID) != "" {
		payload["threadParentPointer"] = map[string]any{
			"table":   "space",
			"id":      account.SpaceID,
			"spaceId": account.SpaceID,
		}
	}
	return payload, threadID
}

func (g *NotionGateway) RunPrompt(ctx context.Context, account model.Account, req NotionPromptRequest) (NotionRunResult, error) {
	payload, threadID := g.BuildPayload(account, req)
	if strings.TrimSpace(account.AuthToken) == "" && strings.TrimSpace(account.Cookie) == "" {
		return NotionRunResult{
			Text:          "Notion request converted successfully, but this account has no auth_token or cookie configured; upstream call was skipped.",
			ThreadID:      threadID,
			ConvertedOnly: true,
			Payload:       payload,
		}, nil
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return NotionRunResult{}, err
	}
	endpoint := strings.TrimRight(firstNonEmpty(g.cfg.Upstream.BaseURL, "https://www.notion.so"), "/") + "/api/v3/runInferenceTranscript"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return NotionRunResult{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/x-ndjson, application/json, text/plain")
	httpReq.Header.Set("Notion-Client-Version", firstNonEmpty(account.ClientVersion, "23.13.0.0"))
	if origin := strings.TrimSpace(g.cfg.Upstream.Origin); origin != "" {
		httpReq.Header.Set("Origin", origin)
		httpReq.Header.Set("Referer", strings.TrimRight(origin, "/")+"/")
	}
	if token := strings.TrimSpace(account.AuthToken); token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+token)
	}
	if cookie := strings.TrimSpace(account.Cookie); cookie != "" {
		httpReq.Header.Set("Cookie", cookie)
	}
	resp, err := g.client.Do(httpReq)
	if err != nil {
		return NotionRunResult{ThreadID: threadID, Payload: payload}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return NotionRunResult{ThreadID: threadID, Payload: payload}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return NotionRunResult{ThreadID: threadID, Payload: payload, RawResponse: string(raw)}, fmt.Errorf("notion upstream failed: %d %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	text, messageID := extractNotionText(raw)
	if strings.TrimSpace(text) == "" {
		text = "Notion upstream returned successfully, but no assistant text was detected."
	}
	return NotionRunResult{Text: text, ThreadID: threadID, MessageID: messageID, Payload: payload, RawResponse: string(raw)}, nil
}

func extractNotionText(raw []byte) (string, string) {
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	var bestText string
	var messageID string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "data:") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		}
		if line == "[DONE]" {
			continue
		}
		var payload any
		if err := json.Unmarshal([]byte(line), &payload); err != nil {
			continue
		}
		text, id := findTextInNotionPayload(payload)
		if strings.TrimSpace(text) != "" {
			bestText = text
		}
		if id != "" {
			messageID = id
		}
	}
	if bestText != "" {
		return bestText, messageID
	}
	var payload any
	if err := json.Unmarshal(raw, &payload); err == nil {
		return findTextInNotionPayload(payload)
	}
	return "", ""
}

func findTextInNotionPayload(value any) (string, string) {
	switch typed := value.(type) {
	case map[string]any:
		if text := stringFromAny(typed["text"]); text != "" {
			return text, stringFromAny(typed["id"])
		}
		if value := stringFromAny(typed["value"]); value != "" {
			return value, stringFromAny(typed["id"])
		}
		if role := stringFromAny(typed["role"]); role == "assistant" || role == "agent" {
			if content := stringFromAny(typed["content"]); content != "" {
				return content, stringFromAny(typed["id"])
			}
		}
		for _, key := range []string{"agent", "message", "thread_message", "recordMap", "value", "state", "result"} {
			if child, ok := typed[key]; ok {
				if text, id := findTextInNotionPayload(child); text != "" {
					if id == "" {
						id = stringFromAny(typed["id"])
					}
					return text, id
				}
			}
		}
		for _, child := range typed {
			if text, id := findTextInNotionPayload(child); text != "" {
				return text, id
			}
		}
	case []any:
		for _, child := range typed {
			if text, id := findTextInNotionPayload(child); text != "" {
				return text, id
			}
		}
	}
	return "", ""
}

func stringFromAny(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	default:
		return ""
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if clean := strings.TrimSpace(value); clean != "" {
			return clean
		}
	}
	return ""
}
