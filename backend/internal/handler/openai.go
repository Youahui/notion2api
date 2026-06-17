package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"notaion2api/backend/internal/dto"
	"notaion2api/backend/internal/pkg/requestctx"
	"notaion2api/backend/internal/pkg/respond"
	"notaion2api/backend/internal/service"

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
	result, err := h.openai.ChatCompletion(c.Request.Context(), req, openAIRequestOptions(c, apiKey.ID))
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
	result, err := h.openai.Response(c.Request.Context(), body, openAIRequestOptions(c, apiKey.ID))
	if err != nil {
		handleOpenAIError(c, err)
		return
	}
	if stream, _ := body["stream"].(bool); stream {
		h.streamResponse(c, result)
	} else {
		c.JSON(http.StatusOK, result.Payload)
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
}

func (h *Handler) ResponseByID(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, service.OpenAIError("response store lookup is not implemented yet", "invalid_request_error", "not_implemented"))
}

func openAIRequestOptions(c *gin.Context, apiKeyID uint) service.OpenAIRequestOptions {
	return service.OpenAIRequestOptions{
		APIKeyID:             apiKeyID,
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

func (h *Handler) streamResponse(c *gin.Context, result service.CompletionResult) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Status(http.StatusOK)

	requestID := c.GetString("request_id")
	outputItems := responseOutputItems(result.Payload["output"])
	if len(outputItems) > 0 && responseStringField(outputItems[0], "type") == "function_call" {
		h.streamResponseFunctionCall(c, result, outputItems[0])
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
	writeEvent("response.completed", map[string]any{
		"response": result.Payload,
	})
	slog.Info("responses sse send done",
		"request_id", requestID,
		"response_id", responseID,
		"events", sequenceNumber,
	)
	writeSSEDone(c.Writer)
}

func (h *Handler) streamResponseFunctionCall(c *gin.Context, result service.CompletionResult, item map[string]any) {
	requestID := c.GetString("request_id")
	responseID := responseStringField(result.Payload, "id")
	outputItemID := responseStringField(item, "id")
	if outputItemID == "" {
		outputItemID = "fc_" + strings.TrimPrefix(strings.ReplaceAll(responseID, "-", ""), "resp_")
	}
	arguments := responseStringField(item, "arguments")
	inProgressResponse := responseObjectForStream(result.Payload, "in_progress")
	inProgressItem := cloneResponseMap(item)
	inProgressItem["id"] = outputItemID
	inProgressItem["status"] = "in_progress"
	inProgressItem["arguments"] = ""
	completedItem := cloneResponseMap(item)
	completedItem["id"] = outputItemID
	completedItem["status"] = "completed"

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
	if arguments != "" {
		writeEvent("response.function_call_arguments.delta", map[string]any{
			"response_id":  responseID,
			"output_index": 0,
			"item_id":      outputItemID,
			"delta":        arguments,
		})
	}
	writeEvent("response.function_call_arguments.done", map[string]any{
		"response_id":  responseID,
		"output_index": 0,
		"item_id":      outputItemID,
		"arguments":    arguments,
	})
	writeEvent("response.output_item.done", map[string]any{
		"response_id":  responseID,
		"output_index": 0,
		"item":         completedItem,
	})
	writeEvent("response.completed", map[string]any{
		"response": result.Payload,
	})
	slog.Info("responses sse send done",
		"request_id", requestID,
		"response_id", responseID,
		"events", sequenceNumber,
	)
	writeSSEDone(c.Writer)
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
