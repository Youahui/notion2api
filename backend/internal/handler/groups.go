package handler

import (
	"net/http"

	"notion2api/backend/internal/dto"
	"notion2api/backend/internal/pkg/respond"

	"github.com/gin-gonic/gin"
)

func (h *Handler) GroupsList(c *gin.Context) {
	rows, err := h.accounts.ListGroups(c.Request.Context())
	if err != nil {
		handleServiceError(c, err)
		return
	}
	respond.JSON(c, http.StatusOK, gin.H{"data": rows})
}

func (h *Handler) GroupsCreate(c *gin.Context) {
	var req dto.CreateGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respond.BadRequest(c, "invalid group payload")
		return
	}
	row, err := h.accounts.CreateGroup(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	respond.JSON(c, http.StatusCreated, row)
}

func (h *Handler) GroupsGet(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	row, err := h.accounts.GetGroup(c.Request.Context(), id)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	respond.JSON(c, http.StatusOK, row)
}

func (h *Handler) GroupsUpdate(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var req dto.UpdateGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respond.BadRequest(c, "invalid group payload")
		return
	}
	row, err := h.accounts.UpdateGroup(c.Request.Context(), id, req)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	respond.JSON(c, http.StatusOK, row)
}

func (h *Handler) GroupsDelete(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	if err := h.accounts.DeleteGroup(c.Request.Context(), id); err != nil {
		handleServiceError(c, err)
		return
	}
	respond.JSON(c, http.StatusOK, gin.H{"ok": true})
}

func (h *Handler) GroupMembers(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	rows, err := h.accounts.GroupMembers(c.Request.Context(), id)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	respond.JSON(c, http.StatusOK, gin.H{"data": rows})
}

func (h *Handler) GroupMembersUpdate(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var req dto.UpdateGroupMembersRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respond.BadRequest(c, "invalid group members payload")
		return
	}
	rows, err := h.accounts.ReplaceGroupMembers(c.Request.Context(), id, req)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	respond.JSON(c, http.StatusOK, gin.H{"data": rows})
}
