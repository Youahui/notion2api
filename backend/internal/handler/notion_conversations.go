package handler

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"notaion2api/backend/internal/model"
	"notaion2api/backend/internal/pkg/respond"
	"notaion2api/backend/internal/service"

	"github.com/gin-gonic/gin"
)

func (h *Handler) NotionConversationsList(c *gin.Context) {
	account, spaceID, ok := h.notionConversationAccount(c)
	if !ok {
		return
	}
	rows, err := h.notion.ListInferenceTranscripts(c.Request.Context(), account, spaceID, notionConversationLimit(c))
	if err != nil {
		handleNotionConversationError(c, err)
		return
	}
	respond.JSON(c, http.StatusOK, gin.H{"data": rows})
}

func (h *Handler) NotionConversationGet(c *gin.Context) {
	account, spaceID, ok := h.notionConversationAccount(c)
	if !ok {
		return
	}
	row, err := h.notion.LoadInferenceTranscript(c.Request.Context(), account, spaceID, c.Param("thread_id"))
	if err != nil {
		handleNotionConversationError(c, err)
		return
	}
	respond.JSON(c, http.StatusOK, row)
}

func (h *Handler) NotionConversationDelete(c *gin.Context) {
	account, spaceID, ok := h.notionConversationAccount(c)
	if !ok {
		return
	}
	if err := h.notion.DeleteThread(c.Request.Context(), account, spaceID, c.Param("thread_id")); err != nil {
		handleNotionConversationError(c, err)
		return
	}
	respond.JSON(c, http.StatusOK, gin.H{"ok": true})
}

func (h *Handler) notionConversationAccount(c *gin.Context) (model.Account, string, bool) {
	if h.notion == nil {
		respond.Error(c, http.StatusServiceUnavailable, "notion gateway is unavailable")
		return model.Account{}, "", false
	}
	rawAccountID := strings.TrimSpace(c.Query("account_id"))
	if rawAccountID == "" {
		respond.BadRequest(c, "account_id is required")
		return model.Account{}, "", false
	}
	accountID, err := strconv.ParseUint(rawAccountID, 10, 64)
	if err != nil || accountID == 0 {
		respond.BadRequest(c, "invalid account_id")
		return model.Account{}, "", false
	}
	account, err := h.accounts.Get(c.Request.Context(), uint(accountID))
	if err != nil {
		handleServiceError(c, err)
		return model.Account{}, "", false
	}
	spaceID := firstQueryValue(c, "space_id", "space")
	if spaceID == "" {
		spaceID = strings.TrimSpace(account.SpaceID)
	}
	if spaceID == "" {
		respond.BadRequest(c, "space_id is required")
		return model.Account{}, "", false
	}
	account.SpaceID = spaceID
	if spaceViewID := firstQueryValue(c, "space_view_id", "space_view"); spaceViewID != "" {
		account.SpaceViewID = spaceViewID
	}
	if spaceName := firstQueryValue(c, "space_name"); spaceName != "" {
		account.SpaceName = spaceName
	}
	return account, spaceID, true
}

func notionConversationLimit(c *gin.Context) int {
	rawLimit := strings.TrimSpace(c.Query("limit"))
	if rawLimit == "" {
		return 50
	}
	limit, err := strconv.Atoi(rawLimit)
	if err != nil || limit <= 0 {
		return 50
	}
	if limit > 100 {
		return 100
	}
	return limit
}

func firstQueryValue(c *gin.Context, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(c.Query(key)); value != "" {
			return value
		}
	}
	return ""
}

func handleNotionConversationError(c *gin.Context, err error) {
	var apiErr *service.NotionAPIError
	if errors.As(err, &apiErr) {
		status := apiErr.StatusCode
		if status < 400 || status > 599 {
			status = http.StatusBadGateway
		}
		respond.Error(c, status, apiErr.Error())
		return
	}
	switch {
	case errors.Is(err, service.ErrInvalid),
		errors.Is(err, service.ErrUnauthorized),
		errors.Is(err, service.ErrForbidden),
		errors.Is(err, service.ErrNotFound),
		errors.Is(err, service.ErrConflict):
		handleServiceError(c, err)
	default:
		respond.Error(c, http.StatusBadGateway, err.Error())
	}
}
