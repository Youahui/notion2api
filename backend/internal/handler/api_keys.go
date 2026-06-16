package handler

import (
	"net/http"
	"strconv"

	"notaion2api/backend/internal/dto"
	"notaion2api/backend/internal/pkg/requestctx"
	"notaion2api/backend/internal/pkg/respond"
	"notaion2api/backend/internal/repository"

	"github.com/gin-gonic/gin"
)

func (h *Handler) APIKeysList(c *gin.Context) {
	rows, err := h.keys.List(c.Request.Context())
	if err != nil {
		handleServiceError(c, err)
		return
	}
	respond.JSON(c, http.StatusOK, gin.H{"data": rows})
}

func (h *Handler) APIKeysCreate(c *gin.Context) {
	var req dto.CreateAPIKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respond.BadRequest(c, "invalid api key payload")
		return
	}
	admin, _ := requestctx.AdminUser(c)
	row, err := h.keys.Create(c.Request.Context(), admin.ID, req)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	respond.JSON(c, http.StatusCreated, row)
}

func (h *Handler) APIKeysGet(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	row, err := h.keys.Get(c.Request.Context(), id)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	respond.JSON(c, http.StatusOK, row)
}

func (h *Handler) APIKeysUpdate(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var req dto.UpdateAPIKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respond.BadRequest(c, "invalid api key payload")
		return
	}
	row, err := h.keys.Update(c.Request.Context(), id, req)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	respond.JSON(c, http.StatusOK, row)
}

func (h *Handler) APIKeysEnable(c *gin.Context) {
	h.setAPIKeyEnabled(c, true)
}

func (h *Handler) APIKeysDisable(c *gin.Context) {
	h.setAPIKeyEnabled(c, false)
}

func (h *Handler) setAPIKeyEnabled(c *gin.Context, enabled bool) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	row, err := h.keys.SetEnabled(c.Request.Context(), id, enabled)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	respond.JSON(c, http.StatusOK, row)
}

func (h *Handler) APIKeysDelete(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	if err := h.keys.Delete(c.Request.Context(), id); err != nil {
		handleServiceError(c, err)
		return
	}
	respond.JSON(c, http.StatusOK, gin.H{"ok": true})
}

func (h *Handler) APIKeyUsage(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	filter := usageFilterFromQuery(c)
	filter.APIKeyID = &id
	rows, err := h.keys.Usage(c.Request.Context(), filter)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	respond.JSON(c, http.StatusOK, gin.H{"data": rows})
}

func (h *Handler) Usage(c *gin.Context) {
	filter := usageFilterFromQuery(c)
	rows, err := h.keys.Usage(c.Request.Context(), filter)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	respond.JSON(c, http.StatusOK, gin.H{"data": rows})
}

func (h *Handler) Logs(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
	rows, err := h.keys.Logs(c.Request.Context(), limit)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	respond.JSON(c, http.StatusOK, gin.H{"data": rows})
}

func usageFilterFromQuery(c *gin.Context) repository.UsageFilter {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
	filter := repository.UsageFilter{
		FromDate: c.Query("from"),
		ToDate:   c.Query("to"),
		Model:    c.Query("model"),
		Limit:    limit,
	}
	if raw := c.Query("api_key_id"); raw != "" {
		if parsed, err := strconv.ParseUint(raw, 10, 64); err == nil && parsed > 0 {
			id := uint(parsed)
			filter.APIKeyID = &id
		}
	}
	return filter
}
