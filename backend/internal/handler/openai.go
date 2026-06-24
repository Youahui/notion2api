package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"notion2api/backend/internal/dto"
	"notion2api/backend/internal/model"
	"notion2api/backend/internal/pkg/requestctx"
	"notion2api/backend/internal/pkg/respond"
	"notion2api/backend/internal/service"

	"github.com/gin-gonic/gin"
)

func (h *Handler) OpenAIModels(c *gin.Context) {
	payload, err := h.openai.Models(c.Request.Context())
	if err != nil {
		handleOpenAIError(c, err)
		return
	}
	respond.JSON(c, http.StatusOK, payload)
}

func (h *Handler) OpenAIModel(c *gin.Context) {
	row, err := h.openai.Model(c.Request.Context(), c.Param("id"))
	if err != nil {
		handleOpenAIError(c, err)
		return
	}
	respond.JSON(c, http.StatusOK, row)
}

func (h *Handler) ChatCompletions(c *gin.Context) {
	start := time.Now()
	var req dto.ChatCompletionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, service.OpenAIError("invalid request body", "invalid_request_error", "invalid_json"))
		return
	}
	apiKey, _ := requestctx.APIKey(c)
	result, err := h.openai.ChatCompletion(c.Request.Context(), req, openAIRequestOptions(c, apiKey))
	if err != nil {
		handleOpenAIError(c, err)
		return
	}
	if req.Stream {
		h.streamChatCompletion(c, result)
	} else {
		c.JSON(http.StatusOK, result.Payload)
	}
	_ = h.keys.RecordUsage(c.Request.Context(), service.UsageRecord{
		APIKeyID:         apiKey.ID,
		Method:           c.Request.Method,
		Route:            "/v1/chat/completions",
		StatusCode:       http.StatusOK,
		LatencyMs:        time.Since(start).Milliseconds(),
		Model:            result.Model,
		PromptTokens:     result.Usage.PromptTokens,
		CompletionTokens: result.Usage.CompletionTokens,
		TotalTokens:      result.Usage.TotalTokens,
		Estimated:        result.Usage.Estimated,
	})
}

func (h *Handler) Responses(c *gin.Context) {
	start := time.Now()
	var body map[string]any
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, service.OpenAIError("invalid request body", "invalid_request_error", "invalid_json"))
		return
	}
	apiKey, _ := requestctx.APIKey(c)
	if stream, _ := body["stream"].(bool); stream {
		streamWriter := newLiveResponsesStreamWriter(c)
		result, streamed, err := h.openai.ResponseStream(c.Request.Context(), body, streamWriter.Start, streamWriter.Delta, openAIRequestOptions(c, apiKey))
		if err != nil {
			if streamed || streamWriter.Started() {
				streamWriter.Error(err)
				return
			}
			handleOpenAIError(c, err)
			return
		}
		if streamed || streamWriter.Started() {
			streamWriter.Finish(result)
		} else {
			h.streamResponse(c, result)
		}
		_ = h.keys.RecordUsage(c.Request.Context(), service.UsageRecord{
			APIKeyID:         apiKey.ID,
			Method:           c.Request.Method,
			Route:            "/v1/responses",
			StatusCode:       http.StatusOK,
			LatencyMs:        time.Since(start).Milliseconds(),
			Model:            result.Model,
			PromptTokens:     result.Usage.PromptTokens,
			CompletionTokens: result.Usage.CompletionTokens,
			TotalTokens:      result.Usage.TotalTokens,
			Estimated:        result.Usage.Estimated,
		})
		return
	}
	result, err := h.openai.Response(c.Request.Context(), body, openAIRequestOptions(c, apiKey))
	if err != nil {
		handleOpenAIError(c, err)
		return
	}
	c.JSON(http.StatusOK, result.Payload)
	_ = h.keys.RecordUsage(c.Request.Context(), service.UsageRecord{
		APIKeyID:         apiKey.ID,
		Method:           c.Request.Method,
		Route:            "/v1/responses",
		StatusCode:       http.StatusOK,
		LatencyMs:        time.Since(start).Milliseconds(),
		Model:            result.Model,
		PromptTokens:     result.Usage.PromptTokens,
		CompletionTokens: result.Usage.CompletionTokens,
		TotalTokens:      result.Usage.TotalTokens,
		Estimated:        result.Usage.Estimated,
	})
}

