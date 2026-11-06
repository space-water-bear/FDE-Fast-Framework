package router

import "github.com/gin-gonic/gin"

// generatedRegistrars 代码生成器产出的路由注册表, 由生成文件的 init() 自动注册
var generatedRegistrars []func(*gin.RouterGroup)

func RegisterGenerated(fn func(*gin.RouterGroup)) {
	generatedRegistrars = append(generatedRegistrars, fn)
}

func applyGenerated(g *gin.RouterGroup) {
	for _, fn := range generatedRegistrars {
		fn(g)
	}
}
