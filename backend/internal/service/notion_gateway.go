package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"notaion2api/backend/internal/config"
	"notaion2api/backend/internal/dto"
	"notaion2api/backend/internal/model"
	proxyclient "notaion2api/backend/internal/proxy"

	enetxg "github.com/enetx/g"
	"github.com/enetx/surf"
	"github.com/google/uuid"
)

type NotionGateway struct {
	cfg     config.Config
	clients *proxyclient.Manager
}

type NotionPromptRequest struct {
	Prompt           string
	HiddenPrompt     string
	HiddenMessages   []hiddenPromptMessage
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

type NotionAPIError struct {
	StatusCode int
	Body       string
	Message    string
}

func (e *NotionAPIError) Error() string {
	if e == nil {
		return ""
	}
	message := strings.TrimSpace(e.Message)
	if message == "" {
		message = strings.TrimSpace(e.Body)
	}
	if message == "" {
		message = http.StatusText(e.StatusCode)
	}
	return fmt.Sprintf("notion upstream failed: %d %s", e.StatusCode, message)
}

type NotionNoAssistantTextError struct {
	ThreadID string
	Cause    string
}

func (e *NotionNoAssistantTextError) Error() string {
	if e == nil {
		return ""
	}
	suffix := ""
	if cause := strings.TrimSpace(e.Cause); cause != "" {
		suffix = ": " + cause
	}
	if threadID := strings.TrimSpace(e.ThreadID); threadID != "" {
		return fmt.Sprintf("notion upstream returned successfully, but no assistant text was detected for thread %s%s", threadID, suffix)
	}
	return "notion upstream returned successfully, but no assistant text was detected" + suffix
}

type NotionInferenceStepError struct {
	ThreadID   string
	MessageID  string
	Message    string
	SubType    string
	TraceID    string
	Retryable  bool
	StackTrace string
}

func (e *NotionInferenceStepError) Error() string {
	if e == nil {
		return ""
	}
	message := firstNonEmpty(strings.TrimSpace(e.Message), "upstream inference failed")
	details := []string{}
	if clean := strings.TrimSpace(e.SubType); clean != "" {
		details = append(details, "sub_type="+clean)
	}
	if clean := strings.TrimSpace(e.TraceID); clean != "" {
		details = append(details, "trace_id="+clean)
	}
	if e.Retryable {
		details = append(details, "retryable=true")
	}
	if len(details) == 0 {
		return fmt.Sprintf("thread %s failed: %s", strings.TrimSpace(e.ThreadID), message)
	}
	return fmt.Sprintf("thread %s failed: %s (%s)", strings.TrimSpace(e.ThreadID), message, strings.Join(details, " "))
}

func NewNotionGateway(cfg config.Config) *NotionGateway {
	timeout := cfg.Upstream.TimeoutSeconds
	if timeout <= 0 {
		timeout = 180
	}
	return &NotionGateway{
		cfg:     cfg,
		clients: proxyclient.NewManager(timeout),
	}
}

func (g *NotionGateway) clientForAccount(account model.Account) (*http.Client, error) {
	return g.clients.ClientForAccount(account, g.cfg.Proxy)
}

func (g *NotionGateway) applyAccountHeaders(req *http.Request, account model.Account, accept string) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", accept)
	req.Header.Set("Notion-Client-Version", notionClientVersion(account.ClientVersion))
	req.Header.Set("Notion-Audit-Log-Platform", "web")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/145.0.0.0 Safari/537.36")
	req.Header.Set("Sec-CH-UA", "\"Google Chrome\";v=\"145\", \"Not?A_Brand\";v=\"8\", \"Chromium\";v=\"145\"")
	req.Header.Set("Sec-CH-UA-Mobile", "?0")
	req.Header.Set("Sec-CH-UA-Platform", "\"Windows\"")
	req.Header.Set("Sec-Fetch-Dest", "empty")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	if userID := strings.TrimSpace(account.UserID); userID != "" {
		req.Header.Set("X-Notion-Active-User-Header", userID)
	}
	if spaceID := strings.TrimSpace(account.SpaceID); spaceID != "" {
		req.Header.Set("X-Notion-Space-ID", spaceID)
	}
	if origin := strings.TrimSpace(g.cfg.Upstream.Origin); origin != "" {
		req.Header.Set("Origin", origin)
		req.Header.Set("Referer", strings.TrimRight(origin, "/")+"/ai")
	}
	cookie := strings.TrimSpace(account.Cookie)
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	} else if token := strings.TrimSpace(account.AuthToken); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
}

func notionClientVersion(value string) string {
	clean := strings.TrimSpace(value)
	if clean == "" || clean == "23.13.0.0" {
		return "23.13.20260616.0256"
	}
	return clean
}

func (g *NotionGateway) ProbeProtocolHealth(ctx context.Context, account model.Account) error {
	if strings.TrimSpace(account.AuthToken) == "" && strings.TrimSpace(account.Cookie) == "" {
		return nil
	}
	payload := inferenceTranscriptsForSpacePayload(account.SpaceID, 50)
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	endpoint := strings.TrimRight(firstNonEmpty(g.cfg.Upstream.BaseURL, "https://www.notion.so"), "/") + "/api/v3/getInferenceTranscriptsForUser"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	g.applyAccountHeaders(httpReq, account, "application/json")
	client, err := g.clientForAccount(account)
	if err != nil {
		return err
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &NotionAPIError{StatusCode: resp.StatusCode, Body: strings.TrimSpace(string(raw))}
	}
	return nil
}

func (g *NotionGateway) ListInferenceTranscripts(ctx context.Context, account model.Account, spaceID string, limit int) ([]dto.NotionConversationSummaryDTO, error) {
	spaceID = firstNonEmpty(spaceID, account.SpaceID)
	if spaceID == "" {
		return nil, fmt.Errorf("%w: space_id is required", ErrInvalid)
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	account.SpaceID = spaceID
	out, err := g.postNotionJSON(ctx, account, "getInferenceTranscriptsForUser", inferenceTranscriptsForSpacePayload(spaceID, limit), g.chatReferer(""))
	if err != nil {
		return nil, err
	}
	rawItems := sliceFromAny(out["transcripts"])
	items := make([]dto.NotionConversationSummaryDTO, 0, len(rawItems))
	for _, rawItem := range rawItems {
		item := mapFromAny(rawItem)
		threadID := firstNonEmpty(stringFromAny(item["id"]), stringFromAny(item["threadId"]))
		if threadID == "" {
			continue
		}
		createdAt := timeFromTranscriptValue(item["created_at"])
		updatedAt := firstNonZeroTime(timeFromTranscriptValue(item["updated_at"]), timeFromTranscriptValue(item["last_edited_time"]), createdAt)
		items = append(items, dto.NotionConversationSummaryDTO{
			ID:                   threadID,
			Title:                firstNonEmpty(stringFromAny(item["title"]), "Untitled conversation"),
			Origin:               "notion",
			RemoteOnly:           true,
			Status:               "completed",
			Source:               "notion",
			Transport:            firstNonEmpty(stringFromAny(item["type"]), "transcript_sync"),
			AccountEmail:         account.Email,
			SpaceID:              spaceID,
			ThreadID:             threadID,
			CreatedByDisplayName: stringFromAny(item["created_by_display_name"]),
			CreatedAt:            timePtrOrNil(createdAt),
			UpdatedAt:            timePtrOrNil(updatedAt),
		})
	}
	sort.SliceStable(items, func(i, j int) bool {
		return notionConversationSortTime(items[i]).After(notionConversationSortTime(items[j]))
	})
	return items, nil
}

func (g *NotionGateway) LoadInferenceTranscript(ctx context.Context, account model.Account, spaceID string, threadID string) (dto.NotionConversationDetailDTO, error) {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return dto.NotionConversationDetailDTO{}, fmt.Errorf("%w: thread_id is required", ErrInvalid)
	}
	spaceID = firstNonEmpty(spaceID, account.SpaceID)
	if spaceID == "" {
		return dto.NotionConversationDetailDTO{}, fmt.Errorf("%w: space_id is required", ErrInvalid)
	}
	account.SpaceID = spaceID
	threadData, err := g.syncThread(ctx, account, threadID)
	if err != nil {
		return dto.NotionConversationDetailDTO{}, err
	}
	recordMap := recordMapFromPayload(threadData)
	messageIDs := messageIDsFromRecordMap(recordMap, threadID)
	if len(messageIDs) > 0 {
		messageData, err := g.syncThreadMessages(ctx, account, threadID, messageIDs)
		if err != nil {
			return dto.NotionConversationDetailDTO{}, err
		}
		if nextRecordMap := recordMapFromPayload(messageData); nextRecordMap != nil {
			recordMap = nextRecordMap
		}
	}
	threadMessages := mapFromAny(recordMap["thread_message"])
	messages := make([]dto.NotionConversationMessageDTO, 0, len(messageIDs))
	for _, messageID := range messageIDs {
		message, ok := extractNotionConversationMessage(messageID, threadMessages[messageID])
		if ok {
			messages = append(messages, message)
		}
	}
	createdAt, updatedAt := notionConversationMessageTimes(messages)
	summary := dto.NotionConversationSummaryDTO{
		ID:           threadID,
		Title:        notionConversationTitle(messages),
		Origin:       "notion",
		RemoteOnly:   true,
		Preview:      notionConversationPreview(messages),
		Status:       "completed",
		Source:       "notion",
		Transport:    "transcript_sync",
		AccountEmail: account.Email,
		SpaceID:      spaceID,
		ThreadID:     threadID,
		CreatedAt:    timePtrOrNil(createdAt),
		UpdatedAt:    timePtrOrNil(updatedAt),
		MessageCount: len(messages),
	}
	return dto.NotionConversationDetailDTO{
		NotionConversationSummaryDTO: summary,
		Messages:                     messages,
	}, nil
}

func inferenceTranscriptsForSpacePayload(spaceID string, limit int) map[string]any {
	payload := map[string]any{
		"threadParentPointer": map[string]any{
			"table":   "space",
			"id":      spaceID,
			"spaceId": spaceID,
		},
		"includeWorkflowThreads": true,
		"includeWriterChats":     false,
	}
	if limit > 0 {
		payload["limit"] = limit
	}
	return payload
}

