package runner

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func excluded(rel string) bool {
	for _, p := range []string{".git", ".pnpm-store", ".fde-gen-backup", ".cache", "server/data", "server/log", "server/uploads", "server/bin", "server/fde-admin", "web/node_modules", "web/dist", "data", "uploads", "log", "backups"} {
		if rel == p || strings.HasPrefix(rel, p+"/") {
			return true
		}
	}
	base := filepath.Base(rel)
	return base == ".DS_Store" || base == ".env" || (strings.HasPrefix(base, ".env.") && base != ".env.example") || strings.HasSuffix(base, ".log")
}

func sourceFiles(workspace string) (string, []string, error) {
	root, err := filepath.Abs(workspace)
	if err != nil {
		return "", nil, err
	}
	st, err := os.Lstat(root)
	if err != nil {
		return "", nil, err
	}
	if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return "", nil, fmt.Errorf("workspace must be a real directory")
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", nil, err
	}
	var files []string
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == root {
			return nil
		}
		rel, e := filepath.Rel(root, p)
		if e != nil {
			return e
		}
		rel = filepath.ToSlash(rel)
		if excluded(rel) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("source contains symlink: %s", rel)
		}
		if d.IsDir() {
			return nil
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		if !info.Mode().IsRegular() || info.Size() > 32<<20 {
			return fmt.Errorf("unsupported source file: %s", rel)
		}
		files = append(files, rel)
		return nil
	})
	sort.Strings(files)
	return root, files, err
}

func SnapshotHash(workspace string) (string, error) {
	root, files, err := sourceFiles(workspace)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	var total int64
	r, err := os.OpenRoot(root)
	if err != nil {
		return "", err
	}
	defer r.Close()
	for _, rel := range files {
		b, err := r.ReadFile(rel)
		if err != nil {
			return "", err
		}
		total += int64(len(b))
		if total > 512<<20 {
			return "", fmt.Errorf("source snapshot too large")
		}
		st, err := r.Stat(rel)
		if err != nil {
			return "", err
		}
		sum := sha256.Sum256(b)
		fmt.Fprintf(h, "%s\x00%o\x00%x\n", rel, st.Mode()&0o111, sum)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func copySnapshot(src, dst string) error {
	root, files, err := sourceFiles(src)
	if err != nil {
		return err
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer r.Close()
	if err := os.MkdirAll(dst, 0o700); err != nil {
		return err
	}
	for _, rel := range files {
		b, err := r.ReadFile(rel)
		if err != nil {
			return err
		}
		st, err := r.Stat(rel)
		if err != nil {
			return err
		}
		p := filepath.Join(dst, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, b, 0o644|(st.Mode()&0o111)); err != nil {
			return err
		}
	}
	return nil
}

func FileHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func HashTree(root string) (map[string]string, error) {
	out := map[string]string{}
	var total int64
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == root {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("artifact symlink rejected: %s", p)
		}
		if d.IsDir() {
			return nil
		}
		st, err := d.Info()
		if err != nil {
			return err
		}
		if !st.Mode().IsRegular() {
			return fmt.Errorf("artifact is not regular file")
		}
		total += st.Size()
		if st.Size() > 256<<20 || total > 512<<20 || len(out) >= 20000 {
			return fmt.Errorf("artifact tree exceeds package limits")
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		sum, err := FileHash(p)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = sum
		return nil
	})
	return out, err
}
