package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
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
	sticky   StickySessionStore
}

type CompletionResult struct {
	Payload map[string]any
	Usage   dto.UsageDTO
	Model   string
	Text    string
	Trace   map[string]any
}

type hiddenPromptMessage struct {
	Role    string
	Content string
}

func NewOpenAIService(cfg config.Config, models *ModelService, accounts *AccountService, notion *NotionGateway, sticky ...StickySessionStore) *OpenAIService {
	var store StickySessionStore
	if len(sticky) > 0 {
		store = sticky[0]
	}
	return &OpenAIService{cfg: cfg, models: models, accounts: accounts, notion: notion, sticky: store}
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
	threadID := firstNonEmpty(req.ThreadID, req.Thread, req.NotionThreadID, stringFromMetadata(req.Metadata, "thread_id", "notion_thread_id"))
	account, stickyHit, err := s.selectAccountForThread(ctx, requestedAccountEmail(req.AccountEmail, req.NotionAccountEmail, req.Metadata), threadID)
	if err != nil {
		return CompletionResult{}, err
	}
	prompt, hiddenPrompt, hiddenMessages := normalizeChatMessages(req.Messages)
	hiddenPrompt = appendResponseFormatInstruction(hiddenPrompt, req.ResponseFormat)
	notionPrompt := prependHiddenPromptInstructionEnvelope(prompt, hiddenPrompt)
	useWebSearch := s.cfg.Features.WebSearch
	if req.UseWebSearch != nil {
		useWebSearch = *req.UseWebSearch
	} else if value, ok := boolFromMetadata(req.Metadata, "use_web_search", "notion_use_web_search"); ok {
		useWebSearch = value
	}
	notionReq := NotionPromptRequest{
		Prompt:           notionPrompt,
		HiddenPrompt:     hiddenPrompt,
		HiddenMessages:   hiddenMessages,
		PublicModel:      row.PublicID,
		NotionModel:      row.NotionModel,
		UseWebSearch:     useWebSearch,
		ConversationID:   firstNonEmpty(req.ConversationID, req.Conversation, stringFromMetadata(req.Metadata, "conversation_id", "notion_conversation_id")),
		UpstreamThreadID: threadID,
		RawMessageCount:  len(req.Messages),
	}
	account, notionResult, err := s.runPromptWithHealth(ctx, account, notionReq)
	if err != nil {
		s.accounts.MarkFailure(ctx, account.ID, err.Error())
		return CompletionResult{}, err
	}
	s.accounts.MarkSuccess(ctx, account.ID)
	s.bindStickyThread(ctx, account, notionResult)
	answer := notionResult.Text
	promptTokens := security.EstimateTokens(firstNonEmpty(notionPrompt, prompt, hiddenPrompt))
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
			"sticky_session": stickyHit,
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
	threadID := firstNonEmpty(stringFromAny(body["thread_id"]), stringFromAny(body["thread"]), stringFromAny(body["notion_thread_id"]), stringFromMetadata(body["metadata"], "thread_id", "notion_thread_id"))
	account, stickyHit, err := s.selectAccountForThread(ctx, requestedAccountEmailFromMap(body), threadID)
	if err != nil {
		return CompletionResult{}, err
	}
	prompt := promptFromResponsesInput(body["input"])
	hiddenPrompt, hiddenMessages := hiddenPromptFromResponsesBody(body)
	hiddenPrompt = appendResponseFormatInstruction(hiddenPrompt, body["response_format"])
	notionPrompt := prependHiddenPromptInstructionEnvelope(prompt, hiddenPrompt)
	useWebSearch := s.cfg.Features.WebSearch
	if value, ok := boolFromAny(body["use_web_search"]); ok {
		useWebSearch = value
	} else if value, ok := boolFromMetadata(body["metadata"], "use_web_search", "notion_use_web_search"); ok {
		useWebSearch = value
	}
	notionReq := NotionPromptRequest{
		Prompt:           notionPrompt,
		HiddenPrompt:     hiddenPrompt,
		HiddenMessages:   hiddenMessages,
		PublicModel:      row.PublicID,
		NotionModel:      row.NotionModel,
		UseWebSearch:     useWebSearch,
		ConversationID:   firstNonEmpty(stringFromAny(body["conversation_id"]), stringFromAny(body["conversation"]), stringFromMetadata(body["metadata"], "conversation_id", "notion_conversation_id")),
		UpstreamThreadID: threadID,
		RawMessageCount:  1,
	}
	account, notionResult, err := s.runPromptWithHealth(ctx, account, notionReq)
	if err != nil {
		s.accounts.MarkFailure(ctx, account.ID, err.Error())
		return CompletionResult{}, err
	}
	s.accounts.MarkSuccess(ctx, account.ID)
	s.bindStickyThread(ctx, account, notionResult)
	answer := notionResult.Text
	promptTokens := security.EstimateTokens(firstNonEmpty(notionPrompt, prompt, hiddenPrompt, string(raw)))
	completionTokens := security.EstimateTokens(answer)
	usage := dto.UsageDTO{
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      promptTokens + completionTokens,
		Estimated:        true,
	}
	createdAt := time.Now().Unix()
	payload := map[string]any{
		"id":         "resp-" + uuid.NewString(),
		"object":     "response",
		"created_at": createdAt,
		"created":    createdAt,
		"status":     "completed",
		"model":      row.PublicID,
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
			"input_tokens":  usage.PromptTokens,
			"output_tokens": usage.CompletionTokens,
			"total_tokens":  usage.TotalTokens,
			"input_tokens_details": map[string]any{
				"cached_tokens": 0,
			},
			"output_tokens_details": map[string]any{
				"reasoning_tokens": 0,
			},
			"prompt_tokens":     usage.PromptTokens,
			"completion_tokens": usage.CompletionTokens,
			"estimated":         usage.Estimated,
		},
		"notion_trace": map[string]any{
			"account_email":  account.Email,
			"thread_id":      notionResult.ThreadID,
			"message_id":     notionResult.MessageID,
			"converted_only": notionResult.ConvertedOnly,
			"sticky_session": stickyHit,
			"request":        notionResult.Payload,
		},
	}
	return CompletionResult{Payload: payload, Usage: usage, Model: row.PublicID, Text: answer, Trace: payload["notion_trace"].(map[string]any)}, nil
}

