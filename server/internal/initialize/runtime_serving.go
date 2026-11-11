package initialize

import (
	"errors"
	"os"
	"syscall"

	"fde-admin/internal/config"
)

// LockRuntimeServing holds a shared maintenance lock for the server lifetime.
func LockRuntimeServing(cfg *config.Config) (func(), error) {
	if cfg.DB.Driver != "sqlite" {
		return nil, errors.New("运行交付仅支持SQLite")
	}
	path, err := canonicalRuntimeSQLitePath(cfg.DB.DSN)
	if err != nil {
		return nil, err
	}
	lockPath := path + ".maintenance.lock"
	if info, err := os.Lstat(lockPath); err == nil && (!info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0) {
		return nil, errors.New("维护锁必须为普通文件")
	}
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_SH|syscall.LOCK_NB); err != nil {
		file.Close()
		return nil, errors.New("升级或恢复正在运行，不能启动服务")
	}
	return func() { _ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN); _ = file.Close() }, nil
}
