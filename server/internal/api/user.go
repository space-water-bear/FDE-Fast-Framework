package api

import (
	"github.com/gin-gonic/gin"

	"fde-admin/internal/service"
	"fde-admin/internal/utils"
)

type userCreateReq struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
	Nickname string `json:"nickname"`
	Enabled  *bool  `json:"enabled"`
	RoleIDs  []uint `json:"roleIds"`
}

type userUpdateReq struct {
	Nickname string `json:"nickname"`
	Password string `json:"password"`
	Enabled  *bool  `json:"enabled"`
	RoleIDs  []uint `json:"roleIds"`
}

func ListUsers(c *gin.Context) {
	page, pageSize := utils.ParsePagination(c)
	users, total, err := service.ListUsers(page, pageSize, c.Query("keyword"))
	if err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, gin.H{"list": users, "total": total})
}

func CreateUser(c *gin.Context) {
	var req userCreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	if err := service.CreateUser(req.Username, req.Password, req.Nickname, enabled, req.RoleIDs); err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, nil)
}

func UpdateUser(c *gin.Context) {
	id, err := utils.ParseID(c)
	if err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	var req userUpdateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	if err := service.UpdateUser(id, req.Nickname, req.Password, req.Enabled, req.RoleIDs); err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, nil)
}

func DeleteUser(c *gin.Context) {
	id, err := utils.ParseID(c)
	if err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	claims := utils.GetClaims(c)
	if claims != nil && claims.UserID == id {
		utils.Fail(c, "不能删除自己")
		return
	}
	if err := service.DeleteUser(id); err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, nil)
}
