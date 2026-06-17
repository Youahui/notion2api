package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

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

type pendingResponseContext struct {
	ResponseID     string
	CallID         string
	Prompt         string
	HiddenPrompt   string
	HiddenMessages []hiddenPromptMessage
	CreatedAt      time.Time
}

var (
	codexMarkdownFileLinkPattern = regexp.MustCompile(`\[([^\]\n]{1,512})\]\(([^)\s]+)\)`)
	pendingResponseContexts      sync.Map
)

const pendingResponseContextTTL = 30 * time.Minute

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
	if result, ok := maybeBuildCodexFileToolCallResult(row.PublicID, body); ok {
		return result, nil
	}
	account, stickyHit, err := s.selectAccountForThread(ctx, requestedAccountEmailFromMap(body), threadID)
	if err != nil {
		return CompletionResult{}, err
	}
	prompt, hiddenPrompt, hiddenMessages := responsesPromptPartsFromBody(body)
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
	trace := map[string]any{
		"account_email":  account.Email,
		"thread_id":      notionResult.ThreadID,
		"message_id":     notionResult.MessageID,
		"converted_only": notionResult.ConvertedOnly,
		"sticky_session": stickyHit,
		"request":        notionResult.Payload,
	}
	payload := buildResponsesPayload(row.PublicID, answer, usage, newResponsesID("resp"), newResponsesID("msg"), createdAt, body, trace)
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
	case nil:
		return ""
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
		if text := structuredFileContentToText(typed); text != "" {
			return text
		}
		if text := strings.TrimSpace(contentToText(typed["content"])); text != "" {
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

func structuredFileContentToText(item map[string]any) string {
	if item == nil {
		return ""
	}
	itemType := strings.ToLower(strings.TrimSpace(stringFromAny(item["type"])))
	isFile := itemType == "file" || itemType == "input_file" || itemType == "attachment"
	isImage := itemType == "image_url" || itemType == "input_image"
	if !isFile && !isImage && !hasFileContentHints(item) {
		return ""
	}
	name := firstNonEmpty(
		stringFromAny(item["filename"]),
		stringFromAny(item["name"]),
		stringFromAny(item["title"]),
	)
	if name == "" {
		if isImage {
			name = "image"
		} else {
			name = "file"
		}
	}
	if rawData := firstNonEmpty(stringFromAny(item["file_data"]), stringFromAny(item["data"])); rawData != "" {
		if text, ok := decodeInlineTextFileData(rawData); ok {
			return formatInlineFileForPrompt(name, text)
		}
		return formatFileReferenceForPrompt(name, "inline binary data")
	}
	if imageURL := mapFromAny(item["image_url"]); imageURL != nil {
		if ref := firstNonEmpty(stringFromAny(imageURL["url"]), stringFromAny(imageURL["value"])); ref != "" {
			return formatFileReferenceForPrompt(name, ref)
		}
	}
	if ref := firstNonEmpty(
		stringFromAny(item["file_url"]),
		stringFromAny(item["url"]),
		stringFromAny(item["path"]),
		stringFromAny(item["file_id"]),
	); ref != "" {
		return formatFileReferenceForPrompt(name, ref)
	}
	return ""
}

func hasFileContentHints(item map[string]any) bool {
	for _, key := range []string{"filename", "file_data", "file_url", "file_id", "path"} {
		if strings.TrimSpace(stringFromAny(item[key])) != "" {
			return true
		}
	}
	return false
}

func decodeInlineTextFileData(raw string) (string, bool) {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return "", false
	}
	if strings.HasPrefix(clean, "data:") {
		comma := strings.Index(clean, ",")
		if comma < 0 {
			return "", false
		}
		meta := strings.ToLower(clean[:comma])
		data := clean[comma+1:]
		if strings.Contains(meta, ";base64") {
			decoded, err := base64.StdEncoding.DecodeString(data)
			if err != nil || !utf8.Valid(decoded) {
				return "", false
			}
			return string(decoded), true
		}
		return strings.ReplaceAll(data, "%0A", "\n"), true
	}
	decoded, err := base64.StdEncoding.DecodeString(clean)
	if err == nil && utf8.Valid(decoded) {
		return string(decoded), true
	}
	if utf8.ValidString(clean) && strings.Contains(clean, "\n") {
		return clean, true
	}
	return "", false
}

