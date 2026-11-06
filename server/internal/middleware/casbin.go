package middleware

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"fde-admin/internal/global"
	"fde-admin/internal/utils"
)

// CasbinAuth RBAC 鉴权: 用户经 g(用户,角色) 获得角色, 角色经 p(角色,路径,方法) 获得接口权限
func CasbinAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		claims := utils.GetClaims(c)
		if claims == nil {
			utils.NoAuth(c, "未登录")
			return
		}
		sub := strconv.Itoa(int(claims.UserID))
		obj := c.Request.URL.Path
		act := c.Request.Method
		ok, err := global.Enforcer.Enforce(sub, obj, act)
		if err != nil || !ok {
			utils.Forbidden(c, "权限不足，请联系管理员")
			return
		}
		c.Next()
	}
}
