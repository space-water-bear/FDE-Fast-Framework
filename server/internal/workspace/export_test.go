package workspace

import (
	"archive/tar"
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func fixtureRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{"LICENSE": "MIT", "server/go.mod": "module fde-admin\n", "web/package.json": "{}", "server/data/private.db": "private", "server/fde-admin": "compiled binary", "server/uploads/person.txt": "private", "server/log/access.log": "private", ".env": "TOKEN=private", ".env.example": "TOKEN=", "server/main.go": "package main\n"}
	for name, content := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"init", "--quiet"}, {"add", "--all"}, {"-c", "user.name=Test", "-c", "user.email=test@localhost", "commit", "--quiet", "-m", "fixture"}} {
		if out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git: %s %v", out, err)
		}
	}
	return root
}

func TestExportCommittedSourceExcludesRuntimeAndDirtyFiles(t *testing.T) {
	repo := fixtureRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "server/main.go"), []byte("uncommitted secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "local-private.txt"), []byte("untracked"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "customer")
	result, err := Export(context.Background(), repo, "HEAD", dest)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.SourceCommit) != 40 || len(result.BaseCommit) != 40 {
		t.Fatalf("missing commits: %+v", result)
	}
	for _, name := range []string{"server/fde-admin", "server/data/private.db", "server/uploads/person.txt", "server/log/access.log", ".env", "local-private.txt"} {
		if _, err := os.Stat(filepath.Join(dest, name)); !os.IsNotExist(err) {
			t.Fatalf("exported runtime file %s", name)
		}
	}
	content, _ := os.ReadFile(filepath.Join(dest, "server/main.go"))
	if string(content) != "package main\n" {
		t.Fatalf("exported dirty source %q", content)
	}
	if _, err := os.Stat(filepath.Join(dest, ".env.example")); err != nil {
		t.Fatal(err)
	}
	if _, err := Export(context.Background(), repo, "HEAD", dest); err == nil {
		t.Fatal("overwrote an existing project")
	}
}

func TestExtractRejectsEscapeAndSymlink(t *testing.T) {
	for _, header := range []*tar.Header{{Name: "../escaped", Typeflag: tar.TypeReg, Size: 1}, {Name: "server/link", Typeflag: tar.TypeSymlink, Linkname: "/tmp"}} {
		var archive bytes.Buffer
		w := tar.NewWriter(&archive)
		if err := w.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if header.Size > 0 {
			_, _ = w.Write([]byte("x"))
		}
		_ = w.Close()
		if err := extract(strings.NewReader(archive.String()), t.TempDir()); err == nil {
			t.Fatalf("accepted unsafe header %+v", header)
		}
	}
}
