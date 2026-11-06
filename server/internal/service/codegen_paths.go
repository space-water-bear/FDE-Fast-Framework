package service

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

type codegenRoots struct{ server, web string }

func loadCodegenRoots(projectID uint) (codegenRoots, error) {
	server, web, err := EngineeringCodegenDirs(projectID)
	if err != nil {
		return codegenRoots{}, err
	}
	server, err = canonicalCodegenRoot(server)
	if err != nil {
		return codegenRoots{}, err
	}
	web, err = canonicalCodegenRoot(web)
	if err != nil {
		return codegenRoots{}, err
	}
	return codegenRoots{server: server, web: web}, nil
}

func canonicalCodegenRoot(dir string) (string, error) {
	if strings.TrimSpace(dir) == "" {
		return "", errors.New("代码输出目录未配置")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	st, err := os.Lstat(abs)
	if err != nil {
		return "", err
	}
	if st.Mode()&os.ModeSymlink != 0 || !st.IsDir() {
		return "", fmt.Errorf("输出根必须是实际目录，不能是符号链接: %s", dir)
	}
	return filepath.EvalSymlinks(abs)
}

func (r codegenRoots) hash() string { return sha256Hex(r.server + "\n" + r.web) }

func (r codegenRoots) location(p string) (string, string, error) {
	if err := validateGenFilePath(p); err != nil {
		return "", "", err
	}
	if strings.HasPrefix(p, "web/") {
		return r.web, strings.TrimPrefix(p, "web/"), nil
	}
	return r.server, p, nil
}

// openCheckedRoot rejects symbolic links in every existing path component.
// Root methods additionally confine IO if a directory changes during an operation.
func openCheckedRoot(dir, rel string) (*os.Root, error) {
	if rel == "" || filepath.IsAbs(rel) || strings.Contains(rel, "\\") || filepath.Clean(rel) != rel || rel == ".." || strings.HasPrefix(rel, "../") {
		return nil, errors.New("不是规范的工作区相对路径")
	}
	r, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	for i := range parts {
		prefix := filepath.Join(parts[:i+1]...)
		st, err := r.Lstat(prefix)
		if errors.Is(err, os.ErrNotExist) {
			break
		}
		if err != nil {
			r.Close()
			return nil, err
		}
		if st.Mode()&os.ModeSymlink != 0 {
			r.Close()
			return nil, fmt.Errorf("路径不允许符号链接: %s", prefix)
		}
		if i < len(parts)-1 && !st.IsDir() {
			r.Close()
			return nil, fmt.Errorf("路径父级不是目录: %s", prefix)
		}
		if i == len(parts)-1 && !st.Mode().IsRegular() {
			r.Close()
			return nil, fmt.Errorf("目标不是普通文件: %s", rel)
		}
	}
	return r, nil
}

func readRootedFile(dir, rel string) ([]byte, error) {
	r, err := openCheckedRoot(dir, rel)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return r.ReadFile(rel)
}

func writeRootedFile(dir, rel string, content []byte) error {
	r, err := openCheckedRoot(dir, rel)
	if err != nil {
		return err
	}
	defer r.Close()
	if err := r.MkdirAll(filepath.Dir(rel), 0o755); err != nil {
		return err
	}
	// A failed write leaves the destination unchanged, allowing exact compensation.
	tmp := filepath.Join(filepath.Dir(rel), ".fde-write-"+uuid.NewString())
	f, err := r.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer r.Remove(tmp)
	_, writeErr := f.Write(content)
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	return r.Rename(tmp, rel)
}

func (r codegenRoots) baseline(p string) (CodegenFileBaseline, error) {
	dir, rel, err := r.location(p)
	if err != nil {
		return CodegenFileBaseline{}, err
	}
	b, err := readRootedFile(dir, rel)
	if errors.Is(err, os.ErrNotExist) {
		return CodegenFileBaseline{Path: p}, nil
	}
	if err != nil {
		return CodegenFileBaseline{}, err
	}
	return CodegenFileBaseline{Path: p, Exists: true, SHA256: sha256Hex(string(b))}, nil
}

func (r codegenRoots) checkBaseline(expected CodegenFileBaseline) error {
	current, err := r.baseline(expected.Path)
	if err != nil {
		return fmt.Errorf("文件路径不可用 %s: %w", expected.Path, err)
	}
	if current.Exists != expected.Exists || current.SHA256 != expected.SHA256 {
		return fmt.Errorf("文件在预览后已改变，请重新预览: %s", expected.Path)
	}
	return nil
}

func (r codegenRoots) writeFile(p string, b []byte) error {
	dir, rel, err := r.location(p)
	if err != nil {
		return err
	}
	return writeRootedFile(dir, rel, b)
}
