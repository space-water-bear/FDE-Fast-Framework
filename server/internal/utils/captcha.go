package utils

import (
	"time"

	"github.com/mojocn/base64Captcha"
)

var captchaStore base64Captcha.Store

// InitCaptchaStore 初始化验证码内存存储(过期自动清理)
func InitCaptchaStore(expireMinutes int) {
	if expireMinutes <= 0 {
		expireMinutes = 5
	}
	captchaStore = base64Captcha.NewMemoryStore(10000, time.Duration(expireMinutes)*time.Minute)
}

// GenerateCaptcha 生成数字验证码, 返回 id 与 data-url 图片
func GenerateCaptcha() (id, picPath string, err error) {
	if captchaStore == nil {
		InitCaptchaStore(5)
	}
	driver := base64Captcha.NewDriverDigit(80, 240, 4, 0.7, 80)
	c := base64Captcha.NewCaptcha(driver, captchaStore)
	id, b64s, _, err := c.Generate()
	if err != nil {
		return "", "", err
	}
	return id, b64s, nil
}

// VerifyCaptcha 校验验证码, 正确即销毁(一次性)
func VerifyCaptcha(id, code string) bool {
	if captchaStore == nil || id == "" || code == "" {
		return false
	}
	return captchaStore.Verify(id, code, true)
}
