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
