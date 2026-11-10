package api

import (
	"io"
	"strconv"

	"github.com/gin-gonic/gin"

	"fde-admin/internal/service"
	"fde-admin/internal/utils"
)

// ListFormgenObjects lists only the current engineering project.
func ListFormgenObjects(c *gin.Context) {
	scope, ok := engineeringRequestScope(c)
	if !ok {
		return
	}
	page, pageSize := utils.ParsePagination(c)
	list, total, err := service.ListBizObjectsInProject(scope.ProjectID, page, pageSize, c.Query("keyword"))
	if err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, gin.H{"list": list, "total": total})
}

func CreateFormgenObject(c *gin.Context) {
	scope, ok := engineeringRequestScope(c)
	if !ok {
		return
	}
	var in service.FormgenObjectInput
	if err := c.ShouldBindJSON(&in); err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	res, err := service.CreateBizObjectInProject(scope.ProjectID, in, scope.ActorID)
	if err != nil {
		utils.Fail(c, err.Error())
		return
	}
	if !res.OK {
		utils.FailData(c, res.Error.Msg, gin.H{"errCode": res.Error.ErrCode})
		return
	}
	key, _ := res.Schema["objectKey"].(string)
	obj, fields, err := service.GetBizObjectByKeyInProject(scope.ProjectID, key)
	if err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, gin.H{"object": obj, "fields": fields, "schema": res.Schema})
}

func UpdateFormgenObject(c *gin.Context) {
	scope, ok := engineeringRequestScope(c)
	if !ok {
		return
	}
	var in service.FormgenObjectInput
	if err := c.ShouldBindJSON(&in); err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	key := c.Param("objectKey")
	res, err := service.UpdateBizObjectInProject(scope.ProjectID, key, in, scope.ActorID)
	if err != nil {
		utils.Fail(c, err.Error())
		return
	}
	if !res.OK {
		utils.FailData(c, res.Error.Msg, gin.H{"errCode": res.Error.ErrCode})
		return
	}
	obj, fields, err := service.GetBizObjectByKeyInProject(scope.ProjectID, key)
	if err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, gin.H{"object": obj, "fields": fields, "schema": res.Schema})
}

// Default reads are active; editing explicitly asks for version=draft.
func GetFormgenSchema(c *gin.Context) {
	scope, ok := engineeringRequestScope(c)
	if !ok {
		return
	}
	key, version := c.Param("objectKey"), c.Query("version")
	var schema map[string]any
	var err error
	switch version {
	case "", "active":
		schema, err = service.GetFormSchemaByKeyInProject(scope.ProjectID, key)
	case "draft":
		schema, err = service.GetDraftFormSchemaByKeyInProject(scope.ProjectID, key)
	default:
		n, e := strconv.Atoi(version)
		if e != nil || n <= 0 {
			utils.Fail(c, "版本必须为 active、draft 或正整数")
			return
		}
		schema, err = service.GetFormSchemaVersionInProject(scope.ProjectID, key, n)
	}
	if err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, schema)
}

func ActivateFormgenObject(c *gin.Context) {
	scope, ok := engineeringRequestScope(c)
	if !ok {
		return
	}
	var in struct {
		Version               int  `json:"version"`
		ExpectedActiveVersion *int `json:"expectedActiveVersion"`
	}
	if err := c.ShouldBindJSON(&in); err != nil || in.Version <= 0 || in.ExpectedActiveVersion == nil {
		utils.Fail(c, "激活需要 version 和 expectedActiveVersion")
		return
	}
	key := c.Param("objectKey")
	res, err := service.ActivateFormSchemaInProject(scope.ProjectID, key, in.Version, *in.ExpectedActiveVersion, scope.ActorID)
	if err != nil {
		utils.Fail(c, err.Error())
		return
	}
	if !res.OK {
		utils.FailData(c, res.Error.Msg, gin.H{"errCode": res.Error.ErrCode})
		return
	}
	obj, _, err := service.GetBizObjectByKeyInProject(scope.ProjectID, key)
	if err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, gin.H{"object": obj, "schema": res.Schema})
}

// FormgenApply 两段式确认写盘(与 apply_files 工具同实现, 从会话取 userID)
func FormgenApply(c *gin.Context) {
	scope, ok := engineeringRequestScope(c)
	if !ok {
		return
	}
	var in service.ApplyFilesInput
	if err := c.ShouldBindJSON(&in); err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	res, err := service.ApplyFormgenFilesInProject(in, scope.ActorID, scope.ProjectID)
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
	var res *service.FormgenResult
	if utils.GetClaims(c) != nil {
		scope, ok := engineeringRequestScope(c)
		if !ok {
			return
		}
		res, err = service.PreviewFormSchemaInProject(scope.ProjectID, string(body))
	} else {
		res, err = service.PreviewFormSchema(string(body))
	}
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
