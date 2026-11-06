package router

import (
	"github.com/gin-gonic/gin"

	"fde-admin/internal/api"
)

func init() {
	RegisterGenerated(registerCustomer)
}

func registerCustomer(g *gin.RouterGroup) {
	g.GET("/customer/list", api.ListCustomers)
	g.POST("/customer", api.CreateCustomer)
	g.PUT("/customer/:id", api.UpdateCustomer)
	g.DELETE("/customer/:id", api.DeleteCustomer)
}