func (h *Handler) ResponseByID(c *gin.Context) {
	apiKey, _ := requestctx.APIKey(c)
	payload, err := h.openai.ResponseByID(c.Request.Context(), c.Param("id"), openAIRequestOptions(c, apiKey))
	if err != nil {
		handleResponseStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, payload)
}

func (h *Handler) ResponseDelete(c *gin.Context) {
	apiKey, _ := requestctx.APIKey(c)
	payload, err := h.openai.DeleteResponse(c.Request.Context(), c.Param("id"), openAIRequestOptions(c, apiKey))
	if err != nil {
		handleResponseStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, payload)
}

func (h *Handler) ResponseInputItems(c *gin.Context) {
	apiKey, _ := requestctx.APIKey(c)
	limit := 0
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 {
			c.JSON(http.StatusBadRequest, service.OpenAIError("invalid limit", "invalid_request_error", "invalid_limit"))
			return
		}
		limit = parsed
	}
	payload, err := h.openai.ResponseInputItems(c.Request.Context(), c.Param("id"), limit, c.Query("after"), openAIRequestOptions(c, apiKey))
	if err != nil {
		handleResponseStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, payload)
}

func openAIRequestOptions(c *gin.Context, apiKey model.APIKey) service.OpenAIRequestOptions {
	return service.OpenAIRequestOptions{
		APIKeyID:             apiKey.ID,
		GroupID:              apiKey.GroupID,
		RequestID:            c.GetString("request_id"),
		HeaderSessionID:      firstOpenAIHeader(c, "session_id", "Session-Id", "X-Session-Id"),
		HeaderConversationID: firstOpenAIHeader(c, "conversation_id", "Conversation-Id", "X-Conversation-Id"),
	}
}

func firstOpenAIHeader(c *gin.Context, names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(c.GetHeader(name)); value != "" {
			return value
		}
	}
	return ""
}

func handleResponseStoreError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrNotFound):
		c.JSON(http.StatusNotFound, service.OpenAIError("response not found", "invalid_request_error", "response_not_found"))
	case errors.Is(err, service.ErrInvalid):
		c.JSON(http.StatusBadRequest, service.OpenAIError("invalid response id", "invalid_request_error", "invalid_response_id"))
	default:
		handleOpenAIError(c, err)
	}
}

func (h *Handler) AdminModels(c *gin.Context) {
	rows, err := h.models.ListAdmin(c.Request.Context())
	if err != nil {
		handleServiceError(c, err)
		return
	}
	respond.JSON(c, http.StatusOK, gin.H{"data": rows})
}

func (h *Handler) streamChatCompletion(c *gin.Context, result service.CompletionResult) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Status(http.StatusOK)

	if toolCalls := chatCompletionToolCalls(result.Payload); len(toolCalls) > 0 {
		chunk := map[string]any{
			"id":      result.Payload["id"],
			"object":  "chat.completion.chunk",
			"created": time.Now().Unix(),
			"model":   result.Model,
			"choices": []map[string]any{
				{
					"index": 0,
					"delta": map[string]any{
						"role":       "assistant",
						"tool_calls": toolCalls,
					},
					"finish_reason": "tool_calls",
				},
			},
		}
		encoded, _ := json.Marshal(chunk)
		writeSSE(c.Writer, "", encoded)
		writeSSEDone(c.Writer)
		return
	}

	chunk := map[string]any{
		"id":      result.Payload["id"],
		"object":  "chat.completion.chunk",
		"created": time.Now().Unix(),
		"model":   result.Model,
		"choices": []map[string]any{
			{
				"index": 0,
				"delta": map[string]any{
					"role":    "assistant",
					"content": result.Text,
				},
				"finish_reason": "stop",
			},
		},
	}
	encoded, _ := json.Marshal(chunk)
	writeSSE(c.Writer, "", encoded)
	writeSSEDone(c.Writer)
}

