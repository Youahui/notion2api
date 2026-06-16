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

func New(cfg config.Config, admins *service.AdminService, keys *service.APIKeyService, models *service.ModelService, accounts *service.AccountService, openai *service.OpenAIService, notion *service.NotionGateway, db *gorm.DB) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery(), middleware.RequestID())
	if gin.IsDebugging() {
		r.Use(middleware.DevRequestDetailsLog())
	}
	r.Use(middleware.AccessLog())
	r.Use(cors.New(cors.Config{
		AllowAllOrigins: true,
		AllowMethods:    []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:    []string{"Authorization", "Content-Type", "X-Admin-Token", "X-Request-ID"},
		ExposeHeaders:   []string{"X-Request-ID"},
	}))

	h := handler.New(cfg, admins, keys, models, accounts, openai, notion, db)

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
	{
		admin.Use(middleware.AdminAuth(admins))
		admin.POST("/logout", h.AdminLogout)
		admin.GET("/verify", h.AdminVerify)

		adminGroup := admin.Group("/admins")
		{
			adminGroup.GET("", h.AdminsList)
			adminGroup.POST("", h.AdminsCreate)
			adminGroup.PUT("/:id", h.AdminsUpdate)
			adminGroup.DELETE("/:id", h.AdminsDelete)
		}

		apiKeysGroup := admin.Group("/api-keys")
		{
			apiKeysGroup.GET("", h.APIKeysList)
			apiKeysGroup.POST("", h.APIKeysCreate)
			apiKeysGroup.GET("/:id/usage", h.APIKeyUsage)
			apiKeysGroup.GET("/:id", h.APIKeysGet)
			apiKeysGroup.PUT("/:id", h.APIKeysUpdate)
			apiKeysGroup.POST("/:id/enable", h.APIKeysEnable)
			apiKeysGroup.POST("/:id/disable", h.APIKeysDisable)
			apiKeysGroup.DELETE("/:id", h.APIKeysDelete)
		}

		modelsGroup := admin.Group("/models")
		{
			modelsGroup.GET("", h.AdminModels)
		}

		accountsGroup := admin.Group("/accounts")
		{
			accountsGroup.GET("", h.AccountsList)
			accountsGroup.POST("", h.AccountsCreate)

			accountsGroup.POST("/manual", h.AccountsManualImport)
			accountsGroup.POST("/login/start", h.AccountsLoginStart)
			accountsGroup.POST("/login/verify", h.AccountsLoginVerify)
			accountsGroup.GET("/login/status", h.AccountsLoginStatus)
			accountsGroup.GET("/:id/spaces", h.AccountsSpaces)
			accountsGroup.PUT("/:id", h.AccountsUpdate)
			accountsGroup.DELETE("/:id", h.AccountsDelete)
			accountsGroup.POST("/:id/activate", h.AccountsActivate)
			accountsGroup.POST("/:id/refresh", h.AccountsRefresh)
			accountsGroup.POST("/:id/test", h.AccountsTest)
		}

		notionConversationsGroup := admin.Group("/notion-conversations")
		{
			notionConversationsGroup.GET("", h.NotionConversationsList)
			notionConversationsGroup.GET("/:thread_id", h.NotionConversationGet)
			notionConversationsGroup.DELETE("/:thread_id", h.NotionConversationDelete)
		}

		admin.GET("/usage", h.Usage)
		admin.GET("/logs", h.Logs)
		admin.GET("/settings", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"data": gin.H{}})
		})

	}

	v1 := r.Group("/v1")
	{
		v1.Use(middleware.APIKeyAuth(keys))
		v1.GET("/models", h.OpenAIModels)
		v1.GET("/models/:id", h.OpenAIModel)
		v1.POST("/chat/completions", h.ChatCompletions)
		v1.POST("/responses", h.Responses)
		v1.GET("/responses/:id", h.ResponseByID)
	}

	return r
}
