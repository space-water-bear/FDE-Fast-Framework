package api

import (
	"github.com/gin-gonic/gin"

	"fde-admin/internal/service"
	"fde-admin/internal/utils"
)

func ListOplogs(c *gin.Context) {
	page, pageSize := utils.ParsePagination(c)
	list, total, err := service.ListOperationLogs(page, pageSize, c.Query("keyword"))
	if err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, gin.H{"list": list, "total": total})
}

func DeleteOplog(c *gin.Context) {
	id, err := utils.ParseID(c)
	if err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	if err := service.DeleteOperationLog(id); err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, nil)
}

func ClearOplogs(c *gin.Context) {
	if err := service.ClearOperationLogs(); err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, nil)
}