func chatCompletionToolCalls(payload map[string]any) []any {
	for _, rawChoice := range handlerSliceFromAny(payload["choices"]) {
		choice := handlerMapFromAny(rawChoice)
		message := handlerMapFromAny(choice["message"])
		if calls := handlerSliceFromAny(message["tool_calls"]); len(calls) > 0 {
			return calls
		}
	}
	return nil
}

func handlerMapFromAny(value any) map[string]any {
	if typed, ok := value.(map[string]any); ok {
		return typed
	}
	return nil
}

func handlerSliceFromAny(value any) []any {
	switch typed := value.(type) {
	case []any:
		return typed
	case []map[string]any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, item)
		}
		return out
	default:
		return nil
	}
}

type liveResponsesStreamWriter struct {
	c              *gin.Context
	requestID      string
	responseID     string
	outputItemID   string
	model          string
	createdAt      int64
	sequenceNumber int
	started        bool
	text           strings.Builder
}

func newLiveResponsesStreamWriter(c *gin.Context) *liveResponsesStreamWriter {
	return &liveResponsesStreamWriter{
		c:         c,
		requestID: c.GetString("request_id"),
	}
}

func (w *liveResponsesStreamWriter) Started() bool {
	return w != nil && w.started
}

func (w *liveResponsesStreamWriter) Start(start service.ResponseStreamStart) error {
	if w == nil || w.c == nil {
		return nil
	}
	if w.c.Request != nil {
		if err := w.c.Request.Context().Err(); err != nil {
			return err
		}
	}
	w.responseID = strings.TrimSpace(start.ResponseID)
	w.outputItemID = strings.TrimSpace(start.OutputItemID)
	w.model = strings.TrimSpace(start.Model)
	w.createdAt = start.CreatedAt
	w.started = true
	w.c.Header("Content-Type", "text/event-stream")
	w.c.Header("Cache-Control", "no-cache")
	w.c.Header("Connection", "keep-alive")
	w.c.Header("X-Accel-Buffering", "no")
	w.c.Status(http.StatusOK)

	inProgressResponse := w.inProgressResponseObject()
	inProgressItem := responseMessageItem(w.outputItemID, "", "in_progress")
	w.writeEvent("response.created", map[string]any{"response": inProgressResponse})
	w.writeEvent("response.in_progress", map[string]any{"response": inProgressResponse})
	w.writeEvent("response.output_item.added", map[string]any{
		"response_id":  w.responseID,
		"output_index": 0,
		"item":         inProgressItem,
	})
	w.writeEvent("response.content_part.added", map[string]any{
		"response_id":   w.responseID,
		"output_index":  0,
		"item_id":       w.outputItemID,
		"content_index": 0,
		"part":          responseOutputTextPart(""),
	})
	return nil
}

func (w *liveResponsesStreamWriter) Delta(delta string) error {
	if w == nil || w.c == nil || delta == "" {
		return nil
	}
	if w.c.Request != nil {
		if err := w.c.Request.Context().Err(); err != nil {
			return err
		}
	}
	if !w.started {
		return nil
	}
	w.text.WriteString(delta)
	w.writeEvent("response.output_text.delta", map[string]any{
		"response_id":   w.responseID,
		"output_index":  0,
		"item_id":       w.outputItemID,
		"content_index": 0,
		"delta":         delta,
	})
	return nil
}

