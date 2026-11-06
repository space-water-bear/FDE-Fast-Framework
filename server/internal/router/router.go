package router

import (
	"github.com/gin-gonic/gin"

	"fde-admin/internal/api"
	"fde-admin/internal/global"
	"fde-admin/internal/middleware"
)

func NewRouter() *gin.Engine {
	r := gin.New()
	r.Use(middleware.ZapLogger(), middleware.ZapRecovery(), middleware.CORS(), middleware.OperationLog())

	// 上传文件静态访问(公开)
	r.Static("/uploads", global.Config.Upload.Dir)

	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	apiGroup := r.Group("/api")

	// API 统一禁止缓存: 认证/菜单等数据必须实时获取
	apiGroup.Use(func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Next()
	})

	// 认证: captcha/login/refresh 公开, userinfo/logout 只需登录
	auth := apiGroup.Group("/auth")
	auth.GET("/captcha", api.Captcha)
	auth.POST("/login", api.Login)
	auth.POST("/refresh", api.Refresh)
	auth.Use(middleware.JWTAuth())
	auth.GET("/userinfo", api.UserInfo)
	auth.POST("/logout", api.Logout)
	auth.GET("/devices", api.Devices)
	auth.DELETE("/devices/:id", api.KickDevice)
	auth.DELETE("/devices", api.KickOtherDevices)

	// 业务接口: JWT + casbin RBAC 双重校验
	authed := apiGroup.Group("", middleware.JWTAuth(), middleware.CasbinAuth())
	{
		authed.GET("/user/list", api.ListUsers)
		authed.POST("/user", api.CreateUser)
		authed.PUT("/user/:id", api.UpdateUser)
		authed.DELETE("/user/:id", api.DeleteUser)

		authed.GET("/role/list", api.ListRoles)
		authed.GET("/role/:id", api.GetRole)
		authed.POST("/role", api.CreateRole)
		authed.PUT("/role/:id", api.UpdateRole)
		authed.DELETE("/role/:id", api.DeleteRole)

		authed.GET("/menu/tree", api.ListMenuTree)
		authed.POST("/menu", api.CreateMenu)
		authed.PUT("/menu/:id", api.UpdateMenu)
		authed.DELETE("/menu/:id", api.DeleteMenu)

		authed.GET("/codegen/options", api.CodegenOptions)
		authed.POST("/codegen/preview", api.CodegenPreview)
		authed.POST("/codegen/generate", api.CodegenGenerate)

		authed.GET("/formgen/objects", api.ListFormgenObjects)
		authed.POST("/formgen/objects", api.CreateFormgenObject)
		authed.PUT("/formgen/objects/:objectKey", api.UpdateFormgenObject)
		authed.GET("/formgen/schema/:objectKey", api.GetFormgenSchema)
		authed.POST("/formgen/schema/preview", api.PreviewFormgenSchema)
		authed.POST("/formgen/apply", api.FormgenApply)

		authed.GET("/dict/types", api.ListDictTypes)
		authed.POST("/dict/type", api.CreateDictType)
		authed.PUT("/dict/type/:id", api.UpdateDictType)
		authed.DELETE("/dict/type/:id", api.DeleteDictType)
		authed.GET("/dict/data/list", api.ListDictData)
		authed.POST("/dict/data", api.CreateDictData)
		authed.PUT("/dict/data/:id", api.UpdateDictData)
		authed.DELETE("/dict/data/:id", api.DeleteDictData)

		authed.GET("/oplog/list", api.ListOplogs)
		authed.DELETE("/oplog/:id", api.DeleteOplog)
		authed.DELETE("/oplog/clear", api.ClearOplogs)

		authed.POST("/file/upload", api.Upload)
		authed.GET("/file/list", api.ListFiles)
		authed.DELETE("/file/:id", api.DeleteFile)

		ai := authed.Group("/ai")
		{
			ai.GET("/provider/list", api.ListAiProviders)
			ai.POST("/provider", api.CreateAiProvider)
			ai.PUT("/provider/:id", api.UpdateAiProvider)
			ai.DELETE("/provider/:id", api.DeleteAiProvider)
			ai.POST("/provider/:id/fetch-models", api.FetchAiProviderModels)

			ai.GET("/agent/list", api.ListAiAgents)
			ai.POST("/agent", api.CreateAiAgent)
			ai.PUT("/agent/:id", api.UpdateAiAgent)
			ai.DELETE("/agent/:id", api.DeleteAiAgent)

			ai.GET("/skill/list", api.ListAiSkills)
			ai.POST("/skill", api.SaveAiSkill)
			ai.PUT("/skill/:id", api.SaveAiSkill)
			ai.DELETE("/skill/:id", api.DeleteAiSkill)

			ai.GET("/tool/list", api.ListAiTools)
			ai.POST("/tool", api.SaveAiTool)
			ai.PUT("/tool/:id", api.SaveAiTool)
			ai.DELETE("/tool/:id", api.DeleteAiTool)

			ai.GET("/mcp/list", api.ListAiMcpServers)
			ai.POST("/mcp", api.SaveAiMcpServer)
			ai.PUT("/mcp/:id", api.SaveAiMcpServer)
			ai.DELETE("/mcp/:id", api.DeleteAiMcpServer)

			ai.GET("/session/list", api.ListAiSessions)
			ai.POST("/session", api.CreateAiSession)
			ai.PUT("/session/:id", api.RenameAiSession)
			ai.DELETE("/session/:id", api.DeleteAiSession)
			ai.DELETE("/session/:id/messages", api.ClearAiSession)
			ai.GET("/session/:id/messages", api.ListAiMessages)
			ai.POST("/session/:id/chat", api.ChatAi)
			ai.POST("/session/:id/chat/stream", api.StreamChatAi)
			ai.POST("/session/:id/regenerate", api.RegenerateAiSession)
			ai.POST("/session/:id/messages/:mid/resend", api.ResendAiMessage)

			ai.GET("/usage/list", api.ListAiUsages)
			ai.GET("/usage/summary", api.AiUsageSummary)
		}
	}

	// MCP 网关专用服务间端点: 仅 x-api-key 鉴权(不经 JWT+casbin), 供 cmd/mcp-gateway 调用
	mcp := r.Group("/api/mcp", middleware.ApiKeyAuth())
	{
		mcp.POST("/formgen/schema/preview", api.PreviewFormgenSchema)
		mcp.POST("/codegen/preview", api.CodegenPreview)
		mcp.POST("/formgen/apply", api.FormgenApply)
	}

	// 代码生成器产出的业务路由(init 注册表自动挂载)
	applyGenerated(authed)

	return r
}
