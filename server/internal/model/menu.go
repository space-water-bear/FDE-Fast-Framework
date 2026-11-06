package model

// Menu 菜单/按钮
// Type: 0 目录(仅用于分组)  1 菜单(前端路由)  2 按钮(接口权限)
// Type 1/2 可配置 ApiPath/ApiMethod, 作为 casbin p 策略同步到角色
type Menu struct {
	Base
	ParentID  uint   `json:"parentId" gorm:"index;default:0"`
	Path      string `json:"path" gorm:"size:128"`
	Name      string `json:"name" gorm:"size:128"`
	Component string `json:"component" gorm:"size:255"`
	Title     string `json:"title" gorm:"size:64"`
	Icon      string `json:"icon" gorm:"size:64"`
	Type      int    `json:"type" gorm:"comment:0目录 1菜单 2按钮"`
	Sort      int    `json:"sort" gorm:"default:0"`
	Hidden    bool   `json:"hidden" gorm:"default:false"`
	KeepAlive bool   `json:"keepAlive" gorm:"default:false"`
	Affix     bool   `json:"affix" gorm:"default:false;comment:固定标签(标签栏不可关闭)"`
	HideTab   bool   `json:"hideTab" gorm:"default:false;comment:隐藏标签(访问不生成标签)"`
	ApiPath   string `json:"apiPath" gorm:"size:255"`
	ApiMethod string `json:"apiMethod" gorm:"size:16"`
	Children  []Menu `json:"children" gorm:"-"`
}

func (Menu) TableName() string { return "sys_menus" }