func (w *liveResponsesStreamWriter) Finish(result service.CompletionResult) {
	if w == nil || w.c == nil || !w.started {
		return
	}
	finalText := result.Text
	if finalText == "" {
		finalText = responseStringField(result.Payload, "output_text")
	}
	if delta := liveResponseMissingDelta(w.text.String(), finalText); delta != "" {
		_ = w.Delta(delta)
	}
	completedItem := responseMessageItem(w.outputItemID, finalText, "completed")
	w.writeEvent("response.output_text.done", map[string]any{
		"response_id":   w.responseID,
		"output_index":  0,
		"item_id":       w.outputItemID,
		"content_index": 0,
		"text":          finalText,
	})
	w.writeEvent("response.content_part.done", map[string]any{
		"response_id":   w.responseID,
		"output_index":  0,
		"item_id":       w.outputItemID,
		"content_index": 0,
		"part":          responseOutputTextPart(finalText),
	})
	w.writeEvent("response.output_item.done", map[string]any{
		"response_id":  w.responseID,
		"output_index": 0,
		"item":         completedItem,
	})
	w.writeEvent(responseFinalStreamEvent(result.Payload), map[string]any{
		"response": result.Payload,
	})
	slog.Info("responses live sse send done",
		"request_id", w.requestID,
		"response_id", w.responseID,
		"events", w.sequenceNumber,
	)
	writeSSEDone(w.c.Writer)
}

func (w *liveResponsesStreamWriter) Error(err error) {
	if w == nil || w.c == nil || !w.started {
		return
	}
	w.writeEvent("error", map[string]any{
		"error": openAIStreamError(err),
	})
	writeSSEDone(w.c.Writer)
}

func (w *liveResponsesStreamWriter) inProgressResponseObject() map[string]any {
	return map[string]any{
		"id":                 w.responseID,
		"object":             "response",
		"created_at":         w.createdAt,
		"status":             "in_progress",
		"model":              w.model,
		"output":             []any{},
		"output_text":        "",
		"completed_at":       nil,
		"error":              nil,
		"incomplete_details": nil,
		"usage":              nil,
	}
}

func (w *liveResponsesStreamWriter) writeEvent(event string, payload map[string]any) {
	if payload == nil {
		payload = map[string]any{}
	}
	eventSequence := w.sequenceNumber
	payload["type"] = event
	payload["sequence_number"] = eventSequence
	w.sequenceNumber++
	encoded, _ := json.Marshal(payload)
	logResponsesSSESendEvent(w.requestID, w.responseID, event, eventSequence, payload, encoded)
	writeSSE(w.c.Writer, event, encoded)
}

func liveResponseMissingDelta(current string, final string) string {
	if final == "" || final == current {
		return ""
	}
	if current == "" {
		return final
	}
	if strings.HasPrefix(final, current) {
		return final[len(current):]
	}
	return ""
}

func openAIStreamError(err error) map[string]any {
	if err == nil {
		return map[string]any{
			"message": "stream failed",
			"type":    "api_error",
			"code":    "stream_error",
		}
	}
	var notionAPIError *service.NotionAPIError
	var noAssistantTextError *service.NotionNoAssistantTextError
	var inferenceStepError *service.NotionInferenceStepError
	switch {
	case errors.As(err, &notionAPIError):
		return service.OpenAIError(notionAPIError.Error(), "api_error", "notion_upstream_error")["error"].(map[string]any)
	case errors.As(err, &inferenceStepError):
		if strings.EqualFold(strings.TrimSpace(inferenceStepError.SubType), "premium-feature-unavailable") {
			return service.OpenAIError(inferenceStepError.Error(), "rate_limit_error", "premium_feature_unavailable")["error"].(map[string]any)
		}
		return service.OpenAIError(inferenceStepError.Error(), "api_error", "notion_inference_error")["error"].(map[string]any)
	case errors.As(err, &noAssistantTextError):
		return service.OpenAIError(noAssistantTextError.Error(), "api_error", "notion_no_assistant_text")["error"].(map[string]any)
	case errors.Is(err, context.Canceled):
		return service.OpenAIError("client canceled the stream", "api_error", "client_canceled")["error"].(map[string]any)
	default:
		return service.OpenAIError(err.Error(), "api_error", "stream_error")["error"].(map[string]any)
	}
}

