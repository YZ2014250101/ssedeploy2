package handler

import (
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"ssedeploy/internal/auth"
)

// Login 账号密码登录，成功后返回 JWT 与用户信息。
func (h *Handler) Login(c *gin.Context) {
	var req struct {
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, 400, "请输入账号和密码")
		return
	}

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

// Me 返回当前登录用户信息，前端刷新页面时校验登录态。
func (h *Handler) Me(c *gin.Context) {
	user, err := h.Store.GetUserByID(currentUserID(c))
	if err != nil {
		Fail(c, 401, "登录已失效")
		return
	}
	OK(c, user)
}
