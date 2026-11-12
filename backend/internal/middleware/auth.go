// Package middleware 提供 Gin 中间件。
package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"ssedeploy/internal/auth"
	"ssedeploy/internal/store"
)

// 上下文键名，handler 通过它们读取当前登录用户。
const (
	CtxUserID   = "userID"
	CtxUsername = "username"
	CtxRole     = "role"
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

// ResolveRole 回查数据库，把当前用户的角色写入上下文。
// 必须挂在 Auth 之后：JWT 只携带用户 id，角色以数据库为准（改角色后立即生效）。
// 接口层据此做资源级权限判断：管理员可管理全部 Pod，普通用户只能管理自己创建的。
func ResolveRole(st *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		v, _ := c.Get(CtxUserID)
		id, _ := v.(int64)
		if id == 0 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "未登录或登录已过期", "data": nil})
			return
		}

		user, err := st.GetUserByID(id)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "登录已失效，请重新登录", "data": nil})
			return
		}

		c.Set(CtxRole, user.Role)
		c.Next()
	}
}
