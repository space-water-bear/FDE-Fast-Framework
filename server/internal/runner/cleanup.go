package runner

import (
	"io/fs"
	"os"
	"path/filepath"
)

// Go's module cache deliberately creates read-only directories. These scratch
// copies are owned by this check, so restore directory write access before removal.
// WalkDir and RemoveAll do not follow symlinks into other filesystem locations.
func removeCheckScratch(root string) error {
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			return os.Chmod(path, info.Mode().Perm()|0o700)
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.RemoveAll(root)
}
