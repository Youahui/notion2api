package service

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"notaion2api/backend/internal/config"
	"notaion2api/backend/internal/dto"
	"notaion2api/backend/internal/model"
	"notaion2api/backend/internal/pkg/security"

	"github.com/google/uuid"
)

type OpenAIService struct {
	cfg      config.Config
	models   *ModelService
	accounts *AccountService
	notion   *NotionGateway
}

type CompletionResult struct {
	Payload map[string]any
	Usage   dto.UsageDTO
	Model   string
	Text    string
	Trace   map[string]any
}

func NewOpenAIService(cfg config.Config, models *ModelService, accounts *AccountService, notion *NotionGateway) *OpenAIService {
	return &OpenAIService{cfg: cfg, models: models, accounts: accounts, notion: notion}
}

func (s *OpenAIService) Models(ctx context.Context) (map[string]any, error) {
	models, err := s.models.ListOpenAI(ctx)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"object": "list",
		"data":   models,
	}, nil
}

func (s *OpenAIService) Model(ctx context.Context, id string) (dto.ModelDTO, error) {
	row, err := s.models.Resolve(ctx, id)
	if err != nil {
		return dto.ModelDTO{}, err
	}
	return ModelDTO(row), nil
}

func (s *OpenAIService) ChatCompletion(ctx context.Context, req dto.ChatCompletionRequest) (CompletionResult, error) {
	row, err := s.models.Resolve(ctx, req.Model)
	if err != nil {
		return CompletionResult{}, err
	}
	account, err := s.accounts.Select(ctx, requestedAccountEmail(req.AccountEmail, req.NotionAccountEmail, req.Metadata))
	if err != nil {
		return CompletionResult{}, err
	}
	prompt := flattenMessages(req.Messages)
	useWebSearch := s.cfg.Features.WebSearch
	if req.UseWebSearch != nil {
		useWebSearch = *req.UseWebSearch
	} else if value, ok := boolFromMetadata(req.Metadata, "use_web_search", "notion_use_web_search"); ok {
		useWebSearch = value
	}
	notionReq := NotionPromptRequest{
		Prompt:           prompt,
		PublicModel:      row.PublicID,
		NotionModel:      row.NotionModel,
		UseWebSearch:     useWebSearch,
		ConversationID:   firstNonEmpty(req.ConversationID, req.Conversation, stringFromMetadata(req.Metadata, "conversation_id", "notion_conversation_id")),
		UpstreamThreadID: firstNonEmpty(req.ThreadID, req.Thread, req.NotionThreadID, stringFromMetadata(req.Metadata, "thread_id", "notion_thread_id")),
		RawMessageCount:  len(req.Messages),
	}
	notionResult, err := s.notion.RunPrompt(ctx, account, notionReq)
	if err != nil {
		s.accounts.MarkFailure(ctx, account.ID, err.Error())
		return CompletionResult{}, err
	}
	s.accounts.MarkSuccess(ctx, account.ID)
	answer := notionResult.Text
	promptTokens := security.EstimateTokens(prompt)
	completionTokens := security.EstimateTokens(answer)
	usage := dto.UsageDTO{
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      promptTokens + completionTokens,
		Estimated:        true,
	}
	payload := map[string]any{
		"id":      "chatcmpl-" + uuid.NewString(),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   row.PublicID,
		"choices": []map[string]any{
			{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": answer,
				},
				"finish_reason": "stop",
			},
		},
		"usage": map[string]any{
			"prompt_tokens":     usage.PromptTokens,
			"completion_tokens": usage.CompletionTokens,
			"total_tokens":      usage.TotalTokens,
			"estimated":         usage.Estimated,
		},
		"notion_trace": map[string]any{
			"account_email":  account.Email,
			"thread_id":      notionResult.ThreadID,
			"message_id":     notionResult.MessageID,
			"converted_only": notionResult.ConvertedOnly,
			"request":        notionResult.Payload,
		},
	}
	return CompletionResult{Payload: payload, Usage: usage, Model: row.PublicID, Text: answer, Trace: payload["notion_trace"].(map[string]any)}, nil
}

