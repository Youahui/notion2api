package router

import (
	"net/http"

	"notaion2api/backend/internal/config"
	"notaion2api/backend/internal/handler"
	"notaion2api/backend/internal/middleware"
	"notaion2api/backend/internal/service"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func New(cfg config.Config, admins *service.AdminService, keys *service.APIKeyService, models *service.ModelService, accounts *service.AccountService, openai *service.OpenAIService, db *gorm.DB) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery(), middleware.RequestID(), middleware.AccessLog())
	r.Use(cors.New(cors.Config{
		AllowAllOrigins: true,
		AllowMethods:    []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:    []string{"Authorization", "Content-Type", "X-Admin-Token", "X-Request-ID"},
		ExposeHeaders:   []string{"X-Request-ID"},
	}))

	h := handler.New(cfg, admins, keys, models, accounts, openai, db)

	r.GET("/", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"service": "notaion2api",
			"admin":   "/admin",
			"api":     "/v1",
			"health":  "/healthz",
		})
	})
	r.GET("/healthz", h.Health)

	r.POST("/admin/login", h.AdminLogin)
	admin := r.Group("/admin")
	admin.Use(middleware.AdminAuth(admins))
	admin.POST("/logout", h.AdminLogout)
	admin.GET("/verify", h.AdminVerify)

	admin.GET("/admins", h.AdminsList)
	admin.POST("/admins", h.AdminsCreate)
	admin.PUT("/admins/:id", h.AdminsUpdate)
	admin.DELETE("/admins/:id", h.AdminsDelete)

	admin.GET("/api-keys", h.APIKeysList)
	admin.POST("/api-keys", h.APIKeysCreate)
	admin.GET("/api-keys/:id/usage", h.APIKeyUsage)
	admin.GET("/api-keys/:id", h.APIKeysGet)
	admin.PUT("/api-keys/:id", h.APIKeysUpdate)
	admin.POST("/api-keys/:id/enable", h.APIKeysEnable)
	admin.POST("/api-keys/:id/disable", h.APIKeysDisable)
	admin.DELETE("/api-keys/:id", h.APIKeysDelete)

	admin.GET("/models", h.AdminModels)
	admin.GET("/accounts", h.AccountsList)
	admin.POST("/accounts", h.AccountsCreate)
	admin.PUT("/accounts/:id", h.AccountsUpdate)
	admin.DELETE("/accounts/:id", h.AccountsDelete)
	admin.POST("/accounts/:id/activate", h.AccountsActivate)
	admin.POST("/accounts/:id/test", h.AccountsTest)
	admin.GET("/usage", h.Usage)
	admin.GET("/logs", h.Logs)
	admin.GET("/settings", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"data": gin.H{}})
	})

	v1 := r.Group("/v1")
	v1.Use(middleware.APIKeyAuth(keys))
	v1.GET("/models", h.OpenAIModels)
	v1.GET("/models/:id", h.OpenAIModel)
	v1.POST("/chat/completions", h.ChatCompletions)
	v1.POST("/responses", h.Responses)
	v1.GET("/responses/:id", h.ResponseByID)

	return r
}
