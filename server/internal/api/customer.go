package api

import (
	"github.com/gin-gonic/gin"

	"fde-admin/internal/model"
	"fde-admin/internal/service"
	"fde-admin/internal/utils"
)

type customerListQuery struct {
	Name string `form:"name"`
	Age  *int   `form:"age"`
}

func ListCustomers(c *gin.Context) {
	var query customerListQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	page, pageSize := utils.ParsePagination(c)
	list, total, err := service.ListCustomers(page, pageSize, query.Name, query.Age)
	if err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, gin.H{"list": list, "total": total})
}

func CreateCustomer(c *gin.Context) {
	var m model.Customer
	if err := c.ShouldBindJSON(&m); err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	if err := service.CreateCustomer(&m); err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, m)
}

func UpdateCustomer(c *gin.Context) {
	id, err := utils.ParseID(c)
	if err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	var m model.Customer
	if err := c.ShouldBindJSON(&m); err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	if err := service.UpdateCustomer(id, &m); err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, nil)
}

func DeleteCustomer(c *gin.Context) {
	id, err := utils.ParseID(c)
	if err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	if err := service.DeleteCustomer(id); err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, nil)
}
