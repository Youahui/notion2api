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
	result, err := h.openai.ChatCompletion(c.Request.Context(), req)
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
	result, err := h.openai.Response(c.Request.Context(), body)
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
	c.JSON(http.StatusNotImplemented, service.OpenAIError("response store lookup is not implemented yet", "invalid_request_error", "not_implemented"))
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
	_, _ = fmt.Fprintf(c.Writer, "data: %s\n\n", encoded)
	_, _ = fmt.Fprint(c.Writer, "data: [DONE]\n\n")
	c.Writer.Flush()
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
