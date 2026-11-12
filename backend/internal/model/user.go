// Package model 定义业务数据结构，字段标签直接决定接口 JSON 结构。
package model

// User 平台用户，对应 users 表。
//
// GORM 在服务启动时通过 AutoMigrate 自动补全表结构：
//   - username、email 建立唯一索引，数据库层面兜底防止并发重复注册；
//   - password_hash 只保存 bcrypt 密文，配合 json:"-" 保证永不返回前端；
//   - 历史字段（username/password_hash/display_name/role/created_at）由既有建表语句创建，
//     这里标记 -:migration，避免 AutoMigrate 反复重建旧表。
type User struct {
	ID           int64  `gorm:"primaryKey;autoIncrement" json:"id"`
	Username     string `gorm:"column:username;type:text;not null;-:migration" json:"username"`                      // 登录账号，唯一
	Email        string `gorm:"column:email;type:text;uniqueIndex:idx_users_email" json:"email"`                     // 邮箱，唯一索引
	PasswordHash string `gorm:"column:password_hash;type:text;not null;-:migration" json:"-"`                        // bcrypt 密文，禁止返回前端
	DisplayName  string `gorm:"column:display_name;type:text;not null;default:'';-:migration" json:"displayName"`    // 显示名
	Role         string `gorm:"column:role;type:text;not null;default:'user';-:migration" json:"role"`               // 角色：admin / user
	CreatedAt    int64  `gorm:"column:created_at;type:integer;not null;autoCreateTime;-:migration" json:"createdAt"` // Unix 秒
	UpdatedAt    int64  `gorm:"column:updated_at;type:integer;autoUpdateTime" json:"updatedAt"`                      // Unix 秒，由 AutoMigrate 新增（历史行为 NULL）
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
