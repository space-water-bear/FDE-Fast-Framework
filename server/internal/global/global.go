package global

import (
	"github.com/casbin/casbin/v2"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"fde-admin/internal/config"
)

var (
	Config   *config.Config
	DB       *gorm.DB
	Enforcer *casbin.Enforcer
	Logger   *zap.Logger
)
