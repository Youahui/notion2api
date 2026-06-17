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

type OpenAIRequestOptions struct {
	APIKeyID             uint
	RequestID            string
	HeaderSessionID      string
	HeaderConversationID string
}

type stickySessionKey struct {
	Key    string
	Source string
}

type accountSelection struct {
	Account   model.Account
	StickyHit bool
	ThreadID  string
	Source    string
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

type responseFunctionToolCall struct {
	Name      string
	Arguments string
	CallID    string
	ItemID    string
}

var (
	codexMarkdownFileLinkPattern   = regexp.MustCompile(`\[([^\]\n]{1,512})\]\(([^)\s]+)\)`)
	codexPlainFileRefPattern       = regexp.MustCompile(`(?:^|[\s"'` + "`" + `([{<，。！？、：；,;:!?])([A-Za-z0-9_.@+-]+(?:/[A-Za-z0-9_.@+-]+)*|Dockerfile|Makefile|compose\.ya?ml|docker-compose\.ya?ml)(?:$|[\s"'` + "`" + `)\]}>，。！？、：；,;:!?])`)
	codexQuotedFilesPattern        = regexp.MustCompile(`(?i)"files"\s*:\s*\[([^\]]*)\]`)
	codexQuotedFilePattern         = regexp.MustCompile(`(?i)"file"\s*:\s*"([^"]{1,512})"`)
	codexJSONStringPattern         = regexp.MustCompile(`"([^"\\]*(?:\\.[^"\\]*)*)"`)
	codexBacktickContentPattern    = regexp.MustCompile("`([^`\\n]{1,512})`")
	codexFencedCodePattern         = regexp.MustCompile("(?s)```(?:[A-Za-z0-9_-]+)?\\s*\\n(.*?)\\n```")
	codexFencedCodeBlockPattern    = regexp.MustCompile("(?s)```([A-Za-z0-9_-]*)\\s*\\n(.*?)\\n```")
	codexToolResultBlockPattern    = regexp.MustCompile(`(?s)<tool_result\b[^>]*>.*?</tool_result>\s*`)
	codexContextResultBlockPattern = regexp.MustCompile(`(?s)<context_result\b[^>]*>.*?</context_result>\s*`)
	responsesEnvContextPattern     = regexp.MustCompile(`(?s)<environment_context>.*?</environment_context>\s*`)
	responsesToolCallPattern       = regexp.MustCompile(`(?s)<tool_call\b[^>]*>.*?</tool_call>\s*`)
	responsesToolResultOpenTag     = regexp.MustCompile(`<tool_result\b[^>]*>`)
	pendingResponseContexts        sync.Map
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

func (s *OpenAIService) ChatCompletion(ctx context.Context, req dto.ChatCompletionRequest, opts ...OpenAIRequestOptions) (CompletionResult, error) {
	options := mergeOpenAIRequestOptions(opts...)
	row, err := s.models.Resolve(ctx, req.Model)
	if err != nil {
		return CompletionResult{}, err
	}
	threadID := firstNonEmpty(req.ThreadID, req.Thread, req.NotionThreadID, stringFromMetadata(req.Metadata, "thread_id", "notion_thread_id"))
	sessionKeys := chatStickySessionKeys(req, options)
	selection, err := s.selectAccountForStickyKeys(ctx, requestedAccountEmail(req.AccountEmail, req.NotionAccountEmail, req.Metadata), threadID, sessionKeys)
	if err != nil {
		return CompletionResult{}, err
	}
	account := selection.Account
	prompt, hiddenPrompt, hiddenMessages := normalizeChatMessages(req.Messages)
	hiddenPrompt, hiddenMessages = appendResponseFormatInstruction(hiddenPrompt, hiddenMessages, req.ResponseFormat)
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
		Attachments:      chatCompletionAttachmentUploads(req),
		RequestID:        options.RequestID,
		PublicModel:      row.PublicID,
		NotionModel:      row.NotionModel,
		UseWebSearch:     useWebSearch,
		ConversationID:   firstNonEmpty(req.ConversationID, req.Conversation, stringFromMetadata(req.Metadata, "conversation_id", "notion_conversation_id")),
		UpstreamThreadID: selection.ThreadID,
		RawMessageCount:  len(req.Messages),
	}
	account, notionResult, err := s.runPromptWithHealth(ctx, account, notionReq)
	if err != nil {
		if isTrustRuleDeniedInferenceError(err) {
			s.clearStickyThreadBindings(ctx, selection.ThreadID, sessionKeys)
		}
		s.accounts.MarkFailure(ctx, account.ID, err.Error())
		return CompletionResult{}, err
	}
	s.accounts.MarkSuccess(ctx, account.ID)
	s.bindStickyThread(ctx, account, notionResult, sessionKeys, "", options.APIKeyID)
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
			"sticky_session": selection.StickyHit,
			"sticky_source":  selection.Source,
			"request":        notionResult.Payload,
		},
	}
	return CompletionResult{Payload: payload, Usage: usage, Model: row.PublicID, Text: answer, Trace: payload["notion_trace"].(map[string]any)}, nil
}

func (s *OpenAIService) Response(ctx context.Context, body map[string]any, opts ...OpenAIRequestOptions) (CompletionResult, error) {
	options := mergeOpenAIRequestOptions(opts...)
	raw, _ := json.Marshal(body)
	modelName, _ := body["model"].(string)
	row, err := s.models.Resolve(ctx, modelName)
	if err != nil {
		return CompletionResult{}, err
	}
	threadID := firstNonEmpty(stringFromAny(body["thread_id"]), stringFromAny(body["thread"]), stringFromAny(body["notion_thread_id"]), stringFromMetadata(body["metadata"], "thread_id", "notion_thread_id"))
	if result, ok := maybeBuildCodexFileToolCallResult(row.PublicID, body); ok {
		logCodexToolCallConversion(options, result)
		return result, nil
	}
	if result, ok := maybeBuildCodexDeterministicEditToolCallResult(row.PublicID, body); ok {
		logCodexToolCallConversion(options, result)
		return result, nil
	}
	if result, ok := maybeBuildCodexApplyPatchCompletionResult(row.PublicID, body); ok {
		slog.Info("responses completed local apply_patch output",
			"request_id", options.RequestID,
			"model", result.Model,
			"response_id", stringFromAny(result.Payload["id"]),
			"answer_preview", truncateRunes(collapseWhitespace(result.Text), 180),
		)
		return result, nil
	}
	sessionKeys := responseStickySessionKeys(body, options)
	selection, err := s.selectAccountForStickyKeys(ctx, requestedAccountEmailFromMap(body), threadID, sessionKeys)
	if err != nil {
		return CompletionResult{}, err
	}
	account := selection.Account
	prompt, hiddenPrompt, hiddenMessages := responsesPromptPartsFromBody(body)
	notionPrompt := responsesNotionVisiblePrompt(prompt, hiddenMessages)
	useWebSearch := s.cfg.Features.WebSearch
	if value, ok := boolFromAny(body["use_web_search"]); ok {
		useWebSearch = value
	} else if value, ok := boolFromMetadata(body["metadata"], "use_web_search", "notion_use_web_search"); ok {
		useWebSearch = value
	}
	notionReq := NotionPromptRequest{
		Prompt:           notionPrompt,
		HiddenPrompt:     "",
		HiddenMessages:   nil,
		Attachments:      responsesAttachmentUploadsFromBody(body),
		RequestID:        options.RequestID,
		PublicModel:      row.PublicID,
		NotionModel:      row.NotionModel,
		UseWebSearch:     useWebSearch,
		ConversationID:   firstNonEmpty(stringFromAny(body["conversation_id"]), stringFromAny(body["conversation"]), stringFromMetadata(body["metadata"], "conversation_id", "notion_conversation_id")),
		UpstreamThreadID: selection.ThreadID,
		RawMessageCount:  1,
	}
	account, notionResult, err := s.runPromptWithHealth(ctx, account, notionReq)
	if err != nil {
		if isTrustRuleDeniedInferenceError(err) {
			s.clearStickyThreadBindings(ctx, selection.ThreadID, sessionKeys)
		}
		s.accounts.MarkFailure(ctx, account.ID, err.Error())
		return CompletionResult{}, err
	}
	s.accounts.MarkSuccess(ctx, account.ID)
	answer := notionResult.Text
	slog.Info("responses extracted notion answer",
		"request_id", options.RequestID,
		"model", row.PublicID,
		"thread_id", notionResult.ThreadID,
		"message_id", notionResult.MessageID,
		"answer_chars", utf8.RuneCountInString(answer),
		"answer_preview", truncateRunes(collapseWhitespace(answer), 180),
	)
	promptTokens := security.EstimateTokens(firstNonEmpty(notionPrompt, prompt, hiddenPrompt, string(raw)))
	completionTokens := security.EstimateTokens(answer)
	usage := dto.UsageDTO{
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      promptTokens + completionTokens,
		Estimated:        true,
	}
	createdAt := time.Now().Unix()
	responseID := newResponsesID("resp")
	trace := map[string]any{
		"account_email":  account.Email,
		"thread_id":      notionResult.ThreadID,
		"message_id":     notionResult.MessageID,
		"converted_only": notionResult.ConvertedOnly,
		"sticky_session": selection.StickyHit,
		"sticky_source":  selection.Source,
		"request":        notionResult.Payload,
	}
	if toolResult, ok := maybeBuildCodexAssistantTextToolCallResult(row.PublicID, body, answer, trace); ok {
		if responseStoreEnabled(body) {
			if responseID := stringFromAny(toolResult.Payload["id"]); responseID != "" {
				s.bindStickyThread(ctx, account, notionResult, sessionKeys, responseID, options.APIKeyID)
			}
		}
		logCodexToolCallConversion(options, toolResult)
		return toolResult, nil
	}
	answer, incompleteDetails := applyResponsesOutputLimit(answer, body)
	if incompleteDetails != nil {
		completionTokens = security.EstimateTokens(answer)
		usage.CompletionTokens = completionTokens
		usage.TotalTokens = usage.PromptTokens + completionTokens
	}
	if responseStoreEnabled(body) {
		s.bindStickyThread(ctx, account, notionResult, sessionKeys, responseID, options.APIKeyID)
	}
	payload := buildResponsesPayload(row.PublicID, answer, usage, responseID, newResponsesID("msg"), createdAt, body, trace)
	if incompleteDetails != nil {
		payload["status"] = "incomplete"
		payload["incomplete_details"] = incompleteDetails
		payload["completed_at"] = nil
	}
	return CompletionResult{Payload: payload, Usage: usage, Model: row.PublicID, Text: answer, Trace: payload["notion_trace"].(map[string]any)}, nil
}

