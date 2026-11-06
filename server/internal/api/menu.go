package api

import (
	"github.com/gin-gonic/gin"

	"fde-admin/internal/model"
	"fde-admin/internal/service"
	"fde-admin/internal/utils"
)

type menuReq struct {
	ParentID  uint   `json:"parentId"`
	Path      string `json:"path"`
	Name      string `json:"name"`
	Component string `json:"component"`
	Title     string `json:"title" binding:"required"`
	Icon      string `json:"icon"`
	Type      int    `json:"type"`
	Sort      int    `json:"sort"`
	Hidden    bool   `json:"hidden"`
	KeepAlive bool   `json:"keepAlive"`
	Affix     bool   `json:"affix"`
	HideTab   bool   `json:"hideTab"`
	ApiPath   string `json:"apiPath"`
	ApiMethod string `json:"apiMethod"`
}

func bindMenu(req menuReq) *model.Menu {
	return &model.Menu{
		ParentID:  req.ParentID,
		Path:      req.Path,
		Name:      req.Name,
		Component: req.Component,
		Title:     req.Title,
		Icon:      req.Icon,
		Type:      req.Type,
		Sort:      req.Sort,
		Hidden:    req.Hidden,
		KeepAlive: req.KeepAlive,
		Affix:     req.Affix,
		HideTab:   req.HideTab,
		ApiPath:   req.ApiPath,
		ApiMethod: req.ApiMethod,
	}
}

func ListMenuTree(c *gin.Context) {
	menus, err := service.ListMenuTree()
	if err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, gin.H{"list": menus})
}

func CreateMenu(c *gin.Context) {
	var req menuReq
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	if err := service.CreateMenu(bindMenu(req)); err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, nil)
}

func UpdateMenu(c *gin.Context) {
	id, err := utils.ParseID(c)
	if err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	var req menuReq
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	if err := service.UpdateMenu(id, bindMenu(req)); err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, nil)
}

func DeleteMenu(c *gin.Context) {
	id, err := utils.ParseID(c)
	if err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	if err := service.DeleteMenu(id); err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, nil)
}
