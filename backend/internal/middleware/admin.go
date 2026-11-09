package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"ssedeploy/internal/store"
)

// RequireAdmin 管理员权限校验中间件。
// 必须挂在 Auth 之后：先从 JWT 得到用户 id，再回查数据库确认角色为 admin。
func RequireAdmin(st *store.Store) gin.HandlerFunc {
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
		if user.Role != "admin" {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": 403, "message": "无权限访问，仅管理员可用", "data": nil})
			return
		}

		c.Next()
	}
}