func (s *OpenAIService) selectAccountForStickyKeys(ctx context.Context, preferredEmail string, threadID string, sessionKeys []stickySessionKey) (accountSelection, error) {
	threadID = strings.TrimSpace(threadID)
	preferredEmail = strings.TrimSpace(preferredEmail)
	if threadID != "" {
		account, stickyHit, err := s.selectAccountForThread(ctx, preferredEmail, threadID)
		return accountSelection{Account: account, StickyHit: stickyHit, ThreadID: threadID, Source: stickySource(stickyHit, "thread_id")}, err
	}
	if s.sticky != nil {
		for _, key := range sessionKeys {
			if key.Key == "" {
				continue
			}
			selection, ok, err := s.selectAccountForBindingKey(ctx, preferredEmail, key)
			if err != nil {
				return accountSelection{}, err
			}
			if ok {
				return selection, nil
			}
		}
	}
	account, err := s.accounts.Select(ctx, preferredEmail)
	return accountSelection{Account: account}, err
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

func (s *OpenAIService) selectAccountForBindingKey(ctx context.Context, preferredEmail string, key stickySessionKey) (accountSelection, bool, error) {
	binding, ok, err := s.sticky.Get(ctx, key.Key)
	if err != nil {
		slog.Warn("sticky session lookup failed", "sticky_key", key.Source, "error", err)
		return accountSelection{}, false, nil
	}
	if !ok || binding.AccountID == 0 {
		return accountSelection{}, false, nil
	}
	account, err := s.accounts.Get(ctx, binding.AccountID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			_ = s.sticky.Delete(ctx, key.Key)
			return accountSelection{}, false, nil
		}
		return accountSelection{}, false, err
	}
	if !account.Enabled {
		_ = s.sticky.Delete(ctx, key.Key)
		return accountSelection{}, false, nil
	}
	if preferredEmail != "" && !strings.EqualFold(account.Email, preferredEmail) {
		return accountSelection{}, false, fmt.Errorf("%w: session %s is bound to account %s, not %s", ErrConflict, key.Source, account.Email, preferredEmail)
	}
	return accountSelection{
		Account:   account,
		StickyHit: true,
		ThreadID:  strings.TrimSpace(binding.ThreadID),
		Source:    key.Source,
	}, true, nil
}

func (s *OpenAIService) bindStickyThread(ctx context.Context, account model.Account, result NotionRunResult, sessionKeys []stickySessionKey, responseID string, apiKeyID uint) {
	if s.sticky == nil || account.ID == 0 || result.ConvertedOnly {
		return
	}
	threadID := strings.TrimSpace(result.ThreadID)
	if threadID == "" {
		return
	}
	err := s.sticky.Set(ctx, StickyThreadBinding{
		StickyKey:    threadID,
		ThreadID:     threadID,
		AccountID:    account.ID,
		AccountEmail: account.Email,
		SpaceID:      account.SpaceID,
		SpaceViewID:  account.SpaceViewID,
	}, stickyTTL(s.cfg.Sticky))
	if err != nil {
		slog.Warn("sticky session bind failed", "thread_id", threadID, "account_id", account.ID, "error", err)
	}
	binding := StickyThreadBinding{
		ThreadID:     threadID,
		AccountID:    account.ID,
		AccountEmail: account.Email,
		SpaceID:      account.SpaceID,
		SpaceViewID:  account.SpaceViewID,
	}
	for _, key := range appendUniqueStickyKeys(sessionKeys, []stickySessionKey{{Key: responseStickyKey(apiKeyID, responseID), Source: "response_id"}}) {
		if key.Key == "" {
			continue
		}
		binding.StickyKey = key.Key
		if err := s.sticky.Set(ctx, binding, stickyTTL(s.cfg.Sticky)); err != nil {
			slog.Warn("sticky session bind failed", "sticky_source", key.Source, "account_id", account.ID, "error", err)
		}
	}
}

func (s *OpenAIService) clearStickyThreadBindings(ctx context.Context, threadID string, sessionKeys []stickySessionKey) {
	if s.sticky == nil {
		return
	}
	keys := []stickySessionKey{}
	if cleanThreadID := strings.TrimSpace(threadID); cleanThreadID != "" {
		keys = append(keys, stickySessionKey{Key: cleanThreadID, Source: "thread_id"})
	}
	keys = appendUniqueStickyKeys(keys, sessionKeys)
	for _, key := range keys {
		if key.Key == "" {
			continue
		}
		if err := s.sticky.Delete(ctx, key.Key); err != nil {
			slog.Warn("sticky session clear failed", "sticky_source", key.Source, "error", err)
			continue
		}
		slog.Info("sticky session cleared after notion trust-rule denial", "sticky_source", key.Source)
	}
}

func stickySource(hit bool, source string) string {
	if !hit {
		return ""
	}
	return source
}

func mergeOpenAIRequestOptions(opts ...OpenAIRequestOptions) OpenAIRequestOptions {
	if len(opts) == 0 {
		return OpenAIRequestOptions{}
	}
	return opts[0]
}

func logCodexToolCallConversion(opts OpenAIRequestOptions, result CompletionResult) {
	toolCall := mapFromAny(result.Trace["tool_call"])
	if toolCall == nil {
		return
	}
	slog.Info("responses converted to codex tool call",
		"request_id", opts.RequestID,
		"model", result.Model,
		"response_id", stringFromAny(result.Payload["id"]),
		"tool_name", stringFromAny(toolCall["name"]),
		"reason", stringFromAny(toolCall["reason"]),
		"arguments", toolCall["arguments"],
		"input_preview", stringFromAny(toolCall["input_preview"]),
		"tool_calls", result.Trace["tool_calls"],
	)
}

func chatStickySessionKeys(req dto.ChatCompletionRequest, opts OpenAIRequestOptions) []stickySessionKey {
	return appendUniqueStickyKeys([]stickySessionKey{
		clientStickyKey(opts.APIKeyID, "header_session_id", opts.HeaderSessionID),
		clientStickyKey(opts.APIKeyID, "header_conversation_id", opts.HeaderConversationID),
		clientStickyKey(opts.APIKeyID, "prompt_cache_key", req.PromptCacheKey),
		clientStickyKey(opts.APIKeyID, "metadata_session_id", stringFromMetadata(req.Metadata, "session_id", "notion_session_id")),
		clientStickyKey(opts.APIKeyID, "conversation_id", firstNonEmpty(req.ConversationID, req.Conversation)),
		clientStickyKey(opts.APIKeyID, "metadata_conversation_id", stringFromMetadata(req.Metadata, "conversation_id", "notion_conversation_id")),
		clientStickyKey(opts.APIKeyID, "user", req.User),
	})
}

func responseStickySessionKeys(body map[string]any, opts OpenAIRequestOptions) []stickySessionKey {
	metadata := body["metadata"]
	return appendUniqueStickyKeys([]stickySessionKey{
		{Key: responseStickyKey(opts.APIKeyID, stringFromAny(body["previous_response_id"])), Source: "previous_response_id"},
		clientStickyKey(opts.APIKeyID, "header_session_id", opts.HeaderSessionID),
		clientStickyKey(opts.APIKeyID, "header_conversation_id", opts.HeaderConversationID),
		clientStickyKey(opts.APIKeyID, "prompt_cache_key", stringFromAny(body["prompt_cache_key"])),
		clientStickyKey(opts.APIKeyID, "metadata_session_id", stringFromMetadata(metadata, "session_id", "notion_session_id")),
		clientStickyKey(opts.APIKeyID, "conversation_id", firstNonEmpty(stringFromAny(body["conversation_id"]), stringFromAny(body["conversation"]))),
		clientStickyKey(opts.APIKeyID, "metadata_conversation_id", stringFromMetadata(metadata, "conversation_id", "notion_conversation_id")),
		clientStickyKey(opts.APIKeyID, "user", stringFromAny(body["user"])),
	})
}

