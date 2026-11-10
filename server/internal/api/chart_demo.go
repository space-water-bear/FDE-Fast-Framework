package api

import (
	"github.com/gin-gonic/gin"

	"fde-admin/internal/service"
	"fde-admin/internal/utils"
)

// ChartDemoSales serves the read-only, explicitly labeled chart example dataset.
func ChartDemoSales(c *gin.Context) {
	data, err := service.GetChartDemoSales(c.Query("startDate"), c.Query("endDate"), c.Query("storeId"))
	if err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, data)
}
