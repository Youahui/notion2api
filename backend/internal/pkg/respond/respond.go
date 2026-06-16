package respond

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type ErrorPayload struct {
	Detail string `json:"detail"`
}

func JSON(c *gin.Context, status int, payload any) {
	c.JSON(status, payload)
}

func Error(c *gin.Context, status int, detail string) {
	c.AbortWithStatusJSON(status, ErrorPayload{Detail: detail})
}

func BadRequest(c *gin.Context, detail string) {
	Error(c, http.StatusBadRequest, detail)
}

func Unauthorized(c *gin.Context, detail string) {
	Error(c, http.StatusUnauthorized, detail)
}

func NotFound(c *gin.Context, detail string) {
	Error(c, http.StatusNotFound, detail)
}

func Internal(c *gin.Context) {
	Error(c, http.StatusInternalServerError, "internal server error")
}
