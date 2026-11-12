package handler

import (
	"errors"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"ssedeploy/internal/auth"
	"ssedeploy/internal/service"
)

// Login 账号密码登录，成功后返回 JWT 与用户信息。
// 路由：POST /api/v1/login（同时保留旧的 /api/v1/auth/login 别名）。
func (h *Handler) Login(c *gin.Context) {
	var req struct {
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, 400, "请输入账号和密码")
		return
	}
	// 记录登录请求日志，密码等敏感信息不落日志
	log.Printf("[auth] 登录请求 username=%s ip=%s", req.Username, c.ClientIP())

	user, err := h.Store.GetUserByUsername(req.Username)
	// 用户不存在与密码错误返回同一提示，避免账号探测
	if err != nil || user == nil ||
		bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)) != nil {
		Fail(c, 401, "账号或密码错误")
		return
	}

	token, err := auth.Sign(h.Cfg.JWTSecret, h.Cfg.TokenTTL, user)
	if err != nil {
		Fail(c, 500, "生成登录令牌失败")
		return
	}
	OK(c, gin.H{"token": token, "user": user})
}

// Register 用户注册：参数校验 → 唯一性校验 → bcrypt 加密 → 事务入库。
// 路由：POST /api/v1/register。
func (h *Handler) Register(c *gin.Context) {
	var req struct {
		Username string `json:"username" binding:"required"`
		Email    string `json:"email" binding:"required"`
		Password string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, "请填写完整的注册信息")
		return
	}
	// 记录注册请求日志，密码不落日志
	log.Printf("[auth] 注册请求 username=%s email=%s ip=%s", req.Username, req.Email, c.ClientIP())

	user, err := h.AuthSvc.Register(service.RegisterInput{
		Username: req.Username,
		Email:    req.Email,
		Password: req.Password,
	})
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidUsername),
			errors.Is(err, service.ErrInvalidEmail),
			errors.Is(err, service.ErrInvalidPassword),
			errors.Is(err, service.ErrUsernameTaken),
			errors.Is(err, service.ErrEmailTaken):
			// 参数非法或账号/邮箱已被占用，统一按 400 返回明确的业务提示
			Fail(c, http.StatusBadRequest, err.Error())
		default:
			log.Printf("[auth] 注册失败 username=%s err=%v", req.Username, err)
			Fail(c, http.StatusInternalServerError, "注册失败，请稍后重试")
		}
		return
	}

	// 密码哈希不会出现在响应中（model.User 上 json:"-"）
	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "注册成功",
		"data": gin.H{
			"id":       user.ID,
			"username": user.Username,
			"email":    user.Email,
		},
	})
}

// Me 返回当前登录用户信息，前端刷新页面时校验登录态。
func (h *Handler) Me(c *gin.Context) {
	user, err := h.Store.GetUserByID(currentUserID(c))
	if err != nil {
		Fail(c, 401, "登录已失效")
		return
	}
	OK(c, user)
}
