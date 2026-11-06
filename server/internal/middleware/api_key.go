package middleware

import (
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"fde-admin/internal/global"
	"fde-admin/internal/utils"
)

// ApiKeyAuth 服务间 API Key 鉴权: 校验 x-api-key 与配置 mcp.api-key 一致。
// 通过后写入 c.Set("serviceAuth", "mcp-gateway"); 配置为空或 header 缺失/不匹配返回 401 + code 40004。
func ApiKeyAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		method := c.Request.Method
		path := c.Request.URL.Path
		fields := []zap.Field{
			zap.String("method", method),
			zap.String("path", path),
			zap.String("source", "mcp-gateway"),
		}

		cfgKey := ""
		if global.Config != nil {
			cfgKey = global.Config.MCP.APIKey
		}
		if cfgKey == "" {
			global.Logger.Warn("mcp api key auth rejected: server key empty", fields...)
			utils.NoAuth(c, "服务未配置 mcp.api-key")
			return
		}
		if key := c.GetHeader("x-api-key"); key != "" && key == cfgKey {
			c.Set("serviceAuth", "mcp-gateway")
			global.Logger.Info("mcp api key auth passed", fields...)
			c.Next()
			return
		}
		global.Logger.Warn("mcp api key auth rejected: key missing or mismatch", fields...)
		utils.NoAuth(c, "API Key 无效或缺失")
	}
}