func formatInlineFileForPrompt(name string, text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	const maxRunes = 200000
	runes := []rune(text)
	truncated := false
	if len(runes) > maxRunes {
		runes = runes[:maxRunes]
		truncated = true
	}
	body := string(runes)
	if truncated {
		body += "\n[truncated]"
	}
	return "<file name=\"" + strings.ReplaceAll(name, "\"", "'") + "\">\n" + body + "\n</file>"
}

func formatFileReferenceForPrompt(name string, ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	return "<file name=\"" + strings.ReplaceAll(name, "\"", "'") + "\" ref=\"" + strings.ReplaceAll(ref, "\"", "'") + "\" />"
}

func responsesPromptPartsFromBody(body map[string]any) (string, string, []hiddenPromptMessage) {
	prompt, inputHiddenPrompt, inputHiddenMessages := promptAndHiddenFromResponsesInput(body["input"])
	if attachmentsPrompt := promptFromResponsesInput(body["attachments"]); attachmentsPrompt != "" {
		prompt = joinPromptParts(prompt, attachmentsPrompt)
	}
	hiddenPrompt, hiddenMessages := hiddenPromptFromResponsesBody(body)
	if inputHiddenPrompt != "" {
		hiddenPrompt = joinHiddenPromptParts(hiddenPrompt, inputHiddenPrompt)
		hiddenMessages = append(hiddenMessages, inputHiddenMessages...)
	}
	if pending, ok := pendingResponseContextFromBody(body); ok {
		if pending.Prompt != "" && !strings.Contains(prompt, pending.Prompt) {
			prompt = joinPromptParts(pending.Prompt, prompt)
		}
		if pending.HiddenPrompt != "" && !strings.Contains(hiddenPrompt, pending.HiddenPrompt) {
			hiddenPrompt = joinHiddenPromptParts(pending.HiddenPrompt, hiddenPrompt)
		}
		if len(pending.HiddenMessages) > 0 {
			hiddenMessages = append(append([]hiddenPromptMessage{}, pending.HiddenMessages...), hiddenMessages...)
		}
	}
	hiddenPrompt = appendResponseFormatInstruction(hiddenPrompt, body["response_format"])
	return prompt, hiddenPrompt, hiddenMessages
}

func promptFromResponsesInput(input any) string {
	prompt, _, _ := promptAndHiddenFromResponsesInput(input)
	return prompt
}

func promptAndHiddenFromResponsesInput(input any) (string, string, []hiddenPromptMessage) {
	visibleParts := []string{}
	hiddenParts := []string{}
	hiddenMessages := []hiddenPromptMessage{}
	appendParts := func(visible string, hidden string, messages []hiddenPromptMessage) {
		if clean := strings.TrimSpace(visible); clean != "" {
			visibleParts = append(visibleParts, clean)
		}
		if clean := strings.TrimSpace(hidden); clean != "" {
			hiddenParts = append(hiddenParts, clean)
		}
		hiddenMessages = append(hiddenMessages, messages...)
	}
	switch typed := input.(type) {
	case string:
		appendParts(typed, "", nil)
	case []any:
		for _, item := range typed {
			visible, hidden, messages := responsesInputItemPromptParts(item)
			appendParts(visible, hidden, messages)
		}
	default:
		visible, hidden, messages := responsesInputItemPromptParts(input)
		appendParts(visible, hidden, messages)
	}
	return strings.TrimSpace(strings.Join(visibleParts, "\n")), strings.TrimSpace(strings.Join(hiddenParts, "\n\n")), hiddenMessages
}

func responsesInputItemPromptParts(input any) (string, string, []hiddenPromptMessage) {
	item := mapFromAny(input)
	if item == nil {
		return contentToText(input), "", nil
	}
	role := strings.ToLower(strings.TrimSpace(stringFromAny(item["role"])))
	itemType := strings.ToLower(strings.TrimSpace(stringFromAny(item["type"])))
	if role == "tool" || itemType == "function_call_output" || itemType == "tool_result" {
		return formatResponsesToolOutputForPrompt(item), "", nil
	}
	if role != "" || itemType == "message" {
		if role == "" {
			role = "user"
		}
		text := contentToText(item["content"])
		if strings.TrimSpace(text) == "" {
			return "", "", nil
		}
		switch role {
		case "system", "developer":
			return "", formatPromptSection(role, text), []hiddenPromptMessage{{Role: role, Content: text}}
		case "user":
			return "user: " + strings.TrimSpace(text), "", nil
		default:
			return role + ": " + strings.TrimSpace(text), "", nil
		}
	}
	return contentToText(item), "", nil
}

