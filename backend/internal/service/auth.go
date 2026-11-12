// Package service 实现账号认证相关的业务逻辑，handler 只负责参数绑定与响应。
package service

import (
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"

	"ssedeploy/internal/model"
	"ssedeploy/internal/store"
)

// 注册相关的业务错误，handler 直接返回给前端展示。
var (
	ErrInvalidUsername = errors.New("用户名需为 4-20 位字母、数字或下划线")
	ErrInvalidEmail    = errors.New("请输入合法的邮箱地址")
	ErrInvalidPassword = errors.New("密码长度需为 6-24 位")
	ErrUsernameTaken   = errors.New("用户名已被占用")
	ErrEmailTaken      = errors.New("该邮箱已注册")
)

var (
	// usernamePattern 4-20 位字母、数字、下划线
	usernamePattern = regexp.MustCompile(`^[A-Za-z0-9_]{4,20}$`)
	// emailPattern 常规邮箱格式：名称@域名.后缀
	emailPattern = regexp.MustCompile(`^[A-Za-z0-9._%+\-]+@[A-Za-z0-9\-]+(\.[A-Za-z0-9\-]+)+$`)
)

// RegisterInput 注册入参。confirmPassword 只在前端校验，不进入后端。
type RegisterInput struct {
	Username string
	Email    string
	Password string
}

// AuthService 账号认证业务服务。
type AuthService struct {
	store *store.Store
}

// NewAuthService 创建认证服务。
func NewAuthService(st *store.Store) *AuthService {
	return &AuthService{store: st}
}

// Register 注册新用户：
//  1. 校验用户名、邮箱、密码格式；
//  2. 检查用户名 / 邮箱是否已被占用；
//  3. bcrypt 加密密码；
//  4. GORM 事务写入数据库（唯一索引兜底并发重复注册）。
func (s *AuthService) Register(in RegisterInput) (*model.User, error) {
	username := strings.TrimSpace(in.Username)
	email := strings.ToLower(strings.TrimSpace(in.Email))
	password := in.Password

	// 1. 参数校验
	if !usernamePattern.MatchString(username) {
		return nil, ErrInvalidUsername
	}
	if len(email) > 128 || !emailPattern.MatchString(email) {
		return nil, ErrInvalidEmail
	}
	if n := utf8.RuneCountInString(password); n < 6 || n > 24 {
		return nil, ErrInvalidPassword
	}

	// 2. 唯一性预校验，给前端明确的失败原因
	if existed, err := s.store.UsernameExists(username); err != nil {
		return nil, err
	} else if existed {
		return nil, ErrUsernameTaken
	}
	if existed, err := s.store.EmailExists(email); err != nil {
		return nil, err
	} else if existed {
		return nil, ErrEmailTaken
	}

	// 3. 密码只保存 bcrypt 密文
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	// 4. 事务入库
	user := &model.User{
		Username:     username,
		Email:        email,
		PasswordHash: string(hash),
		DisplayName:  "",
		Role:         "user",
	}
	if err := s.store.CreateUser(user); err != nil {
		// 并发注册时可能绕过预校验，这里按唯一索引冲突再区分一次
		if errors.Is(err, store.ErrDuplicate) {
			if existed, _ := s.store.UsernameExists(username); existed {
				return nil, ErrUsernameTaken
			}
			return nil, ErrEmailTaken
		}
		return nil, err
	}
	return user, nil
}
