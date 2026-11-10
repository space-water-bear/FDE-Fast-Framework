package service

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"fde-admin/internal/config"
	"fde-admin/internal/delivery"
	"fde-admin/internal/global"
	"fde-admin/internal/model"
	"fde-admin/internal/runner"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestGeneratedRuntimeCredentialsSurviveRetry(t *testing.T) {
	shared := t.TempDir()
	first, err := ensureDevelopmentRuntimeConfig(shared, 2, "test-release")
	if err != nil {
		t.Fatal(err)
	}
	second, err := ensureDevelopmentRuntimeConfig(shared, 2, "test-release")
	if err != nil {
		t.Fatal(err)
	}
	if first["password"] == "" || second["password"] != first["password"] {
		t.Fatal("failed deployment retry lost its generated login credentials")
	}
}

func TestGeneratedRuntimeConfigProvenanceAndPreparedRecovery(t *testing.T) {
	shared := t.TempDir()
	first, err := ensureDevelopmentRuntimeConfig(shared, 2, "release-a")
	if err != nil {
		t.Fatal(err)
	}
	if creds, err := ensureDevelopmentRuntimeConfig(shared, 3, "release-a"); err == nil || creds != nil {
		t.Fatal("credentials crossed project scope")
	}
	if creds, err := ensureDevelopmentRuntimeConfig(shared, 2, "release-b"); err == nil || creds != nil {
		t.Fatal("credentials crossed release scope")
	}
	data, err := os.ReadFile(filepath.Join(shared, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	marker := generatedDevelopmentConfig{ProjectID: 2, ReleaseID: "release-a", ConfigHash: sha256Hex(string(data)), State: "prepared", PreparedConfig: data}
	if err := writeDevelopmentPrivateJSON(filepath.Join(shared, "config.generated.json"), marker); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(shared, "config.yaml")); err != nil {
		t.Fatal(err)
	}
	recovered, err := ensureDevelopmentRuntimeConfig(shared, 2, "release-a")
	if err != nil || recovered["password"] != first["password"] {
		t.Fatalf("prepared config was not recovered: %v", err)
	}
	if err := os.WriteFile(filepath.Join(shared, "config.yaml"), []byte("manual: changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if creds, err := ensureDevelopmentRuntimeConfig(shared, 2, "release-a"); err != nil || creds != nil {
		t.Fatal("edited config secret was returned")
	}
	external := t.TempDir()
	if err := os.WriteFile(filepath.Join(external, "config.yaml"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if creds, err := ensureDevelopmentRuntimeConfig(external, 2, "release-a"); err != nil || creds != nil {
		t.Fatal("unmarked configuration was treated as generated")
	}
}

func releaseHTTPFixture(t *testing.T, releaseID string, failure string) (*httptest.Server, string, string, *atomic.Int32) {
	t.Helper()
	shared := t.TempDir()
	expectedLogin, err := ensureDevelopmentRuntimeConfig(shared, 2, releaseID)
	if err != nil {
		t.Fatal(err)
	}
	release := t.TempDir()
	if err := os.MkdirAll(filepath.Join(release, "assets"), 0o700); err != nil {
		t.Fatal(err)
	}
	assets := BusinessInstallationAssets{Objects: []BusinessInstallationAsset{{ObjectKey: "biz_repair_ticket", Schema: map[string]any{"version": 1}, Menu: CodegenMenuPlanItem{Path: "repair_ticket"}}, {ObjectKey: "biz_customer_note", Schema: map[string]any{"version": 1}, Menu: CodegenMenuPlanItem{Path: "customer_note"}}}}
	data, _ := json.Marshal(assets)
	if err := os.WriteFile(filepath.Join(release, "assets/business.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	logins := new(atomic.Int32)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/ready":
			fmt.Fprintf(w, `{"status":"ready","releaseId":%q}`, releaseID)
			return
		case "/":
			w.WriteHeader(200)
			_, _ = w.Write([]byte("<html>release</html>"))
			return
		case "/api/auth/login":
			logins.Add(1)
			if failure != "accept_any_credentials" {
				var login map[string]string
				_ = json.NewDecoder(r.Body).Decode(&login)
				if login["username"] != expectedLogin["username"] || login["password"] != expectedLogin["password"] {
					fmt.Fprint(w, `{"code":40001}`)
					return
				}
			}
			if failure == "login" {
				fmt.Fprint(w, `{"code":40001}`)
				return
			}
			fmt.Fprint(w, `{"code":0,"data":{"token":"private-test-token","refreshToken":"refresh"}}`)
			return
		case "/api/auth/logout":
			fmt.Fprint(w, `{"code":0,"data":null}`)
			return
		}
		if r.Method != "GET" {
			t.Error("business acceptance wrote business data")
		}
		if r.Header.Get("x-token") != "private-test-token" {
			fmt.Fprint(w, `{"code":40004}`)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/list") {
			if failure == "list" {
				fmt.Fprint(w, `{"code":40003}`)
				return
			}
			fmt.Fprint(w, `{"code":0,"data":{"list":[],"total":0}}`)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/biz/schema/") {
			key := strings.TrimPrefix(r.URL.Path, "/api/biz/schema/")
			if failure == "key" {
				key = "biz_wrong"
			}
			version := 1
			if failure == "version" {
				version = 2
			}
			activeVersion := version
			if failure == "activeVersion" {
				activeVersion = 2
			}
			fmt.Fprintf(w, `{"code":0,"data":{"objectKey":%q,"version":%d,"activeVersion":%d,"fields":{}}}`, key, version, activeVersion)
			return
		}
		w.WriteHeader(404)
	}))
	return server, shared, release, logins
}

func TestBusinessAcceptanceChecksRealListAndActiveSchema(t *testing.T) {
	for _, failure := range []string{"", "list", "key", "version", "activeVersion", "login"} {
		t.Run("case_"+failure, func(t *testing.T) {
			s, shared, release, _ := releaseHTTPFixture(t, "release-http", failure)
			defer s.Close()
			err := verifyDevelopmentBusinessHTTP(context.Background(), s.URL, shared, release)
			if (err == nil) != (failure == "") {
				t.Fatalf("business failure %q incorrectly accepted: %v", failure, err)
			}
		})
	}
}

func TestDevelopmentCleanupIsBoundedAndTargetScoped(t *testing.T) {
	original := developmentDockerCommand
	t.Cleanup(func() { developmentDockerCommand = original })
	called := false
	developmentDockerCommand = func(ctx context.Context, target string, args ...string) (string, error) {
		called = true
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 11*time.Second {
			t.Error("cleanup has no bound")
		}
		return "", errors.New("Error response from daemon: No such container: temporary")
	}
	if err := removeDevelopmentContainer(t.TempDir(), "temporary"); err != nil || !called {
		t.Fatal("safe nonexistent container cleanup failed")
	}
	if developmentContainerName("/tmp/control-a/2/local-linux") == developmentContainerName("/tmp/control-b/2/local-linux") {
		t.Fatal("different control instances share a container name")
	}
}

func releasePackageFixture(t *testing.T) (delivery.Package, model.DevRun, runner.Report, string) {
	t.Helper()
	workspace := t.TempDir()
	for rel, b := range map[string]string{"LICENSE": "MIT", "server/main.go": "package main\n", "web/package.json": "{}"} {
		p := filepath.Join(workspace, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(b), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	assets := BusinessInstallationAssets{Objects: []BusinessInstallationAsset{{ObjectKey: "biz_repair_ticket", Schema: map[string]any{"objectKey": "biz_repair_ticket", "version": 1}, Menu: CodegenMenuPlanItem{Path: "repair_ticket"}}}}
	assetBytes, _ := json.Marshal(assets)
	if err := os.MkdirAll(filepath.Join(workspace, "fde/assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(workspace, "fde/assets/business.json"), assetBytes, 0o644)
	artifacts := t.TempDir()
	elf := make([]byte, 64)
	copy(elf, []byte{0x7f, 'E', 'L', 'F', 2, 1, 1})
	binary.LittleEndian.PutUint16(elf[16:], 2)
	binary.LittleEndian.PutUint16(elf[18:], 183)
	binary.LittleEndian.PutUint32(elf[20:], 1)
	binary.LittleEndian.PutUint16(elf[52:], 64)
	binary.LittleEndian.PutUint16(elf[54:], 56)
	binary.LittleEndian.PutUint16(elf[58:], 64)
	for _, p := range []string{"server", "fde"} {
		_ = os.WriteFile(filepath.Join(artifacts, p), elf, 0o755)
	}
	_ = os.MkdirAll(filepath.Join(artifacts, "web"), 0o755)
	_ = os.WriteFile(filepath.Join(artifacts, "web/index.html"), []byte("<html>verified</html>"), 0o644)
	hash, err := runner.SnapshotHash(workspace)
	if err != nil {
		t.Fatal(err)
	}
	files, err := runner.HashTree(artifacts)
	if err != nil {
		t.Fatal(err)
	}
	report := runner.Report{ID: uuid.NewString(), Profile: runner.Profile, SnapshotHash: hash, Success: true, ArtifactsDir: artifacts, Artifacts: files, GOOS: "linux", GOARCH: "arm64", ImageID: "sha256:fixed", Toolchain: map[string]string{"go": "1.26.0", "node": "22.22.3", "pnpm": "11.23.0"}, ReportPath: filepath.Join(t.TempDir(), "report.json")}
	for _, name := range []string{"go-version", "node-version", "pnpm-version", "go-dependencies", "web-dependencies", "go-format", "go-test", "go-build", "cli-build", "web-test", "typescript", "vite-build"} {
		report.Steps = append(report.Steps, runner.Step{Name: name})
	}
	data, _ := json.Marshal(report)
	_ = os.WriteFile(report.ReportPath, data, 0o600)
	output := t.TempDir()
	run := model.DevRun{WorkspacePath: workspace, SourceHash: hash, BaseCommit: "test-base"}
	pkg, err := delivery.Pack(context.Background(), delivery.PackRequest{Workspace: workspace, CheckReportPath: report.ReportPath, OutputDir: output, SourceCommit: run.BaseCommit})
	if err != nil {
		t.Fatal(err)
	}
	return pkg, run, report, output
}

func TestReleaseCandidateCanRecoverAfterDatabaseSaveFailure(t *testing.T) {
	pkg, run, report, root := releasePackageFixture(t)
	recovered, err := developmentPackageCandidate(context.Background(), run, report, root)
	if err != nil || recovered.ID != pkg.ID {
		t.Fatalf("verified orphan candidate was not recoverable: %v", err)
	}
	if err := os.WriteFile(report.ReportPath, []byte("changed report"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := developmentPackageCandidate(context.Background(), run, report, root); err == nil {
		t.Fatal("orphan candidate accepted different check evidence")
	}
}

func TestCreateReleaseRetriesVerifiedCandidateAfterDatabaseFailure(t *testing.T) {
	pkg, run, report, output := releasePackageFixture(t)
	setupTestDB(t)
	if err := global.DB.AutoMigrate(&model.User{}, &model.Role{}, &model.DevProject{}, &model.DevRun{}, &model.DevRunEvent{}, &model.DevRelease{}); err != nil {
		t.Fatal(err)
	}
	user := model.User{Username: "candidate-owner", Enabled: true}
	if err := global.DB.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	project := model.DevProject{Key: "candidate-project", Name: "project", OwnerID: user.ID}
	if err := global.DB.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(report)
	run.Key = uuid.NewString()
	run.ActorID = user.ID
	run.ProjectID = project.ID
	run.State = "succeeded"
	run.CheckResult = string(data)
	if err := global.DB.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	global.Config = &config.Config{Development: config.Development{ReleaseDir: output}}
	if err := global.DB.Callback().Create().Before("gorm:create").Register("test:reject_release", func(tx *gorm.DB) {
		if tx.Statement.Table == "sys_dev_releases" {
			tx.AddError(errors.New("injected release save failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	scope := EngineeringScope{ActorID: user.ID, ProjectID: project.ID}
	if _, err := CreateDevelopmentRelease(context.Background(), scope, run.Key); err == nil {
		t.Fatal("database save failure hidden")
	}
	if err := global.DB.Callback().Create().Remove("test:reject_release"); err != nil {
		t.Fatal(err)
	}
	saved, err := CreateDevelopmentRelease(context.Background(), scope, run.Key)
	if err != nil || saved.Key != pkg.ID || saved.PackagePath != pkg.Path {
		t.Fatalf("retry did not register verified candidate: %+v %v", saved, err)
	}
}

func setupReleaseControl(t *testing.T, pkg delivery.Package) EngineeringScope {
	t.Helper()
	setupTestDB(t)
	if err := global.DB.AutoMigrate(&model.User{}, &model.Role{}, &model.Menu{}, &model.DevProject{}, &model.DevRelease{}, &model.DevDeployment{}); err != nil {
		t.Fatal(err)
	}
	user := model.User{Username: "release-owner", Enabled: true}
	if err := global.DB.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	project := model.DevProject{Key: "release-project", Name: "project", OwnerID: user.ID}
	if err := global.DB.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	manifestHash, err := runner.FileHash(filepath.Join(pkg.Path, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	release := model.DevRelease{Key: pkg.ID, ProjectID: project.ID, ActorID: user.ID, Status: "ready", SourceHash: pkg.Manifest.SourceHash, ArtifactHash: manifestHash, PackagePath: pkg.Path}
	if err := global.DB.Create(&release).Error; err != nil {
		t.Fatal(err)
	}
	global.Config = &config.Config{Development: config.Development{TargetDir: t.TempDir()}}
	return EngineeringScope{ActorID: user.ID, ProjectID: project.ID}
}

func TestDeploymentCleansServeContainerWhenPortLookupFails(t *testing.T) {
	pkg, _, _, _ := releasePackageFixture(t)
	scope := setupReleaseControl(t, pkg)
	original := developmentDockerCommand
	t.Cleanup(func() { developmentDockerCommand = original })
	started := ""
	removedAfterStart := false
	developmentDockerCommand = func(ctx context.Context, target string, args ...string) (string, error) {
		if args[0] == "run" && args[1] == "--detach" {
			for i, arg := range args {
				if arg == "--name" {
					started = args[i+1]
				}
			}
			return "container", nil
		}
		if args[0] == "port" {
			return "", errors.New("injected port lookup failure")
		}
		if args[0] == "rm" && started != "" && args[len(args)-1] == started {
			removedAfterStart = true
		}
		return "", nil
	}
	if _, err := DeployDevelopmentRelease(context.Background(), scope, pkg.ID, "local-linux", true, false); err == nil {
		t.Fatal("port failure hidden")
	}
	if started == "" || !removedAfterStart {
		t.Fatal("started service was left running after failed port lookup")
	}
	var deployment model.DevDeployment
	if err := global.DB.First(&deployment).Error; err != nil {
		t.Fatal(err)
	}
	if deployment.Status != "failed" {
		t.Fatalf("failed attempt status: %s", deployment.Status)
	}
}

func TestDeploymentCleansServeWhenPersistingSuccessFails(t *testing.T) {
	pkg, _, _, _ := releasePackageFixture(t)
	scope := setupReleaseControl(t, pkg)
	server, _, _, _ := releaseHTTPFixture(t, pkg.ID, "accept_any_credentials")
	defer server.Close()
	original := developmentDockerCommand
	t.Cleanup(func() { developmentDockerCommand = original })
	started := ""
	cleaned := false
	developmentDockerCommand = func(ctx context.Context, target string, args ...string) (string, error) {
		if args[0] == "run" && args[1] == "--detach" {
			for i, arg := range args {
				if arg == "--name" {
					started = args[i+1]
				}
			}
		}
		if args[0] == "port" {
			return strings.TrimPrefix(server.URL, "http://"), nil
		}
		if args[0] == "rm" && started != "" && args[len(args)-1] == started {
			cleaned = true
		}
		return "", nil
	}
	if err := global.DB.Callback().Update().Before("gorm:update").Register("test:reject_deployed", func(tx *gorm.DB) {
		if changes, ok := tx.Statement.Dest.(map[string]any); ok && changes["status"] == "deployed" {
			tx.AddError(errors.New("injected state persistence failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := DeployDevelopmentRelease(context.Background(), scope, pkg.ID, "local-linux", true, false); err == nil {
		t.Fatal("database persistence failure hidden")
	}
	if !cleaned {
		t.Fatal("service retained after deployment state was not saved")
	}
}

func TestCanceledDetachedStartStillCleansItsNamedContainer(t *testing.T) {
	pkg, _, _, _ := releasePackageFixture(t)
	scope := setupReleaseControl(t, pkg)
	original := developmentDockerCommand
	t.Cleanup(func() { developmentDockerCommand = original })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := ""
	cleaned := false
	developmentDockerCommand = func(commandCtx context.Context, target string, args ...string) (string, error) {
		if args[0] == "run" && args[1] == "--detach" {
			for i, arg := range args {
				if arg == "--name" {
					started = args[i+1]
				}
			}
			cancel()
			return "", commandCtx.Err()
		}
		if args[0] == "rm" && started != "" && args[len(args)-1] == started {
			if commandCtx.Err() != nil {
				t.Error("cleanup inherited canceled request")
			}
			cleaned = true
		}
		return "", nil
	}
	if _, err := DeployDevelopmentRelease(ctx, scope, pkg.ID, "local-linux", true, false); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation was lost: %v", err)
	}
	if !cleaned {
		t.Fatal("uncertain detached start left container alive")
	}
}

func TestDeployedReleaseReusesHealthyAcceptedTargetWithoutLogin(t *testing.T) {
	pkg, _, _, _ := releasePackageFixture(t)
	scope := setupReleaseControl(t, pkg)
	server, _, _, logins := releaseHTTPFixture(t, pkg.ID, "accept_any_credentials")
	defer server.Close()
	original := developmentDockerCommand
	t.Cleanup(func() { developmentDockerCommand = original })
	runs := 0
	developmentDockerCommand = func(ctx context.Context, target string, args ...string) (string, error) {
		if args[0] == "run" {
			runs++
		}
		if args[0] == "port" {
			return strings.TrimPrefix(server.URL, "http://"), nil
		}
		return "", nil
	}
	first, err := DeployDevelopmentRelease(context.Background(), scope, pkg.ID, "local-linux", true, false)
	if err != nil || first.Status != "deployed" || first.Credentials["password"] == "" {
		t.Fatalf("first acceptance failed: %+v %v", first, err)
	}
	priorRuns, priorLogins := runs, logins.Load()
	second, err := DeployDevelopmentRelease(context.Background(), scope, pkg.ID, "local-linux", true, false)
	if err != nil || second.Status != "deployed" || second.Credentials != nil {
		t.Fatalf("repeat acceptance failed: %+v %v", second, err)
	}
	if runs != priorRuns || logins.Load() != priorLogins {
		t.Fatal("healthy accepted deployment restarted or reused initialization password")
	}
}

func TestDevelopmentRuntimePortRemainsExplicitAcrossRestarts(t *testing.T) {
	port, err := developmentRuntimePort("")
	if err != nil || port == "" {
		t.Fatalf("missing loopback port: %s %v", port, err)
	}
	next, err := developmentRuntimePort("http://127.0.0.1:" + port)
	if err != nil || next != port {
		t.Fatal("runtime port changed for existing target")
	}
}