func (g *NotionGateway) DeleteThread(ctx context.Context, account model.Account, spaceID string, threadID string) error {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return fmt.Errorf("%w: thread_id is required", ErrInvalid)
	}
	spaceID = firstNonEmpty(spaceID, account.SpaceID)
	if spaceID == "" {
		return fmt.Errorf("%w: space_id is required", ErrInvalid)
	}
	account.SpaceID = spaceID
	_, err := g.postNotionJSON(ctx, account, "saveTransactionsFanout", map[string]any{
		"requestId": uuid.NewString(),
		"transactions": []map[string]any{{
			"id": uuid.NewString(),
			"operations": []map[string]any{{
				"command": "update",
				"table":   "thread",
				"id":      threadID,
				"path":    []string{},
				"args": map[string]any{
					"alive": false,
				},
			}},
		}},
	}, g.chatReferer(threadID))
	return err
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
		"type":                                           threadType,
		"enableAgentAutomations":                         true,
		"enableAgentIntegrations":                        true,
		"enableCustomAgents":                             true,
		"enableExperimentalIntegrations":                 false,
		"enableAgentDiffs":                               true,
		"enableAgentUpdatePagePatch":                     true,
		"enableAgentCreateDbTemplate":                    true,
		"enableCsvAttachmentSupport":                     g.cfg.Features.CSVAttachment,
		"enableDatabaseAgents":                           false,
		"showDatabaseAgentsDiscoverability":              false,
		"enableAgentThreadTools":                         false,
		"enableCrdtOperations":                           false,
		"enableAgentCardCustomization":                   true,
		"enableSystemPromptAsPage":                       false,
		"enableUserSessionContext":                       false,
		"enableScriptAgentAdvanced":                      false,
		"enableScriptAgent":                              true,
		"enableScriptAgentSearchConnectorsInCustomAgent": false,
		"enableScriptAgentGoogleDriveInCustomAgent":      false,
		"enableScriptAgentGoogleDriveOAuthInCustomAgent": false,
		"enableScriptAgentSlack":                         true,
		"enableScriptAgentMcpServers":                    false,
		"enableScriptAgentMail":                          true,
		"enableScriptAgentCalendar":                      true,
		"enableScriptAgentCustomToolCalling":             false,
		"enableCreateAndRunThread":                       true,
		"enableSoftwareFactoryPage":                      false,
		"enableAgentGenerateImage":                       g.cfg.Features.GenerateImage,
		"enableSpeculativeSearch":                        false,
		"enableQueryCalendar":                            false,
		"enableQueryMail":                                false,
		"enableMailExplicitToolCalls":                    true,
		"enableMailNotificationPreferences":              false,
		"enableMailAgentMultiProviderSupport":            false,
		"useRulePrioritization":                          true,
		"availableConnectors":                            []any{},
		"customConnectorInfo":                            []any{},
		"searchScopes":                                   []map[string]any{},
		"useSearchToolV2":                                false,
		"enableUnifiedSearch":                            false,
		"useWebSearch":                                   req.UseWebSearch,
		"isHipaa":                                        false,
		"yoloMode":                                       false,
		"useReadOnlyMode":                                false,
		"writerMode":                                     false,
		"modelFromUser":                                  strings.TrimSpace(req.NotionModel) != "",
		"isCustomAgent":                                  false,
		"isCustomAgentBuilder":                           false,
		"isAgentResearchRequest":                         false,
		"useCustomAgentDraft":                            false,
		"use_draft_actor_pointer":                        false,
		"enableUpdatePageAutofixer":                      true,
		"enableMarkdownVNext":                            false,
		"enableUpdatePageOrderUpdates":                   true,
		"enableAgentSupportPropertyReorder":              true,
		"agentShortUpdatePageResult":                     false,
		"enableAgentAskSurvey":                           true,
		"databaseAgentConfigMode":                        false,
		"isOnboardingAgent":                              false,
		"isMobile":                                       false,
	}
	if req.UseWebSearch {
		configValue["searchScopes"] = []map[string]any{{"type": "everything"}}
	}
	if cleanModel := strings.TrimSpace(req.NotionModel); cleanModel != "" {
		configValue["model"] = cleanModel
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
	if hiddenPrompt := strings.TrimSpace(req.HiddenPrompt); hiddenPrompt != "" {
		contextValue["instructions"] = hiddenPrompt
		contextValue["runtimePromptHint"] = hiddenPrompt
	}
	transcript := []map[string]any{
		{"id": configID, "type": "config", "value": configValue},
		{"id": contextID, "type": "context", "value": contextValue},
	}
	transcript = append(transcript, map[string]any{
		"id":        userStepID,
		"type":      "user",
		"value":     [][]string{{req.Prompt}},
		"userId":    account.UserID,
		"createdAt": now,
	})
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
	g.applyAccountHeaders(httpReq, account, "application/x-ndjson")
	client, err := g.clientForAccount(account)
	if err != nil {
		return NotionRunResult{ThreadID: threadID, Payload: payload}, err
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return NotionRunResult{ThreadID: threadID, Payload: payload}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, err := io.ReadAll(resp.Body)
		if err != nil {
			return NotionRunResult{ThreadID: threadID, Payload: payload}, err
		}
		return NotionRunResult{ThreadID: threadID, Payload: payload, RawResponse: string(raw)}, &NotionAPIError{StatusCode: resp.StatusCode, Body: strings.TrimSpace(string(raw))}
	}
	text, messageID, rawResponse, err := consumeNotionTextStream(resp.Body, threadID, notionIdleAfterAnswerTimeout)
	if err != nil {
		return NotionRunResult{ThreadID: threadID, Payload: payload, RawResponse: rawResponse}, err
	}
	if strings.TrimSpace(text) == "" {
		if stepErr := inferenceErrorFromRawNotionResponse(rawResponse, threadID); stepErr != nil {
			if isTrustRuleDeniedInferenceError(stepErr) {
				fallbackText, fallbackMessageID, fallbackRaw, fallbackErr := g.runInferenceTranscriptWithSurfFallback(ctx, account, payload, threadID, stepErr)
				if fallbackErr != nil {
					return NotionRunResult{ThreadID: threadID, Payload: payload, RawResponse: firstNonEmpty(fallbackRaw, rawResponse)}, fallbackErr
				}
				if strings.TrimSpace(fallbackText) != "" {
					return NotionRunResult{Text: fallbackText, ThreadID: threadID, MessageID: fallbackMessageID, Payload: payload, RawResponse: fallbackRaw}, nil
				}
			}
			return NotionRunResult{ThreadID: threadID, Payload: payload, RawResponse: rawResponse}, stepErr
		}
		slog.Warn("notion run inference returned no assistant text",
			"thread_id", threadID,
			"raw_summary", summarizeNotionRawResponse(rawResponse, threadID),
		)
		if messageIDs := messageIDsFromRawNotionResponse(rawResponse, threadID); len(messageIDs) > 0 {
			syncCtx, cancel := context.WithTimeout(ctx, notionAnswerSyncTimeout)
			recoveredText, recoveredMessageID, recoverErr := g.loadFinalAnswerFromMessageIDs(syncCtx, account, threadID, messageIDs)
			cancel()
			if strings.TrimSpace(recoveredText) != "" {
				return NotionRunResult{Text: recoveredText, ThreadID: threadID, MessageID: recoveredMessageID, Payload: payload, RawResponse: rawResponse}, nil
			}
			if recoverErr != nil {
				slog.Warn("notion direct message sync did not recover assistant text",
					"thread_id", threadID,
					"message_ids", messageIDs,
					"error", recoverErr,
				)
			}
		}
		recoveredText, recoveredMessageID, recoverErr := g.loadFinalAnswerWithPolling(ctx, account, threadID)
		if strings.TrimSpace(recoveredText) != "" {
			return NotionRunResult{Text: recoveredText, ThreadID: threadID, MessageID: recoveredMessageID, Payload: payload, RawResponse: rawResponse}, nil
		}
		cause := ""
		if recoverErr != nil {
			cause = recoverErr.Error()
		}
		return NotionRunResult{ThreadID: threadID, Payload: payload, RawResponse: rawResponse}, &NotionNoAssistantTextError{ThreadID: threadID, Cause: cause}
	}
	return NotionRunResult{Text: text, ThreadID: threadID, MessageID: messageID, Payload: payload, RawResponse: rawResponse}, nil
}

func isTrustRuleDeniedInferenceError(err error) bool {
	var stepErr *NotionInferenceStepError
	if !errors.As(err, &stepErr) {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(stepErr.SubType), "trust-rule-denied")
}

