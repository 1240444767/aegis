package middleware

import (
	"net/http"

	authdomain "aegis/internal/domain/auth"
	"aegis/internal/service"
	"aegis/pkg/response"
	"github.com/gin-gonic/gin"
)

func Recovery() gin.HandlerFunc {
	return gin.CustomRecovery(func(c *gin.Context, recovered any) {
		response.Error(c, http.StatusInternalServerError, 50000, "服务器内部错误")
	})
}

func Auth(authService *service.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := bearerToken(c.GetHeader("Authorization"))
		if token == "" {
			response.Error(c, http.StatusUnauthorized, 40100, "缺少 Authorization Bearer Token")
			c.Abort()
			return
		}
		session, err := authService.ValidateAccessToken(c.Request.Context(), token)
		if err != nil {
			response.Error(c, http.StatusUnauthorized, 40100, err.Error())
			c.Abort()
			return
		}
		c.Set("auth.session", session)
		c.Set("auth.token", token)
		registerUserIDAlias(c, authService, session)
		c.Next()
	}
}

// registerUserIDAlias 让这次响应里调用者自己的主键一律以对外 ID 出现（docs/user-public-id.md）。
func registerUserIDAlias(c *gin.Context, authService *service.AuthService, session *authdomain.Session) {
	if session == nil {
		return
	}
	publicID := authService.PublicIDs().Of(c.Request.Context(), session.UserID)
	response.SetUserIDAlias(c, session.UserID, publicID)
}

func OptionalAuth(authService *service.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := bearerToken(c.GetHeader("Authorization"))
		if token == "" {
			c.Next()
			return
		}
		session, err := authService.ValidateAccessToken(c.Request.Context(), token)
		if err == nil && session != nil {
			c.Set("auth.session", session)
			c.Set("auth.token", token)
			registerUserIDAlias(c, authService, session)
		}
		c.Next()
	}
}

func bearerToken(header string) string {
	const prefix = "Bearer "
	if len(header) <= len(prefix) || header[:len(prefix)] != prefix {
		return ""
	}
	return header[len(prefix):]
}
