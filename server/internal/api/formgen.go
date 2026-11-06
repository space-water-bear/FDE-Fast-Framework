package api

import (
	"io"

	"github.com/gin-gonic/gin"

	"fde-admin/internal/service"
	"fde-admin/internal/utils"
)

// ListFormgenObjects 业务对象分页列表
func ListFormgenObjects(c *gin.Context) {
	page, pageSize := utils.ParsePagination(c)
	list, total, err := service.ListBizObjects(page, pageSize, c.Query("keyword"))
	if err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, gin.H{"list": list, "total": total})
}

// CreateFormgenObject 保存对象+字段, 校验并落库 sys_biz_objects 与 sys_biz_fields
func CreateFormgenObject(c *gin.Context) {
	var in service.FormgenObjectInput
	if err := c.ShouldBindJSON(&in); err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	var userID uint
	if claims := utils.GetClaims(c); claims != nil {
		userID = claims.UserID
	}
	res, err := service.CreateBizObject(in, userID)
	if err != nil {
		utils.Fail(c, err.Error())
		return
	}
	if !res.OK {
		utils.FailData(c, res.Error.Msg, gin.H{"errCode": res.Error.ErrCode})
		return
	}
	obj, fields, err := service.GetBizObjectByKey(in.ObjectKey)
	if err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, gin.H{"object": obj, "fields": fields, "schema": res.Schema})
}

// UpdateFormgenObject 更新对象+字段(前端可视化编辑器写回), 校验并落库
func UpdateFormgenObject(c *gin.Context) {
	var in service.FormgenObjectInput
	if err := c.ShouldBindJSON(&in); err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	var userID uint
	if claims := utils.GetClaims(c); claims != nil {
		userID = claims.UserID
	}
	res, err := service.UpdateBizObject(c.Param("objectKey"), in, userID)
	if err != nil {
		utils.Fail(c, err.Error())
		return
	}
	if !res.OK {
		utils.FailData(c, res.Error.Msg, gin.H{"errCode": res.Error.ErrCode})
		return
	}
	obj, fields, err := service.GetBizObjectByKey(c.Param("objectKey"))
	if err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, gin.H{"object": obj, "fields": fields, "schema": res.Schema})
}

// GetFormgenSchema 渲染器拉取最新发布 schema
func GetFormgenSchema(c *gin.Context) {
	schema, err := service.GetFormSchemaByKey(c.Param("objectKey"))
	if err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, schema)
}

// FormgenApply 两段式确认写盘(与 apply_files 工具同实现, 从会话取 userID)
func FormgenApply(c *gin.Context) {
	var in service.ApplyFilesInput
	if err := c.ShouldBindJSON(&in); err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	var userID uint
	if claims := utils.GetClaims(c); claims != nil {
		userID = claims.UserID
	}
	res, err := service.ApplyFormgenFiles(in, userID)
	if err != nil {
		utils.Fail(c, err.Error())
		return
	}
	if !res.OK {
		utils.FailData(c, res.Error.Msg, gin.H{"errCode": res.Error.ErrCode, "rollback": res.Rollback})
		return
	}
	utils.OK(c, res)
}

// PreviewFormgenSchema 表单 schema 预览(强制 dryRun=true 不落库)
func PreviewFormgenSchema(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	res, err := service.PreviewFormSchema(string(body))
	if err != nil {
		utils.Fail(c, err.Error())
		return
	}
	if !res.OK {
		utils.FailData(c, res.Error.Msg, gin.H{"errCode": res.Error.ErrCode})
		return
	}
	utils.OK(c, res)
}
