// Package middleware 提供 Gin 中间件。
package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"ssedeploy/internal/auth"
)

// 上下文键名，handler 通过它们读取当前登录用户。
const (
	CtxUserID   = "userID"
	CtxUsername = "username"
)

// Auth 校验请求携带的 JWT。
// 普通接口从 Authorization: Bearer 读取；WebSocket 因浏览器无法设置请求头，支持 ?token= 形式。
func Auth(secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := ""
		if h := c.GetHeader("Authorization"); strings.HasPrefix(h, "Bearer ") {
			token = strings.TrimPrefix(h, "Bearer ")
		}
		if token == "" {
			token = c.Query("token")
		}
		if token == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "未登录或登录已过期", "data": nil})
			return
		}

		claims, err := auth.Parse(secret, token)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "未登录或登录已过期", "data": nil})
			return
		}

		c.Set(CtxUserID, claims.UserID)
		c.Set(CtxUsername, claims.Username)
		c.Next()
	}
}
