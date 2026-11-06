package api

import (
	"github.com/gin-gonic/gin"

	"fde-admin/internal/model"
	"fde-admin/internal/service"
	"fde-admin/internal/utils"
)

type productListQuery struct {
	Name  string `form:"name"`
	Stock *int   `form:"stock"`
}

func ListProducts(c *gin.Context) {
	var query productListQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	page, pageSize := utils.ParsePagination(c)
	list, total, err := service.ListProducts(page, pageSize, query.Name, query.Stock)
	if err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, gin.H{"list": list, "total": total})
}

func CreateProduct(c *gin.Context) {
	var m model.Product
	if err := c.ShouldBindJSON(&m); err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	if err := service.CreateProduct(&m); err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, m)
}

func UpdateProduct(c *gin.Context) {
	id, err := utils.ParseID(c)
	if err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	var m model.Product
	if err := c.ShouldBindJSON(&m); err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	if err := service.UpdateProduct(id, &m); err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, nil)
}

func DeleteProduct(c *gin.Context) {
	id, err := utils.ParseID(c)
	if err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	if err := service.DeleteProduct(id); err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, nil)
}