func (g *NotionGateway) runInferenceTranscriptWithSurfFallback(ctx context.Context, account model.Account, payload map[string]any, threadID string, upstreamErr error) (string, string, string, error) {
	if strings.TrimSpace(account.Cookie) == "" {
		return "", "", "", upstreamErr
	}
	if !g.supportsSurfRunInferenceFallback() {
		return "", "", "", upstreamErr
	}
	timeout := time.Duration(g.cfg.Upstream.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	if timeout < 30*time.Second {
		timeout = 30 * time.Second
	}
	fallbackCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	body, err := g.runInferenceTranscriptWithSurf(fallbackCtx, account, payload)
	if err != nil {
		return "", "", "", fmt.Errorf("%w; surf fallback failed: %v", upstreamErr, err)
	}
	text, messageID, rawResponse, err := consumeNotionTextStream(io.NopCloser(strings.NewReader(body)), threadID, notionIdleAfterAnswerTimeout)
	if err != nil {
		return "", "", rawResponse, fmt.Errorf("%w; surf fallback stream parse failed: %v", upstreamErr, err)
	}
	if strings.TrimSpace(text) != "" {
		return text, messageID, rawResponse, nil
	}
	if stepErr := inferenceErrorFromRawNotionResponse(rawResponse, threadID); stepErr != nil {
		return "", "", rawResponse, stepErr
	}
	return "", "", rawResponse, fmt.Errorf("%w; surf fallback returned no assistant text", upstreamErr)
}

func (g *NotionGateway) supportsSurfRunInferenceFallback() bool {
	originURL := firstNonEmpty(strings.TrimSpace(g.cfg.Upstream.Origin), strings.TrimSpace(g.cfg.Upstream.BaseURL), "https://www.notion.so")
	parsed, err := url.Parse(originURL)
	if err != nil {
		return false
	}
	if !strings.EqualFold(parsed.Scheme, "https") {
		return false
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	return host != "" && host != "localhost" && host != "::1" && !strings.HasPrefix(host, "127.")
}

func (g *NotionGateway) runInferenceTranscriptWithSurf(ctx context.Context, account model.Account, payload map[string]any) (string, error) {
	endpoint := strings.TrimRight(firstNonEmpty(g.cfg.Upstream.BaseURL, "https://www.notion.so"), "/") + "/api/v3/runInferenceTranscript"
	parsedEndpoint, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	proxyURL, err := g.surfProxyURLForAccount(account, parsedEndpoint)
	if err != nil {
		return "", err
	}
	client, err := newSurfStdClient(proxyURL)
	if err != nil {
		return "", err
	}
	timeout := time.Duration(g.cfg.Upstream.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	client.Timeout = timeout
	if client.Jar == nil {
		jar, err := cookiejar.New(nil)
		if err != nil {
			return "", err
		}
		client.Jar = jar
	}
	loadCookieHeaderIntoJar(client.Jar, parsedEndpoint, account.Cookie)

	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, parsedEndpoint.String(), bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	g.applyAccountHeaders(httpReq, account, "application/x-ndjson")
	httpReq.Header.Del("Cookie")
	httpReq.Header.Del("Authorization")
	httpReq.Header.Set("Referer", strings.TrimRight(firstNonEmpty(g.cfg.Upstream.Origin, g.cfg.Upstream.BaseURL, "https://www.notion.so"), "/")+"/ai")

	resp, err := client.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("surf fallback returned non-success status=%d content_type=%q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	text := string(raw)
	if err := detectInferenceStreamResponseFormat(text); err != nil {
		return "", err
	}
	return text, nil
}

func newSurfStdClient(proxyURL string) (*http.Client, error) {
	builder := surf.NewClient().Builder().Session().Impersonate().Chrome()
	if strings.TrimSpace(proxyURL) != "" {
		builder = builder.Proxy(enetxg.String(proxyURL))
	}
	result := builder.Build()
	if err := result.Err(); err != nil {
		return nil, err
	}
	return result.Unwrap().Std(), nil
}

func (g *NotionGateway) surfProxyURLForAccount(account model.Account, target *url.URL) (string, error) {
	if target == nil {
		return "", nil
	}
	policy := proxyclient.PolicyForAccount(g.cfg.Proxy, account)
	switch policy.Mode {
	case "", proxyclient.ModeOff:
		return "", nil
	}
	raw := policy.ProxyURLForScheme(target.Scheme)
	if strings.TrimSpace(raw) == "" {
		return "", nil
	}
	parsed, err := proxyclient.ParseURL(raw, policy.Mode)
	if err != nil {
		return "", err
	}
	if parsed == nil {
		return "", nil
	}
	return parsed.String(), nil
}

func loadCookieHeaderIntoJar(jar http.CookieJar, target *url.URL, cookieHeader string) {
	if jar == nil || target == nil {
		return
	}
	req := http.Request{Header: http.Header{"Cookie": []string{strings.TrimSpace(cookieHeader)}}}
	cookies := req.Cookies()
	if len(cookies) == 0 {
		return
	}
	loaded := make([]*http.Cookie, 0, len(cookies))
	for _, cookie := range cookies {
		if cookie == nil || strings.TrimSpace(cookie.Name) == "" {
			continue
		}
		loaded = append(loaded, &http.Cookie{Name: cookie.Name, Value: cookie.Value, Path: "/"})
	}
	if len(loaded) > 0 {
		jar.SetCookies(target, loaded)
	}
}

func detectInferenceStreamResponseFormat(body string) error {
	trimmed := strings.TrimSpace(strings.TrimPrefix(body, "\uFEFF"))
	if trimmed == "" {
		return fmt.Errorf("surf fallback returned empty response")
	}
	if strings.HasPrefix(trimmed, "{") {
		return nil
	}
	compact := strings.Join(strings.Fields(trimmed), " ")
	if len(compact) > 220 {
		compact = compact[:220] + "..."
	}
	lower := strings.ToLower(trimmed)
	if strings.HasPrefix(trimmed, "<") || strings.Contains(lower, "cookiepart") || strings.Contains(lower, "cloudflare") || strings.Contains(lower, "cf-chl") {
		return fmt.Errorf("surf fallback returned challenge/html content instead of NDJSON: %s", compact)
	}
	return fmt.Errorf("surf fallback returned non-NDJSON content: %s", compact)
}

func (g *NotionGateway) loadFinalAnswerWithPolling(ctx context.Context, account model.Account, threadID string) (string, string, error) {
	text, messageID, err := g.loadFinalAnswerOnceWithTimeout(ctx, account, threadID)
	if strings.TrimSpace(text) != "" || err == nil {
		return text, messageID, err
	}
	lastErr := err
	rounds := g.cfg.Upstream.PollMaxRounds
	if rounds <= 0 {
		rounds = 8
	}
	if rounds > 8 {
		rounds = 8
	}
	interval := time.Duration(g.cfg.Upstream.PollIntervalSeconds * float64(time.Second))
	if interval <= 0 {
		interval = 1500 * time.Millisecond
	}
	for i := 0; i < rounds; i++ {
		select {
		case <-ctx.Done():
			return "", "", ctx.Err()
		case <-time.After(interval):
		}
		text, messageID, err = g.loadFinalAnswerOnceWithTimeout(ctx, account, threadID)
		if strings.TrimSpace(text) != "" {
			return text, messageID, nil
		}
		if err != nil {
			lastErr = err
		}
	}
	if lastErr != nil {
		return "", "", lastErr
	}
	return "", "", fmt.Errorf("thread %s did not produce final assistant text after polling", strings.TrimSpace(threadID))
}

func (g *NotionGateway) loadFinalAnswerOnceWithTimeout(ctx context.Context, account model.Account, threadID string) (string, string, error) {
	syncCtx, cancel := context.WithTimeout(ctx, notionAnswerSyncTimeout)
	defer cancel()
	return g.loadFinalAnswerOnce(syncCtx, account, threadID)
}

func (g *NotionGateway) loadFinalAnswerOnce(ctx context.Context, account model.Account, threadID string) (string, string, error) {
	threadData, err := g.syncThread(ctx, account, threadID)
	if err != nil {
		return "", "", err
	}
	recordMap := recordMapFromPayload(threadData)
	messageIDs := messageIDsFromRecordMap(recordMap, threadID)
	if len(messageIDs) == 0 {
		return "", "", fmt.Errorf("thread %s did not produce any messages", strings.TrimSpace(threadID))
	}
	messageData, err := g.syncThreadMessages(ctx, account, threadID, messageIDs)
	if err != nil {
		return "", "", err
	}
	recordMap = recordMapFromPayload(messageData)
	if stepErr := finalErrorFromRecordMap(recordMap, threadID); stepErr != nil {
		return "", "", stepErr
	}
	text, messageID := finalAgentFromRecordMap(recordMap, threadID)
	if strings.TrimSpace(text) == "" {
		return "", "", fmt.Errorf("thread %s did not produce any agent-inference message; message_ids=%v message_shapes=%v", strings.TrimSpace(threadID), messageIDs, summarizeThreadMessageShapes(recordMap, messageIDs))
	}
	return text, messageID, nil
}

func (g *NotionGateway) loadFinalAnswerFromMessageIDs(ctx context.Context, account model.Account, threadID string, messageIDs []string) (string, string, error) {
	if len(messageIDs) == 0 {
		return "", "", fmt.Errorf("thread %s did not produce any message ids", strings.TrimSpace(threadID))
	}
	messageData, err := g.syncThreadMessages(ctx, account, threadID, messageIDs)
	if err != nil {
		return "", "", err
	}
	recordMap := recordMapFromPayload(messageData)
	if stepErr := finalErrorFromRecordMap(recordMap, threadID); stepErr != nil {
		return "", "", stepErr
	}
	text, messageID := finalAgentFromRecordMap(recordMap, threadID)
	if strings.TrimSpace(text) == "" {
		return "", "", fmt.Errorf("thread %s did not produce any agent-inference message; message_ids=%v message_shapes=%v", strings.TrimSpace(threadID), messageIDs, summarizeThreadMessageShapes(recordMap, messageIDs))
	}
	return text, messageID, nil
}

func (g *NotionGateway) syncThread(ctx context.Context, account model.Account, threadID string) (map[string]any, error) {
	payload := map[string]any{
		"requests": []map[string]any{{
			"pointer": map[string]any{
				"table":   "thread",
				"id":      strings.TrimSpace(threadID),
				"spaceId": account.SpaceID,
			},
			"version": -1,
		}},
	}
	return g.postNotionJSON(ctx, account, "syncRecordValuesSpaceInitial", payload, g.chatReferer(threadID))
}

func (g *NotionGateway) syncThreadMessages(ctx context.Context, account model.Account, threadID string, messageIDs []string) (map[string]any, error) {
	requests := make([]map[string]any, 0, len(messageIDs))
	for _, messageID := range messageIDs {
		messageID = strings.TrimSpace(messageID)
		if messageID == "" {
			continue
		}
		requests = append(requests, map[string]any{
			"pointer": map[string]any{
				"table":   "thread_message",
				"id":      messageID,
				"spaceId": account.SpaceID,
			},
			"version": -1,
		})
	}
	if len(requests) == 0 {
		return nil, fmt.Errorf("thread %s did not produce any valid message ids", strings.TrimSpace(threadID))
	}
	return g.postNotionJSON(ctx, account, "syncRecordValuesSpaceInitial", map[string]any{"requests": requests}, g.chatReferer(threadID))
}

func (g *NotionGateway) postNotionJSON(ctx context.Context, account model.Account, endpointName string, payload map[string]any, referer string) (map[string]any, error) {
	if strings.TrimSpace(account.AuthToken) == "" && strings.TrimSpace(account.Cookie) == "" {
		return nil, fmt.Errorf("%w: account credentials are required", ErrInvalid)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	endpoint := strings.TrimRight(firstNonEmpty(g.cfg.Upstream.BaseURL, "https://www.notion.so"), "/") + "/api/v3/" + strings.TrimLeft(endpointName, "/")
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	g.applyAccountHeaders(httpReq, account, "application/json")
	if clean := strings.TrimSpace(referer); clean != "" {
		httpReq.Header.Set("Referer", clean)
	}
	client, err := g.clientForAccount(account)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &NotionAPIError{StatusCode: resp.StatusCode, Body: strings.TrimSpace(string(raw))}
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode notion %s response: %w", endpointName, err)
	}
	return out, nil
}

func (g *NotionGateway) chatReferer(threadID string) string {
	base := strings.TrimRight(firstNonEmpty(g.cfg.Upstream.Origin, g.cfg.Upstream.BaseURL, "https://www.notion.so"), "/")
	clean := strings.ReplaceAll(strings.TrimSpace(threadID), "-", "")
	if clean == "" {
		return base + "/ai"
	}
	return base + "/chat?t=" + clean + "&wfv=chat"
}

type notionAgentInferenceValue struct {
	Type    string `json:"type"`
	Content string `json:"content"`
}

type notionStepState struct {
	ID        string
	Type      string
	Text      string
	Completed bool
}

type notionTranscriptState struct {
	Steps            []notionStepState
	ActiveAgentIndex int
	FinalText        string
	MessageID        string
	terminal         bool
	patchValueTypes  map[string]string
	patchValueText   map[string]string
	patchValueCounts map[string]int
}

func extractNotionText(raw []byte, threadID string) (string, string) {
	state := &notionTranscriptState{ActiveAgentIndex: -1}
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	var bestText string
	var messageID string
	seenLine := false
	for scanner.Scan() {
		line := cleanNotionStreamLine(scanner.Text())
		if line == "" {
			continue
		}
		if line == "[DONE]" {
			continue
		}
		var payload any
		if err := json.Unmarshal([]byte(line), &payload); err != nil {
			continue
		}
		seenLine = true
		text, id := state.handlePayload(payload, threadID)
		if text = sanitizeNotionAssistantText(text); text != "" {
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
		if seenLine {
			state = &notionTranscriptState{ActiveAgentIndex: -1}
		}
		text, id := state.handlePayload(payload, threadID)
		if text = sanitizeNotionAssistantText(text); text != "" {
			return text, id
		}
		return findTextInNotionPayload(payload, threadID)
	}
	return "", ""
}

const notionIdleAfterAnswerTimeout = 5 * time.Second
const notionAnswerSyncTimeout = 10 * time.Second

type notionStreamReadEvent struct {
	line []byte
	err  error
}

func consumeNotionTextStream(reader io.ReadCloser, threadID string, idleAfterAnswer time.Duration) (string, string, string, error) {
	state := &notionTranscriptState{ActiveAgentIndex: -1}
	events := make(chan notionStreamReadEvent, 1)
	done := make(chan struct{})
	defer close(done)
	defer reader.Close()

	go func() {
		scanner := bufio.NewScanner(reader)
		scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
		for scanner.Scan() {
			line := append([]byte(nil), scanner.Bytes()...)
			select {
			case events <- notionStreamReadEvent{line: line}:
			case <-done:
				return
			}
		}
		if err := scanner.Err(); err != nil {
			select {
			case events <- notionStreamReadEvent{err: err}:
			case <-done:
			}
			return
		}
		select {
		case events <- notionStreamReadEvent{err: io.EOF}:
		case <-done:
		}
	}()

	var raw bytes.Buffer
	var bestText string
	var messageID string
	var idleTimer *time.Timer
	var idleC <-chan time.Time
	defer func() {
		if idleTimer != nil {
			idleTimer.Stop()
		}
	}()
	resetIdle := func() {
		if idleAfterAnswer <= 0 {
			return
		}
		if idleTimer == nil {
			idleTimer = time.NewTimer(idleAfterAnswer)
		} else {
			if !idleTimer.Stop() {
				select {
				case <-idleTimer.C:
				default:
				}
			}
			idleTimer.Reset(idleAfterAnswer)
		}
		idleC = idleTimer.C
	}

	for {
		select {
		case event := <-events:
			if event.err != nil {
				if bestText == "" {
					if text, id := extractNotionText(raw.Bytes(), threadID); text != "" {
						bestText = text
						messageID = firstNonEmpty(messageID, id)
					}
				}
				if event.err == io.EOF || (bestText != "" && state.hasTerminalAnswer()) {
					return bestText, messageID, raw.String(), nil
				}
				return "", "", raw.String(), event.err
			}

			raw.Write(event.line)
			raw.WriteByte('\n')
			line := cleanNotionStreamLine(string(event.line))
			if line == "" || line == "[DONE]" {
				continue
			}
			var payload any
			if err := json.Unmarshal([]byte(line), &payload); err != nil {
				continue
			}
			text, id := state.handlePayload(payload, threadID)
			if text = sanitizeNotionAssistantText(text); text != "" {
				bestText = text
				resetIdle()
			}
			if id != "" {
				messageID = id
			}
			if state.hasTerminalAnswer() {
				return bestText, messageID, raw.String(), nil
			}
		case <-idleC:
			return bestText, messageID, raw.String(), nil
		}
	}
}

func cleanNotionStreamLine(line string) string {
	line = strings.TrimSpace(line)
	if strings.HasPrefix(line, "data:") {
		line = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
	}
	return line
}

func (s *notionTranscriptState) handlePayload(value any, threadID string) (string, string) {
	typed := mapFromAny(value)
	if typed == nil {
		return s.result()
	}
	switch strings.TrimSpace(stringFromAny(typed["type"])) {
	case "markdown-chat":
		if text := stringFromAny(typed["value"]); text != "" {
			s.FinalText = text
			s.MessageID = stringFromAny(typed["id"])
			s.terminal = true
		}
	case "patch-start":
		s.applyPatchStart(mapFromAny(typed["data"]))
	case "patch":
		s.applyPatchOperations(sliceFromAny(typed["v"]))
	case "agent-inference":
		s.mergeAgentInference(typed)
	case "record-map":
		if text, id := finalAgentFromRecordMap(recordMapFromPayload(typed), threadID); text != "" {
			s.FinalText = text
			s.MessageID = id
			s.terminal = true
		}
	default:
		if text, id := finalAgentFromRecordMap(recordMapFromPayload(typed), threadID); text != "" {
			s.FinalText = text
			s.MessageID = id
			s.terminal = true
		}
	}
	return s.result()
}

func (s *notionTranscriptState) hasTerminalAnswer() bool {
	if s == nil || !s.terminal {
		return false
	}
	text, _ := s.result()
	return strings.TrimSpace(text) != ""
}

func (s *notionTranscriptState) result() (string, string) {
	text := sanitizeNotionAssistantText(s.FinalText)
	id := strings.TrimSpace(s.MessageID)
	if text == "" && s.ActiveAgentIndex >= 0 && s.ActiveAgentIndex < len(s.Steps) {
		step := s.Steps[s.ActiveAgentIndex]
		text = sanitizeNotionAssistantText(step.Text)
		if id == "" {
			id = step.ID
		}
	}
	return text, id
}

func (s *notionTranscriptState) ensurePatchMaps() {
	if s.patchValueTypes == nil {
		s.patchValueTypes = map[string]string{}
	}
	if s.patchValueText == nil {
		s.patchValueText = map[string]string{}
	}
	if s.patchValueCounts == nil {
		s.patchValueCounts = map[string]int{}
	}
}

func (s *notionTranscriptState) applyPatchStart(data map[string]any) {
	if data == nil {
		return
	}
	steps := sliceFromAny(data["s"])
	if len(steps) == 0 {
		return
	}
	s.Steps = s.Steps[:0]
	s.ActiveAgentIndex = -1
	for _, rawStep := range steps {
		step := s.stepStateFromMap(mapFromAny(rawStep))
		s.Steps = append(s.Steps, step)
		index := len(s.Steps) - 1
		s.registerStepValueTypes(index, mapFromAny(rawStep)["value"])
		if step.Type == "agent-inference" {
			s.ActiveAgentIndex = index
			if step.ID != "" {
				s.MessageID = step.ID
			}
			if step.Text != "" {
				s.FinalText = step.Text
			}
		}
	}
}

func (s *notionTranscriptState) stepStateFromMap(stepMap map[string]any) notionStepState {
	if stepMap == nil {
		return notionStepState{}
	}
	step := notionStepState{
		ID:   strings.TrimSpace(stringFromAny(stepMap["id"])),
		Type: strings.TrimSpace(stringFromAny(stepMap["type"])),
	}
	switch step.Type {
	case "agent-inference":
		step.Text = extractAssistantStepText(stepMap["value"])
		if step.Text != "" {
			step.Completed = true
		}
	case "markdown-chat":
		step.Text = strings.TrimSpace(stringFromAny(stepMap["value"]))
		if step.Text != "" {
			step.Completed = true
		}
	}
	return step
}

func (s *notionTranscriptState) applyPatchOperations(rawOps []any) {
	for _, rawOp := range rawOps {
		op := mapFromAny(rawOp)
		s.applyPatchOperation(
			strings.TrimSpace(stringFromAny(op["o"])),
			strings.TrimSpace(stringFromAny(op["p"])),
			op["v"],
		)
	}
}

func (s *notionTranscriptState) applyPatchOperation(opType string, path string, rawValue any) {
	switch opType {
	case "a":
		if path == "/s/-" {
			step := s.stepStateFromMap(mapFromAny(rawValue))
			s.Steps = append(s.Steps, step)
			index := len(s.Steps) - 1
			s.registerStepValueTypes(index, mapFromAny(rawValue)["value"])
			if step.Type == "agent-inference" || step.Type == "markdown-chat" {
				s.ActiveAgentIndex = index
				if step.ID != "" {
					s.MessageID = step.ID
				}
				if step.Text != "" {
					s.FinalText = step.Text
				}
			}
			return
		}
		index, rest, ok := parseNotionPatchStepIndex(path)
		if !ok || index < 0 || index >= len(s.Steps) {
			return
		}
		if text := stringFromAny(rawValue); text != "" && rest == "/value" {
			s.appendStepTextFromPatch(index, text)
			return
		}
		if strings.Contains(rest, "/value/-") {
			s.registerPatchValueAppend(path, rawValue)
			if s.Steps[index].Type == "agent-inference" {
				s.refreshAgentStepFromPatchState(index)
			} else if s.Steps[index].Type == "markdown-chat" {
				s.appendStepTextFromPatch(index, extractAssistantPartContent(rawValue))
			}
			return
		}
		if s.Steps[index].Type == "agent-inference" && rest == "/finishedAt" {
			s.Steps[index].Completed = true
			s.ActiveAgentIndex = index
			if text, _ := s.result(); text != "" {
				s.terminal = true
			}
			return
		}
		if s.Steps[index].Type == "agent-inference" {
			s.applyAgentPatchField(index, rest, opType, rawValue)
		}
	case "x", "p":
		index, rest, ok := parseNotionPatchStepIndex(path)
		if !ok || index < 0 {
			return
		}
		s.ensureStepIndex(index)
		if text := stringFromAny(rawValue); text != "" && (rest == "/value" || strings.HasPrefix(rest, "/value/")) {
			s.appendStepTextFromPatch(index, text)
			return
		}
		if index >= len(s.Steps) || s.Steps[index].Type != "agent-inference" {
			return
		}
		s.applyAgentPatchField(index, rest, opType, rawValue)
	case "r":
		index, rest, ok := parseNotionPatchStepIndex(path)
		if !ok || index < 0 || index >= len(s.Steps) || s.Steps[index].Type != "agent-inference" {
			return
		}
		if valueIndex, ok := parseNotionPatchValueRemovalIndex(rest); ok {
			s.removePatchValueEntry(index, valueIndex)
			s.refreshAgentStepFromPatchState(index)
		}
	}
}

func (s *notionTranscriptState) ensureStepIndex(index int) {
	for len(s.Steps) <= index {
		s.Steps = append(s.Steps, notionStepState{})
	}
}

func (s *notionTranscriptState) appendStepTextFromPatch(stepIndex int, text string) {
	text = strings.TrimSpace(text)
	if text == "" || stepIndex < 0 {
		return
	}
	s.ensureStepIndex(stepIndex)
	step := s.Steps[stepIndex]
	switch step.Type {
	case "", "agent-inference", "markdown-chat":
		if step.Type == "" {
			step.Type = "markdown-chat"
		}
	default:
		return
	}
	step.Text = appendAgentPatchDelta(step.Text, text)
	s.Steps[stepIndex] = step
	s.ActiveAgentIndex = stepIndex
	s.FinalText = step.Text
	if step.ID != "" {
		s.MessageID = step.ID
	}
	if step.Completed && strings.TrimSpace(step.Text) != "" {
		s.terminal = true
	}
}

func (s *notionTranscriptState) mergeAgentInference(event map[string]any) {
	stepID := strings.TrimSpace(stringFromAny(event["id"]))
	index := s.ensureAgentStep(stepID)
	step := s.Steps[index]
	for valueIndex, rawValue := range sliceFromAny(event["value"]) {
		value := notionAgentInferenceValue{
			Type:    strings.TrimSpace(stringFromAny(mapFromAny(rawValue)["type"])),
			Content: extractAssistantPartContent(mapFromAny(rawValue)),
		}
		s.mergeEventValueIntoPatchState(index, valueIndex, value)
		if normalizeNotionPatchEntryType(value.Type) == "text" {
			step.Text = mergeCumulativeAgentContent(step.Text, value.Content)
			s.FinalText = step.Text
		}
	}
	if event["finishedAt"] != nil {
		step.Completed = true
	}
	s.Steps[index] = step
	s.ActiveAgentIndex = index
	if step.ID != "" {
		s.MessageID = step.ID
	}
	if step.Completed && strings.TrimSpace(step.Text) != "" {
		s.terminal = true
	}
}

func (s *notionTranscriptState) ensureAgentStep(stepID string) int {
	for index, step := range s.Steps {
		if step.ID == stepID && stepID != "" {
			return index
		}
	}
	s.Steps = append(s.Steps, notionStepState{ID: stepID, Type: "agent-inference"})
	return len(s.Steps) - 1
}

func (s *notionTranscriptState) applyAgentPatchField(stepIndex int, rest string, opType string, rawValue any) {
	if entryKey, ok := notionPatchEntryKeyFromRest(stepIndex, rest, "/type"); ok {
		s.ensurePatchMaps()
		s.patchValueTypes[entryKey] = normalizeNotionPatchEntryType(stringFromAny(rawValue))
		s.refreshAgentStepFromPatchState(stepIndex)
		return
	}
	if entryKey, ok := notionPatchEntryKeyFromRest(stepIndex, rest, "/content"); ok {
		s.ensurePatchMaps()
		if content := extractAssistantPartContent(rawValue); content != "" {
			s.patchValueText[entryKey] = applyAgentPatchContent(s.patchValueText[entryKey], opType, content)
		}
		s.refreshAgentStepFromPatchState(stepIndex)
	}
}

func (s *notionTranscriptState) refreshAgentStepFromPatchState(stepIndex int) {
	if stepIndex < 0 || stepIndex >= len(s.Steps) {
		return
	}
	text, hasText := s.composeStepAgentText(stepIndex)
	if !hasText {
		return
	}
	step := s.Steps[stepIndex]
	step.Text = text
	s.Steps[stepIndex] = step
	s.ActiveAgentIndex = stepIndex
	s.FinalText = text
	if step.ID != "" {
		s.MessageID = step.ID
	}
}

func (s *notionTranscriptState) composeStepAgentText(stepIndex int) (string, bool) {
	s.ensurePatchMaps()
	statePrefix := notionPatchStatePrefix(stepIndex)
	count := s.patchValueCounts[statePrefix]
	if count <= 0 {
		return "", false
	}
	text := ""
	hasText := false
	for valueIndex := 0; valueIndex < count; valueIndex++ {
		entryKey := notionPatchStateEntryKey(statePrefix, valueIndex)
		if normalizeNotionPatchEntryType(s.patchValueTypes[entryKey]) != "text" {
			continue
		}
		content := s.patchValueText[entryKey]
		if content == "" {
			continue
		}
		text = combineAgentContentParts(text, content)
		hasText = true
	}
	return text, hasText
}

func (s *notionTranscriptState) mergeEventValueIntoPatchState(stepIndex int, valueIndex int, value notionAgentInferenceValue) {
	entryType := normalizeNotionPatchEntryType(value.Type)
	if entryType == "" {
		return
	}
	s.ensurePatchMaps()
	statePrefix := notionPatchStatePrefix(stepIndex)
	if s.patchValueCounts[statePrefix] <= valueIndex {
		s.patchValueCounts[statePrefix] = valueIndex + 1
	}
	entryKey := notionPatchStateEntryKey(statePrefix, valueIndex)
	s.patchValueTypes[entryKey] = entryType
	if strings.TrimSpace(value.Content) != "" {
		s.patchValueText[entryKey] = mergeCumulativeAgentContent(s.patchValueText[entryKey], value.Content)
	}
}

func (s *notionTranscriptState) registerStepValueTypes(stepIndex int, value any) {
	s.ensurePatchMaps()
	statePrefix := notionPatchStatePrefix(stepIndex)
	parts := sliceFromAny(value)
	s.patchValueCounts[statePrefix] = len(parts)
	for valueIndex, rawPart := range parts {
		part := mapFromAny(rawPart)
		partType := normalizeNotionPatchEntryType(stringFromAny(part["type"]))
		if partType == "" {
			continue
		}
		entryKey := notionPatchStateEntryKey(statePrefix, valueIndex)
		s.patchValueTypes[entryKey] = partType
		if content := extractAssistantPartContent(part); content != "" {
			s.patchValueText[entryKey] = mergeCumulativeAgentContent(s.patchValueText[entryKey], content)
		}
	}
}

func (s *notionTranscriptState) registerPatchValueAppend(path string, rawValue any) {
	valueIndexMarker := strings.Index(path, "/value/")
	if valueIndexMarker < 0 {
		return
	}
	s.ensurePatchMaps()
	statePrefix := path[:valueIndexMarker]
	part := mapFromAny(rawValue)
	partType := normalizeNotionPatchEntryType(stringFromAny(part["type"]))
	if partType == "" {
		return
	}
	valueIndex := s.patchValueCounts[statePrefix]
	s.patchValueCounts[statePrefix] = valueIndex + 1
	entryKey := notionPatchStateEntryKey(statePrefix, valueIndex)
	s.patchValueTypes[entryKey] = partType
	if content := extractAssistantPartContent(part); content != "" {
		s.patchValueText[entryKey] = mergeCumulativeAgentContent(s.patchValueText[entryKey], content)
	}
}

func (s *notionTranscriptState) removePatchValueEntry(stepIndex int, valueIndex int) {
	if valueIndex < 0 {
		return
	}
	s.ensurePatchMaps()
	statePrefix := notionPatchStatePrefix(stepIndex)
	count := s.patchValueCounts[statePrefix]
	if count <= 0 || valueIndex >= count {
		return
	}
	for idx := valueIndex; idx < count-1; idx++ {
		currentKey := notionPatchStateEntryKey(statePrefix, idx)
		nextKey := notionPatchStateEntryKey(statePrefix, idx+1)
		if nextType, ok := s.patchValueTypes[nextKey]; ok {
			s.patchValueTypes[currentKey] = nextType
		} else {
			delete(s.patchValueTypes, currentKey)
		}
		if nextText, ok := s.patchValueText[nextKey]; ok {
			s.patchValueText[currentKey] = nextText
		} else {
			delete(s.patchValueText, currentKey)
		}
	}
	lastKey := notionPatchStateEntryKey(statePrefix, count-1)
	delete(s.patchValueTypes, lastKey)
	delete(s.patchValueText, lastKey)
	s.patchValueCounts[statePrefix] = count - 1
}

func parseNotionPatchStepIndex(path string) (int, string, bool) {
	if !strings.HasPrefix(path, "/s/") {
		return 0, "", false
	}
	trimmed := strings.TrimPrefix(path, "/s/")
	parts := strings.SplitN(trimmed, "/", 2)
	if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
		return 0, "", false
	}
	index, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, "", false
	}
	rest := ""
	if len(parts) == 2 {
		rest = "/" + parts[1]
	}
	return index, rest, true
}

func parseNotionPatchValueRemovalIndex(rest string) (int, bool) {
	if !strings.HasPrefix(rest, "/value/") {
		return 0, false
	}
	tail := strings.TrimPrefix(rest, "/value/")
	if tail == "" || strings.Contains(tail, "/") {
		return 0, false
	}
	index, err := strconv.Atoi(tail)
	if err != nil || index < 0 {
		return 0, false
	}
	return index, true
}

func notionPatchStatePrefix(stepIndex int) string {
	return fmt.Sprintf("/s/%d", stepIndex)
}

func notionPatchStateEntryKey(statePrefix string, valueIndex int) string {
	return fmt.Sprintf("%s/value/%d", statePrefix, valueIndex)
}

func normalizeNotionPatchEntryType(value string) string {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "text":
		return "text"
	case "thinking", "reasoning":
		return "reasoning"
	default:
		return ""
	}
}

func notionPatchEntryKeyFromRest(stepIndex int, rest string, field string) (string, bool) {
	switch field {
	case "/content":
		contentIndex := strings.LastIndex(rest, field)
		if contentIndex < 0 {
			return "", false
		}
		entryPath := rest[:contentIndex]
		if entryPath == "" || !strings.Contains(entryPath, "/value/") {
			return "", false
		}
		return fmt.Sprintf("/s/%d%s", stepIndex, entryPath), true
	default:
		if !strings.HasSuffix(rest, field) {
			return "", false
		}
		entryPath := strings.TrimSuffix(rest, field)
		if entryPath == rest || !strings.Contains(entryPath, "/value/") {
			return "", false
		}
		return fmt.Sprintf("/s/%d%s", stepIndex, entryPath), true
	}
}

func recordMapFromPayload(payload map[string]any) map[string]any {
	if payload == nil {
		return nil
	}
	for _, key := range []string{"recordMap", "record_map", "recordmap"} {
		if recordMap := mapFromAny(payload[key]); recordMap != nil {
			return recordMap
		}
	}
	return nil
}

func finalAgentFromRecordMap(recordMap map[string]any, threadID string) (string, string) {
	if recordMap == nil {
		return "", ""
	}
	agents := extractAgentMessagesFromRecordMap(recordMap)
	if len(agents) == 0 {
		return "", ""
	}
	messageIDs := messageIDsFromRecordMap(recordMap, threadID)
	for i := len(messageIDs) - 1; i >= 0; i-- {
		if text := sanitizeNotionAssistantText(agents[messageIDs[i]]); text != "" {
			return text, messageIDs[i]
		}
	}
	keys := make([]string, 0, len(agents))
	for id := range agents {
		keys = append(keys, id)
	}
	sort.Strings(keys)
	for i := len(keys) - 1; i >= 0; i-- {
		if text := sanitizeNotionAssistantText(agents[keys[i]]); text != "" {
			return text, keys[i]
		}
	}
	return "", ""
}

func finalErrorFromRecordMap(recordMap map[string]any, threadID string) error {
	if recordMap == nil {
		return nil
	}
	messageIDs := messageIDsFromRecordMap(recordMap, threadID)
	if len(messageIDs) == 0 {
		return nil
	}
	agents := extractAgentMessagesFromRecordMap(recordMap)
	errorsByID := extractInferenceErrorsFromRecordMap(recordMap, threadID)
	var lastErr *NotionInferenceStepError
	haveOutcome := false
	for _, messageID := range messageIDs {
		if stepErr, ok := errorsByID[messageID]; ok {
			copyErr := stepErr
			lastErr = &copyErr
			haveOutcome = true
			continue
		}
		if text := sanitizeNotionAssistantText(agents[messageID]); text != "" {
			lastErr = nil
			haveOutcome = true
		}
	}
	if !haveOutcome {
		return nil
	}
	if lastErr != nil {
		return lastErr
	}
	return nil
}

func inferenceErrorFromRawNotionResponse(raw string, threadID string) error {
	for _, rawLine := range strings.Split(raw, "\n") {
		line := cleanNotionStreamLine(rawLine)
		if line == "" || line == "[DONE]" {
			continue
		}
		var payload any
		if err := json.Unmarshal([]byte(line), &payload); err != nil {
			continue
		}
		if item := mapFromAny(payload); strings.TrimSpace(stringFromAny(item["type"])) == "patch-start" {
			if stepErr := patchStartInferenceError(mapFromAny(item["data"]), threadID); stepErr != nil {
				return stepErr
			}
		}
		if stepErr := finalErrorFromRecordMap(recordMapFromPayload(mapFromAny(payload)), threadID); stepErr != nil {
			return stepErr
		}
	}
	if strings.TrimSpace(raw) != "" {
		var payload any
		if err := json.Unmarshal([]byte(raw), &payload); err == nil {
			if item := mapFromAny(payload); strings.TrimSpace(stringFromAny(item["type"])) == "patch-start" {
				if stepErr := patchStartInferenceError(mapFromAny(item["data"]), threadID); stepErr != nil {
					return stepErr
				}
			}
			return finalErrorFromRecordMap(recordMapFromPayload(mapFromAny(payload)), threadID)
		}
	}
	return nil
}

func patchStartInferenceError(data map[string]any, threadID string) error {
	for _, rawStep := range sliceFromAny(data["s"]) {
		step := mapFromAny(rawStep)
		stepType := strings.TrimSpace(stringFromAny(step["type"]))
		if stepType != "premium-feature-unavailable" {
			continue
		}
		availability := mapFromAny(step["featureAvailability"])
		limit := mapFromAny(availability["limit"])
		details := []string{"premium feature unavailable"}
		if limitType := strings.TrimSpace(stringFromAny(limit["type"])); limitType != "" {
			details = append(details, "limit_type="+limitType)
		}
		current := intFromAny(limit["current"])
		total := intFromAny(limit["total"])
		if current > 0 || total > 0 {
			details = append(details, fmt.Sprintf("current=%d total=%d", current, total))
		}
		return &NotionInferenceStepError{
			ThreadID:  strings.TrimSpace(threadID),
			MessageID: strings.TrimSpace(stringFromAny(step["id"])),
			Message:   strings.Join(details, " "),
			SubType:   stepType,
		}
	}
	return nil
}

func messageIDsFromRecordMap(recordMap map[string]any, threadID string) []string {
	if recordMap == nil || strings.TrimSpace(threadID) == "" {
		return nil
	}
	threadRecord := threadRecordFromRecordMap(recordMap, threadID)
	value := unwrapNotionRecordValue(threadRecord)
	rawMessages := sliceFromAny(value["messages"])
	if len(rawMessages) == 0 {
		rawMessages = sliceFromAny(value["messageIds"])
	}
	if len(rawMessages) == 0 {
		rawMessages = sliceFromAny(value["message_ids"])
	}
	out := make([]string, 0, len(rawMessages))
	for _, rawMessageID := range rawMessages {
		if messageID := strings.TrimSpace(stringFromAny(rawMessageID)); messageID != "" {
			out = append(out, messageID)
		}
	}
	if len(out) == 0 {
		for messageID := range mapFromAny(recordMap["thread_message"]) {
			if clean := strings.TrimSpace(messageID); clean != "" {
				out = append(out, clean)
			}
		}
		sort.Strings(out)
	}
	return out
}

func threadRecordFromRecordMap(recordMap map[string]any, threadID string) map[string]any {
	threadMap := mapFromAny(recordMap["thread"])
	if threadMap == nil {
		return nil
	}
	for _, candidate := range notionRecordIDCandidates(threadID) {
		if record := mapFromAny(threadMap[candidate]); record != nil {
			return record
		}
	}
	return nil
}

func notionRecordIDCandidates(id string) []string {
	clean := strings.TrimSpace(id)
	if clean == "" {
		return nil
	}
	lower := strings.ToLower(clean)
	dashless := strings.ReplaceAll(lower, "-", "")
	candidates := []string{clean, lower}
	if dashless != "" && dashless != lower {
		candidates = append(candidates, dashless)
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		out = append(out, candidate)
	}
	return out
}

func extractAgentMessagesFromRecordMap(recordMap map[string]any) map[string]string {
	out := map[string]string{}
	for messageID, rawItem := range mapFromAny(recordMap["thread_message"]) {
		value, _ := threadMessageRecordValue(rawItem)
		text := assistantTextFromStep(stepFromThreadMessageRecord(rawItem))
		if text == "" && isAssistantRecordValue(value) {
			text = assistantTextFromRecordValue(value)
		}
		if text == "" && isAssistantRecordValue(mapFromAny(rawItem)) {
			text = assistantTextFromRecordValue(mapFromAny(rawItem))
		}
		if text != "" {
			out[messageID] = text
		}
	}
	return out
}

func extractInferenceErrorsFromRecordMap(recordMap map[string]any, threadID string) map[string]NotionInferenceStepError {
	out := map[string]NotionInferenceStepError{}
	for messageID, rawItem := range mapFromAny(recordMap["thread_message"]) {
		value, _ := threadMessageRecordValue(rawItem)
		step := stepFromThreadMessageRecord(rawItem)
		if strings.TrimSpace(stringFromAny(step["type"])) != "error" {
			continue
		}
		data := mapFromAny(value["data"])
		retryable, _ := boolFromAny(step["isRetryable"])
		traceID := firstNonEmpty(strings.TrimSpace(stringFromAny(step["traceId"])), strings.TrimSpace(stringFromAny(data["inference_id"])))
		out[messageID] = NotionInferenceStepError{
			ThreadID:   strings.TrimSpace(threadID),
			MessageID:  strings.TrimSpace(messageID),
			Message:    strings.TrimSpace(stringFromAny(step["message"])),
			SubType:    strings.TrimSpace(stringFromAny(step["subType"])),
			TraceID:    traceID,
			Retryable:  retryable,
			StackTrace: strings.TrimSpace(stringFromAny(step["stack"])),
		}
	}
	return out
}

func unwrapNotionRecordValue(item map[string]any) map[string]any {
	if item == nil {
		return nil
	}
	if step := mapFromAny(item["step"]); step != nil {
		return item
	}
	valueWrapper := mapFromAny(item["value"])
	if step := mapFromAny(valueWrapper["step"]); step != nil {
		return valueWrapper
	}
	if value := mapFromAny(valueWrapper["value"]); value != nil {
		return value
	}
	return item
}

func threadMessageRecordValue(rawItem any) (map[string]any, map[string]any) {
	item := mapFromAny(rawItem)
	if item == nil {
		return nil, nil
	}
	valueWrapper := mapFromAny(item["value"])
	if value := mapFromAny(valueWrapper["value"]); value != nil {
		return value, valueWrapper
	}
	if valueWrapper != nil {
		return valueWrapper, valueWrapper
	}
	return item, nil
}

func stepFromThreadMessageRecord(rawItem any) map[string]any {
	value, _ := threadMessageRecordValue(rawItem)
	if value == nil {
		return nil
	}
	if step := mapFromAny(value["step"]); step != nil {
		return step
	}
	if nested := mapFromAny(value["value"]); nested != nil {
		if step := mapFromAny(nested["step"]); step != nil {
			return step
		}
	}
	if stepType := strings.TrimSpace(stringFromAny(value["type"])); stepType != "" && value["value"] != nil {
		return value
	}
	return nil
}

func assistantTextFromStep(step map[string]any) string {
	if step == nil {
		return ""
	}
	stepType := strings.TrimSpace(stringFromAny(step["type"]))
	switch stepType {
	case "agent-inference":
		return extractAssistantStepText(step["value"])
	case "markdown-chat":
		return firstNonEmpty(strings.TrimSpace(stringFromAny(step["value"])), extractAssistantStepText(step["value"]))
	default:
		if !isAssistantStepType(stepType) {
			return ""
		}
		for _, key := range []string{"value", "content", "text", "message", "output"} {
			if text := firstNonEmpty(extractAssistantStepText(step[key]), extractAssistantPartContent(step[key])); text != "" {
				return text
			}
		}
	}
	return ""
}

func isAssistantStepType(stepType string) bool {
	normalized := strings.ToLower(strings.TrimSpace(strings.ReplaceAll(stepType, "_", "-")))
	if normalized == "" {
		return false
	}
	switch normalized {
	case "agent-inference", "markdown-chat", "assistant", "assistant-message", "assistant-response", "ai-response", "ai-message":
		return true
	default:
		return strings.Contains(normalized, "assistant") || strings.Contains(normalized, "agent")
	}
}

func isAssistantRecordValue(value map[string]any) bool {
	if value == nil {
		return false
	}
	role := strings.ToLower(strings.TrimSpace(firstNonEmpty(stringFromAny(value["role"]), stringFromAny(value["author_role"]))))
	if role == "assistant" || role == "agent" {
		return true
	}
	return isAssistantStepType(stringFromAny(value["type"]))
}

func assistantTextFromRecordValue(value map[string]any) string {
	if value == nil {
		return ""
	}
	for _, key := range []string{"content", "text", "message", "output", "value"} {
		if text := firstNonEmpty(extractAssistantStepText(value[key]), extractAssistantPartContent(value[key])); text != "" {
			return text
		}
	}
	return ""
}

func extractAssistantStepText(value any) string {
	if parts := sliceFromAny(value); len(parts) > 0 {
		text := ""
		for _, rawPart := range parts {
			part := mapFromAny(rawPart)
			if normalizeNotionPatchEntryType(stringFromAny(part["type"])) != "text" {
				continue
			}
			text = combineAgentContentParts(text, extractAssistantPartContent(part))
		}
		return strings.TrimSpace(text)
	}
	if wrapper := mapFromAny(value); wrapper != nil {
		if parts := sliceFromAny(wrapper["value"]); len(parts) > 0 {
			return extractAssistantStepText(parts)
		}
		if normalizeNotionPatchEntryType(stringFromAny(wrapper["type"])) == "reasoning" {
			return ""
		}
	}
	return strings.TrimSpace(extractAssistantPartContent(value))
}

func extractAssistantPartContent(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	}
	parts := []string{}
	collectAssistantContentLeaves(value, &parts)
	if len(parts) == 0 {
		return ""
	}
	return strings.TrimSpace(strings.Join(parts, ""))
}

func collectAssistantContentLeaves(value any, out *[]string) {
	switch typed := value.(type) {
	case string:
		if typed != "" {
			*out = append(*out, typed)
		}
	case []any:
		for _, item := range typed {
			collectAssistantContentLeaves(item, out)
		}
	case []string:
		for _, item := range typed {
			collectAssistantContentLeaves(item, out)
		}
	case map[string]any:
		for _, key := range []string{"content", "text", "title", "label", "value"} {
			if nested, ok := typed[key]; ok {
				collectAssistantContentLeaves(nested, out)
			}
		}
	}
}

func mergeCumulativeAgentContent(existing string, next string) string {
	if next == "" {
		return existing
	}
	if existing == "" || next == existing {
		return next
	}
	existingClean := sanitizeNotionAssistantText(existing)
	nextClean := sanitizeNotionAssistantText(next)
	switch {
	case nextClean != "" && nextClean == existingClean:
		return existing
	case nextClean != "" && (existingClean == "" || strings.HasPrefix(nextClean, existingClean)):
		return next
	case existingClean != "" && strings.HasPrefix(existingClean, nextClean):
		return existing
	case strings.HasPrefix(next, existing):
		return next
	case strings.HasPrefix(existing, next):
		return existing
	default:
		return next
	}
}

func combineAgentContentParts(existing string, next string) string {
	if next == "" {
		return existing
	}
	if existing == "" {
		return next
	}
	switch {
	case strings.HasPrefix(next, existing):
		return next
	case strings.HasPrefix(existing, next):
		return existing
	default:
		return existing + next
	}
}

func applyAgentPatchContent(existing string, op string, patch string) string {
	switch op {
	case "x":
		return appendAgentPatchDelta(existing, patch)
	case "p":
		return handleAgentPatchReplace(existing, patch)
	default:
		return mergeCumulativeAgentContent(existing, patch)
	}
}

func appendAgentPatchDelta(existing string, delta string) string {
	if delta == "" {
		return existing
	}
	if existing == "" {
		return delta
	}
	existingClean := sanitizeNotionAssistantText(existing)
	deltaClean := sanitizeNotionAssistantText(delta)
	switch {
	case strings.HasSuffix(existing, delta):
		return existing
	case deltaClean != "" && strings.HasSuffix(existingClean, deltaClean):
		return existing
	case strings.HasPrefix(delta, existing):
		return delta
	case deltaClean != "" && existingClean != "" && strings.HasPrefix(deltaClean, existingClean):
		return delta
	default:
		return existing + delta
	}
}

func handleAgentPatchReplace(current string, replacement string) string {
	if replacement == "" {
		return current
	}
	if current == "" || replacement == current {
		return replacement
	}
	if idx := strings.LastIndex(current, "<lang"); idx >= 0 {
		return current[:idx] + replacement
	}
	currentClean := sanitizeNotionAssistantText(current)
	replacementClean := sanitizeNotionAssistantText(replacement)
	if strings.HasPrefix(replacement, current) {
		return replacement
	}
	if replacementClean != "" && currentClean != "" && strings.HasPrefix(replacementClean, currentClean) {
		return replacement
	}
	return current + replacement
}

func sanitizeNotionAssistantText(text string) string {
	clean := strings.TrimSpace(strings.TrimPrefix(text, "\uFEFF"))
	for {
		lower := strings.ToLower(clean)
		if !strings.HasPrefix(lower, "<lang") {
			break
		}
		end := strings.Index(clean, ">")
		if end < 0 {
			return ""
		}
		clean = strings.TrimSpace(clean[end+1:])
	}
	return clean
}

func extractNotionConversationMessage(messageID string, rawItem any) (dto.NotionConversationMessageDTO, bool) {
	value, valueWrapper := threadMessageRecordValue(rawItem)
	step := stepFromThreadMessageRecord(rawItem)
	if step == nil {
		return dto.NotionConversationMessageDTO{}, false
	}
	data := mapFromAny(value["data"])
	createdAt := firstNonZeroTime(
		timeFromTranscriptValue(valueWrapper["created_time"]),
		timeFromTranscriptValue(valueWrapper["created_at"]),
		timeFromTranscriptValue(step["createdAt"]),
		timeFromTranscriptValue(data["completed_time"]),
	)
	updatedAt := firstNonZeroTime(
		timeFromTranscriptValue(valueWrapper["last_edited_time"]),
		timeFromTranscriptValue(valueWrapper["updated_at"]),
		timeFromTranscriptValue(data["completed_time"]),
		createdAt,
	)
	stepType := strings.TrimSpace(stringFromAny(step["type"]))
	switch stepType {
	case "user":
		return dto.NotionConversationMessageDTO{
			ID:        strings.TrimSpace(messageID),
			Role:      "user",
			Status:    "completed",
			Content:   extractUserStepText(step["value"]),
			CreatedAt: timePtrOrNil(createdAt),
			UpdatedAt: timePtrOrNil(updatedAt),
		}, true
	case "agent-inference":
		completed, _ := boolFromAny(data["completed"])
		status := "streaming"
		if completed {
			status = "completed"
		}
		return dto.NotionConversationMessageDTO{
			ID:        strings.TrimSpace(messageID),
			Role:      "assistant",
			Status:    status,
			Content:   sanitizeNotionAssistantText(assistantTextFromStep(step)),
			CreatedAt: timePtrOrNil(createdAt),
			UpdatedAt: timePtrOrNil(updatedAt),
		}, true
	case "markdown-chat":
		return dto.NotionConversationMessageDTO{
			ID:        strings.TrimSpace(messageID),
			Role:      "assistant",
			Status:    "completed",
			Content:   sanitizeNotionAssistantText(assistantTextFromStep(step)),
			CreatedAt: timePtrOrNil(createdAt),
			UpdatedAt: timePtrOrNil(updatedAt),
		}, true
	case "attachment":
		attachment := dto.NotionConversationAttachmentDTO{
			Name:        stringFromAny(step["fileName"]),
			ContentType: stringFromAny(step["contentType"]),
			Source:      "notion",
			URL:         stringFromAny(step["fileUrl"]),
		}
		return dto.NotionConversationMessageDTO{
			ID:          strings.TrimSpace(messageID),
			Role:        "user",
			Status:      "completed",
			CreatedAt:   timePtrOrNil(createdAt),
			UpdatedAt:   timePtrOrNil(updatedAt),
			Attachments: []dto.NotionConversationAttachmentDTO{attachment},
		}, true
	default:
		if isAssistantStepType(stepType) {
			return dto.NotionConversationMessageDTO{
				ID:        strings.TrimSpace(messageID),
				Role:      "assistant",
				Status:    "completed",
				Content:   sanitizeNotionAssistantText(assistantTextFromStep(step)),
				CreatedAt: timePtrOrNil(createdAt),
				UpdatedAt: timePtrOrNil(updatedAt),
			}, true
		}
		return dto.NotionConversationMessageDTO{}, false
	}
}

func extractUserStepText(value any) string {
	lines := []string{}
	collectStringLeaves(value, &lines)
	if len(lines) == 0 {
		return ""
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func collectStringLeaves(value any, out *[]string) {
	switch typed := value.(type) {
	case string:
		if clean := strings.TrimSpace(typed); clean != "" {
			*out = append(*out, clean)
		}
	case []any:
		for _, item := range typed {
			collectStringLeaves(item, out)
		}
	case []string:
		for _, item := range typed {
			collectStringLeaves(item, out)
		}
	case map[string]any:
		for _, key := range []string{"content", "text", "title", "label", "value"} {
			if nested, ok := typed[key]; ok {
				collectStringLeaves(nested, out)
			}
		}
	}
}

func timeFromTranscriptValue(value any) time.Time {
	switch typed := value.(type) {
	case time.Time:
		if typed.IsZero() {
			return time.Time{}
		}
		return typed.UTC()
	case int64:
		if typed > 0 {
			return time.UnixMilli(typed).UTC()
		}
	case int:
		if typed > 0 {
			return time.UnixMilli(int64(typed)).UTC()
		}
	case float64:
		if typed > 0 {
			return time.UnixMilli(int64(typed)).UTC()
		}
	case json.Number:
		if parsed, err := typed.Int64(); err == nil && parsed > 0 {
			return time.UnixMilli(parsed).UTC()
		}
	case string:
		clean := strings.TrimSpace(typed)
		if clean == "" {
			return time.Time{}
		}
		if parsedInt, err := strconv.ParseInt(clean, 10, 64); err == nil && parsedInt > 0 {
			return time.UnixMilli(parsedInt).UTC()
		}
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05.000Z07:00"} {
			if parsed, err := time.Parse(layout, clean); err == nil {
				return parsed.UTC()
			}
		}
	}
	return time.Time{}
}

func firstNonZeroTime(values ...time.Time) time.Time {
	for _, value := range values {
		if !value.IsZero() {
			return value.UTC()
		}
	}
	return time.Time{}
}

func timePtrOrNil(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	clean := value.UTC()
	return &clean
}

func notionConversationSortTime(item dto.NotionConversationSummaryDTO) time.Time {
	if item.UpdatedAt != nil {
		return item.UpdatedAt.UTC()
	}
	if item.CreatedAt != nil {
		return item.CreatedAt.UTC()
	}
	return time.Time{}
}

func notionConversationMessageTimes(messages []dto.NotionConversationMessageDTO) (time.Time, time.Time) {
	var createdAt time.Time
	var updatedAt time.Time
	for _, message := range messages {
		if message.CreatedAt != nil {
			createdAt = mergeConversationTime(createdAt, *message.CreatedAt, false)
		}
		if message.UpdatedAt != nil {
			updatedAt = mergeConversationTime(updatedAt, *message.UpdatedAt, true)
		}
	}
	if updatedAt.IsZero() {
		updatedAt = createdAt
	}
	return createdAt, updatedAt
}

func mergeConversationTime(current time.Time, candidate time.Time, preferLatest bool) time.Time {
	if candidate.IsZero() {
		return current.UTC()
	}
	if current.IsZero() {
		return candidate.UTC()
	}
	if preferLatest {
		if candidate.After(current) {
			return candidate.UTC()
		}
		return current.UTC()
	}
	if candidate.Before(current) {
		return candidate.UTC()
	}
	return current.UTC()
}

func notionConversationTitle(messages []dto.NotionConversationMessageDTO) string {
	for _, message := range messages {
		if strings.TrimSpace(message.Role) != "user" {
			continue
		}
		if title := truncateRunes(collapseWhitespace(message.Content), 72); title != "" {
			return title
		}
		if len(message.Attachments) > 0 {
			names := make([]string, 0, 2)
			for index, attachment := range message.Attachments {
				if index >= 2 {
					break
				}
				name := firstNonEmpty(attachment.Name, fmt.Sprintf("attachment-%d", index+1))
				names = append(names, name)
			}
			return truncateRunes("Attachment - "+strings.Join(names, ", "), 72)
		}
	}
	return "Untitled conversation"
}

func notionConversationPreview(messages []dto.NotionConversationMessageDTO) string {
	for index := len(messages) - 1; index >= 0; index-- {
		if preview := truncateRunes(collapseWhitespace(messages[index].Content), 180); preview != "" {
			return preview
		}
	}
	return ""
}

func collapseWhitespace(text string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
}

func truncateRunes(text string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= limit {
		return string(runes)
	}
	if limit <= 1 {
		return string(runes[:limit])
	}
	return strings.TrimSpace(string(runes[:limit-1])) + "..."
}

func findTextInNotionPayload(value any, threadID string) (string, string) {
	switch typed := value.(type) {
	case map[string]any:
		if text, id := finalAgentFromRecordMap(recordMapFromPayload(typed), threadID); text != "" {
			return text, id
		}
		if strings.TrimSpace(stringFromAny(typed["type"])) == "markdown-chat" {
			if text := stringFromAny(typed["value"]); text != "" {
				return text, stringFromAny(typed["id"])
			}
		}
		if strings.TrimSpace(stringFromAny(typed["type"])) == "agent-inference" {
			if text := extractAssistantStepText(typed["value"]); text != "" {
				return text, stringFromAny(typed["id"])
			}
		}
		if role := stringFromAny(typed["role"]); role == "assistant" || role == "agent" {
			if content := extractAssistantPartContent(typed["content"]); content != "" {
				return content, stringFromAny(typed["id"])
			}
		}
		if text := stringFromAny(typed["text"]); text != "" {
			return text, stringFromAny(typed["id"])
		}
		for _, key := range []string{"agent", "message", "thread_message", "recordMap", "record_map", "state", "result", "output", "content"} {
			if child, ok := typed[key]; ok {
				if text, id := findTextInNotionPayload(child, threadID); text != "" {
					if id == "" {
						id = stringFromAny(typed["id"])
					}
					return text, id
				}
			}
		}
		for _, child := range typed {
			if text, id := findTextInNotionPayload(child, threadID); text != "" {
				return text, id
			}
		}
	case []any:
		for _, child := range typed {
			if text, id := findTextInNotionPayload(child, threadID); text != "" {
				return text, id
			}
		}
	}
	return "", ""
}

func summarizeNotionRawResponse(raw string, threadID string) map[string]any {
	summary := map[string]any{
		"bytes": len(raw),
	}
	lines := strings.Split(raw, "\n")
	lineCount := 0
	types := map[string]int{}
	firstObjects := make([]map[string]any, 0, 5)
	for _, rawLine := range lines {
		line := cleanNotionStreamLine(rawLine)
		if line == "" || line == "[DONE]" {
			continue
		}
		lineCount++
		var payload any
		if err := json.Unmarshal([]byte(line), &payload); err != nil {
			continue
		}
		item := mapFromAny(payload)
		if item == nil {
			continue
		}
		typ := firstNonEmpty(stringFromAny(item["type"]), stringFromAny(item["event"]), stringFromAny(item["kind"]), "(missing)")
		types[typ]++
		if len(firstObjects) < 5 {
			firstObjects = append(firstObjects, summarizeNotionObject(item, threadID))
		}
	}
	summary["lines"] = lineCount
	summary["types"] = types
	summary["first_objects"] = firstObjects
	if lineCount == 0 && strings.TrimSpace(raw) != "" {
		var payload any
		if err := json.Unmarshal([]byte(raw), &payload); err == nil {
			summary["single_object"] = summarizeNotionObject(mapFromAny(payload), threadID)
		}
	}
	return summary
}

func messageIDsFromRawNotionResponse(raw string, threadID string) []string {
	seen := map[string]struct{}{}
	var out []string
	appendID := func(messageID string) {
		messageID = strings.TrimSpace(messageID)
		if messageID == "" {
			return
		}
		if _, ok := seen[messageID]; ok {
			return
		}
		seen[messageID] = struct{}{}
		out = append(out, messageID)
	}
	for _, rawLine := range strings.Split(raw, "\n") {
		line := cleanNotionStreamLine(rawLine)
		if line == "" || line == "[DONE]" {
			continue
		}
		var payload any
		if err := json.Unmarshal([]byte(line), &payload); err != nil {
			continue
		}
		for _, messageID := range messageIDsFromRecordMap(recordMapFromPayload(mapFromAny(payload)), threadID) {
			appendID(messageID)
		}
	}
	if len(out) == 0 && strings.TrimSpace(raw) != "" {
		var payload any
		if err := json.Unmarshal([]byte(raw), &payload); err == nil {
			for _, messageID := range messageIDsFromRecordMap(recordMapFromPayload(mapFromAny(payload)), threadID) {
				appendID(messageID)
			}
		}
	}
	return out
}

func summarizeNotionObject(item map[string]any, threadID string) map[string]any {
	out := map[string]any{}
	if item == nil {
		return out
	}
	for _, key := range []string{"type", "event", "kind", "id", "role", "status", "name"} {
		if value := stringFromAny(item[key]); value != "" {
			out[key] = value
		}
	}
	if recordMap := recordMapFromPayload(item); recordMap != nil {
		out["record_map_keys"] = sortedMapKeys(recordMap)
		messageIDs := messageIDsFromRecordMap(recordMap, threadID)
		out["message_ids"] = messageIDs
		out["thread_message_shapes"] = summarizeThreadMessageShapes(recordMap, messageIDs)
	}
	if data := mapFromAny(item["data"]); data != nil {
		out["data_keys"] = sortedMapKeys(data)
	}
	if value := item["value"]; value != nil {
		out["value_shape"] = notionValueShape(value)
	}
	if content := item["content"]; content != nil {
		out["content_shape"] = notionValueShape(content)
	}
	return out
}

func summarizeThreadMessageShapes(recordMap map[string]any, messageIDs []string) []map[string]any {
	threadMessages := mapFromAny(recordMap["thread_message"])
	if len(threadMessages) == 0 {
		return nil
	}
	ids := append([]string(nil), messageIDs...)
	if len(ids) == 0 {
		for messageID := range threadMessages {
			ids = append(ids, messageID)
		}
		sort.Strings(ids)
	}
	const maxShapes = 8
	if len(ids) > maxShapes {
		ids = ids[:maxShapes]
	}
	shapes := make([]map[string]any, 0, len(ids))
	for _, messageID := range ids {
		item := mapFromAny(threadMessages[messageID])
		value, valueWrapper := threadMessageRecordValue(item)
		step := stepFromThreadMessageRecord(item)
		shape := map[string]any{
			"id":                   messageID,
			"item_keys":            sortedMapKeys(item),
			"value_wrapper_keys":   sortedMapKeys(valueWrapper),
			"record_value_keys":    sortedMapKeys(value),
			"record_type":          stringFromAny(value["type"]),
			"record_role":          stringFromAny(value["role"]),
			"record_author_role":   stringFromAny(value["author_role"]),
			"record_status":        stringFromAny(value["status"]),
			"record_content_shape": notionValueShape(value["content"]),
			"record_text_shape":    notionValueShape(value["text"]),
			"record_output_shape":  notionValueShape(value["output"]),
			"record_value_shape":   notionValueShape(value["value"]),
		}
		if step != nil {
			shape["step_keys"] = sortedMapKeys(step)
			shape["step_type"] = stringFromAny(step["type"])
			shape["step_role"] = stringFromAny(step["role"])
			shape["step_content_shape"] = notionValueShape(step["content"])
			shape["step_text_shape"] = notionValueShape(step["text"])
			shape["step_output_shape"] = notionValueShape(step["output"])
			shape["step_value_shape"] = notionValueShape(step["value"])
		}
		shapes = append(shapes, shape)
	}
	return shapes
}

func sortedMapKeys(item map[string]any) []string {
	if item == nil {
		return nil
	}
	keys := make([]string, 0, len(item))
	for key := range item {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func notionValueShape(value any) string {
	switch typed := value.(type) {
	case nil:
		return "nil"
	case string:
		return fmt.Sprintf("string:%d", len(typed))
	case []any:
		return fmt.Sprintf("array:%d", len(typed))
	case map[string]any:
		return "object:" + strings.Join(sortedMapKeys(typed), ",")
	default:
		return fmt.Sprintf("%T", value)
	}
}

func sliceFromAny(value any) []any {
	switch typed := value.(type) {
	case []any:
		return typed
	case []map[string]any:
		out := make([]any, len(typed))
		for index, item := range typed {
			out[index] = item
		}
		return out
	case []string:
		out := make([]any, len(typed))
		for index, item := range typed {
			out[index] = item
		}
		return out
	default:
		return nil
	}
}

func stringFromAny(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case json.Number:
		return strings.TrimSpace(typed.String())
	case float64:
		return strings.TrimSpace(strconv.FormatFloat(typed, 'f', -1, 64))
	case int64:
		return strconv.FormatInt(typed, 10)
	case int:
		return strconv.Itoa(typed)
	default:
		return ""
	}
}

func intFromAny(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		parsed, _ := typed.Int64()
		return int(parsed)
	case string:
		parsed, _ := strconv.Atoi(strings.TrimSpace(typed))
		return parsed
	default:
		return 0
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
