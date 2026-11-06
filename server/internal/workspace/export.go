// Package workspace exports committed template source without copying runtime data.
package workspace

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type ExportResult struct {
	Path         string
	SourceCommit string
	BaseCommit   string
}

func excluded(name string) bool {
	for _, prefix := range []string{"server/fde-admin", "server/data", "server/log", "server/uploads", "server/bin", "server/.fde-gen-backup", "web/node_modules", "web/dist", ".pnpm-store", ".fde-gen-backup"} {
		if name == prefix || strings.HasPrefix(name, prefix+"/") {
			return true
		}
	}
	base := filepath.Base(name)
	return base == ".git" || base == ".env" || (strings.HasPrefix(base, ".env.") && base != ".env.example") || strings.HasSuffix(base, ".log")
}

// Export creates a new local repository from one immutable, committed template.
// Neither untracked files nor changes in the developer's worktree are exported.
func Export(ctx context.Context, repo, ref, destination string) (*ExportResult, error) {
	if ref == "" {
		ref = "HEAD"
	}
	commitBytes, err := exec.CommandContext(ctx, "git", "-C", repo, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}").Output()
	if err != nil {
		return nil, fmt.Errorf("模板版本不存在: %w", err)
	}
	commit := strings.TrimSpace(string(commitBytes))
	if len(commit) != 40 && len(commit) != 64 {
		return nil, errors.New("模板提交标识无效")
	}
	parent := filepath.Dir(destination)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return nil, err
	}
	parent, err = filepath.EvalSymlinks(parent)
	if err != nil {
		return nil, err
	}
	destination = filepath.Join(parent, filepath.Base(destination))
	if _, err := os.Lstat(destination); !os.IsNotExist(err) {
		return nil, errors.New("工作区路径已存在")
	}
	staging, err := os.MkdirTemp(parent, ".export-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(staging)
	cmd := exec.CommandContext(ctx, "git", "-C", repo, "archive", "--format=tar", commit)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	extractErr := extract(stdout, staging)
	if extractErr != nil {
		_ = cmd.Process.Kill()
	}
	waitErr := cmd.Wait()
	if extractErr != nil {
		return nil, extractErr
	}
	if waitErr != nil {
		return nil, fmt.Errorf("模板导出失败: %w", waitErr)
	}
	for _, name := range []string{"LICENSE", "server/go.mod", "web/package.json"} {
		if _, err := os.Stat(filepath.Join(staging, name)); err != nil {
			return nil, fmt.Errorf("模板缺少 %s", name)
		}
	}
	for _, args := range [][]string{{"init", "--quiet"}, {"add", "--all"}, {"-c", "user.name=FDE", "-c", "user.email=fde@localhost", "commit", "--quiet", "-m", "Initialize customer project"}} {
		command := exec.CommandContext(ctx, "git", append([]string{"-C", staging}, args...)...)
		if err := command.Run(); err != nil {
			return nil, fmt.Errorf("初始化项目源码库失败: %w", err)
		}
	}
	baseBytes, err := exec.CommandContext(ctx, "git", "-C", staging, "rev-parse", "HEAD").Output()
	if err != nil {
		return nil, err
	}
	if err := os.Rename(staging, destination); err != nil {
		return nil, err
	}
	return &ExportResult{Path: destination, SourceCommit: commit, BaseCommit: strings.TrimSpace(string(baseBytes))}, nil
}

func extract(reader io.Reader, destination string) error {
	archive := tar.NewReader(reader)
	var total int64
	for {
		header, err := archive.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if header.Typeflag == tar.TypeXGlobalHeader {
			continue
		}
		name := strings.TrimSuffix(header.Name, "/")
		if !fs.ValidPath(name) || strings.Contains(name, "\\") {
			return errors.New("模板包含越界路径")
		}
		if excluded(name) {
			continue
		}
		if header.Typeflag != tar.TypeDir && header.Typeflag != tar.TypeReg {
			return errors.New("模板不允许符号链接或特殊文件")
		}
		target := filepath.Join(destination, filepath.FromSlash(name))
		if header.Typeflag == tar.TypeDir {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		total += header.Size
		if header.Size < 0 || header.Size > 16<<20 || total > 128<<20 {
			return errors.New("模板超过源码大小限制")
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if header.Mode&0o111 != 0 {
			mode = 0o755
		}
		file, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
		if err != nil {
			return err
		}
		_, writeErr := io.CopyN(file, archive, header.Size)
		closeErr := file.Close()
		if writeErr != nil {
			return writeErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
}
