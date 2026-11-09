// Package model 定义业务数据结构，字段标签直接决定接口 JSON 结构。
package model

// User 平台用户。第一阶段只做单角色（admin），不做细粒度权限。
type User struct {
	ID           int64  `json:"id"`
	Username     string `json:"username"`
	PasswordHash string `json:"-"` // 密码哈希永远不返回给前端
	DisplayName  string `json:"displayName"`
	Role         string `json:"role"`
	CreatedAt    int64  `json:"createdAt"`
}

// Display 返回用于页面展示的名字：优先显示昵称，其次账号名。
func (u *User) Display() string {
	if u.DisplayName != "" {
		return u.DisplayName
	}
	return u.Username
}

// UserProfile 是 GET /api/user/profile 返回的个人管理页数据。
type UserProfile struct {
	UID             string `json:"uid"`             // 平台展示用用户 ID，格式 uni_<id>
	Username        string `json:"username"`        // 登录账号
	Role            string `json:"role"`            // 角色，例如 admin
	PermissionCount int    `json:"permissionCount"` // 权限数量（按角色映射）
	DisplayName     string `json:"displayName"`     // 显示名
	Email           string `json:"email"`           // 邮箱
	Theme           string `json:"theme"`           // 主题默认值；实际偏好保存在浏览器 localStorage
}
