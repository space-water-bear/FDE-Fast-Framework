package delivery

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fde-admin/internal/runner"
	"github.com/google/uuid"
)

func fakeLinuxELF() []byte {
	b := make([]byte, 64)
	copy(b, []byte{0x7f, 'E', 'L', 'F', 2, 1, 1})
	binary.LittleEndian.PutUint16(b[16:], 2)
	binary.LittleEndian.PutUint16(b[18:], 183)
	binary.LittleEndian.PutUint32(b[20:], 1)
	binary.LittleEndian.PutUint16(b[52:], 64)
	binary.LittleEndian.PutUint16(b[54:], 56)
	binary.LittleEndian.PutUint16(b[58:], 64)
	return b
}
func verifiedFixture(t *testing.T) (string, string) {
	t.Helper()
	workspace := t.TempDir()
	for rel, b := range map[string]string{"LICENSE": "MIT", "server/main.go": "package main\n", "web/package.json": "{}", "fde/assets/business.json": "{\"objects\":[]}", "fde/spec/repair.json": "{\"version\":1}"} {
		if err := writeFile(workspace, rel, []byte(b), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	artifacts := t.TempDir()
	for _, p := range []string{"server", "fde"} {
		if err := writeFile(artifacts, p, fakeLinuxELF(), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := writeFile(artifacts, "web/index.html", []byte("<html>checked</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	hash, err := runner.SnapshotHash(workspace)
	if err != nil {
		t.Fatal(err)
	}
	files, err := runner.HashTree(artifacts)
	if err != nil {
		t.Fatal(err)
	}
	report := runner.Report{ID: uuid.NewString(), Profile: runner.Profile, SnapshotHash: hash, Success: true, GOOS: "linux", GOARCH: "arm64", ImageID: "sha256:fixed-image", ArtifactsDir: artifacts, Artifacts: files, Toolchain: map[string]string{"go": "1.26.0", "node": "22.22.3", "pnpm": "11.23.0"}}
	for _, name := range checkNames {
		report.Steps = append(report.Steps, runner.Step{Name: name, ExitCode: 0})
	}
	b, _ := json.Marshal(report)
	path := filepath.Join(t.TempDir(), "report.json")
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return workspace, path
}
func packFixture(t *testing.T) Package {
	t.Helper()
	workspace, report := verifiedFixture(t)
	pkg, err := Pack(context.Background(), PackRequest{Workspace: workspace, CheckReportPath: report, OutputDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	return pkg
}

func TestPackBindsCheckedSourceAndArtifacts(t *testing.T) {
	for _, change := range []string{"source", "artifact", "failed-report"} {
		t.Run(change, func(t *testing.T) {
			workspace, path := verifiedFixture(t)
			var report runner.Report
			b, _ := os.ReadFile(path)
			_ = json.Unmarshal(b, &report)
			switch change {
			case "source":
				_ = os.WriteFile(filepath.Join(workspace, "server/main.go"), []byte("changed"), 0o644)
			case "artifact":
				_ = os.WriteFile(filepath.Join(report.ArtifactsDir, "server"), []byte("tampered"), 0o755)
			case "failed-report":
				report.Success = false
				b, _ = json.Marshal(report)
				_ = os.WriteFile(path, b, 0o600)
			}
			if _, err := Pack(context.Background(), PackRequest{Workspace: workspace, CheckReportPath: path, OutputDir: t.TempDir()}); err == nil {
				t.Fatalf("pack accepted %s", change)
			}
		})
	}
}
func TestPackageInspectionRejectsTampering(t *testing.T) {
	pkg := packFixture(t)
	if _, err := Inspect(pkg.Path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkg.Path, "web/index.html"), []byte("changed after approval"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Inspect(pkg.Path); err == nil {
		t.Fatal("tampered package accepted")
	}
}
func TestInstallIsIdempotentAndDoesNotClaimDeployment(t *testing.T) {
	pkg := packFixture(t)
	target := t.TempDir()
	first, err := Install(pkg.Path, target)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Installed || first.Ready || first.Bootstrapped || !first.NeedsConfiguration {
		t.Fatalf("installation falsely reported readiness: %+v", first)
	}
	example, err := os.ReadFile(first.ConfigExamplePath)
	if err != nil || strings.Contains(string(example), "__FDE_") || !strings.Contains(string(example), first.SharedDir) {
		t.Fatalf("persistent config example has unresolved paths: %s %v", example, err)
	}
	if err := os.WriteFile(filepath.Join(first.SharedDir, "config.yaml"), []byte("private configuration"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(first.SharedDir, "data/app.db"), []byte("existing business data"), 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := Install(pkg.Path, target)
	if err != nil || !second.Installed || second.NeedsConfiguration {
		t.Fatalf("same package retry: %+v %v", second, err)
	}
	for rel, want := range map[string]string{"config.yaml": "private configuration", "data/app.db": "existing business data"} {
		b, err := os.ReadFile(filepath.Join(first.SharedDir, rel))
		if err != nil || string(b) != want {
			t.Fatalf("retry overwrote shared %s", rel)
		}
	}
	other := packFixture(t)
	if _, err := Install(other.Path, target); err == nil {
		t.Fatal("P1 upgraded an existing installation")
	}
}

func TestInstallResumesOwnedStagingAfterProcessExit(t *testing.T) {
	pkg := packFixture(t)
	target := t.TempDir()
	hash, _ := runner.FileHash(filepath.Join(pkg.Path, "manifest.json"))
	if err := os.MkdirAll(filepath.Join(target, "journals"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := saveJournal(filepath.Join(target, "journals/install-"+pkg.ID+".json"), installJournal{ReleaseID: pkg.ID, ManifestHash: hash, State: "installing"}); err != nil {
		t.Fatal(err)
	}
	stage := filepath.Join(target, "releases/.install-"+pkg.ID+"-interrupted")
	if err := os.MkdirAll(stage, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage, "partial"), []byte("incomplete"), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(target, ".install.lock"), nil, 0o600)
	res, err := Install(pkg.Path, target)
	if err != nil || !res.Installed {
		t.Fatalf("interrupted install did not resume: %+v %v", res, err)
	}
	if _, err := os.Stat(stage); !os.IsNotExist(err) {
		t.Fatalf("stale staging retained: %v", err)
	}
}
func TestInstallReconcilesRenameBeforeJournalCompletion(t *testing.T) {
	pkg := packFixture(t)
	target := t.TempDir()
	installed, err := Install(pkg.Path, target)
	if err != nil {
		t.Fatal(err)
	}
	hash, _ := runner.FileHash(filepath.Join(pkg.Path, "manifest.json"))
	if err := saveJournal(installed.JournalPath, installJournal{ReleaseID: pkg.ID, ManifestHash: hash, State: "installing"}); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(pkg.Path, target); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(installed.JournalPath)
	var j installJournal
	_ = json.Unmarshal(b, &j)
	if j.State != "installed" {
		t.Fatalf("retry did not reconcile journal: %s", j.State)
	}
}
func TestInstallRejectsNonemptyBusinessDataAndSymlink(t *testing.T) {
	pkg := packFixture(t)
	target := t.TempDir()
	if err := os.MkdirAll(filepath.Join(target, "shared/data"), 0o700); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(target, "shared/data/old.db"), []byte("old"), 0o600)
	if _, err := Install(pkg.Path, target); err == nil {
		t.Fatal("nonempty business data accepted")
	}
	target = t.TempDir()
	if err := os.Symlink(t.TempDir(), filepath.Join(target, "shared")); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(pkg.Path, target); err == nil {
		t.Fatal("symlink shared directory accepted")
	}
}
