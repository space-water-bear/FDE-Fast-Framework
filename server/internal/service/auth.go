package service

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sort"
	"time"

	"gorm.io/gorm"

	"fde-admin/internal/global"
	"fde-admin/internal/model"
	"fde-admin/internal/utils"
)

func Login(username, password string) (*model.User, error) {
	var user model.User
	if err := global.DB.Where("username = ?", username).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("用户名或密码错误")
		}
		return nil, err
	}
	if !user.Enabled {
		return nil, errors.New("账号已被禁用，请联系管理员")
	}
	if !utils.CheckPassword(user.Password, password) {
		return nil, errors.New("用户名或密码错误")
	}
	return &user, nil
}

// GetUserMenus 返回该用户可见的菜单树(不含按钮)
func GetUserMenus(userID uint) ([]model.Menu, error) {
	var user model.User
	if err := global.DB.Preload("Roles.Menus").First(&user, userID).Error; err != nil {
		return nil, err
	}
	seen := map[uint]model.Menu{}
	for _, r := range user.Roles {
		for _, m := range r.Menus {
			if m.Type == 2 {
				continue
			}
			seen[m.ID] = m
		}
	}
	all := make([]model.Menu, 0, len(seen))
	for _, m := range seen {
		all = append(all, m)
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].Sort != all[j].Sort {
			return all[i].Sort < all[j].Sort
		}
		return all[i].ID < all[j].ID
	})
	return BuildMenuTree(all, 0), nil
}

// IssueRefreshToken 签发刷新令牌, 返回原始令牌(仅此一次可见), 并顺带清理失效令牌
func IssueRefreshToken(userID uint, expiresHours int) (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	raw := hex.EncodeToString(buf)
	rt := &model.RefreshToken{
		UserID:    userID,
		TokenHash: utils.HashToken(raw),
		ExpiresAt: time.Now().Add(time.Duration(expiresHours) * time.Hour),
	}
	if err := global.DB.Create(rt).Error; err != nil {
		return "", err
	}
	global.DB.Where("expires_at < ? OR revoked = ?", time.Now(), true).Delete(&model.RefreshToken{})
	return raw, nil
}

// RefreshAccessToken 校验刷新令牌并轮换(旧令牌立即吊销)
func RefreshAccessToken(raw string) (*model.User, string, string, error) {
	if raw == "" {
		return nil, "", "", errors.New("刷新令牌不能为空")
	}
	var rt model.RefreshToken
	if err := global.DB.Where("token_hash = ?", utils.HashToken(raw)).First(&rt).Error; err != nil {
		return nil, "", "", errors.New("刷新令牌无效")
	}
	if rt.Revoked {
		return nil, "", "", errors.New("刷新令牌已被吊销，请重新登录")
	}
	if time.Now().After(rt.ExpiresAt) {
		return nil, "", "", errors.New("刷新令牌已过期，请重新登录")
	}
	var user model.User
	if err := global.DB.First(&user, rt.UserID).Error; err != nil {
		return nil, "", "", errors.New("用户不存在")
	}
	if !user.Enabled {
		return nil, "", "", errors.New("账号已被禁用，请联系管理员")
	}
	rt.Revoked = true
	if err := global.DB.Save(&rt).Error; err != nil {
		return nil, "", "", err
	}
	access, err := utils.GenerateToken(user.ID, user.Username, global.Config.JWT.Secret, global.Config.JWT.AccessExpiresHours, global.Config.JWT.Issuer)
	if err != nil {
		return nil, "", "", err
	}
	newRaw, err := IssueRefreshToken(user.ID, global.Config.JWT.RefreshExpiresHours)
	if err != nil {
		return nil, "", "", err
	}
	return &user, access, newRaw, nil
}

// RevokeRefreshToken 吊销刷新令牌(注销时调用)
func RevokeRefreshToken(raw string) {
	if raw == "" {
		return
	}
	global.DB.Model(&model.RefreshToken{}).Where("token_hash = ?", utils.HashToken(raw)).Update("revoked", true)
}

// ---- 多设备登录与令牌黑名单 ----

// RecordActiveToken 记录活跃访问令牌(登录/刷新时调用)
func RecordActiveToken(userID uint, username, token, ip, userAgent string, expiresHours int) error {
	if err := global.DB.Where("expires_at < ?", time.Now()).Delete(&model.ActiveToken{}).Error; err != nil {
		return err
	}
	at := &model.ActiveToken{
		UserID:    userID,
		Username:  username,
		TokenHash: utils.HashToken(token),
		ExpiresAt: time.Now().Add(time.Duration(expiresHours) * time.Hour),
		IP:        ip,
		UserAgent: userAgent,
	}
	return global.DB.Create(at).Error
}

// BlacklistToken 将访问令牌移入黑名单(注销/踢下线时调用)
func BlacklistToken(userID uint, username, token string, expiresAt time.Time) {
	global.DB.Create(&model.JwtBlacklist{
		UserID:    userID,
		Username:  username,
		TokenHash: utils.HashToken(token),
		ExpiresAt: expiresAt,
	})
	global.DB.Where("expires_at < ?", time.Now()).Delete(&model.JwtBlacklist{})
}

// IsTokenBlacklisted 判断令牌是否在黑名单
func IsTokenBlacklisted(token string) (bool, error) {
	var n int64
	err := global.DB.Model(&model.JwtBlacklist{}).Where("token_hash = ?", utils.HashToken(token)).Count(&n).Error
	return n > 0, err
}

// IsTokenActive 判断令牌是否为活跃令牌(被踢下线/单点登录挤掉后为 false)
func IsTokenActive(token string) (bool, error) {
	var n int64
	err := global.DB.Model(&model.ActiveToken{}).
		Where("token_hash = ? AND expires_at > ?", utils.HashToken(token), time.Now()).
		Count(&n).Error
	return n > 0, err
}

// InvalidateOtherActiveTokens 注销该用户除 keepToken 外的全部活跃令牌(单点登录/踢出其他设备)
func InvalidateOtherActiveTokens(userID uint, keepToken string) error {
	var list []model.ActiveToken
	if err := global.DB.Where("user_id = ? AND token_hash <> ?", userID, utils.HashToken(keepToken)).Find(&list).Error; err != nil {
		return err
	}
	for _, at := range list {
		global.DB.Create(&model.JwtBlacklist{
			UserID:    at.UserID,
			Username:  at.Username,
			TokenHash: at.TokenHash,
			ExpiresAt: at.ExpiresAt,
		})
		global.DB.Delete(&at)
	}
	return nil
}

// RemoveActiveToken 按令牌原文移除活跃记录
func RemoveActiveToken(token string) {
	global.DB.Where("token_hash = ?", utils.HashToken(token)).Delete(&model.ActiveToken{})
}

// ListActiveTokens 当前用户活跃设备列表
func ListActiveTokens(userID uint) ([]model.ActiveToken, error) {
	var list []model.ActiveToken
	if err := global.DB.Where("user_id = ? AND expires_at > ?", userID, time.Now()).
		Order("id desc").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

// KickDevice 踢下线指定设备(移入黑名单)
func KickDevice(id, userID uint) error {
	var at model.ActiveToken
	if err := global.DB.Where("id = ? AND user_id = ?", id, userID).First(&at).Error; err != nil {
		return err
	}
	global.DB.Create(&model.JwtBlacklist{
		UserID:    at.UserID,
		Username:  at.Username,
		TokenHash: at.TokenHash,
		ExpiresAt: at.ExpiresAt,
	})
	return global.DB.Delete(&at).Error
}