func (s *OpenAIService) Response(ctx context.Context, body map[string]any) (CompletionResult, error) {
	raw, _ := json.Marshal(body)
	modelName, _ := body["model"].(string)
	row, err := s.models.Resolve(ctx, modelName)
	if err != nil {
		return CompletionResult{}, err
	}
	account, err := s.accounts.Select(ctx, requestedAccountEmailFromMap(body))
	if err != nil {
		return CompletionResult{}, err
	}
	prompt := promptFromResponsesInput(body["input"])
	useWebSearch := s.cfg.Features.WebSearch
	if value, ok := boolFromAny(body["use_web_search"]); ok {
		useWebSearch = value
	} else if value, ok := boolFromMetadata(body["metadata"], "use_web_search", "notion_use_web_search"); ok {
		useWebSearch = value
	}
	notionReq := NotionPromptRequest{
		Prompt:           prompt,
		PublicModel:      row.PublicID,
		NotionModel:      row.NotionModel,
		UseWebSearch:     useWebSearch,
		ConversationID:   firstNonEmpty(stringFromAny(body["conversation_id"]), stringFromAny(body["conversation"]), stringFromMetadata(body["metadata"], "conversation_id", "notion_conversation_id")),
		UpstreamThreadID: firstNonEmpty(stringFromAny(body["thread_id"]), stringFromAny(body["thread"]), stringFromAny(body["notion_thread_id"]), stringFromMetadata(body["metadata"], "thread_id", "notion_thread_id")),
		RawMessageCount:  1,
	}
	notionResult, err := s.notion.RunPrompt(ctx, account, notionReq)
	if err != nil {
		s.accounts.MarkFailure(ctx, account.ID, err.Error())
		return CompletionResult{}, err
	}
	s.accounts.MarkSuccess(ctx, account.ID)
	answer := notionResult.Text
	promptTokens := security.EstimateTokens(firstNonEmpty(prompt, string(raw)))
	completionTokens := security.EstimateTokens(answer)
	usage := dto.UsageDTO{
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      promptTokens + completionTokens,
		Estimated:        true,
	}
	payload := map[string]any{
		"id":      "resp-" + uuid.NewString(),
		"object":  "response",
		"created": time.Now().Unix(),
		"model":   row.PublicID,
		"output": []map[string]any{
			{
				"type": "message",
				"role": "assistant",
				"content": []map[string]any{
					{"type": "output_text", "text": answer},
				},
			},
		},
		"usage": map[string]any{
			"prompt_tokens":     usage.PromptTokens,
			"completion_tokens": usage.CompletionTokens,
			"total_tokens":      usage.TotalTokens,
			"estimated":         usage.Estimated,
		},
		"notion_trace": map[string]any{
			"account_email":  account.Email,
			"thread_id":      notionResult.ThreadID,
			"message_id":     notionResult.MessageID,
			"converted_only": notionResult.ConvertedOnly,
			"request":        notionResult.Payload,
		},
	}
	return CompletionResult{Payload: payload, Usage: usage, Model: row.PublicID, Text: answer, Trace: payload["notion_trace"].(map[string]any)}, nil
}

func flattenMessages(messages []dto.ChatMessage) string {
	var b strings.Builder
	for _, message := range messages {
		role := strings.TrimSpace(message.Role)
		content := contentToText(message.Content)
		if role == "system" || role == "developer" {
			b.WriteString("[")
			b.WriteString(role)
			b.WriteString("]\n")
			b.WriteString(content)
			b.WriteString("\n\n")
			continue
		}
		if role != "" {
			b.WriteString(role)
			b.WriteString(": ")
		}
		b.WriteString(content)
		b.WriteString("\n")
	}
	return b.String()
}

func contentToText(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := contentToText(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	case []map[string]any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := contentToText(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	case map[string]any:
		if text := strings.TrimSpace(stringFromAny(typed["text"])); text != "" {
			return text
		}
		if text := strings.TrimSpace(stringFromAny(typed["content"])); text != "" {
			return text
		}
		if imageURL, ok := typed["image_url"].(map[string]any); ok {
			if url := stringFromAny(imageURL["url"]); url != "" {
				return "[image] " + url
			}
		}
		if typ := stringFromAny(typed["type"]); typ != "" {
			if url := stringFromAny(typed["url"]); url != "" {
				return "[" + typ + "] " + url
			}
		}
	}
	raw, _ := json.Marshal(value)
	return strings.TrimSpace(string(raw))
}

func promptFromResponsesInput(input any) string {
	switch typed := input.(type) {
	case string:
		return strings.TrimSpace(typed)
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := contentToText(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	default:
		return contentToText(input)
	}
}

func requestedAccountEmail(accountEmail string, notionAccountEmail string, metadata any) string {
	return firstNonEmpty(accountEmail, notionAccountEmail, stringFromMetadata(metadata, "account_email", "notion_account_email"))
}

func requestedAccountEmailFromMap(body map[string]any) string {
	return firstNonEmpty(stringFromAny(body["account_email"]), stringFromAny(body["notion_account_email"]), stringFromMetadata(body["metadata"], "account_email", "notion_account_email"))
}

func stringFromMetadata(metadata any, keys ...string) string {
	meta, ok := metadata.(map[string]any)
	if !ok {
		return ""
	}
	for _, key := range keys {
		if value := stringFromAny(meta[key]); value != "" {
			return value
		}
	}
	return ""
}

func boolFromMetadata(metadata any, keys ...string) (bool, bool) {
	meta, ok := metadata.(map[string]any)
	if !ok {
		return false, false
	}
	for _, key := range keys {
		if value, ok := boolFromAny(meta[key]); ok {
			return value, true
		}
	}
	return false, false
}

func boolFromAny(value any) (bool, bool) {
	switch typed := value.(type) {
	case bool:
		return typed, true
	default:
		return false, false
	}
}

func OpenAIError(message string, errType string, code string) map[string]any {
	return map[string]any{
		"error": map[string]any{
			"message": message,
			"type":    errType,
			"code":    code,
		},
	}
}

func ModelName(row model.Model) string {
	if row.PublicID != "" {
		return row.PublicID
	}
	return "auto"
}
