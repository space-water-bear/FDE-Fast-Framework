// Package delivery installs verified version directories without rebuilding code.
package delivery

import (
	"context"
	"debug/elf"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fde-admin/internal/runner"
	"github.com/google/uuid"
)

type Manifest struct {
	FormatVersion int               `json:"formatVersion"`
	ReleaseID     string            `json:"releaseId"`
	SourceHash    string            `json:"sourceHash"`
	SourceCommit  string            `json:"sourceCommit,omitempty"`
	ChecksHash    string            `json:"checksHash"`
	SpecHash      string            `json:"specHash"`
	AssetsHash    string            `json:"assetsHash"`
	GOOS          string            `json:"goos"`
	GOARCH        string            `json:"goarch"`
	Toolchain     map[string]string `json:"toolchain"`
	Files         map[string]string `json:"files"`
	CreatedAt     time.Time         `json:"createdAt"`
}
type PackRequest struct{ Workspace, CheckReportPath, OutputDir, SourceCommit string }
type Package struct {
	ID       string   `json:"id"`
	Path     string   `json:"path"`
	Manifest Manifest `json:"manifest"`
}

var checkNames = []string{"go-version", "node-version", "pnpm-version", "go-dependencies", "web-dependencies", "go-format", "go-test", "go-build", "cli-build", "web-test", "typescript", "vite-build"}

func Pack(ctx context.Context, req PackRequest) (Package, error) {
	if err := ctx.Err(); err != nil {
		return Package{}, err
	}
	b, err := os.ReadFile(req.CheckReportPath)
	if err != nil {
		return Package{}, err
	}
	var report runner.Report
	if err := json.Unmarshal(b, &report); err != nil {
		return Package{}, err
	}
	if !report.Success || report.Profile != runner.Profile || report.SnapshotHash == "" || report.ImageID == "" || report.GOOS != "linux" || len(report.Steps) != len(checkNames) {
		return Package{}, errors.New("a complete successful Docker check report is required")
	}
	for i, name := range checkNames {
		if report.Steps[i].Name != name || report.Steps[i].ExitCode != 0 {
			return Package{}, fmt.Errorf("check %s was not passed", name)
		}
	}
	if report.Toolchain["go"] != "1.26.0" || report.Toolchain["node"] != "22.22.3" || report.Toolchain["pnpm"] != "11.23.0" {
		return Package{}, errors.New("check toolchain does not match fixed profile")
	}
	hash, err := runner.SnapshotHash(req.Workspace)
	if err != nil || hash != report.SnapshotHash {
		return Package{}, errors.New("workspace changed after checks; verify again")
	}
	artifacts, err := runner.HashTree(report.ArtifactsDir)
	if err != nil {
		return Package{}, err
	}
	if !sameHashes(artifacts, report.Artifacts) {
		return Package{}, errors.New("verified artifacts changed")
	}
	for _, name := range []string{"server", "fde"} {
		if err := linuxBinary(filepath.Join(report.ArtifactsDir, name), report.GOARCH); err != nil {
			return Package{}, err
		}
	}
	if artifacts["web/index.html"] == "" {
		return Package{}, errors.New("missing built web entry")
	}
	if req.OutputDir == "" {
		return Package{}, errors.New("package output directory is required")
	}
	if err := os.MkdirAll(req.OutputDir, 0o700); err != nil {
		return Package{}, err
	}
	id := report.ID
	if _, err := uuid.Parse(id); err != nil {
		return Package{}, errors.New("invalid check report identity")
	}
	dest := filepath.Join(req.OutputDir, id)
	if _, err := os.Lstat(dest); !os.IsNotExist(err) {
		return Package{}, errors.New("package already exists")
	}
	stage, err := os.MkdirTemp(req.OutputDir, ".pack-")
	if err != nil {
		return Package{}, err
	}
	defer os.RemoveAll(stage)
	for path := range artifacts {
		if path != "server" && path != "fde" && !strings.HasPrefix(path, "web/") {
			return Package{}, fmt.Errorf("unexpected artifact: %s", path)
		}
		if err := copyChecked(report.ArtifactsDir, stage, path); err != nil {
			return Package{}, err
		}
	}
	assets := filepath.Join(req.Workspace, "fde/assets/business.json")
	assetBytes, err := os.ReadFile(assets)
	if err != nil {
		return Package{}, errors.New("frozen fde/assets/business.json is required before checks")
	}
	var assetDocument struct {
		Objects []json.RawMessage `json:"objects"`
	}
	if json.Unmarshal(assetBytes, &assetDocument) != nil || assetDocument.Objects == nil {
		return Package{}, errors.New("invalid business initialization asset")
	}
	if err := writeFile(stage, "assets/business.json", assetBytes, 0o644); err != nil {
		return Package{}, err
	}
	for _, documentDir := range []string{"spec", "requirements"} {
		sourceDir := filepath.Join(req.Workspace, "fde", documentDir)
		if st, err := os.Stat(sourceDir); err == nil && st.IsDir() {
			files, err := runner.HashTree(sourceDir)
			if err != nil {
				return Package{}, err
			}
			for path := range files {
				if err := copyChecked(sourceDir, filepath.Join(stage, documentDir), path); err != nil {
					return Package{}, err
				}
			}
		}
	}
	if err := copyChecked(req.Workspace, stage, "LICENSE"); err != nil {
		return Package{}, err
	}
	if err := writeFile(stage, "reports/checks.json", b, 0o644); err != nil {
		return Package{}, err
	}
	if err := writeFile(stage, "config.example.yaml", []byte(configExample), 0o600); err != nil {
		return Package{}, err
	}
	files, err := runner.HashTree(stage)
	if err != nil {
		return Package{}, err
	}
	checksHash, _ := runner.FileHash(req.CheckReportPath)
	assetsHash, _ := runner.FileHash(filepath.Join(stage, "assets/business.json"))
	specHash := hashListing(files, "spec/")
	if specHash == byteHash([]byte("{}")) {
		specHash = assetsHash
	}
	manifest := Manifest{FormatVersion: 1, ReleaseID: id, SourceHash: hash, SourceCommit: req.SourceCommit, ChecksHash: checksHash, SpecHash: specHash, AssetsHash: assetsHash, GOOS: "linux", GOARCH: report.GOARCH, Toolchain: report.Toolchain, Files: files, CreatedAt: time.Now().UTC()}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return Package{}, err
	}
	if err := writeFile(stage, "manifest.json", data, 0o644); err != nil {
		return Package{}, err
	}
	if after, err := runner.SnapshotHash(req.Workspace); err != nil || after != hash {
		return Package{}, errors.New("workspace changed while packaging")
	}
	if _, err := Inspect(stage); err != nil {
		return Package{}, err
	}
	if err := ctx.Err(); err != nil {
		return Package{}, err
	}
	if err := os.Rename(stage, dest); err != nil {
		return Package{}, err
	}
	return Package{ID: id, Path: dest, Manifest: manifest}, nil
}