func clientStickyKey(apiKeyID uint, source string, value string) stickySessionKey {
	value = strings.TrimSpace(value)
	source = strings.TrimSpace(source)
	if value == "" || source == "" {
		return stickySessionKey{}
	}
	return stickySessionKey{
		Key:    fmt.Sprintf("client:k%d:%s:%s", apiKeyID, source, value),
		Source: source,
	}
}

func responseStickyKey(apiKeyID uint, responseID string) string {
	responseID = strings.TrimSpace(responseID)
	if responseID == "" {
		return ""
	}
	return fmt.Sprintf("response:k%d:%s", apiKeyID, responseID)
}

func appendUniqueStickyKeys(groups ...[]stickySessionKey) []stickySessionKey {
	seen := map[string]struct{}{}
	keys := []stickySessionKey{}
	for _, group := range groups {
		for _, key := range group {
			if key.Key == "" {
				continue
			}
			if _, ok := seen[key.Key]; ok {
				continue
			}
			seen[key.Key] = struct{}{}
			keys = append(keys, key)
		}
	}
	return keys
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
			if strings.HasPrefix(strings.TrimSpace(ref), "data:") {
				return formatFileReferenceForPrompt(name, "inline image data")
			}
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
	prompt = normalizeRolePrefixesInPrompt(prompt)
	prompt = applyResponsesInputTruncation(prompt, body)
	hiddenPrompt = appendResponsesRequestControlInstructions(hiddenPrompt, body)
	hiddenPrompt, hiddenMessages = appendResponseFormatInstruction(hiddenPrompt, hiddenMessages, body["response_format"])
	return prompt, hiddenPrompt, hiddenMessages
}

func promptFromResponsesInput(input any) string {
	prompt, _, _ := promptAndHiddenFromResponsesInput(input)
	return prompt
}

func responsesNotionVisiblePrompt(prompt string, hiddenMessageGroups ...[]hiddenPromptMessage) string {
	prompt = strings.TrimSpace(prompt)
	if len(hiddenMessageGroups) > 0 {
		prompt = joinPromptParts(responsesVisibleInstructionSections(hiddenMessageGroups[0]), prompt)
	}
	if prompt == "" {
		return ""
	}
	prompt = normalizeRolePrefixesInPrompt(prompt)
	prompt = responsesEnvContextPattern.ReplaceAllString(prompt, "")
	prompt = responsesToolCallPattern.ReplaceAllString(prompt, "")
	prompt = responsesToolResultOpenTag.ReplaceAllString(prompt, "<context_result>")
	prompt = strings.ReplaceAll(prompt, "</tool_result>", "</context_result>")
	prompt = strings.ReplaceAll(prompt, "<tool_result>", "<context_result>")
	return strings.TrimSpace(prompt)
}

func responsesVisibleInstructionSections(messages []hiddenPromptMessage) string {
	parts := []string{}
	seen := map[string]struct{}{}
	for _, message := range messages {
		role := strings.ToLower(strings.TrimSpace(message.Role))
		if role != "system" && role != "developer" {
			continue
		}
		content := strings.TrimSpace(message.Content)
		if content == "" {
			continue
		}
		if responsesHiddenMessageUnsafeForVisible(content) {
			content = responsesSanitizedVisibleInstructionContent(role, content)
			if content == "" {
				continue
			}
		}
		const maxForwardedRunes = 4000
		if utf8.RuneCountInString(content) > maxForwardedRunes {
			content = truncateRunes(content, maxForwardedRunes) + "\n[truncated]"
		}
		key := role + "\n" + content
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		parts = append(parts, formatPromptSection(role, content))
	}
	return joinPromptParts(parts...)
}

func responsesSanitizedVisibleInstructionContent(role string, text string) string {
	role = strings.ToLower(strings.TrimSpace(role))
	if role == "" {
		role = "system"
	}
	lower := strings.ToLower(text)
	if strings.Contains(lower, "you are codex") || strings.Contains(lower, "apply_patch") || strings.Contains(lower, "exec_command") || strings.Contains(lower, "custom_tool_call") {
		return "The upstream client provided " + role + " instructions. Follow the user-facing task and requested output format. For file edits, provide a concrete patch/diff instead of saying local file writing is unavailable."
	}
	return ""
}

func responsesHiddenMessageUnsafeForVisible(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	lower := strings.ToLower(text)
	if utf8.RuneCountInString(text) > 8000 {
		return true
	}
	for _, marker := range []string{
		"you are codex",
		"filesystem sandbox",
		"sandbox_permissions",
		"workspace_roots",
		"tools are grouped by namespace",
		"namespace functions",
		"custom_tool_call",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	hits := 0
	for _, marker := range []string{
		"apply_patch",
		"exec_command",
		"function_call",
		"tool_call",
		"mcp",
		"node_repl",
		"developer message",
		"system/developer",
		"local filesystem",
		"run shell",
	} {
		if strings.Contains(lower, marker) {
			hits++
		}
	}
	return hits >= 2
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
	if role == "tool" || itemType == "function_call_output" || itemType == "custom_tool_call_output" || itemType == "tool_result" {
		return formatResponsesToolOutputForPrompt(item), "", nil
	}
	if itemType == "function_call" || itemType == "custom_tool_call" {
		return formatResponsesToolCallForPrompt(item), "", nil
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
			return formatRolePromptLine("user", text), "", nil
		default:
			return formatRolePromptLine(role, text), "", nil
		}
	}
	return contentToText(item), "", nil
}

func formatRolePromptLine(role string, text string) string {
	role = strings.ToLower(strings.TrimSpace(role))
	text = strings.TrimSpace(text)
	if role == "" || text == "" {
		return ""
	}
	for {
		rest, ok := stripRolePrefix(text, role)
		if !ok {
			break
		}
		rest = strings.TrimSpace(rest)
		if rest == "" {
			return ""
		}
		text = rest
	}
	return role + ": " + text
}

