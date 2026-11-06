package api

import (
	"github.com/gin-gonic/gin"

	"fde-admin/internal/service"
	"fde-admin/internal/utils"
)

type roleCreateReq struct {
	Name        string `json:"name" binding:"required"`
	Code        string `json:"code" binding:"required"`
	Description string `json:"description"`
	Enabled     *bool  `json:"enabled"`
	MenuIDs     []uint `json:"menuIds"`
}

type roleUpdateReq struct {
	Name        string `json:"name" binding:"required"`
	Description string `json:"description"`
	Enabled     *bool  `json:"enabled"`
	MenuIDs     []uint `json:"menuIds"`
}

func ListRoles(c *gin.Context) {
	roles, err := service.ListRoles()
	if err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, gin.H{"list": roles})
}

func GetRole(c *gin.Context) {
	id, err := utils.ParseID(c)
	if err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	role, menuIDs, err := service.GetRole(id)
	if err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, gin.H{"role": role, "menuIds": menuIDs})
}

func CreateRole(c *gin.Context) {
	var req roleCreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	if err := service.CreateRole(req.Name, req.Code, req.Description, enabled, req.MenuIDs); err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, nil)
}

func UpdateRole(c *gin.Context) {
	id, err := utils.ParseID(c)
	if err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	var req roleUpdateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	if err := service.UpdateRole(id, req.Name, req.Description, req.Enabled, req.MenuIDs); err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, nil)
}

func DeleteRole(c *gin.Context) {
	id, err := utils.ParseID(c)
	if err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	if err := service.DeleteRole(id); err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, nil)
}
