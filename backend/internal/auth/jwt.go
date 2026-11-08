// Package auth 负责 JWT 令牌的签发与校验。
package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"ssedeploy/internal/model"
)

// Claims 自定义 JWT 负载，携带用户 id 与账号名。
type Claims struct {
	UserID   int64  `json:"uid"`
	Username string `json:"username"`
	jwt.RegisteredClaims
}

// Sign 为登录成功的用户签发令牌。
func Sign(secret string, ttl time.Duration, u *model.User) (string, error) {
	now := time.Now()
	claims := Claims{
		UserID:   u.ID,
		Username: u.Username,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "ssedeploy",
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

// Parse 校验令牌并返回负载信息。
func Parse(secret, tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(t *jwt.Token) (any, error) {
		// 只接受 HMAC 签名，防止算法混淆攻击
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("不支持的签名算法")
		}
		return []byte(secret), nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, errors.New("令牌无效")
	}
	return claims, nil
}
