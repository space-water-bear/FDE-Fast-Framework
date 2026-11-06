package api

import (
	"github.com/gin-gonic/gin"

	"fde-admin/internal/service"
	"fde-admin/internal/utils"
)

type dictTypeReq struct {
	Name   string `json:"name" binding:"required"`
	Code   string `json:"code" binding:"required"`
	Remark string `json:"remark"`
}

type dictDataReq struct {
	TypeID  uint   `json:"typeId" binding:"required"`
	Label   string `json:"label" binding:"required"`
	Value   string `json:"value" binding:"required"`
	Sort    int    `json:"sort"`
	Enabled *bool  `json:"enabled"`
	Remark  string `json:"remark"`
}

func ListDictTypes(c *gin.Context) {
	list, err := service.ListDictTypes()
	if err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, gin.H{"list": list})
}

func CreateDictType(c *gin.Context) {
	var req dictTypeReq
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	if err := service.CreateDictType(req.Name, req.Code, req.Remark); err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, nil)
}

func UpdateDictType(c *gin.Context) {
	id, err := utils.ParseID(c)
	if err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	var req dictTypeReq
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	if err := service.UpdateDictType(id, req.Name, req.Code, req.Remark); err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, nil)
}

func DeleteDictType(c *gin.Context) {
	id, err := utils.ParseID(c)
	if err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	if err := service.DeleteDictType(id); err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, nil)
}

func ListDictData(c *gin.Context) {
	typeID, err := utils.ParseUintQuery(c, "typeId")
	if err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	list, err := service.ListDictData(typeID)
	if err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, gin.H{"list": list})
}

func CreateDictData(c *gin.Context) {
	var req dictDataReq
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	if err := service.CreateDictData(req.TypeID, req.Label, req.Value, req.Sort, enabled, req.Remark); err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, nil)
}

func UpdateDictData(c *gin.Context) {
	id, err := utils.ParseID(c)
	if err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	var req dictDataReq
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	if err := service.UpdateDictData(id, req.Label, req.Value, req.Sort, enabled, req.Remark); err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, nil)
}

func DeleteDictData(c *gin.Context) {
	id, err := utils.ParseID(c)
	if err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	if err := service.DeleteDictData(id); err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, nil)
}
