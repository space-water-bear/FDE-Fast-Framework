package middleware

import (
	"bytes"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"fde-admin/internal/global"
	"fde-admin/internal/model"
	"fde-admin/internal/utils"
)

const maxLogBody = 4096

var (
	maskRe     = regexp.MustCompile(`"(password|captchaCode|refreshToken|token)"\s*:\s*"[^"]*"`)
	respCodeRe = regexp.MustCompile(`"code"\s*:\s*(-?\d+)`)
	respMsgRe  = regexp.MustCompile(`"msg"\s*:\s*"([^"]*)"`)
)

// captureWriter 捕获响应体用于记录业务码
type captureWriter struct {
	gin.ResponseWriter
	body bytes.Buffer
}

func (w *captureWriter) Write(b []byte) (int, error) {
	w.body.Write(b)
	return w.ResponseWriter.Write(b)
}

func (w *captureWriter) WriteString(s string) (int, error) {
	w.body.WriteString(s)
	return w.ResponseWriter.WriteString(s)
}

// Replay only the sampled prefix, then continue from the untouched request body.
type loggedRequestBody struct {
	io.Reader
	io.Closer
}

// OperationLog 操作日志中间件: 记录方法/路径/用户/耗时/状态码/请求体(脱敏)
func OperationLog() gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		if path == "/health" || path == "/api/auth/captcha" || strings.HasPrefix(path, "/api/oplog") || strings.HasPrefix(path, "/api/ai/bot/callback/") {
			c.Next()
			return
		}
		start := time.Now()

		body := ""
		// Robot credentials are write-only; never copy their management payload
		// into operation logs. Callbacks are also exempt from the 4 KiB log reader.
		if c.Request.Body != nil && path != "/api/ai/bot" && !strings.HasPrefix(path, "/api/ai/bot/") {
			raw, _ := io.ReadAll(io.LimitReader(c.Request.Body, maxLogBody))
			c.Request.Body = &loggedRequestBody{Reader: io.MultiReader(bytes.NewReader(raw), c.Request.Body), Closer: c.Request.Body}
			body = maskRe.ReplaceAllString(string(raw), `"$1":"***"`)
		}

		userID, username := parseLogUser(c)

		bw := &captureWriter{ResponseWriter: c.Writer}
		c.Writer = bw
		c.Next()

		code, msg := parseLogResponse(bw.body.String())
		log := &model.OperationLog{
			UserID:    userID,
			Username:  username,
			Method:    c.Request.Method,
			Path:      path,
			Query:     c.Request.URL.RawQuery,
			Body:      truncate(body, maxLogBody),
			IP:        c.ClientIP(),
			UserAgent: truncate(c.GetHeader("User-Agent"), 255),
			Status:    c.Writer.Status(),
			Code:      code,
			Msg:       truncate(msg, 255),
			LatencyMs: time.Since(start).Milliseconds(),
		}
		_ = global.DB.Create(log).Error
	}
}

func parseLogUser(c *gin.Context) (uint, string) {
	token := utils.GetToken(c)
	if token == "" {
		return 0, ""
	}
	claims, err := utils.ParseToken(token, global.Config.JWT.Secret)
	if err != nil {
		return 0, ""
	}
	return claims.UserID, claims.Username
}

func parseLogResponse(body string) (int, string) {
	code := 0
	if m := respCodeRe.FindStringSubmatch(body); len(m) > 1 {
		code, _ = strconv.Atoi(m[1])
	}
	msg := ""
	if m := respMsgRe.FindStringSubmatch(body); len(m) > 1 {
		msg = m[1]
	}
	return code, msg
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