func Inspect(path string) (Package, error) {
	st, err := os.Lstat(path)
	if err != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return Package{}, errors.New("release must be a real directory")
	}
	data, err := os.ReadFile(filepath.Join(path, "manifest.json"))
	if err != nil {
		return Package{}, err
	}
	var m Manifest
	if json.Unmarshal(data, &m) != nil || m.FormatVersion != 1 || m.GOOS != "linux" || m.SourceHash == "" || m.ChecksHash == "" {
		return Package{}, errors.New("invalid release manifest")
	}
	if _, err := uuid.Parse(m.ReleaseID); err != nil {
		return Package{}, errors.New("invalid release ID")
	}
	actual, err := runner.HashTree(path)
	if err != nil {
		return Package{}, err
	}
	delete(actual, "manifest.json")
	if !sameHashes(actual, m.Files) {
		return Package{}, errors.New("release files do not match manifest")
	}
	for _, p := range []string{"server", "fde", "web/index.html", "assets/business.json", "reports/checks.json", "config.example.yaml", "LICENSE"} {
		if m.Files[p] == "" {
			return Package{}, fmt.Errorf("release is missing %s", p)
		}
	}
	expectedSpecHash := hashListing(m.Files, "spec/")
	if expectedSpecHash == byteHash([]byte("{}")) {
		expectedSpecHash = m.AssetsHash
	}
	if m.Files["reports/checks.json"] != m.ChecksHash || m.Files["assets/business.json"] != m.AssetsHash || expectedSpecHash != m.SpecHash {
		return Package{}, errors.New("release asset/check hashes do not match")
	}
	for _, p := range []string{"server", "fde"} {
		if err := linuxBinary(filepath.Join(path, p), m.GOARCH); err != nil {
			return Package{}, err
		}
	}
	checkedBytes, err := os.ReadFile(filepath.Join(path, "reports/checks.json"))
	if err != nil {
		return Package{}, err
	}
	var checked runner.Report
	if json.Unmarshal(checkedBytes, &checked) != nil || !checked.Success || checked.ID != m.ReleaseID || checked.SnapshotHash != m.SourceHash || checked.GOOS != m.GOOS || checked.GOARCH != m.GOARCH || checked.Profile != runner.Profile || len(checked.Steps) != len(checkNames) {
		return Package{}, errors.New("packaged check report does not certify this release")
	}
	if checked.ImageID == "" || checked.Artifacts["server"] == "" || checked.Artifacts["fde"] == "" || checked.Artifacts["web/index.html"] == "" || checked.Toolchain["go"] != "1.26.0" || checked.Toolchain["node"] != "22.22.3" || checked.Toolchain["pnpm"] != "11.23.0" {
		return Package{}, errors.New("release check evidence is incomplete")
	}
	for key, value := range checked.Toolchain {
		if m.Toolchain[key] != value {
			return Package{}, errors.New("release toolchain differs from check evidence")
		}
	}
	for i, name := range checkNames {
		if checked.Steps[i].Name != name || checked.Steps[i].ExitCode != 0 {
			return Package{}, errors.New("release contains failed or incomplete checks")
		}
	}
	for p, h := range checked.Artifacts {
		if m.Files[p] != h {
			return Package{}, errors.New("release artifacts differ from check report")
		}
	}
	abs, _ := filepath.Abs(path)
	return Package{ID: m.ReleaseID, Path: abs, Manifest: m}, nil
}

