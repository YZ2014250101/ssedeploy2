package handler

import (
	"fmt"

	"github.com/gin-gonic/gin"

	"ssedeploy/internal/model"
)

// permissionCountByRole 把角色映射为权限数量。
// 第一阶段没有细粒度权限表，这里只用于个人页展示。
func permissionCountByRole(role string) int {
	if role == "admin" {
		return 12
	}
	return 6
}

// UserProfile 返回个人管理页所需的账户信息。
func (h *Handler) UserProfile(c *gin.Context) {
	user, err := h.Store.GetUserByID(currentUserID(c))
	if err != nil {
		Fail(c, 401, "登录已失效")
		return
	}

	OK(c, model.UserProfile{
		UID:             fmt.Sprintf("uni_%d", user.ID),
		Username:        user.Username,
		Role:            user.Role,
		PermissionCount: permissionCountByRole(user.Role),
		DisplayName:     user.Display(),
		// 当前用户表没有邮箱字段，按账号名生成平台内邮箱展示
		Email: user.Username + "@ssedeploy.local",
		// 主题偏好只存在浏览器 localStorage，后端固定返回默认值
		Theme: "light",
	})
}