func legacyPromptFromResponsesInput(input any) string {
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

func newResponsesID(prefix string) string {
	prefix = strings.Trim(strings.TrimSpace(prefix), "_-")
	if prefix == "" {
		prefix = "resp"
	}
	return prefix + "_" + strings.ReplaceAll(uuid.NewString(), "-", "")
}

func buildResponsesPayload(modelID string, answer string, usage dto.UsageDTO, responseID string, outputItemID string, createdAt int64, request map[string]any, trace map[string]any) map[string]any {
	if strings.TrimSpace(responseID) == "" {
		responseID = newResponsesID("resp")
	}
	if strings.TrimSpace(outputItemID) == "" {
		outputItemID = newResponsesID("msg")
	}
	payload := buildResponsesBaseObject(responseID, modelID, createdAt, "completed", request)
	payload["completed_at"] = createdAt
	payload["output"] = []any{buildResponsesMessageItem(outputItemID, answer, "completed")}
	payload["output_text"] = answer
	payload["usage"] = buildResponsesUsage(usage)
	if trace != nil {
		payload["notion_trace"] = trace
	}
	return payload
}

func buildResponsesFunctionCallPayload(modelID string, toolName string, arguments string, callID string, usage dto.UsageDTO, responseID string, outputItemID string, createdAt int64, request map[string]any, trace map[string]any) map[string]any {
	if strings.TrimSpace(responseID) == "" {
		responseID = newResponsesID("resp")
	}
	if strings.TrimSpace(outputItemID) == "" {
		outputItemID = newResponsesID("fc")
	}
	if strings.TrimSpace(callID) == "" {
		callID = newResponsesID("call")
	}
	payload := buildResponsesBaseObject(responseID, modelID, createdAt, "completed", request)
	payload["completed_at"] = createdAt
	payload["output"] = []any{buildResponsesFunctionCallItem(outputItemID, toolName, arguments, callID, "completed")}
	payload["output_text"] = ""
	payload["usage"] = buildResponsesUsage(usage)
	if trace != nil {
		payload["notion_trace"] = trace
	}
	return payload
}

func buildResponsesBaseObject(responseID string, modelID string, createdAt int64, status string, request map[string]any) map[string]any {
	if strings.TrimSpace(status) == "" {
		status = "completed"
	}
	return map[string]any{
		"id":                   responseID,
		"object":               "response",
		"created_at":           createdAt,
		"created":              createdAt,
		"status":               status,
		"completed_at":         nil,
		"error":                nil,
		"incomplete_details":   nil,
		"instructions":         nullableStringFromMap(request, "instructions"),
		"max_output_tokens":    valueOrNil(request, "max_output_tokens"),
		"model":                modelID,
		"output":               []any{},
		"output_text":          "",
		"parallel_tool_calls":  boolFromMapDefault(request, "parallel_tool_calls", true),
		"previous_response_id": nullableStringFromMap(request, "previous_response_id"),
		"reasoning":            responsesReasoningObject(request),
		"store":                boolFromMapDefault(request, "store", true),
		"temperature":          valueOrDefault(request, "temperature", 1.0),
		"text":                 responsesTextObject(request),
		"tool_choice":          valueOrDefault(request, "tool_choice", "auto"),
		"tools":                arrayFromMapDefault(request, "tools"),
		"top_p":                valueOrDefault(request, "top_p", 1.0),
		"truncation":           valueOrDefault(request, "truncation", "disabled"),
		"usage":                nil,
		"user":                 valueOrNil(request, "user"),
		"metadata":             metadataFromRequest(request),
	}
}

func buildResponsesUsage(usage dto.UsageDTO) map[string]any {
	return map[string]any{
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
	}
}

func buildResponsesOutputTextPart(text string) map[string]any {
	return map[string]any{
		"type":        "output_text",
		"text":        text,
		"annotations": []any{},
	}
}

func buildResponsesMessageItem(itemID string, text string, status string) map[string]any {
	if strings.TrimSpace(status) == "" {
		status = "completed"
	}
	return map[string]any{
		"id":     itemID,
		"type":   "message",
		"status": status,
		"role":   "assistant",
		"content": []any{
			buildResponsesOutputTextPart(text),
		},
	}
}

func buildResponsesFunctionCallItem(itemID string, toolName string, arguments string, callID string, status string) map[string]any {
	if strings.TrimSpace(status) == "" {
		status = "completed"
	}
	return map[string]any{
		"id":        itemID,
		"type":      "function_call",
		"status":    status,
		"call_id":   callID,
		"name":      toolName,
		"arguments": arguments,
	}
}

func responsesReasoningObject(request map[string]any) map[string]any {
	reasoning := mapFromAny(valueOrNil(request, "reasoning"))
	return map[string]any{
		"effort":  nullableStringFromMap(reasoning, "effort"),
		"summary": nullableStringFromMap(reasoning, "summary"),
	}
}

func responsesTextObject(request map[string]any) map[string]any {
	if text := mapFromAny(valueOrNil(request, "text")); text != nil {
		return text
	}
	formatType := "text"
	if format := mapFromAny(valueOrNil(request, "response_format")); format != nil {
		if clean := strings.TrimSpace(stringFromAny(format["type"])); clean != "" {
			formatType = clean
		}
	}
	return map[string]any{"format": map[string]any{"type": formatType}}
}

func boolFromMapDefault(request map[string]any, key string, fallback bool) bool {
	if value, ok := boolFromAny(valueOrNil(request, key)); ok {
		return value
	}
	return fallback
}

func nullableStringFromMap(request map[string]any, key string) any {
	if clean := strings.TrimSpace(stringFromAny(valueOrNil(request, key))); clean != "" {
		return clean
	}
	return nil
}

func valueOrDefault(request map[string]any, key string, fallback any) any {
	if value := valueOrNil(request, key); value != nil {
		return value
	}
	return fallback
}

func valueOrNil(request map[string]any, key string) any {
	if request == nil {
		return nil
	}
	return request[key]
}

func arrayFromMapDefault(request map[string]any, key string) []any {
	if items := sliceFromAny(valueOrNil(request, key)); len(items) > 0 {
		return items
	}
	return []any{}
}

func metadataFromRequest(request map[string]any) map[string]any {
	if metadata := mapFromAny(valueOrNil(request, "metadata")); metadata != nil {
		return metadata
	}
	return map[string]any{}
}

func hiddenPromptFromResponsesBody(body map[string]any) (string, []hiddenPromptMessage) {
	instructions := strings.TrimSpace(stringFromAny(body["instructions"]))
	if instructions == "" {
		return "", nil
	}
	return formatPromptSection("system", instructions), []hiddenPromptMessage{{Role: "system", Content: instructions}}
}

func joinPromptParts(parts ...string) string {
	clean := make([]string, 0, len(parts))
	for _, part := range parts {
		if text := strings.TrimSpace(part); text != "" {
			clean = append(clean, text)
		}
	}
	return strings.Join(clean, "\n")
}

func joinHiddenPromptParts(parts ...string) string {
	clean := make([]string, 0, len(parts))
	for _, part := range parts {
		if text := strings.TrimSpace(part); text != "" {
			clean = append(clean, text)
		}
	}
	return strings.Join(clean, "\n\n")
}

func storePendingResponseContext(ctx pendingResponseContext) {
	ctx.ResponseID = strings.TrimSpace(ctx.ResponseID)
	ctx.CallID = strings.TrimSpace(ctx.CallID)
	if ctx.ResponseID == "" && ctx.CallID == "" {
		return
	}
	if ctx.CreatedAt.IsZero() {
		ctx.CreatedAt = time.Now()
	}
	cleanupPendingResponseContexts(time.Now())
	if ctx.ResponseID != "" {
		pendingResponseContexts.Store("resp:"+ctx.ResponseID, ctx)
	}
	if ctx.CallID != "" {
		pendingResponseContexts.Store("call:"+ctx.CallID, ctx)
	}
}

func pendingResponseContextFromBody(body map[string]any) (pendingResponseContext, bool) {
	keys := []string{}
	if previousResponseID := stringFromAny(body["previous_response_id"]); previousResponseID != "" {
		keys = append(keys, "resp:"+previousResponseID)
	}
	for _, callID := range responseToolOutputCallIDs(body["input"]) {
		keys = append(keys, "call:"+callID)
	}
	now := time.Now()
	for _, key := range keys {
		if value, ok := pendingResponseContexts.Load(key); ok {
			ctx, valid := value.(pendingResponseContext)
			if !valid || now.Sub(ctx.CreatedAt) > pendingResponseContextTTL {
				pendingResponseContexts.Delete(key)
				continue
			}
			if ctx.ResponseID != "" {
				pendingResponseContexts.Delete("resp:" + ctx.ResponseID)
			}
			if ctx.CallID != "" {
				pendingResponseContexts.Delete("call:" + ctx.CallID)
			}
			return ctx, true
		}
	}
	return pendingResponseContext{}, false
}

func responseToolOutputCallIDs(input any) []string {
	switch typed := input.(type) {
	case []any:
		out := []string{}
		for _, item := range typed {
			out = append(out, responseToolOutputCallIDs(item)...)
		}
		return out
	case []map[string]any:
		out := []string{}
		for _, item := range typed {
			out = append(out, responseToolOutputCallIDs(item)...)
		}
		return out
	case map[string]any:
		role := strings.ToLower(strings.TrimSpace(stringFromAny(typed["role"])))
		itemType := strings.ToLower(strings.TrimSpace(stringFromAny(typed["type"])))
		if role == "tool" || itemType == "function_call_output" || itemType == "tool_result" {
			if callID := stringFromAny(typed["call_id"]); callID != "" {
				return []string{callID}
			}
		}
		return responseToolOutputCallIDs(typed["content"])
	default:
		return nil
	}
}

func cleanupPendingResponseContexts(now time.Time) {
	pendingResponseContexts.Range(func(key any, value any) bool {
		ctx, ok := value.(pendingResponseContext)
		if !ok || now.Sub(ctx.CreatedAt) > pendingResponseContextTTL {
			pendingResponseContexts.Delete(key)
		}
		return true
	})
}

func maybeBuildCodexFileToolCallResult(modelID string, body map[string]any) (CompletionResult, bool) {
	if body == nil || responsesInputHasToolOutput(body["input"]) || isCodexTitleGenerationRequest(body) {
		return CompletionResult{}, false
	}
	files := codexFileRefsFromResponsesInput(body["input"])
	if len(files) == 0 {
		return CompletionResult{}, false
	}
	toolName, args, ok := codexFileReadToolCall(body["tools"], files)
	if !ok {
		return CompletionResult{}, false
	}
	encodedArgs, err := json.Marshal(args)
	if err != nil {
		return CompletionResult{}, false
	}
	promptTokens := security.EstimateTokens(legacyPromptFromResponsesInput(body["input"]))
	completionTokens := security.EstimateTokens(string(encodedArgs))
	usage := dto.UsageDTO{
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      promptTokens + completionTokens,
		Estimated:        true,
	}
	trace := map[string]any{
		"converted_only": true,
		"tool_call": map[string]any{
			"name":      toolName,
			"arguments": args,
			"reason":    "codex_file_reference",
		},
	}
	responseID := newResponsesID("resp")
	callID := newResponsesID("call")
	prompt, hiddenPrompt, hiddenMessages := responsesPromptPartsFromBody(body)
	storePendingResponseContext(pendingResponseContext{
		ResponseID:     responseID,
		CallID:         callID,
		Prompt:         prompt,
		HiddenPrompt:   hiddenPrompt,
		HiddenMessages: hiddenMessages,
	})
	payload := buildResponsesFunctionCallPayload(modelID, toolName, string(encodedArgs), callID, usage, responseID, newResponsesID("fc"), time.Now().Unix(), body, trace)
	return CompletionResult{Payload: payload, Usage: usage, Model: modelID, Trace: trace}, true
}

func codexFileReadToolCall(tools any, files []string) (string, map[string]any, bool) {
	if len(files) == 0 {
		return "", nil, false
	}
	switch {
	case responseHasTool(tools, "connections.fs.readFiles"):
		return "connections.fs.readFiles", map[string]any{"files": files}, true
	case responseHasTool(tools, "connections.fs.readFile"):
		return "connections.fs.readFile", map[string]any{"file": files[0]}, true
	case responseHasTool(tools, "exec_command"):
		return "exec_command", map[string]any{
			"cmd":               codexReadFileCommand(files[0]),
			"yield_time_ms":     10000,
			"max_output_tokens": 20000,
		}, true
	default:
		return "", nil, false
	}
}

func responseHasTool(tools any, names ...string) bool {
	wanted := make(map[string]struct{}, len(names))
	for _, name := range names {
		if clean := strings.TrimSpace(name); clean != "" {
			wanted[clean] = struct{}{}
		}
	}
	for _, rawTool := range sliceFromAny(tools) {
		tool := mapFromAny(rawTool)
		if tool == nil {
			continue
		}
		if _, ok := wanted[stringFromAny(tool["name"])]; ok {
			return true
		}
		if function := mapFromAny(tool["function"]); function != nil {
			if _, ok := wanted[stringFromAny(function["name"])]; ok {
				return true
			}
		}
	}
	return false
}

func codexReadFileCommand(file string) string {
	return "sed -n '1,400p' " + shellQuote(file)
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func codexFileRefsFromResponsesInput(input any) []string {
	return codexFileRefsFromText(legacyPromptFromResponsesInput(input))
}

func codexFileRefsFromText(text string) []string {
	matches := codexMarkdownFileLinkPattern.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	files := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) < 3 {
			continue
		}
		file := normalizeCodexFileRef(match[1], match[2])
		if file == "" {
			continue
		}
		if _, exists := seen[file]; exists {
			continue
		}
		seen[file] = struct{}{}
		files = append(files, file)
		if len(files) >= 8 {
			break
		}
	}
	return files
}

