package requestctx

import (
	"notaion2api/backend/internal/model"

	"github.com/gin-gonic/gin"
)

const (
	adminUserKey = "admin_user"
	apiKeyKey    = "api_key"
)

func SetAdminUser(c *gin.Context, admin model.AdminUser) {
	c.Set(adminUserKey, admin)
}

func AdminUser(c *gin.Context) (model.AdminUser, bool) {
	value, ok := c.Get(adminUserKey)
	if !ok {
		return model.AdminUser{}, false
	}
	admin, ok := value.(model.AdminUser)
	return admin, ok
}

func SetAPIKey(c *gin.Context, key model.APIKey) {
	c.Set(apiKeyKey, key)
}

func APIKey(c *gin.Context) (model.APIKey, bool) {
	value, ok := c.Get(apiKeyKey)
	if !ok {
		return model.APIKey{}, false
	}
	key, ok := value.(model.APIKey)
	return key, ok
}
