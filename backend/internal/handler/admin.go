package handler

import (
	"net/http"

	"notaion2api/backend/internal/dto"
	"notaion2api/backend/internal/middleware"
	"notaion2api/backend/internal/pkg/requestctx"
	"notaion2api/backend/internal/pkg/respond"
	"notaion2api/backend/internal/service"

	"github.com/gin-gonic/gin"
)

func (h *Handler) AdminLogin(c *gin.Context) {
	var req dto.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respond.BadRequest(c, "invalid login payload")
		return
	}
	resp, err := h.admins.Login(c.Request.Context(), req, c.GetHeader("User-Agent"), c.ClientIP())
	if err != nil {
		handleServiceError(c, err)
		return
	}
	maxAge := h.cfg.Security.AdminTokenTTLHours * 3600
	if maxAge <= 0 {
		maxAge = 24 * 3600
	}
	c.SetCookie("n2a_admin_token", resp.Token, maxAge, "/admin", "", false, true)
	respond.JSON(c, http.StatusOK, resp)
}

func (h *Handler) AdminLogout(c *gin.Context) {
	token := middleware.AdminTokenFromRequest(c)
	if err := h.admins.Logout(c.Request.Context(), token); err != nil {
		handleServiceError(c, err)
		return
	}
	c.SetCookie("n2a_admin_token", "", -1, "/admin", "", false, true)
	respond.JSON(c, http.StatusOK, gin.H{"ok": true})
}

func (h *Handler) AdminVerify(c *gin.Context) {
	admin, _ := requestctx.AdminUser(c)
	respond.JSON(c, http.StatusOK, gin.H{"admin": service.AdminDTO(admin)})
}

func (h *Handler) AdminsList(c *gin.Context) {
	rows, err := h.admins.List(c.Request.Context())
	if err != nil {
		handleServiceError(c, err)
		return
	}
	respond.JSON(c, http.StatusOK, gin.H{"data": rows})
}

func (h *Handler) AdminsCreate(c *gin.Context) {
	var req dto.CreateAdminRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respond.BadRequest(c, "invalid admin payload")
		return
	}
	row, err := h.admins.Create(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	respond.JSON(c, http.StatusCreated, row)
}

func (h *Handler) AdminsUpdate(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var req dto.UpdateAdminRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respond.BadRequest(c, "invalid admin payload")
		return
	}
	row, err := h.admins.Update(c.Request.Context(), id, req)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	respond.JSON(c, http.StatusOK, row)
}

func (h *Handler) AdminsDelete(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	if err := h.admins.Delete(c.Request.Context(), id); err != nil {
		handleServiceError(c, err)
		return
	}
	respond.JSON(c, http.StatusOK, gin.H{"ok": true})
}
