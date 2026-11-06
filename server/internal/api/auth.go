package api

import (
	"github.com/gin-gonic/gin"

	"fde-admin/internal/global"
	"fde-admin/internal/service"
	"fde-admin/internal/utils"
)

type loginReq struct {
	Username    string `json:"username" binding:"required"`
	Password    string `json:"password" binding:"required"`
	CaptchaID   string `json:"captchaId"`
	CaptchaCode string `json:"captchaCode"`
}

type refreshReq struct {
	RefreshToken string `json:"refreshToken" binding:"required"`
}

type logoutReq struct {
	RefreshToken string `json:"refreshToken"`
}

// Captcha 获取登录验证码(captcha.enabled=false 时前端隐藏验证码行)
func Captcha(c *gin.Context) {
	if !global.Config.Captcha.Enabled {
		utils.OK(c, gin.H{"captchaEnabled": false})
		return
	}
	id, picPath, err := utils.GenerateCaptcha()
	if err != nil {
		utils.Fail(c, "生成验证码失败")
		return
	}
	utils.OK(c, gin.H{"captchaEnabled": true, "captchaId": id, "picPath": picPath})
}

func Login(c *gin.Context) {
	var req loginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Fail(c, "请输入用户名和密码")
		return
	}
	if global.Config.Captcha.Enabled {
		if req.CaptchaID == "" || req.CaptchaCode == "" {
			utils.Fail(c, "请输入验证码")
			return
		}
		if !utils.VerifyCaptcha(req.CaptchaID, req.CaptchaCode) {
			utils.Fail(c, "验证码错误或已过期")
			return
		}
	}
	user, err := service.Login(req.Username, req.Password)
	if err != nil {
		utils.Fail(c, err.Error())
		return
	}
	accessToken, err := utils.GenerateToken(user.ID, user.Username, global.Config.JWT.Secret, global.Config.JWT.AccessExpiresHours, global.Config.JWT.Issuer)
	if err != nil {
		utils.Fail(c, "生成令牌失败")
		return
	}
	refreshToken, err := service.IssueRefreshToken(user.ID, global.Config.JWT.RefreshExpiresHours)
	if err != nil {
		utils.Fail(c, "签发刷新令牌失败")
		return
	}
	// 单点登录: 新登录挤掉该用户其他设备
	if !global.Config.JWT.UseMultipoint {
		if err := service.InvalidateOtherActiveTokens(user.ID, accessToken); err != nil {
			utils.Fail(c, "处理登录状态失败")
			return
		}
	}
	if err := service.RecordActiveToken(user.ID, user.Username, accessToken, c.ClientIP(), c.GetHeader("User-Agent"), global.Config.JWT.AccessExpiresHours); err != nil {
		utils.Fail(c, "记录登录状态失败: "+err.Error())
		return
	}
	utils.OK(c, gin.H{"token": accessToken, "refreshToken": refreshToken, "user": user})
}

// Refresh 用刷新令牌换新访问令牌(刷新令牌同步轮换, 新访问令牌登记为活跃设备)
func Refresh(c *gin.Context) {
	var req refreshReq
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Fail(c, "刷新令牌不能为空")
		return
	}
	user, accessToken, refreshToken, err := service.RefreshAccessToken(req.RefreshToken)
	if err != nil {
		utils.Fail(c, err.Error())
		return
	}
	if err := service.RecordActiveToken(user.ID, user.Username, accessToken, c.ClientIP(), c.GetHeader("User-Agent"), global.Config.JWT.AccessExpiresHours); err != nil {
		utils.Fail(c, "记录登录状态失败")
		return
	}
	utils.OK(c, gin.H{"token": accessToken, "refreshToken": refreshToken, "user": user})
}

func UserInfo(c *gin.Context) {
	claims := utils.GetClaims(c)
	if claims == nil {
		utils.NoAuth(c, "未登录")
		return
	}
	user, err := service.GetUser(claims.UserID)
	if err != nil {
		utils.NoAuth(c, "用户不存在或已被删除")
		return
	}
	menus, err := service.GetUserMenus(claims.UserID)
	if err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, gin.H{"user": user, "roles": user.Roles, "menus": menus})
}

// Logout 注销: 访问令牌入黑名单 + 移除活跃记录 + 吊销刷新令牌
func Logout(c *gin.Context) {
	var req logoutReq
	_ = c.ShouldBindJSON(&req)
	if claims := utils.GetClaims(c); claims != nil {
		if token := utils.GetToken(c); token != "" {
			service.BlacklistToken(claims.UserID, claims.Username, token, claims.ExpiresAt.Time)
		}
		service.RemoveActiveToken(utils.GetToken(c))
	}
	service.RevokeRefreshToken(req.RefreshToken)
	utils.OK(c, nil)
}

// Devices 当前登录设备列表
func Devices(c *gin.Context) {
	claims := utils.GetClaims(c)
	if claims == nil {
		utils.NoAuth(c, "未登录")
		return
	}
	list, err := service.ListActiveTokens(claims.UserID)
	if err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, gin.H{"list": list})
}

// KickDevice 踢下线指定设备
func KickDevice(c *gin.Context) {
	claims := utils.GetClaims(c)
	if claims == nil {
		utils.NoAuth(c, "未登录")
		return
	}
	id, err := utils.ParseID(c)
	if err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	if err := service.KickDevice(id, claims.UserID); err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, nil)
}

// KickOtherDevices 踢下线除当前设备外的全部设备
func KickOtherDevices(c *gin.Context) {
	claims := utils.GetClaims(c)
	if claims == nil {
		utils.NoAuth(c, "未登录")
		return
	}
	token := utils.GetToken(c)
	if token == "" {
		utils.Fail(c, "令牌缺失")
		return
	}
	if err := service.InvalidateOtherActiveTokens(claims.UserID, token); err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, nil)
}
