package router

import (
	"github.com/gin-gonic/gin"

	"fde-admin/internal/api"
)

func init() {
	RegisterGenerated(registerProduct)
}

func registerProduct(g *gin.RouterGroup) {
	g.GET("/product/list", api.ListProducts)
	g.POST("/product", api.CreateProduct)
	g.PUT("/product/:id", api.UpdateProduct)
	g.DELETE("/product/:id", api.DeleteProduct)
}