func (h *Handler) streamResponse(c *gin.Context, result service.CompletionResult) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Status(http.StatusOK)

	requestID := c.GetString("request_id")
	outputItems := responseOutputItems(result.Payload["output"])
	if responseOutputItemsAreToolCalls(outputItems) {
		h.streamResponseToolCalls(c, result, outputItems)
		return
	}

	responseID := responseStringField(result.Payload, "id")
	outputItemID := responseOutputItemID(result.Payload)
	if outputItemID == "" {
		outputItemID = "msg_" + strings.TrimPrefix(strings.ReplaceAll(responseID, "-", ""), "resp_")
	}
	inProgressResponse := responseObjectForStream(result.Payload, "in_progress")
	inProgressItem := responseMessageItem(outputItemID, "", "in_progress")
	completedItem := responseMessageItem(outputItemID, result.Text, "completed")

	sequenceNumber := 0
	writeEvent := func(event string, payload map[string]any) {
		if payload == nil {
			payload = map[string]any{}
		}
		eventSequence := sequenceNumber
		payload["type"] = event
		payload["sequence_number"] = eventSequence
		sequenceNumber++
		encoded, _ := json.Marshal(payload)
		logResponsesSSESendEvent(requestID, responseID, event, eventSequence, payload, encoded)
		writeSSE(c.Writer, event, encoded)
	}

	writeEvent("response.created", map[string]any{"response": inProgressResponse})
	writeEvent("response.in_progress", map[string]any{"response": inProgressResponse})
	writeEvent("response.output_item.added", map[string]any{
		"response_id":  responseID,
		"output_index": 0,
		"item":         inProgressItem,
	})
	writeEvent("response.content_part.added", map[string]any{
		"response_id":   responseID,
		"output_index":  0,
		"item_id":       outputItemID,
		"content_index": 0,
		"part":          responseOutputTextPart(""),
	})
	for _, part := range splitResponseTextChunks(result.Text, 64) {
		if part == "" {
			continue
		}
		writeEvent("response.output_text.delta", map[string]any{
			"response_id":   responseID,
			"output_index":  0,
			"item_id":       outputItemID,
			"content_index": 0,
			"delta":         part,
		})
	}
	writeEvent("response.output_text.done", map[string]any{
		"response_id":   responseID,
		"output_index":  0,
		"item_id":       outputItemID,
		"content_index": 0,
		"text":          result.Text,
	})
	writeEvent("response.content_part.done", map[string]any{
		"response_id":   responseID,
		"output_index":  0,
		"item_id":       outputItemID,
		"content_index": 0,
		"part":          responseOutputTextPart(result.Text),
	})
	writeEvent("response.output_item.done", map[string]any{
		"response_id":  responseID,
		"output_index": 0,
		"item":         completedItem,
	})
	writeEvent(responseFinalStreamEvent(result.Payload), map[string]any{
		"response": result.Payload,
	})
	slog.Info("responses sse send done",
		"request_id", requestID,
		"response_id", responseID,
		"events", sequenceNumber,
	)
	writeSSEDone(c.Writer)
}

