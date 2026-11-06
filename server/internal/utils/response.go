package utils

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

const (
	CodeSuccess   = 0     // 成功
	CodeError     = 40001 // 业务错误
	CodeForbidden = 40003 // 无权限
	CodeNoAuth    = 40004 // 未登录/令牌失效
)

type Response struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data any    `json:"data"`
}

func OK(c *gin.Context, data any) {
	c.JSON(http.StatusOK, Response{Code: CodeSuccess, Msg: "success", Data: data})
}

func Fail(c *gin.Context, msg string) {
	FailData(c, msg, nil)
}

// FailData 业务错误并在 data 中携带细分信息(如 errCode)
func FailData(c *gin.Context, msg string, data any) {
	c.JSON(http.StatusOK, Response{Code: CodeError, Msg: msg, Data: data})
}

func NoAuth(c *gin.Context, msg string) {
	c.JSON(http.StatusUnauthorized, Response{Code: CodeNoAuth, Msg: msg})
	c.Abort()
}

func Forbidden(c *gin.Context, msg string) {
	c.JSON(http.StatusForbidden, Response{Code: CodeForbidden, Msg: msg})
	c.Abort()
}