func normalizeCodexFileRef(label string, target string) string {
	label = strings.TrimSpace(label)
	target = strings.TrimSpace(target)
	if target == "" {
		target = label
	}
	if strings.HasPrefix(strings.ToLower(target), "https://app.notion.com/") {
		parsed, err := url.Parse(target)
		if err == nil {
			target = strings.TrimPrefix(parsed.EscapedPath(), "/")
			if decoded, decodeErr := url.PathUnescape(target); decodeErr == nil {
				target = decoded
			}
		}
		if target == "" {
			target = label
		}
	}
	target = strings.Trim(strings.TrimSpace(target), "/")
	target = strings.TrimPrefix(target, "./")
	if target == "" || strings.Contains(target, "\x00") || strings.HasPrefix(target, "~") || strings.HasPrefix(target, "/") {
		return ""
	}
	for _, part := range strings.Split(target, "/") {
		if part == "" || part == "." || part == ".." {
			return ""
		}
	}
	if strings.Contains(target, "://") {
		return ""
	}
	return target
}

func responsesInputHasToolOutput(input any) bool {
	switch typed := input.(type) {
	case []any:
		for _, item := range typed {
			if responsesInputHasToolOutput(item) {
				return true
			}
		}
		return false
	case []map[string]any:
		for _, item := range typed {
			if responsesInputHasToolOutput(item) {
				return true
			}
		}
		return false
	case map[string]any:
		role := strings.ToLower(strings.TrimSpace(stringFromAny(typed["role"])))
		itemType := strings.ToLower(strings.TrimSpace(stringFromAny(typed["type"])))
		if role == "tool" || itemType == "function_call_output" || itemType == "tool_result" {
			return true
		}
		if content, ok := typed["content"]; ok {
			return responsesInputHasToolOutput(content)
		}
		return false
	default:
		return false
	}
}

