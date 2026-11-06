package api

import (
	"github.com/gin-gonic/gin"

	"fde-admin/internal/global"
	"fde-admin/internal/service"
	"fde-admin/internal/utils"
)

// Upload 文件上传(multipart/form-data, 字段名 file)
func Upload(c *gin.Context) {
	claims := utils.GetClaims(c)
	if claims == nil {
		utils.NoAuth(c, "未登录")
		return
	}
	fh, err := c.FormFile("file")
	if err != nil {
		utils.Fail(c, "请选择要上传的文件")
		return
	}
	file, err := service.SaveUpload(fh, claims.UserID, claims.Username, global.Config.Upload.Dir, global.Config.Upload.MaxSizeMB)
	if err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, gin.H{"file": file})
}

func ListFiles(c *gin.Context) {
	page, pageSize := utils.ParsePagination(c)
	list, total, err := service.ListFiles(page, pageSize, c.Query("keyword"))
	if err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, gin.H{"list": list, "total": total})
}

func DeleteFile(c *gin.Context) {
	id, err := utils.ParseID(c)
	if err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	if err := service.DeleteFile(id, global.Config.Upload.Dir); err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, nil)
}