func normalizeRolePrefixesInPrompt(prompt string) string {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return ""
	}
	lines := strings.Split(strings.ReplaceAll(prompt, "\r\n", "\n"), "\n")
	for idx, line := range lines {
		trimmed := strings.TrimSpace(line)
		for _, role := range []string{"user", "assistant", "system", "developer"} {
			if _, ok := stripRolePrefix(trimmed, role); ok {
				lines[idx] = formatRolePromptLine(role, trimmed)
				break
			}
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func stripRolePrefix(text string, role string) (string, bool) {
	text = strings.TrimSpace(text)
	role = strings.ToLower(strings.TrimSpace(role))
	lower := strings.ToLower(text)
	for _, prefix := range []string{role + ":", role + "："} {
		if strings.HasPrefix(lower, prefix) {
			return strings.TrimSpace(text[len(prefix):]), true
		}
	}
	return "", false
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
	return buildResponsesFunctionCallPayloads(modelID, []responseFunctionToolCall{{
		Name:      toolName,
		Arguments: arguments,
		CallID:    callID,
		ItemID:    outputItemID,
	}}, usage, responseID, createdAt, request, trace)
}

func buildResponsesFunctionCallPayloads(modelID string, calls []responseFunctionToolCall, usage dto.UsageDTO, responseID string, createdAt int64, request map[string]any, trace map[string]any) map[string]any {
	if strings.TrimSpace(responseID) == "" {
		responseID = newResponsesID("resp")
	}
	output := make([]any, 0, len(calls))
	for _, call := range calls {
		if strings.TrimSpace(call.ItemID) == "" {
			call.ItemID = newResponsesID("fc")
		}
		if strings.TrimSpace(call.CallID) == "" {
			call.CallID = newResponsesID("call")
		}
		output = append(output, buildResponsesFunctionCallItem(call.ItemID, call.Name, call.Arguments, call.CallID, "completed"))
	}
	if len(output) == 0 {
		itemID := newResponsesID("fc")
		callID := newResponsesID("call")
		output = append(output, buildResponsesFunctionCallItem(itemID, "", "{}", callID, "completed"))
	}
	payload := buildResponsesBaseObject(responseID, modelID, createdAt, "completed", request)
	payload["completed_at"] = createdAt
	payload["output"] = output
	payload["output_text"] = ""
	payload["usage"] = buildResponsesUsage(usage)
	if trace != nil {
		payload["notion_trace"] = trace
	}
	return payload
}

func buildResponsesCustomToolCallPayload(modelID string, toolName string, input string, callID string, usage dto.UsageDTO, responseID string, outputItemID string, createdAt int64, request map[string]any, trace map[string]any) map[string]any {
	if strings.TrimSpace(responseID) == "" {
		responseID = newResponsesID("resp")
	}
	if strings.TrimSpace(outputItemID) == "" {
		outputItemID = newResponsesID("ctc")
	}
	if strings.TrimSpace(callID) == "" {
		callID = newResponsesID("call")
	}
	payload := buildResponsesBaseObject(responseID, modelID, createdAt, "completed", request)
	payload["completed_at"] = createdAt
	payload["output"] = []any{buildResponsesCustomToolCallItem(outputItemID, toolName, input, callID, "completed")}
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

func buildResponsesCustomToolCallItem(itemID string, toolName string, input string, callID string, status string) map[string]any {
	if strings.TrimSpace(status) == "" {
		status = "completed"
	}
	return map[string]any{
		"id":      itemID,
		"type":    "custom_tool_call",
		"status":  status,
		"call_id": callID,
		"name":    toolName,
		"input":   input,
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
	instructions := strings.TrimSpace(firstNonEmpty(stringFromAny(body["instructions"]), stringFromAny(body["istructions"])))
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
		if role == "tool" || itemType == "function_call_output" || itemType == "custom_tool_call_output" || itemType == "tool_result" {
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
	files := codexFileRefsFromResponsesBody(body)
	if len(files) == 0 {
		return CompletionResult{}, false
	}
	return buildCodexFileToolCallResult(modelID, body, files, "codex_file_reference", nil)
}

func maybeBuildCodexAssistantTextToolCallResult(modelID string, body map[string]any, answer string, baseTrace map[string]any) (CompletionResult, bool) {
	if body == nil || isCodexTitleGenerationRequest(body) {
		return CompletionResult{}, false
	}
	if result, ok := maybeBuildCodexApplyPatchToolCallResult(modelID, body, answer, baseTrace); ok {
		return result, true
	}
	if responsesInputHasToolOutput(body["input"]) {
		return CompletionResult{}, false
	}
	if !codexAssistantTextLooksLikeFileToolCall(answer) {
		return CompletionResult{}, false
	}
	files := codexFileRefsFromAssistantToolText(answer)
	if len(files) == 0 {
		files = codexFileRefsFromResponsesBody(body)
	}
	if len(files) == 0 {
		return CompletionResult{}, false
	}
	return buildCodexFileToolCallResult(modelID, body, files, "assistant_text_tool_call", baseTrace)
}

func maybeBuildCodexDeterministicEditToolCallResult(modelID string, body map[string]any) (CompletionResult, bool) {
	if body == nil || !responsesInputHasToolOutput(body["input"]) || responsesInputHasBlockingApplyPatchToolState(body["input"]) || isCodexTitleGenerationRequest(body) {
		return CompletionResult{}, false
	}
	if !responseHasTool(body["tools"], "apply_patch") || !responseToolChoiceAllowsTool(body["tool_choice"], "apply_patch") {
		return CompletionResult{}, false
	}
	prompt, hiddenPrompt, hiddenMessages := responsesPromptPartsFromBody(body)
	if !codexPromptHasEditIntent(prompt) {
		return CompletionResult{}, false
	}
	patch := codexDeterministicApplyPatchFromPrompt(prompt)
	if patch == "" {
		return CompletionResult{}, false
	}
	const maxPatchRunes = 200000
	if utf8.RuneCountInString(patch) > maxPatchRunes {
		return CompletionResult{}, false
	}
	usagePrompt := joinHiddenPromptParts(prompt, hiddenPrompt)
	if usagePrompt == "" {
		usagePrompt = legacyPromptFromResponsesInput(body["input"])
	}
	promptTokens := security.EstimateTokens(usagePrompt)
	completionTokens := security.EstimateTokens(patch)
	usage := dto.UsageDTO{
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      promptTokens + completionTokens,
		Estimated:        true,
	}
	trace := map[string]any{
		"converted_only": true,
		"tool_call": map[string]any{
			"name":          "apply_patch",
			"reason":        "deterministic_edit",
			"input_chars":   utf8.RuneCountInString(patch),
			"input_preview": truncateRunes(collapseWhitespace(patch), 180),
		},
	}
	responseID := newResponsesID("resp")
	callID := newResponsesID("call")
	storePendingResponseContext(pendingResponseContext{
		ResponseID:     responseID,
		CallID:         callID,
		Prompt:         prompt,
		HiddenPrompt:   hiddenPrompt,
		HiddenMessages: hiddenMessages,
	})
	payload := buildResponsesCustomToolCallPayload(modelID, "apply_patch", patch, callID, usage, responseID, newResponsesID("ctc"), time.Now().Unix(), body, trace)
	return CompletionResult{Payload: payload, Usage: usage, Model: modelID, Trace: trace}, true
}

func maybeBuildCodexApplyPatchCompletionResult(modelID string, body map[string]any) (CompletionResult, bool) {
	if body == nil || !responsesInputHasCustomToolCallOutput(body["input"]) || isCodexTitleGenerationRequest(body) {
		return CompletionResult{}, false
	}
	if responsesInputHasUserMessageAfterCustomToolCallOutput(body["input"]) {
		return CompletionResult{}, false
	}
	if !responsesInputHasApplyPatchToolCall(body["input"]) && !responseHasTool(body["tools"], "apply_patch") {
		return CompletionResult{}, false
	}
	output := responsesFirstCustomToolCallOutputText(body["input"])
	answer := codexApplyPatchCompletionAnswer(output)
	prompt, hiddenPrompt, _ := responsesPromptPartsFromBody(body)
	promptForUsage := joinHiddenPromptParts(prompt, hiddenPrompt)
	if promptForUsage == "" {
		promptForUsage = legacyPromptFromResponsesInput(body["input"])
	}
	promptTokens := security.EstimateTokens(promptForUsage)
	completionTokens := security.EstimateTokens(answer)
	usage := dto.UsageDTO{
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      promptTokens + completionTokens,
		Estimated:        true,
	}
	trace := map[string]any{
		"converted_only": true,
		"tool_result": map[string]any{
			"name":           "apply_patch",
			"reason":         "apply_patch_output_completion",
			"output_chars":   utf8.RuneCountInString(output),
			"output_preview": truncateRunes(collapseWhitespace(output), 180),
		},
	}
	payload := buildResponsesPayload(modelID, answer, usage, newResponsesID("resp"), newResponsesID("msg"), time.Now().Unix(), body, trace)
	return CompletionResult{Payload: payload, Usage: usage, Model: modelID, Text: answer, Trace: trace}, true
}

func maybeBuildCodexApplyPatchToolCallResult(modelID string, body map[string]any, answer string, baseTrace map[string]any) (CompletionResult, bool) {
	if body == nil || !responseHasTool(body["tools"], "apply_patch") || !responseToolChoiceAllowsTool(body["tool_choice"], "apply_patch") || responsesInputHasBlockingApplyPatchToolState(body["input"]) {
		return CompletionResult{}, false
	}
	prompt, hiddenPrompt, hiddenMessages := responsesPromptPartsFromBody(body)
	if !codexPromptHasEditIntent(prompt) {
		return CompletionResult{}, false
	}
	patch := codexApplyPatchFromAssistantText(answer, prompt)
	if patch == "" {
		return CompletionResult{}, false
	}
	const maxPatchRunes = 200000
	if utf8.RuneCountInString(patch) > maxPatchRunes {
		return CompletionResult{}, false
	}
	usagePrompt := joinHiddenPromptParts(prompt, hiddenPrompt)
	if usagePrompt == "" {
		usagePrompt = legacyPromptFromResponsesInput(body["input"])
	}
	promptTokens := security.EstimateTokens(usagePrompt)
	completionTokens := security.EstimateTokens(patch)
	usage := dto.UsageDTO{
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      promptTokens + completionTokens,
		Estimated:        true,
	}
	trace := map[string]any{}
	for key, value := range baseTrace {
		trace[key] = value
	}
	trace["assistant_text_tool_call_converted"] = true
	trace["tool_call"] = map[string]any{
		"name":          "apply_patch",
		"reason":        "assistant_text_apply_patch",
		"input_chars":   utf8.RuneCountInString(patch),
		"input_preview": truncateRunes(collapseWhitespace(patch), 180),
	}
	responseID := newResponsesID("resp")
	callID := newResponsesID("call")
	storePendingResponseContext(pendingResponseContext{
		ResponseID:     responseID,
		CallID:         callID,
		Prompt:         prompt,
		HiddenPrompt:   hiddenPrompt,
		HiddenMessages: hiddenMessages,
	})
	payload := buildResponsesCustomToolCallPayload(modelID, "apply_patch", patch, callID, usage, responseID, newResponsesID("ctc"), time.Now().Unix(), body, trace)
	return CompletionResult{Payload: payload, Usage: usage, Model: modelID, Trace: trace}, true
}

func buildCodexFileToolCallResult(modelID string, body map[string]any, files []string, reason string, baseTrace map[string]any) (CompletionResult, bool) {
	calls, ok := codexFileReadToolCalls(body["tools"], body["tool_choice"], files, boolFromMapDefault(body, "parallel_tool_calls", true))
	if !ok || len(calls) == 0 {
		return CompletionResult{}, false
	}
	prompt, hiddenPrompt, hiddenMessages := responsesPromptPartsFromBody(body)
	promptForUsage := joinHiddenPromptParts(prompt, hiddenPrompt)
	if promptForUsage == "" {
		promptForUsage = legacyPromptFromResponsesInput(body["input"])
	}
	callBytes, _ := json.Marshal(calls)
	promptTokens := security.EstimateTokens(promptForUsage)
	completionTokens := security.EstimateTokens(string(callBytes))
	usage := dto.UsageDTO{
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      promptTokens + completionTokens,
		Estimated:        true,
	}
	trace := map[string]any{}
	for key, value := range baseTrace {
		trace[key] = value
	}
	if reason == "codex_file_reference" {
		trace["converted_only"] = true
	} else {
		trace["assistant_text_tool_call_converted"] = true
	}
	traceCalls := make([]map[string]any, 0, len(calls))
	callIDs := make([]string, 0, len(calls))
	for idx := range calls {
		if strings.TrimSpace(calls[idx].CallID) == "" {
			calls[idx].CallID = newResponsesID("call")
		}
		if strings.TrimSpace(calls[idx].ItemID) == "" {
			calls[idx].ItemID = newResponsesID("fc")
		}
		callIDs = append(callIDs, calls[idx].CallID)
		var args any
		_ = json.Unmarshal([]byte(calls[idx].Arguments), &args)
		traceCalls = append(traceCalls, map[string]any{
			"name":      calls[idx].Name,
			"arguments": args,
			"reason":    reason,
		})
	}
	trace["tool_call"] = map[string]any{
		"name":      calls[0].Name,
		"arguments": traceCalls[0]["arguments"],
		"reason":    reason,
	}
	trace["tool_calls"] = traceCalls
	responseID := newResponsesID("resp")
	storePendingResponseContext(pendingResponseContext{
		ResponseID:     responseID,
		Prompt:         prompt,
		HiddenPrompt:   hiddenPrompt,
		HiddenMessages: hiddenMessages,
	})
	for _, callID := range callIDs {
		storePendingResponseContext(pendingResponseContext{
			CallID:         callID,
			Prompt:         prompt,
			HiddenPrompt:   hiddenPrompt,
			HiddenMessages: hiddenMessages,
		})
	}
	payload := buildResponsesFunctionCallPayloads(modelID, calls, usage, responseID, time.Now().Unix(), body, trace)
	return CompletionResult{Payload: payload, Usage: usage, Model: modelID, Trace: trace}, true
}

func codexFileReadToolCalls(tools any, toolChoice any, files []string, parallel bool) ([]responseFunctionToolCall, bool) {
	if len(files) == 0 {
		return nil, false
	}
	switch {
	case responseHasTool(tools, "connections.fs.readFiles") && responseToolChoiceAllowsTool(toolChoice, "connections.fs.readFiles"):
		arguments, err := json.Marshal(map[string]any{"files": files})
		if err != nil {
			return nil, false
		}
		return []responseFunctionToolCall{{Name: "connections.fs.readFiles", Arguments: string(arguments)}}, true
	case responseHasTool(tools, "connections.fs.readFile") && responseToolChoiceAllowsTool(toolChoice, "connections.fs.readFile"):
		if !parallel && len(files) > 1 {
			files = files[:1]
		}
		calls := make([]responseFunctionToolCall, 0, len(files))
		for _, file := range files {
			arguments, err := json.Marshal(map[string]any{"file": file})
			if err != nil {
				return nil, false
			}
			calls = append(calls, responseFunctionToolCall{Name: "connections.fs.readFile", Arguments: string(arguments)})
		}
		return calls, len(calls) > 0
	case responseHasTool(tools, "exec_command") && responseToolChoiceAllowsTool(toolChoice, "exec_command"):
		if !parallel && len(files) > 1 {
			files = files[:1]
		}
		calls := make([]responseFunctionToolCall, 0, len(files))
		for _, file := range files {
			arguments, err := json.Marshal(map[string]any{
				"cmd":               codexReadFileCommand(file),
				"yield_time_ms":     10000,
				"max_output_tokens": 20000,
			})
			if err != nil {
				return nil, false
			}
			calls = append(calls, responseFunctionToolCall{Name: "exec_command", Arguments: string(arguments)})
		}
		return calls, len(calls) > 0
	default:
		return nil, false
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

func responseToolChoiceAllowsTool(toolChoice any, toolName string) bool {
	toolName = strings.TrimSpace(toolName)
	if toolName == "" {
		return false
	}
	mode, selectedName := responseToolChoiceMode(toolChoice)
	switch mode {
	case "none":
		return false
	case "named":
		return strings.EqualFold(selectedName, toolName)
	default:
		return true
	}
}

func responseToolChoiceMode(toolChoice any) (string, string) {
	switch typed := toolChoice.(type) {
	case nil:
		return "auto", ""
	case string:
		mode := strings.ToLower(strings.TrimSpace(typed))
		switch mode {
		case "", "auto":
			return "auto", ""
		case "none":
			return "none", ""
		case "required":
			return "required", ""
		default:
			return "named", typed
		}
	default:
		choice := mapFromAny(toolChoice)
		if choice == nil {
			return "auto", ""
		}
		name := firstNonEmpty(
			stringFromAny(choice["name"]),
			stringFromAny(choice["tool_name"]),
		)
		if function := mapFromAny(choice["function"]); function != nil {
			name = firstNonEmpty(name, stringFromAny(function["name"]))
		}
		if custom := mapFromAny(choice["custom"]); custom != nil {
			name = firstNonEmpty(name, stringFromAny(custom["name"]))
		}
		if name != "" {
			return "named", name
		}
		mode := strings.ToLower(strings.TrimSpace(stringFromAny(choice["type"])))
		switch mode {
		case "none":
			return "none", ""
		case "required":
			return "required", ""
		default:
			return "auto", ""
		}
	}
}

func codexReadFileCommand(file string) string {
	return "sed -n '1,400p' " + shellQuote(file)
}

func codexFileRefsFromResponsesBody(body map[string]any) []string {
	if body == nil {
		return nil
	}
	return uniqueCodexFileRefs(
		codexFileRefsFromResponsesInput(body["input"]),
		codexFileRefsFromResponsesInput(body["attachments"]),
	)
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func codexFileRefsFromResponsesInput(input any) []string {
	return codexFileRefsFromText(promptFromResponsesInput(input))
}

func codexFileRefsFromText(text string) []string {
	matches := codexMarkdownFileLinkPattern.FindAllStringSubmatch(text, -1)
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
	if len(files) < 8 && codexTextHasFileReadIntent(text) {
		for _, match := range codexPlainFileRefPattern.FindAllStringSubmatch(text, -1) {
			if len(match) < 2 {
				continue
			}
			file := normalizeCodexFileRef(match[1], match[1])
			if file == "" || !codexLooksLikePlainFileRef(file) {
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
	}
	return files
}

func codexAssistantTextLooksLikeFileToolCall(text string) bool {
	lower := strings.ToLower(text)
	return strings.Contains(lower, "connections.fs.readfiles") ||
		strings.Contains(lower, "connections.fs.readfile") ||
		strings.Contains(lower, `"exec_command"`) ||
		strings.Contains(lower, `function":"exec_command`) ||
		strings.Contains(lower, `function": "exec_command`)
}

func codexFileRefsFromAssistantToolText(text string) []string {
	files := []string{}
	for _, match := range codexQuotedFilesPattern.FindAllStringSubmatch(text, -1) {
		if len(match) < 2 {
			continue
		}
		var parsed []string
		if err := json.Unmarshal([]byte("["+match[1]+"]"), &parsed); err == nil {
			for _, file := range parsed {
				files = append(files, file)
			}
			continue
		}
		for _, quoted := range codexJSONStringPattern.FindAllStringSubmatch(match[1], -1) {
			if len(quoted) < 2 {
				continue
			}
			var decoded string
			if err := json.Unmarshal([]byte(`"`+quoted[1]+`"`), &decoded); err == nil {
				files = append(files, decoded)
			}
		}
	}
	for _, match := range codexQuotedFilePattern.FindAllStringSubmatch(text, -1) {
		if len(match) >= 2 {
			files = append(files, match[1])
		}
	}
	return normalizeUniqueCodexFileRefs(files)
}

func codexApplyPatchFromAssistantText(text string, contextParts ...string) string {
	for _, candidate := range codexPatchCandidates(text) {
		if patch := normalizeCodexApplyPatch(candidate); patch != "" {
			return patch
		}
		if patch := codexUnifiedDiffToApplyPatch(candidate); patch != "" {
			return patch
		}
	}
	if patch := codexInstructionalInsertionToApplyPatch(text, strings.Join(contextParts, "\n")); patch != "" {
		return patch
	}
	return ""
}

func codexDeterministicApplyPatchFromPrompt(prompt string) string {
	if patch := codexYAMLServerPortApplyPatch(prompt); patch != "" {
		return patch
	}
	return ""
}

func codexYAMLServerPortApplyPatch(prompt string) string {
	targetText := codexPromptBeforeContext(prompt)
	newPort := codexRequestedPort(targetText)
	if newPort == "" {
		return ""
	}
	file := codexPatchTargetFile(targetText, prompt)
	if file == "" {
		return ""
	}
	contextLines, oldLine, newLine := codexYAMLServerPortLines(prompt, newPort)
	if oldLine == "" || newLine == "" || oldLine == newLine {
		return ""
	}
	var b strings.Builder
	b.WriteString("*** Begin Patch\n")
	b.WriteString("*** Update File: ")
	b.WriteString(file)
	b.WriteString("\n")
	b.WriteString("@@\n")
	for _, line := range contextLines {
		b.WriteString(" ")
		b.WriteString(line)
		b.WriteString("\n")
	}
	b.WriteString("-")
	b.WriteString(oldLine)
	b.WriteString("\n")
	b.WriteString("+")
	b.WriteString(newLine)
	b.WriteString("\n")
	b.WriteString("*** End Patch\n")
	return b.String()
}

func codexPromptBeforeContext(prompt string) string {
	prompt = strings.TrimSpace(prompt)
	prompt = codexToolResultBlockPattern.ReplaceAllString(prompt, "")
	prompt = codexContextResultBlockPattern.ReplaceAllString(prompt, "")
	return strings.TrimSpace(prompt)
}

func codexRequestedPort(text string) string {
	text = strings.TrimSpace(text)
	bestStart := -1
	bestPort := ""
	for _, pattern := range []string{
		`(?i)(?:port|prot|端口)[^\d]{0,40}(\d{2,5})`,
		`(?i)(?:改为|改成|改回|设置为|设为|to)\s*(\d{2,5})`,
	} {
		re := regexp.MustCompile(pattern)
		for _, match := range re.FindAllStringSubmatchIndex(text, -1) {
			if len(match) < 4 || match[2] < 0 || match[3] < 0 {
				continue
			}
			if match[0] >= bestStart {
				bestStart = match[0]
				bestPort = text[match[2]:match[3]]
			}
		}
	}
	return bestPort
}

func codexYAMLServerPortLines(text string, newPort string) ([]string, string, string) {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	inServer := false
	serverIndent := -1
	context := []string{}
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if inServer {
				context = append(context, line)
			}
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if trimmed == "server:" {
			inServer = true
			serverIndent = indent
			context = []string{line}
			continue
		}
		if inServer && indent <= serverIndent {
			inServer = false
			context = nil
		}
		if !inServer {
			continue
		}
		if strings.HasPrefix(trimmed, "port:") {
			prefixLen := strings.Index(line, "port:")
			if prefixLen < 0 {
				return nil, "", ""
			}
			prefix := line[:prefixLen] + "port:"
			oldValue := strings.TrimSpace(strings.TrimPrefix(line[prefixLen:], "port:"))
			if oldValue == "" || oldValue == newPort {
				return nil, "", ""
			}
			return context, line, prefix + " " + newPort
		}
		context = append(context, line)
	}
	return nil, "", ""
}

func codexPatchCandidates(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	candidates := []string{text}
	for _, match := range codexFencedCodePattern.FindAllStringSubmatch(text, -1) {
		if len(match) >= 2 {
			candidates = append(candidates, strings.TrimSpace(match[1]))
		}
	}
	return candidates
}

type codexFencedCodeBlock struct {
	Lang string
	Body string
}

func codexFencedCodeBlocks(text string) []codexFencedCodeBlock {
	blocks := []codexFencedCodeBlock{}
	for _, match := range codexFencedCodeBlockPattern.FindAllStringSubmatch(text, -1) {
		if len(match) >= 3 {
			blocks = append(blocks, codexFencedCodeBlock{
				Lang: strings.ToLower(strings.TrimSpace(match[1])),
				Body: strings.TrimSpace(match[2]),
			})
		}
	}
	return blocks
}

func normalizeCodexApplyPatch(text string) string {
	start := strings.Index(text, "*** Begin Patch")
	if start < 0 {
		return ""
	}
	end := strings.Index(text[start:], "*** End Patch")
	if end < 0 {
		return ""
	}
	patch := text[start : start+end+len("*** End Patch")]
	return strings.TrimSpace(patch) + "\n"
}

func codexUnifiedDiffToApplyPatch(text string) string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	type filePatch struct {
		path  string
		lines []string
	}
	patches := []filePatch{}
	var current *filePatch
	inHunk := false
	flush := func() {
		if current != nil && current.path != "" && len(current.lines) > 0 {
			patches = append(patches, *current)
		}
		current = nil
		inHunk = false
	}
	for _, line := range lines {
		if strings.HasPrefix(line, "diff --git ") {
			flush()
			parts := strings.Fields(line)
			if len(parts) >= 4 {
				current = &filePatch{path: trimUnifiedDiffPath(parts[3])}
			}
			continue
		}
		if current == nil {
			continue
		}
		if strings.HasPrefix(line, "+++ ") {
			path := strings.TrimSpace(strings.TrimPrefix(line, "+++ "))
			if path == "/dev/null" {
				current = nil
				inHunk = false
				continue
			}
			current.path = trimUnifiedDiffPath(path)
			continue
		}
		if strings.HasPrefix(line, "--- ") || strings.HasPrefix(line, "index ") || strings.HasPrefix(line, "new file mode ") || strings.HasPrefix(line, "deleted file mode ") {
			continue
		}
		if strings.HasPrefix(line, "@@") {
			inHunk = true
			current.lines = append(current.lines, line)
			continue
		}
		if inHunk {
			if line == `\ No newline at end of file` {
				continue
			}
			if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "+") || strings.HasPrefix(line, "-") || line == "" {
				current.lines = append(current.lines, line)
			}
		}
	}
	flush()
	if len(patches) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("*** Begin Patch\n")
	for _, patch := range patches {
		if normalizeCodexFileRef(patch.path, patch.path) == "" {
			return ""
		}
		b.WriteString("*** Update File: ")
		b.WriteString(patch.path)
		b.WriteString("\n")
		for _, line := range patch.lines {
			b.WriteString(line)
			b.WriteString("\n")
		}
	}
	b.WriteString("*** End Patch\n")
	return b.String()
}

func trimUnifiedDiffPath(path string) string {
	path = strings.TrimSpace(path)
	path = strings.Trim(path, "\"")
	path = strings.TrimPrefix(path, "a/")
	path = strings.TrimPrefix(path, "b/")
	return path
}

func codexInstructionalInsertionToApplyPatch(text string, context string) string {
	file := codexPatchTargetFile(text, context)
	if file == "" {
		return ""
	}
	heading := codexInsertionHeading(text)
	if heading == "" {
		heading = codexInsertionHeading(context)
	}
	if heading == "" {
		return ""
	}
	for _, block := range codexFencedCodeBlocks(text) {
		if patch := codexPseudoDiffInsertionToApplyPatch(block.Body, file); patch != "" {
			return patch
		}
	}
	for _, block := range codexFencedCodeBlocks(text) {
		if !codexFencedBlockLooksLikeMarkdownInsertion(block) {
			continue
		}
		insert := codexMarkdownInsertionFromBlock(block.Body, heading)
		if insert == "" {
			continue
		}
		return codexBuildHeadingInsertionPatch(file, heading, insert)
	}
	return ""
}

func codexPatchTargetFile(text string, context string) string {
	files := uniqueCodexFileRefs(
		codexFileRefsFromText(text),
		codexFileRefsFromText(context),
		codexFileRefsFromBacktickMentions(text),
		codexFileRefsFromBacktickMentions(context),
	)
	if len(files) == 0 {
		return ""
	}
	return files[0]
}

func codexFileRefsFromBacktickMentions(text string) []string {
	files := []string{}
	for _, match := range codexBacktickContentPattern.FindAllStringSubmatch(text, -1) {
		if len(match) < 2 {
			continue
		}
		raw := strings.TrimSpace(match[1])
		if raw == "" || strings.Contains(raw, "\n") {
			continue
		}
		if file := normalizeCodexFileRef(raw, raw); file != "" {
			files = append(files, file)
			continue
		}
		if strings.HasPrefix(raw, "/") {
			clean := strings.TrimRight(raw, "/")
			if idx := strings.LastIndex(clean, "/"); idx >= 0 {
				clean = clean[idx+1:]
			}
			if file := normalizeCodexFileRef(clean, clean); file != "" {
				files = append(files, file)
			}
		}
	}
	return normalizeUniqueCodexFileRefs(files)
}

func codexInsertionHeading(text string) string {
	for _, block := range codexFencedCodeBlocks(text) {
		if heading := firstMarkdownHeadingLine(block.Body); heading != "" {
			return heading
		}
	}
	if match := regexp.MustCompile("`(#{1,6}\\s+[^`\\n]{1,160})`").FindStringSubmatch(text); len(match) >= 2 {
		return strings.TrimSpace(match[1])
	}
	return firstMarkdownHeadingLine(text)
}

func firstMarkdownHeadingLine(text string) string {
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(line, "+"))
		if strings.HasPrefix(line, "#") {
			fields := strings.Fields(line)
			if len(fields) >= 2 && strings.HasPrefix(fields[0], "#") && strings.Trim(fields[0], "#") == "" {
				return strings.TrimSpace(line)
			}
		}
	}
	return ""
}

func codexPseudoDiffInsertionToApplyPatch(text string, file string) string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	heading := ""
	inserted := []string{}
	seenInsertion := false
	for _, raw := range lines {
		line := strings.TrimRight(raw, "\r")
		if heading == "" {
			clean := strings.TrimSpace(line)
			if strings.HasPrefix(clean, "#") {
				heading = strings.TrimSpace(clean)
			}
			continue
		}
		if strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---") {
			return ""
		}
		if strings.HasPrefix(line, "+") {
			inserted = append(inserted, strings.TrimPrefix(line, "+"))
			seenInsertion = true
			continue
		}
		if seenInsertion && strings.TrimSpace(line) != "" {
			break
		}
	}
	if heading == "" || len(inserted) == 0 || !codexInsertedLinesLookUseful(inserted) {
		return ""
	}
	return codexBuildHeadingInsertionPatch(file, heading, strings.TrimSpace(strings.Join(inserted, "\n")))
}

func codexFencedBlockLooksLikeMarkdownInsertion(block codexFencedCodeBlock) bool {
	switch block.Lang {
	case "", "md", "markdown", "text", "diff":
	default:
		return false
	}
	body := strings.TrimSpace(block.Body)
	if body == "" || strings.Contains(body, "*** Begin Patch") || strings.Contains(body, "diff --git ") {
		return false
	}
	if strings.HasPrefix(body, "curl ") || strings.HasPrefix(body, "curl\t") {
		return false
	}
	return strings.Contains(body, "Responses") || strings.Contains(body, "LiteLLM") || strings.Contains(body, "注意")
}

func codexMarkdownInsertionFromBlock(text string, heading string) string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	out := []string{}
	headingSeen := false
	for _, raw := range lines {
		line := strings.TrimRight(raw, "\r")
		clean := strings.TrimSpace(strings.TrimPrefix(line, "+"))
		if !headingSeen {
			if clean == heading {
				headingSeen = true
			}
			continue
		}
		if strings.HasPrefix(line, "+") {
			out = append(out, strings.TrimPrefix(line, "+"))
			continue
		}
		out = append(out, line)
	}
	if headingSeen {
		return strings.TrimSpace(strings.Join(out, "\n"))
	}
	return strings.TrimSpace(text)
}

func codexInsertedLinesLookUseful(lines []string) bool {
	return strings.TrimSpace(strings.Join(lines, "\n")) != ""
}

func codexBuildHeadingInsertionPatch(file string, heading string, insert string) string {
	file = normalizeCodexFileRef(file, file)
	heading = strings.TrimSpace(heading)
	insert = strings.TrimSpace(insert)
	if file == "" || heading == "" || insert == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("*** Begin Patch\n")
	b.WriteString("*** Update File: ")
	b.WriteString(file)
	b.WriteString("\n")
	b.WriteString("@@\n")
	b.WriteString(" ")
	b.WriteString(heading)
	b.WriteString("\n")
	if !strings.HasPrefix(insert, "\n") {
		b.WriteString("+\n")
	}
	for _, line := range strings.Split(insert, "\n") {
		b.WriteString("+")
		b.WriteString(line)
		b.WriteString("\n")
	}
	b.WriteString("*** End Patch\n")
	return b.String()
}

func codexPromptHasEditIntent(prompt string) bool {
	lower := strings.ToLower(prompt)
	for _, marker := range []string{
		"修改", "改", "更新", "新增", "加上", "加到", "修复", "写入", "应用", "替换", "删除",
		"edit", "modify", "update", "add", "fix", "write", "apply", "replace", "delete",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func codexTextHasFileReadIntent(text string) bool {
	lower := strings.ToLower(text)
	for _, marker := range []string{
		"查看", "读取", "打开", "内容", "文件", "部署",
		"read", "view", "open", "show", "inspect", "look at", "cat ",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func codexLooksLikePlainFileRef(file string) bool {
	base := file
	if idx := strings.LastIndex(base, "/"); idx >= 0 {
		base = base[idx+1:]
	}
	switch strings.ToLower(base) {
	case "dockerfile", "makefile", "compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml":
		return true
	}
	if strings.HasPrefix(base, ".") {
		return false
	}
	return strings.Contains(base, ".")
}

func uniqueCodexFileRefs(groups ...[]string) []string {
	files := []string{}
	for _, group := range groups {
		files = append(files, group...)
	}
	return normalizeUniqueCodexFileRefs(files)
}

func normalizeUniqueCodexFileRefs(rawFiles []string) []string {
	seen := map[string]struct{}{}
	files := make([]string, 0, len(rawFiles))
	for _, raw := range rawFiles {
		file := normalizeCodexFileRef(raw, raw)
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
		if role == "tool" || itemType == "function_call_output" || itemType == "custom_tool_call_output" || itemType == "tool_result" {
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

func responsesInputHasApplyPatchToolOutput(input any) bool {
	switch typed := input.(type) {
	case []any:
		for _, item := range typed {
			if responsesInputHasApplyPatchToolOutput(item) {
				return true
			}
		}
		return false
	case []map[string]any:
		for _, item := range typed {
			if responsesInputHasApplyPatchToolOutput(item) {
				return true
			}
		}
		return false
	case map[string]any:
		itemType := strings.ToLower(strings.TrimSpace(stringFromAny(typed["type"])))
		if itemType == "custom_tool_call" && strings.EqualFold(stringFromAny(typed["name"]), "apply_patch") {
			return true
		}
		if itemType == "custom_tool_call_output" {
			return true
		}
		return responsesInputHasApplyPatchToolOutput(typed["content"])
	default:
		return false
	}
}

func responsesInputHasBlockingApplyPatchToolState(input any) bool {
	return responsesInputHasApplyPatchToolOutput(input) && !responsesInputHasUserMessageAfterCustomToolCallOutput(input)
}

func responsesInputHasApplyPatchToolCall(input any) bool {
	switch typed := input.(type) {
	case []any:
		for _, item := range typed {
			if responsesInputHasApplyPatchToolCall(item) {
				return true
			}
		}
		return false
	case []map[string]any:
		for _, item := range typed {
			if responsesInputHasApplyPatchToolCall(item) {
				return true
			}
		}
		return false
	case map[string]any:
		itemType := strings.ToLower(strings.TrimSpace(stringFromAny(typed["type"])))
		if itemType == "custom_tool_call" && strings.EqualFold(stringFromAny(typed["name"]), "apply_patch") {
			return true
		}
		return responsesInputHasApplyPatchToolCall(typed["content"])
	default:
		return false
	}
}

func responsesInputHasUserMessageAfterCustomToolCallOutput(input any) bool {
	seenOutput := false
	foundUser := false
	var walk func(any)
	walk = func(value any) {
		if foundUser {
			return
		}
		switch typed := value.(type) {
		case []any:
			for _, item := range typed {
				walk(item)
				if foundUser {
					return
				}
			}
		case []map[string]any:
			for _, item := range typed {
				walk(item)
				if foundUser {
					return
				}
			}
		case map[string]any:
			itemType := strings.ToLower(strings.TrimSpace(stringFromAny(typed["type"])))
			role := strings.ToLower(strings.TrimSpace(stringFromAny(typed["role"])))
			if itemType == "custom_tool_call_output" {
				seenOutput = true
			}
			if seenOutput && role == "user" && strings.TrimSpace(contentToText(typed["content"])) != "" {
				foundUser = true
				return
			}
			if content, ok := typed["content"]; ok {
				walk(content)
			}
		}
	}
	walk(input)
	return foundUser
}

func responsesInputHasCustomToolCallOutput(input any) bool {
	switch typed := input.(type) {
	case []any:
		for _, item := range typed {
			if responsesInputHasCustomToolCallOutput(item) {
				return true
			}
		}
		return false
	case []map[string]any:
		for _, item := range typed {
			if responsesInputHasCustomToolCallOutput(item) {
				return true
			}
		}
		return false
	case map[string]any:
		itemType := strings.ToLower(strings.TrimSpace(stringFromAny(typed["type"])))
		if itemType == "custom_tool_call_output" {
			return true
		}
		return responsesInputHasCustomToolCallOutput(typed["content"])
	default:
		return false
	}
}

func responsesFirstCustomToolCallOutputText(input any) string {
	switch typed := input.(type) {
	case []any:
		for _, item := range typed {
			if output := responsesFirstCustomToolCallOutputText(item); output != "" {
				return output
			}
		}
		return ""
	case []map[string]any:
		for _, item := range typed {
			if output := responsesFirstCustomToolCallOutputText(item); output != "" {
				return output
			}
		}
		return ""
	case map[string]any:
		itemType := strings.ToLower(strings.TrimSpace(stringFromAny(typed["type"])))
		if itemType == "custom_tool_call_output" {
			return firstNonEmpty(responsesToolOutputText(typed["output"]), contentToText(typed["content"]))
		}
		return responsesFirstCustomToolCallOutputText(typed["content"])
	default:
		return ""
	}
}

func codexApplyPatchCompletionAnswer(output string) string {
	output = strings.TrimSpace(output)
	lower := strings.ToLower(output)
	switch {
	case output == "":
		return "apply_patch 已执行完成。"
	case strings.Contains(lower, "success") || strings.Contains(output, "Updated the following files") || strings.Contains(output, "更新") || strings.Contains(output, "成功"):
		if files := codexApplyPatchUpdatedFiles(output); len(files) > 0 {
			return "修改已完成：" + strings.Join(files, "、")
		}
		return "修改已完成。"
	default:
		return "apply_patch 已返回结果，请查看工具输出确认修改状态。"
	}
}

func codexApplyPatchUpdatedFiles(output string) []string {
	lines := strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n")
	files := []string{}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "M ") || strings.HasPrefix(line, "A ") || strings.HasPrefix(line, "D ") {
			file := strings.TrimSpace(line[2:])
			if file != "" {
				files = append(files, file)
			}
		}
	}
	return files
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

func formatResponsesToolCallForPrompt(item map[string]any) string {
	if item == nil {
		return ""
	}
	name := firstNonEmpty(stringFromAny(item["name"]), stringFromAny(item["tool_name"]))
	callID := stringFromAny(item["call_id"])
	body := firstNonEmpty(stringFromAny(item["arguments"]), stringFromAny(item["input"]))
	if body == "" {
		if raw, err := json.Marshal(item); err == nil {
			body = string(raw)
		}
	}
	attrs := []string{}
	if name != "" {
		attrs = append(attrs, `name="`+strings.ReplaceAll(name, "\"", "'")+`"`)
	}
	if callID != "" {
		attrs = append(attrs, `call_id="`+strings.ReplaceAll(callID, "\"", "'")+`"`)
	}
	if len(attrs) == 0 {
		return "<tool_call>\n" + strings.TrimSpace(body) + "\n</tool_call>"
	}
	return "<tool_call " + strings.Join(attrs, " ") + ">\n" + strings.TrimSpace(body) + "\n</tool_call>"
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

func appendResponsesRequestControlInstructions(hiddenPrompt string, body map[string]any) string {
	instructions := responsesSafeRequestControlInstructions(body)
	if len(instructions) == 0 {
		return strings.TrimSpace(hiddenPrompt)
	}
	control := formatPromptSection("openai_request_controls", strings.Join(instructions, "\n"))
	return joinHiddenPromptParts(hiddenPrompt, control)
}

func responsesSafeRequestControlInstructions(body map[string]any) []string {
	instructions := []string{}
	if maxTokens, ok := positiveIntFromAny(valueOrNil(body, "max_output_tokens")); ok {
		instructions = append(instructions, fmt.Sprintf("The client requested max_output_tokens=%d. Keep the final answer within roughly that many output tokens.", maxTokens))
	}
	if reasoning := mapFromAny(valueOrNil(body, "reasoning")); reasoning != nil {
		parts := []string{}
		if effort := strings.TrimSpace(stringFromAny(reasoning["effort"])); effort != "" {
			parts = append(parts, "effort="+effort)
		}
		if summary := strings.TrimSpace(stringFromAny(reasoning["summary"])); summary != "" {
			parts = append(parts, "summary="+summary)
		}
		if len(parts) > 0 {
			instructions = append(instructions, "The client provided reasoning preferences ("+strings.Join(parts, ", ")+"). Use them internally, but do not reveal private chain-of-thought.")
		}
	}
	if temp, ok := finiteNumberString(valueOrNil(body, "temperature")); ok {
		instructions = append(instructions, "Approximate the client's temperature preference: "+temp+". Lower values should be more deterministic; higher values may be more exploratory.")
	}
	if topP, ok := finiteNumberString(valueOrNil(body, "top_p")); ok {
		instructions = append(instructions, "Approximate the client's top_p preference: "+topP+".")
	}
	if toolInstruction := responseToolBridgeInstruction(body); toolInstruction != "" {
		instructions = append(instructions, toolInstruction)
	}
	return instructions
}

func responseToolBridgeInstruction(body map[string]any) string {
	tools := responseToolNames(valueOrNil(body, "tools"))
	if len(tools) == 0 {
		return ""
	}
	mode, selected := responseToolChoiceMode(valueOrNil(body, "tool_choice"))
	switch mode {
	case "none":
		return "The client requested a direct answer. Do not emit patch-shaped text unless the user explicitly asks for a concrete edit."
	case "named":
		if strings.EqualFold(selected, "apply_patch") {
			return "If the user asks for a file or document edit, provide a concrete text patch block instead of saying that file editing is unavailable."
		}
		return ""
	default:
		if responseHasTool(valueOrNil(body, "tools"), "apply_patch") {
			return "If the user asks for a file or document edit, provide a concrete text patch block instead of saying that file editing is unavailable."
		}
		return ""
	}
}

func responseToolNames(tools any) []string {
	out := []string{}
	seen := map[string]struct{}{}
	for _, rawTool := range sliceFromAny(tools) {
		tool := mapFromAny(rawTool)
		if tool == nil {
			continue
		}
		name := stringFromAny(tool["name"])
		if function := mapFromAny(tool["function"]); function != nil {
			name = firstNonEmpty(name, stringFromAny(function["name"]))
		}
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
		if len(out) >= 20 {
			break
		}
	}
	return out
}

func applyResponsesInputTruncation(prompt string, body map[string]any) string {
	if !strings.EqualFold(strings.TrimSpace(stringFromAny(valueOrNil(body, "truncation"))), "auto") {
		return strings.TrimSpace(prompt)
	}
	const maxPromptRunes = 180000
	runes := []rune(strings.TrimSpace(prompt))
	if len(runes) <= maxPromptRunes {
		return string(runes)
	}
	head := maxPromptRunes / 4
	tail := maxPromptRunes - head
	return string(runes[:head]) + "\n\n[notion2api: middle of prompt truncated because truncation=auto]\n\n" + string(runes[len(runes)-tail:])
}

func applyResponsesOutputLimit(answer string, body map[string]any) (string, map[string]any) {
	maxTokens, ok := positiveIntFromAny(valueOrNil(body, "max_output_tokens"))
	if !ok || maxTokens <= 0 {
		return answer, nil
	}
	estimated := security.EstimateTokens(answer)
	if estimated <= int64(maxTokens) {
		return answer, nil
	}
	maxRunes := maxTokens * 4
	if maxRunes < 1 {
		maxRunes = 1
	}
	runes := []rune(answer)
	if len(runes) > maxRunes {
		answer = strings.TrimSpace(string(runes[:maxRunes]))
	}
	return answer, map[string]any{"reason": "max_output_tokens"}
}

func responseStoreEnabled(body map[string]any) bool {
	return boolFromMapDefault(body, "store", true)
}

func positiveIntFromAny(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, typed > 0
	case int64:
		return int(typed), typed > 0
	case float64:
		return int(typed), typed > 0
	case json.Number:
		i, err := typed.Int64()
		if err == nil {
			return int(i), i > 0
		}
		f, err := typed.Float64()
		return int(f), err == nil && f > 0
	default:
		clean := strings.TrimSpace(stringFromAny(value))
		if clean == "" {
			return 0, false
		}
		var parsed json.Number = json.Number(clean)
		i, err := parsed.Int64()
		if err == nil {
			return int(i), i > 0
		}
		f, err := parsed.Float64()
		return int(f), err == nil && f > 0
	}
}

func finiteNumberString(value any) (string, bool) {
	switch typed := value.(type) {
	case nil:
		return "", false
	case int:
		return fmt.Sprintf("%d", typed), true
	case int64:
		return fmt.Sprintf("%d", typed), true
	case float64:
		return strconvFormatFloat(typed), true
	case json.Number:
		if _, err := typed.Float64(); err == nil {
			return typed.String(), true
		}
		return "", false
	default:
		clean := strings.TrimSpace(stringFromAny(value))
		if clean == "" {
			return "", false
		}
		var parsed json.Number = json.Number(clean)
		if _, err := parsed.Float64(); err != nil {
			return "", false
		}
		return clean, true
	}
}

func strconvFormatFloat(value float64) string {
	raw := fmt.Sprintf("%.6f", value)
	raw = strings.TrimRight(raw, "0")
	raw = strings.TrimRight(raw, ".")
	if raw == "" || raw == "-0" {
		return "0"
	}
	return raw
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

func appendResponseFormatInstruction(hiddenPrompt string, hiddenMessages []hiddenPromptMessage, responseFormat any) (string, []hiddenPromptMessage) {
	hiddenPrompt = strings.TrimSpace(hiddenPrompt)
	instruction := responseFormatInstruction(responseFormat)
	if instruction == "" {
		return hiddenPrompt, hiddenMessages
	}
	hiddenMessages = append(hiddenMessages, hiddenPromptMessage{Role: "system", Content: instruction})
	if hiddenPrompt == "" {
		return instruction, hiddenMessages
	}
	return hiddenPrompt + "\n\n" + instruction, hiddenMessages
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