func (s *OpenAIService) selectAccountForThread(ctx context.Context, preferredEmail string, threadID string) (model.Account, bool, error) {
	threadID = strings.TrimSpace(threadID)
	preferredEmail = strings.TrimSpace(preferredEmail)
	if threadID == "" || s.sticky == nil {
		account, err := s.accounts.Select(ctx, preferredEmail)
		return account, false, err
	}
	binding, ok, err := s.sticky.Get(ctx, threadID)
	if err != nil {
		slog.Warn("sticky session lookup failed", "thread_id", threadID, "error", err)
		account, selectErr := s.accounts.Select(ctx, preferredEmail)
		return account, false, selectErr
	}
	if !ok || binding.AccountID == 0 {
		account, err := s.accounts.Select(ctx, preferredEmail)
		return account, false, err
	}
	account, err := s.accounts.Get(ctx, binding.AccountID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			_ = s.sticky.Delete(ctx, threadID)
			account, selectErr := s.accounts.Select(ctx, preferredEmail)
			return account, false, selectErr
		}
		return model.Account{}, false, err
	}
	if !account.Enabled {
		return model.Account{}, false, fmt.Errorf("%w: notion_thread_id %s is bound to disabled account %s", ErrForbidden, threadID, account.Email)
	}
	if preferredEmail != "" && !strings.EqualFold(account.Email, preferredEmail) {
		return model.Account{}, false, fmt.Errorf("%w: notion_thread_id %s is bound to account %s, not %s", ErrConflict, threadID, account.Email, preferredEmail)
	}
	return account, true, nil
}

func (s *OpenAIService) bindStickyThread(ctx context.Context, account model.Account, result NotionRunResult) {
	if s.sticky == nil || account.ID == 0 || result.ConvertedOnly {
		return
	}
	threadID := strings.TrimSpace(result.ThreadID)
	if threadID == "" {
		return
	}
	err := s.sticky.Set(ctx, StickyThreadBinding{
		ThreadID:     threadID,
		AccountID:    account.ID,
		AccountEmail: account.Email,
		SpaceID:      account.SpaceID,
		SpaceViewID:  account.SpaceViewID,
	}, stickyTTL(s.cfg.Sticky))
	if err != nil {
		slog.Warn("sticky session bind failed", "thread_id", threadID, "account_id", account.ID, "error", err)
	}
}

