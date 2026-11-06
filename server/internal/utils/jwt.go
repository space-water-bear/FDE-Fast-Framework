package utils

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

type Claims struct {
	UserID   uint   `json:"userId"`
	Username string `json:"username"`
	jwt.RegisteredClaims
}

var ErrTokenInvalid = errors.New("无效的令牌")

func GenerateToken(userID uint, username, secret string, expiresHours int, issuer string) (string, error) {
	now := time.Now()
	idBuf := make([]byte, 16)
	_, _ = rand.Read(idBuf)
	claims := Claims{
		UserID:   userID,
		Username: username,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        hex.EncodeToString(idBuf), // jti: 保证同秒签发也不重复
			Issuer:    issuer,
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Duration(expiresHours) * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(now),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

func ParseToken(tokenString, secret string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrTokenInvalid
		}
		return []byte(secret), nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, ErrTokenInvalid
	}
	return claims, nil
}

// GetToken 优先取 x-token 头, 兼容 Authorization: Bearer
// HashToken 令牌 SHA256 哈希(黑名单/活跃表只存哈希)
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func GetToken(c *gin.Context) string {
	t := c.GetHeader("x-token")
	if t == "" {
		t = c.GetHeader("Authorization")
		if len(t) > 7 && t[:7] == "Bearer " {
			t = t[7:]
		}
	}
	return t
}

func GetClaims(c *gin.Context) *Claims {
	if v, ok := c.Get("claims"); ok {
		if claims, ok := v.(*Claims); ok {
			return claims
		}
	}
	return nil
}
