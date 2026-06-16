package handler

import (
	"net/http"

	"notaion2api/backend/internal/dto"
	"notaion2api/backend/internal/pkg/respond"
	"notaion2api/backend/internal/service"

	"github.com/gin-gonic/gin"
)

func (h *Handler) AccountsList(c *gin.Context) {
	includeDisabled := c.Query("include_disabled") == "true"
	rows, err := h.accounts.List(c.Request.Context(), includeDisabled)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	respond.JSON(c, http.StatusOK, gin.H{"data": rows})
}

func (h *Handler) AccountsCreate(c *gin.Context) {
	var req dto.UpsertAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respond.BadRequest(c, "invalid account payload")
		return
	}
	row, err := h.accounts.Create(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	respond.JSON(c, http.StatusCreated, row)
}

func (h *Handler) AccountsUpdate(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var req dto.UpsertAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respond.BadRequest(c, "invalid account payload")
		return
	}
	row, err := h.accounts.Update(c.Request.Context(), id, req)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	respond.JSON(c, http.StatusOK, row)
}

func (h *Handler) AccountsDelete(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	if err := h.accounts.Delete(c.Request.Context(), id); err != nil {
		handleServiceError(c, err)
		return
	}
	respond.JSON(c, http.StatusOK, gin.H{"ok": true})
}

func (h *Handler) AccountsActivate(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	row, err := h.accounts.Activate(c.Request.Context(), id)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	respond.JSON(c, http.StatusOK, row)
}

func (h *Handler) AccountsTest(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	accountModel, err := h.accounts.Get(c.Request.Context(), id)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	account := service.AccountDTO(accountModel)
	var req dto.AccountTestRequest
	_ = c.ShouldBindJSON(&req)
	if req.Prompt == "" {
		req.Prompt = "Reply with NOTION2API_ACCOUNT_OK only."
	}
	modelID := req.Model
	if modelID == "" {
		modelID = "auto"
	}
	result, err := h.openai.ChatCompletion(c.Request.Context(), dto.ChatCompletionRequest{
		Model: modelID,
		Messages: []dto.ChatMessage{
			{Role: "user", Content: req.Prompt},
		},
		AccountEmail: account.Email,
	})
	if err != nil {
		handleServiceError(c, err)
		return
	}
	trace, _ := result.Payload["notion_trace"].(map[string]any)
	respond.JSON(c, http.StatusOK, dto.AccountTestResponse{
		Success:       true,
		Account:       account,
		NotionRequest: trace,
		Text:          result.Text,
	})
}