func (h *Handler) streamResponseToolCalls(c *gin.Context, result service.CompletionResult, items []map[string]any) {
	requestID := c.GetString("request_id")
	responseID := responseStringField(result.Payload, "id")
	inProgressResponse := responseObjectForStream(result.Payload, "in_progress")

	sequenceNumber := 0
	writeEvent := func(event string, payload map[string]any) {
		if payload == nil {
			payload = map[string]any{}
		}
		eventSequence := sequenceNumber
		payload["type"] = event
		payload["sequence_number"] = eventSequence
		sequenceNumber++
		encoded, _ := json.Marshal(payload)
		logResponsesSSESendEvent(requestID, responseID, event, eventSequence, payload, encoded)
		writeSSE(c.Writer, event, encoded)
	}

	writeEvent("response.created", map[string]any{"response": inProgressResponse})
	writeEvent("response.in_progress", map[string]any{"response": inProgressResponse})
	for index, item := range items {
		itemType := responseStringField(item, "type")
		outputItemID := responseStringField(item, "id")
		if outputItemID == "" {
			prefix := "fc_"
			switch itemType {
			case "custom_tool_call":
				prefix = "ctc_"
			case "shell_call":
				prefix = "sh_"
			case "local_shell_call":
				prefix = "lsh_"
			case "apply_patch_call":
				prefix = "apc_"
			}
			outputItemID = prefix + strings.TrimPrefix(strings.ReplaceAll(responseID, "-", ""), "resp_")
			if len(items) > 1 {
				outputItemID = fmt.Sprintf("%s%d_%s", prefix, index, strings.TrimPrefix(strings.ReplaceAll(responseID, "-", ""), "resp_"))
			}
		}
		inProgressItem := cloneResponseMap(item)
		inProgressItem["id"] = outputItemID
		inProgressItem["status"] = "in_progress"
		completedItem := cloneResponseMap(item)
		completedItem["id"] = outputItemID
		completedItem["status"] = "completed"

		writeEvent("response.output_item.added", map[string]any{
			"response_id":  responseID,
			"output_index": index,
			"item":         toolCallStreamStartItem(inProgressItem),
		})
		switch itemType {
		case "function_call":
			arguments := responseStringField(item, "arguments")
			if arguments != "" {
				writeEvent("response.function_call_arguments.delta", map[string]any{
					"response_id":  responseID,
					"output_index": index,
					"item_id":      outputItemID,
					"delta":        arguments,
				})
			}
			writeEvent("response.function_call_arguments.done", map[string]any{
				"response_id":  responseID,
				"output_index": index,
				"item_id":      outputItemID,
				"arguments":    arguments,
			})
		case "custom_tool_call":
			input := responseStringField(item, "input")
			if input != "" {
				writeEvent("response.custom_tool_call_input.delta", map[string]any{
					"response_id":  responseID,
					"output_index": index,
					"item_id":      outputItemID,
					"delta":        input,
				})
			}
			writeEvent("response.custom_tool_call_input.done", map[string]any{
				"response_id":  responseID,
				"output_index": index,
				"item_id":      outputItemID,
				"input":        input,
			})
		}
		writeEvent("response.output_item.done", map[string]any{
			"response_id":  responseID,
			"output_index": index,
			"item":         completedItem,
		})
	}
	writeEvent(responseFinalStreamEvent(result.Payload), map[string]any{
		"response": result.Payload,
	})
	slog.Info("responses sse send done",
		"request_id", requestID,
		"response_id", responseID,
		"events", sequenceNumber,
	)
	writeSSEDone(c.Writer)
}

func toolCallStreamStartItem(item map[string]any) map[string]any {
	out := cloneResponseMap(item)
	switch responseStringField(out, "type") {
	case "function_call":
		out["arguments"] = ""
	case "custom_tool_call":
		out["input"] = ""
	}
	return out
}

func responseObjectForStream(payload map[string]any, status string) map[string]any {
	out := cloneResponseMap(payload)
	out["status"] = status
	out["completed_at"] = nil
	out["output"] = []any{}
	out["output_text"] = ""
	out["usage"] = nil
	return out
}

func cloneResponseMap(payload map[string]any) map[string]any {
	out := make(map[string]any, len(payload))
	for key, value := range payload {
		out[key] = value
	}
	return out
}

func responseOutputItemID(payload map[string]any) string {
	for _, rawItem := range responseOutputItems(payload["output"]) {
		if id := responseStringField(rawItem, "id"); id != "" {
			return id
		}
	}
	return ""
}

func responseOutputItems(value any) []map[string]any {
	switch typed := value.(type) {
	case []any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			if mapped, ok := item.(map[string]any); ok {
				out = append(out, mapped)
			}
		}
		return out
	case []map[string]any:
		return typed
	default:
		return nil
	}
}

