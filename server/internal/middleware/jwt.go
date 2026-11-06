package middleware

import (
	"github.com/gin-gonic/gin"

	"fde-admin/internal/global"
	"fde-admin/internal/service"
	"fde-admin/internal/utils"
)

// JWTAuth 解析并校验 JWT, 将 claims 放入上下文
func JWTAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := utils.GetToken(c)
		if token == "" {
			utils.NoAuth(c, "未登录或非法访问，请先登录")
			return
		}
		claims, err := utils.ParseToken(token, global.Config.JWT.Secret)
		if err != nil {
			utils.NoAuth(c, "登录已过期，请重新登录")
			return
		}
		blacklisted, err := service.IsTokenBlacklisted(token)
		if err != nil {
			utils.NoAuth(c, "校验令牌状态失败")
			return
		}
		if blacklisted {
			utils.NoAuth(c, "令牌已被吊销，请重新登录")
			return
		}
		active, err := service.IsTokenActive(token)
		if err != nil {
			utils.NoAuth(c, "校验令牌状态失败")
			return
		}
		if !active {
			utils.NoAuth(c, "账号已在其他设备登录或令牌已失效，请重新登录")
			return
		}
		c.Set("claims", claims)
		c.Next()
	}
}
