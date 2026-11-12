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

// userEmail 优先返回用户真实邮箱，历史账号没有邮箱时生成平台内占位邮箱。
func userEmail(user *model.User) string {
	if user.Email != "" {
		return user.Email
	}
	return user.Username + "@ssedeploy.local"
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
		// 注册用户展示真实邮箱；历史内置管理员没有邮箱时退回平台内占位邮箱
		Email: userEmail(user),
		// 主题偏好只存在浏览器 localStorage，后端固定返回默认值
		Theme: "light",
	})
}
