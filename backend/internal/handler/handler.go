package handler

import (
	"errors"
	"net/http"
	"strconv"

	"notaion2api/backend/internal/config"
	"notaion2api/backend/internal/pkg/respond"
	"notaion2api/backend/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type Handler struct {
	cfg      config.Config
	admins   *service.AdminService
	keys     *service.APIKeyService
	models   *service.ModelService
	accounts *service.AccountService
	openai   *service.OpenAIService
	storeDB  *gorm.DB
}

func New(cfg config.Config, admins *service.AdminService, keys *service.APIKeyService, models *service.ModelService, accounts *service.AccountService, openai *service.OpenAIService, db *gorm.DB) *Handler {
	return &Handler{cfg: cfg, admins: admins, keys: keys, models: models, accounts: accounts, openai: openai, storeDB: db}
}

func (h *Handler) Health(c *gin.Context) {
	sqlDB, err := h.storeDB.DB()
	if err != nil {
		respond.Error(c, http.StatusServiceUnavailable, "database unavailable")
		return
	}
	if err := sqlDB.PingContext(c.Request.Context()); err != nil {
		respond.Error(c, http.StatusServiceUnavailable, "database ping failed")
		return
	}
	respond.JSON(c, http.StatusOK, gin.H{
		"ok":              true,
		"service":         "notaion2api",
		"database_driver": h.cfg.Database.Driver,
	})
}

func parseID(c *gin.Context) (uint, bool) {
	raw := c.Param("id")
	id, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || id == 0 {
		respond.BadRequest(c, "invalid id")
		return 0, false
	}
	return uint(id), true
}

func handleServiceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrInvalid):
		respond.BadRequest(c, "invalid request")
	case errors.Is(err, service.ErrUnauthorized):
		respond.Unauthorized(c, "unauthorized")
	case errors.Is(err, service.ErrForbidden):
		respond.Error(c, http.StatusForbidden, "operation is not allowed")
	case errors.Is(err, service.ErrNotFound):
		respond.NotFound(c, "resource not found")
	case errors.Is(err, service.ErrConflict):
		respond.Error(c, http.StatusConflict, "resource conflict")
	default:
		respond.Internal(c)
	}
}
