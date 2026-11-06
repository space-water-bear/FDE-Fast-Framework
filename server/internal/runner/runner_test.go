package runner

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeExecutor struct {
	calls  []Command
	fail   string
	cancel bool
}

func (f *fakeExecutor) Run(ctx context.Context, c Command) (Result, error) {
	f.calls = append(f.calls, c)
	if len(c.Args) > 0 && c.Args[0] == "image" {
		return Result{Output: "sha256:immutable-image"}, nil
	}
	if c.Args[0] == "rm" {
		return Result{}, nil
	}
	step := strings.TrimSuffix(filepath.Base(c.LogPath), ".log")
	if step == f.fail {
		if f.cancel {
			return Result{ExitCode: -1}, context.Canceled
		}
		return Result{ExitCode: 17, Output: "compiler rejected candidate"}, nil
	}
	versions := map[string]string{"go-version": "go version go1.26.0 linux/arm64", "node-version": "v22.22.3", "pnpm-version": "11.23.0"}
	if v := versions[step]; v != "" {
		return Result{Output: v}, nil
	}
	if step == "go-build" || step == "cli-build" || step == "vite-build" {
		for _, a := range c.Args {
			if strings.HasPrefix(a, "type=bind,src=") && strings.HasSuffix(a, ",dst=/out") {
				dir := strings.TrimSuffix(strings.TrimPrefix(a, "type=bind,src="), ",dst=/out")
				name := "server"
				if step == "cli-build" {
					name = "fde"
				}
				_ = os.WriteFile(filepath.Join(dir, name), []byte("checked Linux output"), 0o755)
			}
			if step == "vite-build" && strings.HasPrefix(a, "type=bind,src=") && strings.HasSuffix(a, ",dst=/out/web") {
				dir := strings.TrimSuffix(strings.TrimPrefix(a, "type=bind,src="), ",dst=/out/web")
				_ = os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>checked</html>"), 0o644)
			}
		}
	}

	return Result{}, nil
}
func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for p, b := range map[string]string{"server/main.go": "package main\n", "server/go.mod": "module fde-admin\n", "web/package.json": "{}\n", "fde/assets/business.json": "{\"objects\":[]}"} {
		f := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f, []byte(b), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestFailureReportKeepsExitCodeAndStopsProfile(t *testing.T) {
	root := fixture(t)
	hash, _ := SnapshotHash(root)
	executor := &fakeExecutor{fail: "go-test"}
	report, err := New(executor).Check(context.Background(), Request{Workspace: root, OutputDir: t.TempDir(), ExpectedSnapshotHash: hash})
	if err == nil || report.Success || report.FailedStep != "go-test" || report.Steps[len(report.Steps)-1].ExitCode != 17 {
		t.Fatalf("failure hidden: %+v %v", report, err)
	}
	data, err := os.ReadFile(report.ReportPath)
	if err != nil {
		t.Fatal(err)
	}
	var saved Report
	if json.Unmarshal(data, &saved) != nil || saved.Success || saved.FailedStep != "go-test" {
		t.Fatal("structured report lost failure")
	}
	for _, c := range executor.calls {
		if strings.Contains(strings.Join(c.Args, " "), "docker.sock") || strings.Contains(strings.Join(c.Args, " "), "sh -c") {
			t.Fatalf("unsafe sandbox invocation: %v", c.Args)
		}
		if strings.HasSuffix(c.LogPath, "cli-build.log") {
			t.Fatal("continued after failed tests")
		}
	}
}

func TestCancellationCleansNamedContainer(t *testing.T) {
	root := fixture(t)
	hash, _ := SnapshotHash(root)
	e := &fakeExecutor{fail: "go-test", cancel: true}
	report, err := New(e).Check(context.Background(), Request{Workspace: root, OutputDir: t.TempDir(), ExpectedSnapshotHash: hash})
	if !errors.Is(err, context.Canceled) || report.Success {
		t.Fatalf("cancellation lost: %+v %v", report, err)
	}
	last := e.calls[len(e.calls)-1]
	if len(last.Args) < 3 || last.Args[0] != "rm" || last.Args[1] != "--force" {
		t.Fatal("container cleanup missing")
	}
}

func TestSnapshotBindsSourceAndExcludesRuntime(t *testing.T) {
	root := fixture(t)
	before, _ := SnapshotHash(root)
	p := filepath.Join(root, "server/data/private.db")
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	_ = os.WriteFile(p, []byte("runtime"), 0o600)
	after, _ := SnapshotHash(root)
	if before != after {
		t.Fatal("runtime changed source snapshot")
	}
	_ = os.WriteFile(filepath.Join(root, "server/main.go"), []byte("changed"), 0o644)
	e := &fakeExecutor{}
	if _, err := New(e).Check(context.Background(), Request{Workspace: root, OutputDir: t.TempDir(), ExpectedSnapshotHash: before}); err == nil || len(e.calls) != 0 {
		t.Fatal("stale snapshot executed")
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	if _, err := SnapshotHash(root); err == nil {
		t.Fatal("symlink source accepted")
	}
}

func TestPassingProfileRequiresArtifactsAndRecordsImageIdentity(t *testing.T) {
	root := fixture(t)
	hash, _ := SnapshotHash(root)
	e := &fakeExecutor{}
	report, err := New(e).Check(context.Background(), Request{Workspace: root, OutputDir: t.TempDir(), ExpectedSnapshotHash: hash})
	if err != nil || !report.Success || len(report.Steps) != 12 || report.ImageID != "sha256:immutable-image" || report.Artifacts["server"] == "" || report.Artifacts["fde"] == "" || report.Artifacts["web/index.html"] == "" {
		t.Fatalf("passing profile incomplete: %+v %v", report, err)
	}
	for _, c := range e.calls {
		if strings.HasSuffix(c.LogPath, "go-test.log") {
			args := strings.Join(c.Args, " ")
			if !strings.Contains(args, "--network none") || !strings.Contains(args, "dst=/out,readonly") || !strings.Contains(args, "dst=/go,readonly") {
				t.Fatalf("test container can mutate verified outputs/dependencies: %s", args)
			}
		}
	}
}
