package delivery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"fde-admin/internal/runner"
)

func byteHash(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

type InstallResult struct {
	ReleaseID          string `json:"releaseId"`
	ReleasePath        string `json:"releasePath"`
	SharedDir          string `json:"sharedDir"`
	JournalPath        string `json:"journalPath"`
	Installed          bool   `json:"installed"`
	Bootstrapped       bool   `json:"bootstrapped"`
	Ready              bool   `json:"ready"`
	NeedsConfiguration bool   `json:"needsConfiguration"`
	ConfigExamplePath  string `json:"configExamplePath"`
}
type installJournal struct {
	ReleaseID    string    `json:"releaseId"`
	ManifestHash string    `json:"manifestHash"`
	State        string    `json:"state"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

func Install(packagePath, targetDir string) (InstallResult, error) {
	return InstallContext(context.Background(), packagePath, targetDir)
}
func InstallContext(ctx context.Context, packagePath, targetDir string) (InstallResult, error) {
	pkg, err := Inspect(packagePath)
	if err != nil {
		return InstallResult{}, err
	}
	if targetDir == "" {
		return InstallResult{}, errors.New("installation directory is required")
	}
	target, err := filepath.Abs(targetDir)
	if err != nil {
		return InstallResult{}, err
	}
	if st, err := os.Lstat(target); err == nil && (st.Mode()&os.ModeSymlink != 0 || !st.IsDir()) {
		return InstallResult{}, errors.New("installation target must be a real directory")
	}
	if err := os.MkdirAll(target, 0o700); err != nil {
		return InstallResult{}, err
	}
	target, err = filepath.EvalSymlinks(target)
	if err != nil {
		return InstallResult{}, err
	}
	lock := filepath.Join(target, ".install.lock")
	f, err := os.OpenFile(lock, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return InstallResult{}, err
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return InstallResult{}, errors.New("another installation is active")
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	for _, rel := range []string{"releases", "shared/data", "shared/config", "shared/uploads", "shared/log", "shared/backups", "journals"} {
		p := filepath.Join(target, rel)
		if err := ensureDirectory(target, rel); err != nil {
			return InstallResult{}, err
		}
		if err := os.MkdirAll(p, 0o700); err != nil {
			return InstallResult{}, err
		}
	}
	manifestHash, err := runner.FileHash(filepath.Join(pkg.Path, "manifest.json"))
	if err != nil {
		return InstallResult{}, err
	}
	release := filepath.Join(target, "releases", pkg.ID)
	journalPath := filepath.Join(target, "journals", "install-"+pkg.ID+".json")
	result := InstallResult{ReleaseID: pkg.ID, ReleasePath: release, SharedDir: filepath.Join(target, "shared"), JournalPath: journalPath, NeedsConfiguration: true}
	result.ConfigExamplePath = filepath.Join(result.SharedDir, "config/config.example.yaml")
	example := strings.NewReplacer("__FDE_DATA_PATH__", strconv.Quote(filepath.Join(result.SharedDir, "data/app.db")), "__FDE_UPLOAD_PATH__", strconv.Quote(filepath.Join(result.SharedDir, "uploads")), "__FDE_LOG_PATH__", strconv.Quote(filepath.Join(result.SharedDir, "log"))).Replace(configExample)
	if f, err := os.OpenFile(result.ConfigExamplePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600); err == nil {
		_, writeErr := f.Write([]byte(example))
		closeErr := f.Close()
		if writeErr != nil {
			return result, writeErr
		}
		if closeErr != nil {
			return result, closeErr
		}
	} else if !os.IsExist(err) {
		return result, err
	}
	entries, err := os.ReadDir(filepath.Join(target, "releases"))
	if err != nil {
		return result, err
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".install-"+pkg.ID+"-") {
			var previous installJournal
			b, e := os.ReadFile(journalPath)
			if e != nil || json.Unmarshal(b, &previous) != nil || previous.ReleaseID != pkg.ID || previous.ManifestHash != manifestHash {
				return result, errors.New("staging directory has no matching install journal")
			}
			if e := os.RemoveAll(filepath.Join(target, "releases", entry.Name())); e != nil {
				return result, e
			}
			continue
		}
		if entry.Name() != pkg.ID {
			return result, errors.New("P1 supports first installation only; existing release requires an upgrade plan")
		}
	}
	if st, err := os.Lstat(release); err == nil {
		if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
			return result, errors.New("installed release path is invalid")
		}
		existing, err := Inspect(release)
		if err != nil {
			return result, err
		}
		oldHash, _ := runner.FileHash(filepath.Join(existing.Path, "manifest.json"))
		if oldHash != manifestHash {
			return result, errors.New("installed release differs from verified package")
		}
		if err := saveJournal(journalPath, installJournal{ReleaseID: pkg.ID, ManifestHash: manifestHash, State: "installed", UpdatedAt: time.Now().UTC()}); err != nil {
			return result, err
		}
		result.Installed = true
		_, err = os.Stat(filepath.Join(result.SharedDir, "config.yaml"))
		result.NeedsConfiguration = os.IsNotExist(err)
		return result, nil
	}
	dataEntries, err := os.ReadDir(filepath.Join(target, "shared/data"))
	if err != nil {
		return result, err
	}
	if len(dataEntries) > 0 {
		return result, errors.New("first installation requires an empty business data directory")
	}
	journal := installJournal{ReleaseID: pkg.ID, ManifestHash: manifestHash, State: "installing", UpdatedAt: time.Now().UTC()}
	if err := saveJournal(journalPath, journal); err != nil {
		return result, err
	}
	stage, err := os.MkdirTemp(filepath.Join(target, "releases"), ".install-"+pkg.ID+"-")
	if err != nil {
		return result, err
	}
	defer os.RemoveAll(stage)
	for rel := range pkg.Manifest.Files {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if err := copyChecked(pkg.Path, stage, rel); err != nil {
			return result, err
		}
	}
	if err := copyChecked(pkg.Path, stage, "manifest.json"); err != nil {
		return result, err
	}
	if _, err := Inspect(stage); err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if err := os.Rename(stage, release); err != nil {
		return result, err
	}
	journal.State = "installed"
	journal.UpdatedAt = time.Now().UTC()
	if err := saveJournal(journalPath, journal); err != nil {
		return result, err
	}
	result.Installed = true
	if _, err := os.Stat(filepath.Join(result.SharedDir, "config.yaml")); err == nil {
		result.NeedsConfiguration = false
	}
	return result, nil
}
func saveJournal(path string, j installJournal) error {
	b, err := json.MarshalIndent(j, "", "  ")
	if err != nil {
		return err
	}
	if info, err := os.Lstat(path); err == nil && (!info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0) {
		return errors.New("installation journal must be a regular file")
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".install-journal-")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(b); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}
func ensureDirectory(root, rel string) error {
	current := root
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		current = filepath.Join(current, part)
		st, err := os.Lstat(current)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if st.Mode()&os.ModeSymlink != 0 || !st.IsDir() {
			return errors.New("installation directory contains a symlink or non-directory")
		}
	}
	return nil
}
