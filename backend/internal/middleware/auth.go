package middleware

import (
	"errors"
	"net/http"
	"strings"

	"notaion2api/backend/internal/pkg/requestctx"
	"notaion2api/backend/internal/pkg/respond"
	"notaion2api/backend/internal/pkg/security"
	"notaion2api/backend/internal/service"

	"github.com/gin-gonic/gin"
)

func AdminAuth(admins *service.AdminService) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := adminToken(c)
		admin, err := admins.AuthenticateToken(c.Request.Context(), token)
		if err != nil {
			if errors.Is(err, service.ErrUnauthorized) {
				respond.Unauthorized(c, "admin authentication required")
				return
			}
			respond.Internal(c)
			return
		}
		requestctx.SetAdminUser(c, admin)
		c.Next()
	}
}

func APIKeyAuth(keys *service.APIKeyService) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := security.BearerToken(c.GetHeader("Authorization"))
		key, err := keys.Authenticate(c.Request.Context(), token)
		if err != nil {
			switch {
			case errors.Is(err, service.ErrForbidden):
				respond.Error(c, http.StatusForbidden, "api key is disabled, expired, or quota exhausted")
			case errors.Is(err, service.ErrUnauthorized):
				respond.Unauthorized(c, "valid api key required")
			default:
				respond.Internal(c)
			}
			return
		}
		requestctx.SetAPIKey(c, key)
		c.Next()
	}
}

func adminToken(c *gin.Context) string {
	if token := strings.TrimSpace(c.GetHeader("X-Admin-Token")); token != "" {
		return token
	}
	if token := security.BearerToken(c.GetHeader("Authorization")); token != "" {
		return token
	}
	if cookie, err := c.Cookie("n2a_admin_token"); err == nil {
		return strings.TrimSpace(cookie)
	}
	return ""
}

func AdminTokenFromRequest(c *gin.Context) string {
	return adminToken(c)
}
