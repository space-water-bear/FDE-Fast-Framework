package service

import (
	"strconv"

	"fde-admin/internal/global"
	"fde-admin/internal/model"
)

// RebuildCasbin 从数据库全量重建 casbin 策略(用户-角色 / 角色-接口)
// 任何用户/角色/菜单变更后调用, 保证内存与库中策略一致
func RebuildCasbin() error {
	e := global.Enforcer
	if e == nil {
		return nil
	}
	e.ClearPolicy()

	var roles []model.Role
	if err := global.DB.Find(&roles).Error; err != nil {
		return err
	}
	var pRules [][]string
	for i := range roles {
		roleID := strconv.Itoa(int(roles[i].ID))
		if roles[i].Code == "admin" {
			pRules = append(pRules, []string{roleID, "/*", "*"})
			continue
		}
		var menus []model.Menu
		if err := global.DB.Model(&roles[i]).Association("Menus").Find(&menus); err != nil {
			return err
		}
		for _, m := range menus {
			if m.ApiPath == "" {
				continue
			}
			method := m.ApiMethod
			if method == "" {
				method = "*"
			}
			pRules = append(pRules, []string{roleID, m.ApiPath, method})
		}
	}
	if _, err := e.AddPolicies(pRules); err != nil {
		return err
	}

	var users []model.User
	if err := global.DB.Find(&users).Error; err != nil {
		return err
	}
	var gRules [][]string
	for i := range users {
		var userRoles []model.Role
		if err := global.DB.Model(&users[i]).Association("Roles").Find(&userRoles); err != nil {
			return err
		}
		for _, r := range userRoles {
			gRules = append(gRules, []string{strconv.Itoa(int(users[i].ID)), strconv.Itoa(int(r.ID))})
		}
	}
	_, err := e.AddGroupingPolicies(gRules)
	return err
}
