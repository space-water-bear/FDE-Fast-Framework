package middleware

import (
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"fde-admin/internal/global"
)

// ZapLogger 结构化请求日志: 方法/路径/状态码/IP/耗时
func ZapLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		c.Next()
		fields := []zap.Field{
			zap.Int("status", c.Writer.Status()),
			zap.String("method", c.Request.Method),
			zap.String("path", path),
			zap.String("ip", c.ClientIP()),
			zap.Duration("latency", time.Since(start)),
			zap.String("ua", c.GetHeader("User-Agent")),
		}
		switch {
		case c.Writer.Status() >= 500:
			global.Logger.Error("request", fields...)
		case c.Writer.Status() >= 400:
			global.Logger.Warn("request", fields...)
		default:
			global.Logger.Info("request", fields...)
		}
	}
}

// ZapRecovery panic 恢复并输出完整堆栈
func ZapRecovery() gin.HandlerFunc {
	return gin.CustomRecovery(func(c *gin.Context, err any) {
		global.Logger.Error("panic recovered",
			zap.Any("error", err),
			zap.String("path", c.Request.URL.Path),
			zap.Stack("stack"),
		)
		c.AbortWithStatus(500)
	})
}