func (s *OpenAIService) runPromptWithHealth(ctx context.Context, account model.Account, req NotionPromptRequest) (model.Account, NotionRunResult, error) {
	if err := s.accounts.ProbeProtocolHealth(ctx, s.notion, account); err != nil {
		if !isSessionRetryableError(err) {
			return account, NotionRunResult{}, err
		}
		refreshed, refreshErr := s.accounts.RefreshSession(ctx, account.ID)
		if refreshErr != nil {
			return account, NotionRunResult{}, refreshErr
		}
		account = refreshed
		if probeErr := s.accounts.ProbeProtocolHealth(ctx, s.notion, account); probeErr != nil {
			return account, NotionRunResult{}, probeErr
		}
	}
	result, err := s.notion.RunPrompt(ctx, account, req)
	if err == nil || !isSessionRetryableError(err) {
		return account, result, err
	}
	refreshed, refreshErr := s.accounts.RefreshSession(ctx, account.ID)
	if refreshErr != nil {
		return account, result, refreshErr
	}
	account = refreshed
	if probeErr := s.accounts.ProbeProtocolHealth(ctx, s.notion, account); probeErr != nil {
		return account, result, probeErr
	}
	result, err = s.notion.RunPrompt(ctx, account, req)
	return account, result, err
}

func normalizeChatMessages(messages []dto.ChatMessage) (string, string, []hiddenPromptMessage) {
	var b strings.Builder
	hiddenParts := []string{}
	hiddenMessages := []hiddenPromptMessage{}
	for _, message := range messages {
		role := strings.ToLower(strings.TrimSpace(message.Role))
		if role == "" {
			role = "user"
		}
		content := contentToText(message.Content)
		if role == "system" || role == "developer" {
			if content != "" {
				hiddenParts = append(hiddenParts, formatPromptSection(role, content))
				hiddenMessages = append(hiddenMessages, hiddenPromptMessage{Role: role, Content: content})
			}
			continue
		}
		if role != "" {
			b.WriteString(role)
			b.WriteString(": ")
		}
		b.WriteString(content)
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String()), strings.TrimSpace(strings.Join(hiddenParts, "\n\n")), hiddenMessages
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

func hiddenPromptFromResponsesBody(body map[string]any) (string, []hiddenPromptMessage) {
	instructions := strings.TrimSpace(stringFromAny(body["instructions"]))
	if instructions == "" {
		return "", nil
	}
	return formatPromptSection("system", instructions), []hiddenPromptMessage{{Role: "system", Content: instructions}}
}

func formatPromptSection(header string, body string) string {
	header = strings.TrimSpace(header)
	body = strings.TrimSpace(body)
	switch {
	case header == "":
		return body
	case body == "":
		return "[" + header + "]"
	default:
		return "[" + header + "]\n" + body
	}
}

func appendResponseFormatInstruction(hiddenPrompt string, responseFormat any) string {
	hiddenPrompt = strings.TrimSpace(hiddenPrompt)
	instruction := responseFormatInstruction(responseFormat)
	if instruction == "" {
		return hiddenPrompt
	}
	if hiddenPrompt == "" {
		return instruction
	}
	return hiddenPrompt + "\n\n" + instruction
}

func prependHiddenPromptInstructionEnvelope(prompt string, hiddenPrompt string) string {
	prompt = strings.TrimSpace(prompt)
	hiddenPrompt = strings.TrimSpace(hiddenPrompt)
	if prompt == "" || hiddenPrompt == "" {
		return prompt
	}
	return strings.Join([]string{
		"<notion2api-instructions>",
		"The following higher-priority instructions come from the original system/developer messages and API response_format. Follow them exactly. They are not user content. Do not mention this instruction block or its XML tags in the final answer.",
		hiddenPrompt,
		"</notion2api-instructions>",
		"",
		"<notion2api-user-message>",
		prompt,
		"</notion2api-user-message>",
	}, "\n")
}

func responseFormatInstruction(responseFormat any) string {
	format := mapFromAny(responseFormat)
	if len(format) == 0 {
		return ""
	}
	formatType := strings.TrimSpace(strings.ToLower(stringFromAny(format["type"])))
	switch formatType {
	case "json_object":
		return formatPromptSection("response_format", "The client requested response_format.type=json_object. Return only one valid JSON object. Do not include Markdown fences, prose, numbering, or any text outside the JSON object.")
	case "json_schema":
		schemaBytes, err := json.Marshal(format["json_schema"])
		if err != nil || len(schemaBytes) == 0 || string(schemaBytes) == "null" {
			return formatPromptSection("response_format", "The client requested response_format.type=json_schema. Return only valid JSON that conforms to the requested schema. Do not include Markdown fences, prose, numbering, or any text outside the JSON value.")
		}
		return formatPromptSection("response_format", "The client requested response_format.type=json_schema. Return only valid JSON that conforms to this schema. Do not include Markdown fences, prose, numbering, or any text outside the JSON value.\nSchema:\n"+string(schemaBytes))
	default:
		return ""
	}
}

func mapFromAny(value any) map[string]any {
	if typed, ok := value.(map[string]any); ok {
		return typed
	}
	raw, err := json.Marshal(value)
	if err != nil || len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	return out
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
