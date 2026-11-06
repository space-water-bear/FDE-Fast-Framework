package api

import (
	"fde-admin/internal/service"
	"fde-admin/internal/utils"
	"github.com/gin-gonic/gin"
)

func ListDevProjects(c *gin.Context) {
	scope, ok := requestEngineeringIdentity(c)
	if !ok {
		return
	}
	projects, err := service.ListDevProjects(scope.ActorID)
	if err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, gin.H{"list": projects})
}

func CreateDevProject(c *gin.Context) {
	scope, ok := requestEngineeringIdentity(c)
	if !ok {
		return
	}
	var in struct {
		Key  string `json:"key"`
		Name string `json:"name"`
	}
	if c.ShouldBindJSON(&in) != nil {
		utils.Fail(c, "参数错误")
		return
	}
	project, err := service.CreateDevProject(c.Request.Context(), scope.ActorID, in.Key, in.Name)
	if err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, project)
}

func CreateDevRun(c *gin.Context) {
	scope, ok := engineeringRequestScope(c)
	if !ok {
		return
	}
	var in struct {
		Requirement string `json:"requirement"`
	}
	if c.ShouldBindJSON(&in) != nil {
		utils.Fail(c, "参数错误")
		return
	}
	run, err := service.CreateDevRun(c.Request.Context(), scope, in.Requirement)
	if err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, run)
}

func GetDevRun(c *gin.Context) {
	scope, ok := engineeringRequestScope(c)
	if !ok {
		return
	}
	run, events, err := service.GetDevRun(scope, c.Param("key"))
	if err != nil {
		utils.Forbidden(c, err.Error())
		return
	}
	utils.OK(c, gin.H{"run": run, "events": events})
}

func CancelDevRun(c *gin.Context) {
	scope, ok := engineeringRequestScope(c)
	if !ok {
		return
	}
	if err := service.CancelDevRun(scope, c.Param("key")); err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, nil)
}
