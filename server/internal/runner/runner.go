package runner

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

//go:embed image.Dockerfile
var imageRecipe string

func PrepareImage(ctx context.Context, outputDir string) error {
	if err := os.MkdirAll(outputDir, 0o700); err != nil {
		return err
	}
	empty, err := os.MkdirTemp(outputDir, "image-context-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(empty)
	result, err := (OSExecutor{}).Run(ctx, Command{Args: []string{"build", "--tag", Image, "--file", "-", empty}, Stdin: imageRecipe, LogPath: filepath.Join(outputDir, "image-build.log"), MaxLogBytes: 1 << 20})
	if err != nil {
		return err
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("runner image build failed (exit %d); see image-build.log", result.ExitCode)
	}
	return nil
}

func (r *Runner) Check(ctx context.Context, req Request) (report Report, retErr error) {
	if req.Profile == "" {
		req.Profile = Profile
	}
	if req.Profile != Profile {
		return Report{}, errors.New("unsupported check profile")
	}
	if req.GOARCH == "" {
		req.GOARCH = "arm64"
	}
	if req.GOARCH != "arm64" && req.GOARCH != "amd64" {
		return Report{}, errors.New("unsupported Linux architecture")
	}
	if req.Timeout <= 0 || req.Timeout > time.Hour {
		req.Timeout = 30 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, req.Timeout)
	defer cancel()
	hash, err := SnapshotHash(req.Workspace)
	if err != nil {
		return Report{}, err
	}
	if req.ExpectedSnapshotHash == "" || hash != req.ExpectedSnapshotHash {
		return Report{}, errors.New("source snapshot does not match requested hash")
	}
	workspace, err := filepath.Abs(req.Workspace)
	if err != nil {
		return Report{}, err
	}
	output, err := filepath.Abs(req.OutputDir)
	if err != nil || req.OutputDir == "" {
		return Report{}, errors.New("external check output directory is required")
	}
	rel, _ := filepath.Rel(workspace, output)
	if rel == "." || rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return Report{}, errors.New("check output must be outside the source workspace")
	}
	if err := os.MkdirAll(output, 0o700); err != nil {
		return Report{}, err
	}
	dir, err := os.MkdirTemp(output, "check-")
	if err != nil {
		return Report{}, err
	}
	report = Report{ID: uuid.NewString(), Profile: Profile, SnapshotHash: hash, ReportPath: filepath.Join(dir, "report.json"), ArtifactsDir: filepath.Join(dir, "artifacts"), Artifacts: map[string]string{}, GOOS: "linux", GOARCH: req.GOARCH, Toolchain: map[string]string{"go": "1.26.0", "node": "22.22.3", "pnpm": "11.23.0"}, StartedAt: time.Now().UTC()}
	defer func() {
		report.FinishedAt = time.Now().UTC()
		if retErr != nil {
			report.Error = retErr.Error()
		}
		b, err := json.MarshalIndent(report, "", "  ")
		if err == nil {
			err = os.WriteFile(report.ReportPath, b, 0o600)
		}
		if err != nil && retErr == nil {
			retErr = err
			report.Success = false
		}
	}()
	source := filepath.Join(dir, "source")
	if err := copySnapshot(workspace, source); err != nil {
		return report, err
	}
	defer os.RemoveAll(source)
	if copied, err := SnapshotHash(source); err != nil || copied != hash {
		return report, errors.New("source changed while creating check snapshot")
	}
	for _, p := range []string{report.ArtifactsDir, filepath.Join(dir, "go"), filepath.Join(dir, "cache")} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			return report, err
		}
	}
	defer os.RemoveAll(filepath.Join(dir, "go"))
	defer os.RemoveAll(filepath.Join(dir, "cache"))
	inspect, err := r.executor.Run(ctx, Command{Args: []string{"image", "inspect", Image, "--format", "{{.Id}}"}, LogPath: filepath.Join(dir, "image.log"), MaxLogBytes: 4096})
	if err != nil || inspect.ExitCode != 0 {
		report.FailedStep = "image"
		return report, fmt.Errorf("Docker runner image unavailable; run fde prepare-image: %v", err)
	}
	report.ImageID = strings.TrimSpace(inspect.Output)
	if !strings.HasPrefix(report.ImageID, "sha256:") || strings.ContainsAny(report.ImageID, " \n\t") {
		report.FailedStep = "image"
		return report, errors.New("invalid Docker image identity")
	}
	type check struct {
		name, work, network string
		args                []string
		version             string
	}
	checks := []check{
		{"go-version", "server", "none", []string{"go", "version"}, "go1.26.0"},
		{"node-version", "web", "none", []string{"node", "--version"}, "v22.22.3"},
		{"pnpm-version", "web", "none", []string{"pnpm", "--version"}, "11.23.0"},
		{"go-dependencies", "server", "bridge", []string{"go", "mod", "download"}, ""},
		{"web-dependencies", "web", "bridge", []string{"pnpm", "install", "--frozen-lockfile", "--ignore-scripts", "--ignore-pnpmfile"}, ""},
		{"go-format", "server", "none", []string{"gofmt", "-l", "."}, ""},
		{"go-test", "server", "none", []string{"go", "test", "./..."}, ""},
		{"go-build", "server", "none", []string{"go", "build", "-trimpath", "-o", "/out/server", "."}, ""},
		{"cli-build", "server", "none", []string{"go", "build", "-trimpath", "-o", "/out/fde", "./cmd/fde"}, ""},
		{"web-test", "web", "none", []string{"node", "--test"}, ""},
		{"typescript", "web", "none", []string{"pnpm", "exec", "vue-tsc", "--noEmit"}, ""},
		{"vite-build", "web", "none", []string{"pnpm", "exec", "vite", "build", "--outDir", "/out/web"}, ""},
	}
	for i, step := range checks {
		name := "fde-check-" + strings.ReplaceAll(report.ID, "-", "") + "-" + strconv.Itoa(i)
		args := []string{"run", "--rm", "--name", name, "--read-only", "--cap-drop=ALL", "--security-opt=no-new-privileges", "--pids-limit=256", "--memory=4g", "--cpus=2", "--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()), "--network", step.network, "--tmpfs", "/tmp:rw,exec,nosuid,nodev,size=1g", "--mount", "type=bind,src=" + source + ",dst=/workspace", "--mount", "type=bind,src=" + report.ArtifactsDir + ",dst=/out", "--mount", "type=bind,src=" + filepath.Join(dir, "go") + ",dst=/go", "--mount", "type=bind,src=" + filepath.Join(dir, "cache") + ",dst=/cache", "--workdir", "/workspace/" + step.work, "--env", "CI=1", "--env", "HOME=/tmp", "--env", "GOTOOLCHAIN=local", "--env", "GOENV=off", "--env", "GOCACHE=/cache/go-build", "--env", "GOMODCACHE=/go/pkg/mod", "--env", "CGO_ENABLED=0", "--env", "GOOS=linux", "--env", "GOARCH=" + req.GOARCH, report.ImageID}
		cacheName := step.name
		if step.name == "go-build" || step.name == "cli-build" {
			cacheName = "trusted-build"
		}
		cacheDir := filepath.Join(dir, "cache", cacheName)
		if err := os.MkdirAll(cacheDir, 0o755); err != nil {
			return report, err
		}
		for n, arg := range args {
			if arg == "type=bind,src="+filepath.Join(dir, "cache")+",dst=/cache" {
				args[n] = "type=bind,src=" + cacheDir + ",dst=/cache"
			}
			if i >= 5 && arg == "type=bind,src="+filepath.Join(dir, "go")+",dst=/go" {
				args[n] = arg + ",readonly"
			}
		}
		if i >= 5 {
			modules := filepath.Join(source, "web/node_modules")
			if st, err := os.Stat(modules); err == nil && st.IsDir() {
				args = append(args[:len(args)-1], "--mount", "type=bind,src="+modules+",dst=/workspace/web/node_modules,readonly", report.ImageID)
			}
		}
		if step.name != "go-build" && step.name != "cli-build" {
			for n, arg := range args {
				if arg == "type=bind,src="+report.ArtifactsDir+",dst=/out" {
					args[n] = arg + ",readonly"
				}
			}
		}
		if step.name == "vite-build" {
			if err := os.MkdirAll(filepath.Join(report.ArtifactsDir, "web"), 0o755); err != nil {
				return report, err
			}
			args = append(args[:len(args)-1], "--mount", "type=bind,src="+filepath.Join(report.ArtifactsDir, "web")+",dst=/out/web", report.ImageID)
		}
		if step.name != "go-build" && step.name != "cli-build" {
			for n := 0; n < len(args)-1; n++ {
				if args[n] == "--env" && strings.HasPrefix(args[n+1], "GOARCH=") {
					args = append(args[:n], args[n+2:]...)
					break
				}
			}
		}
		args = append(args, step.args...)
		logPath := filepath.Join(dir, step.name+".log")
		started := time.Now()
		stepCtx, stop := context.WithTimeout(ctx, 10*time.Minute)
		result, execErr := r.executor.Run(stepCtx, Command{Args: args, LogPath: logPath, MaxLogBytes: 256 << 10})
		stop()
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		_, _ = r.executor.Run(cleanupCtx, Command{Args: []string{"rm", "--force", name}, LogPath: filepath.Join(dir, step.name+"-cleanup.log"), MaxLogBytes: 4096})
		cleanupCancel()
		report.Steps = append(report.Steps, Step{Name: step.name, ExitCode: result.ExitCode, LogPath: logPath, Truncated: result.Truncated, DurationMS: time.Since(started).Milliseconds()})
		if execErr != nil || result.ExitCode != 0 || (step.version != "" && !strings.Contains(strings.TrimSpace(result.Output), step.version)) || (step.name == "go-format" && strings.TrimSpace(result.Output) != "") {
			report.FailedStep = step.name
			report.Diagnostic = result.Output
			if len(report.Diagnostic) > 16000 {
				report.Diagnostic = report.Diagnostic[:16000]
			}
			if execErr != nil {
				return report, execErr
			}
			return report, fmt.Errorf("check %s failed (exit %d); see step log", step.name, result.ExitCode)
		}
	}
	if after, err := SnapshotHash(source); err != nil || after != hash {
		report.FailedStep = "snapshot"
		return report, errors.New("check commands modified source inputs")
	}
	if after, err := SnapshotHash(workspace); err != nil || after != hash {
		report.FailedStep = "snapshot"
		return report, errors.New("workspace changed during verification")
	}
	report.Artifacts, err = HashTree(report.ArtifactsDir)
	if err != nil {
		return report, err
	}
	for _, p := range []string{"server", "fde", "web/index.html"} {
		if report.Artifacts[p] == "" {
			report.FailedStep = "artifacts"
			return report, fmt.Errorf("missing Linux artifact %s", p)
		}
	}
	if err := ctx.Err(); err != nil {
		report.FailedStep = "canceled"
		return report, err
	}
	report.Success = true
	return report, nil
}