func responseOutputItemsAreToolCalls(items []map[string]any) bool {
	if len(items) == 0 {
		return false
	}
	for _, item := range items {
		switch responseStringField(item, "type") {
		case "function_call", "custom_tool_call", "shell_call", "local_shell_call", "apply_patch_call":
		default:
			return false
		}
	}
	return true
}

func responseFinalStreamEvent(payload map[string]any) string {
	if strings.EqualFold(responseStringField(payload, "status"), "incomplete") {
		return "response.incomplete"
	}
	return "response.completed"
}

func responseMessageItem(itemID string, text string, status string) map[string]any {
	if strings.TrimSpace(status) == "" {
		status = "completed"
	}
	return map[string]any{
		"id":     itemID,
		"type":   "message",
		"status": status,
		"role":   "assistant",
		"content": []any{
			responseOutputTextPart(text),
		},
	}
}

func responseOutputTextPart(text string) map[string]any {
	return map[string]any{
		"type":        "output_text",
		"text":        text,
		"annotations": []any{},
	}
}

func responseStringField(item map[string]any, key string) string {
	if item == nil {
		return ""
	}
	if value, ok := item[key].(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

func splitResponseTextChunks(text string, chunkRunes int) []string {
	if chunkRunes <= 0 {
		chunkRunes = 64
	}
	runes := []rune(text)
	if len(runes) == 0 {
		return []string{""}
	}
	chunks := make([]string, 0, len(runes)/chunkRunes+1)
	for start := 0; start < len(runes); start += chunkRunes {
		end := start + chunkRunes
		if end > len(runes) {
			end = len(runes)
		}
		chunks = append(chunks, string(runes[start:end]))
	}
	return chunks
}

func logResponsesSSESendEvent(requestID string, responseID string, event string, sequenceNumber int, payload map[string]any, encoded []byte) {
	slog.Info("responses sse send event",
		"request_id", strings.TrimSpace(requestID),
		"response_id", strings.TrimSpace(responseID),
		"event", event,
		"sequence_number", sequenceNumber,
		"json_bytes", len(encoded),
		"payload_summary", summarizeResponsesSSEPayload(payload),
	)
}

func summarizeResponsesSSEPayload(payload map[string]any) map[string]any {
	summary := map[string]any{}
	if payload == nil {
		return summary
	}
	for _, key := range []string{"type", "response_id", "item_id"} {
		if value := responseSSEString(payload[key]); value != "" {
			summary[key] = value
		}
	}
	for _, key := range []string{"sequence_number", "output_index", "content_index"} {
		if value, ok := payload[key]; ok {
			summary[key] = value
		}
	}
	addTextSummary(summary, "delta", responseSSEString(payload["delta"]))
	addTextSummary(summary, "text", responseSSEString(payload["text"]))
	addTextSummary(summary, "arguments", responseSSEString(payload["arguments"]))
	addTextSummary(summary, "input", responseSSEString(payload["input"]))
	if part := responseSSEMap(payload["part"]); part != nil {
		summary["part_type"] = responseSSEString(part["type"])
		addTextSummary(summary, "part_text", responseSSEString(part["text"]))
	}
	if item := responseSSEMap(payload["item"]); item != nil {
		summary["item"] = summarizeResponsesSSEItem(item)
	}
	if response := responseSSEMap(payload["response"]); response != nil {
		summary["response"] = summarizeResponsesSSEResponse(response)
	}
	return summary
}

func summarizeResponsesSSEItem(item map[string]any) map[string]any {
	summary := map[string]any{}
	for _, key := range []string{"id", "type", "status", "role", "call_id", "name"} {
		if value := responseSSEString(item[key]); value != "" {
			summary[key] = value
		}
	}
	addTextSummary(summary, "arguments", responseSSEString(item["arguments"]))
	addTextSummary(summary, "input", responseSSEString(item["input"]))
	if content := responseSSESlice(item["content"]); len(content) > 0 {
		if part := responseSSEMap(content[0]); part != nil {
			summary["content_part_type"] = responseSSEString(part["type"])
			addTextSummary(summary, "content_text", responseSSEString(part["text"]))
		}
		summary["content_parts"] = len(content)
	}
	return summary
}

func summarizeResponsesSSEResponse(response map[string]any) map[string]any {
	summary := map[string]any{}
	for _, key := range []string{"id", "status", "model"} {
		if value := responseSSEString(response[key]); value != "" {
			summary[key] = value
		}
	}
	addTextSummary(summary, "output_text", responseSSEString(response["output_text"]))
	if output := responseSSESlice(response["output"]); output != nil {
		summary["output_items"] = len(output)
	}
	return summary
}

func addTextSummary(summary map[string]any, key string, text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	summary[key+"_chars"] = len([]rune(text))
	summary[key+"_preview"] = responseSSEPreview(text, 240)
}

func responseSSEMap(value any) map[string]any {
	if mapped, ok := value.(map[string]any); ok {
		return mapped
	}
	return nil
}

func responseSSESlice(value any) []any {
	if items, ok := value.([]any); ok {
		return items
	}
	return nil
}

func responseSSEString(value any) string {
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text)
	}
	return ""
}