func isCodexTitleGenerationRequest(body map[string]any) bool {
	if text := strings.ToLower(legacyPromptFromResponsesInput(body["input"])); strings.Contains(text, "generate a concise ui title") || strings.Contains(text, "short title for a task") {
		return true
	}
	textConfig := mapFromAny(valueOrNil(body, "text"))
	format := mapFromAny(valueOrNil(textConfig, "format"))
	if strings.EqualFold(stringFromAny(format["name"]), "codex_output_schema") {
		schema := mapFromAny(format["schema"])
		properties := mapFromAny(schema["properties"])
		if _, ok := properties["title"]; ok {
			return true
		}
	}
	return false
}

func formatResponsesToolOutputForPrompt(item map[string]any) string {
	if item == nil {
		return ""
	}
	output := responsesToolOutputText(item["output"])
	if output == "" {
		output = responsesToolOutputText(item["content"])
	}
	if output == "" {
		return ""
	}
	name := firstNonEmpty(stringFromAny(item["name"]), stringFromAny(item["tool_name"]))
	callID := stringFromAny(item["call_id"])
	attrs := []string{}
	if name != "" {
		attrs = append(attrs, `name="`+strings.ReplaceAll(name, "\"", "'")+`"`)
	}
	if callID != "" {
		attrs = append(attrs, `call_id="`+strings.ReplaceAll(callID, "\"", "'")+`"`)
	}
	if len(attrs) == 0 {
		return "<tool_result>\n" + strings.TrimSpace(output) + "\n</tool_result>"
	}
	return "<tool_result " + strings.Join(attrs, " ") + ">\n" + strings.TrimSpace(output) + "\n</tool_result>"
}

func responsesToolOutputText(value any) string {
	if value == nil {
		return ""
	}
	if mapped := mapFromAny(value); mapped != nil {
		for _, key := range []string{"output", "stdout", "stderr", "text", "content"} {
			if text := contentToText(mapped[key]); text != "" {
				return text
			}
		}
	}
	return contentToText(value)
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