func linuxBinary(path, arch string) error {
	f, err := elf.Open(path)
	if err != nil {
		return fmt.Errorf("not a Linux ELF artifact %s: %w", filepath.Base(path), err)
	}
	defer f.Close()
	machine := elf.EM_NONE
	if arch == "arm64" {
		machine = elf.EM_AARCH64
	}
	if arch == "amd64" {
		machine = elf.EM_X86_64
	}
	if machine == elf.EM_NONE || f.Machine != machine || (f.Type != elf.ET_EXEC && f.Type != elf.ET_DYN) {
		return fmt.Errorf("ELF architecture mismatch: %s", filepath.Base(path))
	}
	return nil
}
func sameHashes(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for p, h := range a {
		if h == "" || b[p] != h {
			return false
		}
	}
	return true
}
func hashListing(files map[string]string, prefix string) string {
	selected := map[string]string{}
	for p, h := range files {
		if strings.HasPrefix(p, prefix) {
			selected[p] = h
		}
	}
	b, _ := json.Marshal(selected)
	return byteHash(b)
}
func copyChecked(src, dst, rel string) error {
	if !fs.ValidPath(rel) || strings.Contains(rel, "\\") {
		return errors.New("invalid release file path")
	}
	r, err := os.OpenRoot(src)
	if err != nil {
		return err
	}
	defer r.Close()
	st, err := r.Lstat(rel)
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() {
		return errors.New("release source must be a regular file")
	}
	b, err := r.ReadFile(rel)
	if err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if rel == "server" || rel == "fde" {
		mode = 0o755
	}
	return writeFile(dst, rel, b, mode)
}
func writeFile(root, rel string, b []byte, mode fs.FileMode) error {
	if !fs.ValidPath(rel) || strings.Contains(rel, "\\") {
		return errors.New("invalid release path")
	}
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, b, mode)
}

const configExample = `# Install writes a resolved copy at shared/config/config.example.yaml.
# Copy that file to shared/config.yaml and provide explicit credentials before bootstrap.
app:
  name: Customer Application
  port: 8080
  mode: release
db:
  driver: sqlite
  dsn: __FDE_DATA_PATH__
jwt:
  secret: REPLACE_WITH_RANDOM_SECRET
seed:
  enabled: true
  admin-username: admin
  admin-password: REPLACE_WITH_INITIAL_PASSWORD
upload:
  dir: __FDE_UPLOAD_PATH__
zap:
  director: __FDE_LOG_PATH__
`
