package api

import (
	"fde-admin/internal/model"
	"fde-admin/internal/service"
	"fde-admin/internal/utils"
	"github.com/gin-gonic/gin"
	"net/http"
)

func botToMap(b model.AiBot) gin.H {
	return gin.H{
		"id": b.ID, "name": b.Name, "platform": b.Platform, "agentId": b.AgentID,
		"enabled": b.Enabled, "remark": b.Remark, "createdAt": b.CreatedAt,
		"callbackPath": "", "botId": b.BotID, "clientId": b.ClientID,
		"hasSecret": b.Secret != "", "hasClientSecret": b.ClientSecret != "",
		"connection": botConnectionStatus(b),
	}
}

func botConnectionStatus(b model.AiBot) service.AiBotConnection {
	if b.Platform == "dingtalk" {
		return service.DingTalkBotConnectionStatus(b)
	}
	return service.WeComBotConnectionStatus(b)
}

func ListAiBots(c *gin.Context) {
	list, err := service.ListAiBots(c.Query("keyword"))
	if err != nil {
		utils.Fail(c, "读取机器人失败")
		return
	}
	items := make([]gin.H, 0, len(list))
	for _, b := range list {
		items = append(items, botToMap(b))
	}
	utils.OK(c, gin.H{"list": items, "total": len(items)})
}

func SaveAiBot(c *gin.Context) {
	var id uint
	if c.Param("id") != "" {
		parsed, err := utils.ParseID(c)
		if err != nil {
			utils.Fail(c, "参数错误")
			return
		}
		id = parsed
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16384)
	var in service.AiBotInput
	if err := c.ShouldBindJSON(&in); err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	b, err := service.SaveAiBot(id, in)
	if err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, botToMap(*b))
}

func DeleteAiBot(c *gin.Context) {
	id, err := utils.ParseID(c)
	if err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	if err := service.DeleteAiBot(id); err != nil {
		utils.Fail(c, "删除机器人失败")
		return
	}
	utils.OK(c, nil)
}

// AiBotCallback explicitly retires the old inbound HTTP mode. Both platforms
// now authenticate through outbound long connections.
func AiBotCallback(c *gin.Context) {
	id, err := utils.ParseID(c)
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	if _, err := service.GetAiBot(id, true); err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	c.Status(http.StatusGone)
}
