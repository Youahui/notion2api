package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"notion2api/backend/internal/dto"
)

// bindResponsesRequest is permissive for Codex/OpenAI Responses API requests.
// Important: do not use DisallowUnknownFields here.
func bindResponsesRequest(c *gin.Context) (dto.ResponsesRequest, bool) {
	var req dto.ResponsesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.NewOpenAIError(
			"Invalid JSON body: "+err.Error(),
			"invalid_request_error",
			nil,
			"invalid_json",
		))
		return req, false
	}

	if strings.TrimSpace(req.Model) == "" {
		req.Model = "auto"
	}

	return req, true
}

// writeResponsesJSON writes a non-streaming Responses API object.
// Use this instead of Chat Completions choices[] for /v1/responses.
func writeResponsesJSON(c *gin.Context, model string, text string, inputTokens int, outputTokens int) {
	c.JSON(http.StatusOK, dto.NewTextResponse(
		"",
		model,
		text,
		dto.NormalizeResponseUsage(inputTokens, outputTokens),
	))
}

// writeResponsesToolCallJSON writes a Responses API function_call output item.
func writeResponsesToolCallJSON(c *gin.Context, model string, callID string, name string, arguments any, inputTokens int, outputTokens int) {
	argsText := "{}"
	switch v := arguments.(type) {
	case nil:
		argsText = "{}"
	case string:
		if strings.TrimSpace(v) != "" {
			argsText = v
		}
	default:
		if b, err := json.Marshal(v); err == nil {
			argsText = string(b)
		}
	}

	c.JSON(http.StatusOK, dto.NewFunctionCallResponse(
		"",
		model,
		callID,
		name,
		argsText,
		dto.NormalizeResponseUsage(inputTokens, outputTokens),
	))
}

func writeResponsesBadRequest(c *gin.Context, message string, param any, code string) {
	c.JSON(http.StatusBadRequest, dto.NewOpenAIError(
		message,
		"invalid_request_error",
		param,
		code,
	))
}

func writeResponsesNotFound(c *gin.Context, message string) {
	c.JSON(http.StatusNotFound, dto.NewOpenAIError(
		message,
		"invalid_request_error",
		"id",
		"response_not_found",
	))
}

func writeResponsesServerError(c *gin.Context, message string) {
	c.JSON(http.StatusInternalServerError, dto.NewOpenAIError(
		message,
		"server_error",
		nil,
		"internal_error",
	))
}
