package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"fde-admin/internal/runner"
)

// InstallUpgrade stages a verified candidate without activating it or touching shared data.
func InstallUpgrade(ctx context.Context, oldPackagePath, newPackagePath, targetDir string) (InstallResult, error) {
	old, next, target, unlock, err := lockUpgradePackages(oldPackagePath, newPackagePath, targetDir)
	if err != nil {
		return InstallResult{}, err
	}
	defer unlock()
	result := InstallResult{ReleaseID: next.ID, ReleasePath: filepath.Join(target, "releases", next.ID), SharedDir: filepath.Join(target, "shared"), JournalPath: filepath.Join(target, "journals", "upgrade-"+next.ID+".json")}
	if _, err := os.Stat(result.ReleasePath); err == nil {
		installed, inspectErr := Inspect(result.ReleasePath)
		wantHash, wantErr := runner.FileHash(filepath.Join(next.Path, "manifest.json"))
		gotHash, gotErr := runner.FileHash(filepath.Join(installed.Path, "manifest.json"))
		if inspectErr != nil || wantErr != nil || gotErr != nil || wantHash != gotHash {
			return result, errors.New("staged candidate differs from verified package")
		}
		result.Installed = true
		return result, nil
	} else if !os.IsNotExist(err) {
		return result, err
	}
	stage, err := os.MkdirTemp(filepath.Join(target, "releases"), ".upgrade-")
	if err != nil {
		return result, err
	}
	defer os.RemoveAll(stage)
	for rel := range next.Manifest.Files {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if err := copyChecked(next.Path, stage, rel); err != nil {
			return result, err
		}
	}
	if err := copyChecked(next.Path, stage, "manifest.json"); err != nil {
		return result, err
	}
	if _, err := Inspect(stage); err != nil {
		return result, err
	}
	if err := os.Rename(stage, result.ReleasePath); err != nil {
		return result, err
	}
	hash, _ := runner.FileHash(filepath.Join(next.Path, "manifest.json"))
	if err := saveJournal(result.JournalPath, installJournal{ReleaseID: next.ID, ManifestHash: hash, State: "staged-from-" + old.ID, UpdatedAt: time.Now().UTC()}); err != nil {
		return result, err
	}
	result.Installed = true
	return result, nil
}

// ActivateUpgrade is called only after the runtime migration and readiness succeeded.
func ActivateUpgrade(ctx context.Context, oldPackagePath, newPackagePath, targetDir string) error {
	return activateUpgrade(ctx, oldPackagePath, newPackagePath, targetDir, false)
}

// RollbackInstall switches the pointer after RollbackRuntime restored the old database.
func RollbackInstall(ctx context.Context, oldPackagePath, newPackagePath, targetDir string) error {
	return activateUpgrade(ctx, oldPackagePath, newPackagePath, targetDir, true)
}

func activateUpgrade(ctx context.Context, oldPath, nextPath, targetDir string, rollback bool) error {
	old, next, target, unlock, err := lockUpgradePackages(oldPath, nextPath, targetDir)
	if err != nil {
		return err
	}
	defer unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	path := filepath.Join(target, "current.json")
	var previous installJournal
	if raw, err := os.ReadFile(path); err == nil {
		if json.Unmarshal(raw, &previous) != nil || (previous.ReleaseID != old.ID && previous.ReleaseID != next.ID) {
			return errors.New("active release changed concurrently")
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	chosen := next
	if rollback {
		chosen = old
	}
	installed, err := Inspect(filepath.Join(target, "releases", chosen.ID))
	if err != nil {
		return err
	}
	hash, err := runner.FileHash(filepath.Join(chosen.Path, "manifest.json"))
	if err != nil {
		return err
	}
	actual, err := runner.FileHash(filepath.Join(installed.Path, "manifest.json"))
	if err != nil || hash != actual {
		return errors.New("installed candidate changed before activation")
	}
	return saveJournal(path, installJournal{ReleaseID: chosen.ID, ManifestHash: hash, State: "active", UpdatedAt: time.Now().UTC()})
}

func lockUpgradePackages(oldPath, newPath, targetDir string) (Package, Package, string, func(), error) {
	old, err := Inspect(oldPath)
	if err != nil {
		return Package{}, Package{}, "", nil, err
	}
	next, err := Inspect(newPath)
	if err != nil {
		return old, Package{}, "", nil, err
	}
	if old.ID == next.ID || old.Manifest.GOARCH != next.Manifest.GOARCH || old.Manifest.GOOS != next.Manifest.GOOS {
		return old, next, "", nil, errors.New("upgrade requires distinct releases for the same platform")
	}
	target, err := filepath.Abs(targetDir)
	if err != nil || targetDir == "" {
		return old, next, "", nil, errors.New("upgrade target is required")
	}
	info, err := os.Lstat(target)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return old, next, "", nil, errors.New("upgrade target must be an existing real directory")
	}
	target, err = filepath.EvalSymlinks(target)
	if err != nil {
		return old, next, "", nil, err
	}
	if err := ensureDirectory(target, "releases"); err != nil {
		return old, next, "", nil, err
	}
	lockPath := filepath.Join(target, ".install.lock")
	if info, err := os.Lstat(lockPath); err == nil && (!info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0) {
		return old, next, "", nil, errors.New("installation lock must be a regular file")
	}
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return old, next, "", nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return old, next, "", nil, errors.New("another installation is active")
	}
	unlock := func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); _ = f.Close() }
	installed, err := Inspect(filepath.Join(target, "releases", old.ID))
	if err == nil {
		want, _ := runner.FileHash(filepath.Join(old.Path, "manifest.json"))
		got, hashErr := runner.FileHash(filepath.Join(installed.Path, "manifest.json"))
		if hashErr != nil || want != got {
			err = errors.New("old release does not match the installed baseline")
		}
	}
	if err != nil {
		unlock()
		return old, next, "", nil, err
	}
	return old, next, target, unlock, nil
}