func responseSSEPreview(text string, limit int) string {
	if limit <= 0 {
		return ""
	}
	clean := strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	runes := []rune(clean)
	if len(runes) <= limit {
		return clean
	}
	if limit == 1 {
		return string(runes[:1])
	}
	return strings.TrimSpace(string(runes[:limit-1])) + "..."
}

func writeSSE(w http.ResponseWriter, event string, payload []byte) {
	if event != "" {
		_, _ = fmt.Fprintf(w, "event: %s\n", event)
	}
	_, _ = fmt.Fprintf(w, "data: %s\n\n", payload)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

func writeSSEDone(w http.ResponseWriter) {
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

func handleOpenAIError(c *gin.Context, err error) {
	if err != nil {
		slog.Error("openai request failed",
			"request_id", c.GetString("request_id"),
			"path", c.Request.URL.Path,
			"error", err,
		)
	}
	var notionAPIError *service.NotionAPIError
	var noAssistantTextError *service.NotionNoAssistantTextError
	var inferenceStepError *service.NotionInferenceStepError
	switch {
	case err == nil:
		return
	case err == service.ErrNotFound:
		c.JSON(http.StatusBadRequest, service.OpenAIError("unknown model", "invalid_request_error", "model_not_found"))
	case errors.Is(err, service.ErrConflict):
		c.JSON(http.StatusBadRequest, service.OpenAIError(err.Error(), "invalid_request_error", "sticky_session_conflict"))
	case errors.Is(err, service.ErrForbidden):
		c.JSON(http.StatusForbidden, service.OpenAIError(err.Error(), "invalid_request_error", "account_forbidden"))
	case errors.As(err, &notionAPIError):
		c.JSON(http.StatusBadGateway, service.OpenAIError(notionAPIError.Error(), "api_error", "notion_upstream_error"))
	case errors.As(err, &inferenceStepError):
		if strings.EqualFold(strings.TrimSpace(inferenceStepError.SubType), "premium-feature-unavailable") {
			c.JSON(http.StatusTooManyRequests, service.OpenAIError(inferenceStepError.Error(), "rate_limit_error", "premium_feature_unavailable"))
			return
		}
		c.JSON(http.StatusBadGateway, service.OpenAIError(inferenceStepError.Error(), "api_error", "notion_inference_error"))
	case errors.As(err, &noAssistantTextError):
		c.JSON(http.StatusBadGateway, service.OpenAIError(noAssistantTextError.Error(), "api_error", "notion_no_assistant_text"))
	default:
		c.JSON(http.StatusInternalServerError, service.OpenAIError("internal server error", "api_error", "internal_error"))
	}
}
