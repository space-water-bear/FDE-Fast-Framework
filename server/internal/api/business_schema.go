package api

import (
	"fde-admin/internal/model"
	"fde-admin/internal/service"
	"fde-admin/internal/utils"
	"github.com/gin-gonic/gin"
)

// Business schema access uses the business page's RBAC, not workbench ownership.
func GetBusinessSchema(c *gin.Context) {
	schema, err := service.GetFormSchemaByKeyInProject(model.DefaultProjectID, c.Param("objectKey"))
	if err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, schema)
}
