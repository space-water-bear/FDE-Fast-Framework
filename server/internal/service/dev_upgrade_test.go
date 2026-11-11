package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"fde-admin/internal/global"
	"fde-admin/internal/model"
)

func TestDevelopmentTargetReadsExistingP1AndRejectsStaleApproval(t *testing.T) {
	pkg, _, _, _ := releasePackageFixture(t)
	scope := setupReleaseControl(t, pkg)
	ctx := context.Background()
	state, err := GetDevelopmentTarget(ctx, scope, "local-linux")
	if err != nil || state.Status != "empty" || state.TargetID != "local-linux" {
		t.Fatalf("empty target: %+v %v", state, err)
	}
	value, _ := developmentDeliveryLocks.LoadOrStore(fmt.Sprintf("target:%d:%s", scope.ProjectID, "local-linux"), &sync.Mutex{})
	lock := value.(*sync.Mutex)
	lock.Lock()
	busy, busyErr := GetDevelopmentTarget(ctx, scope, "local-linux")
	lock.Unlock()
	if busyErr != nil || !busy.OperationActive {
		t.Fatalf("active operation not reported: %+v %v", busy, busyErr)
	}
	if _, err := GetDevelopmentTarget(ctx, scope, "other"); err == nil {
		t.Fatal("accepted arbitrary target")
	}
	var release model.DevRelease
	if err := global.DB.Where("key = ?", pkg.ID).First(&release).Error; err != nil {
		t.Fatal(err)
	}
	prior := model.DevDeployment{ReleaseID: release.ID, TargetID: "local-linux", Status: "deployed", URL: "http://127.0.0.1:18080"}
	if err := global.DB.Create(&prior).Error; err != nil {
		t.Fatal(err)
	}
	state, err = GetDevelopmentTarget(ctx, scope, "local-linux")
	if err != nil || state.CurrentReleaseID != pkg.ID || state.Status != "deployed" || state.TargetID != "local-linux" {
		t.Fatalf("P1 target: %+v %v", state, err)
	}
	original := developmentDockerCommand
	t.Cleanup(func() { developmentDockerCommand = original })
	developmentDockerCommand = func(context.Context, string, ...string) (string, error) {
		t.Fatal("rejected approval touched the running target")
		return "", nil
	}
	if _, err := UpgradeDevelopmentRelease(ctx, scope, pkg.ID, "local-linux", pkg.ID, false); err == nil {
		t.Fatal("upgrade lacked explicit approval")
	}
	if _, err := UpgradeDevelopmentRelease(ctx, scope, pkg.ID, "local-linux", "outdated", true); err == nil {
		t.Fatal("stale target approval accepted")
	}
	if _, err := RollbackDevelopmentRelease(ctx, scope, pkg.ID, "local-linux", pkg.ID, true); err == nil {
		t.Fatal("P1 target had no previous version")
	}
}

func TestDevelopmentTargetAliasesUseTheInstalledContainerIdentity(t *testing.T) {
	pkg, _, _, _ := releasePackageFixture(t)
	scope := setupReleaseControl(t, pkg)
	target, err := developmentTargetDirectory(scope, "local-linux")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	canonical, err := filepath.EvalSymlinks(target)
	if err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "targets-alias")
	if err := os.Symlink(global.Config.Development.TargetDir, alias); err != nil {
		t.Fatal(err)
	}
	global.Config.Development.TargetDir = alias
	observed, err := developmentTargetDirectory(scope, "local-linux")
	if err != nil || observed != canonical || developmentContainerName(observed) != developmentContainerName(canonical) {
		t.Fatalf("aliased path changed the running container identity: %s %v", observed, err)
	}
}

func TestDevelopmentTargetRequiresBackupEvidenceAndValidState(t *testing.T) {
	pkg, _, _, _ := releasePackageFixture(t)
	scope := setupReleaseControl(t, pkg)
	target, err := developmentTargetDirectory(scope, "local-linux")
	if err != nil {
		t.Fatal(err)
	}
	shared := filepath.Join(target, "shared")
	if err := os.MkdirAll(shared, 0o700); err != nil {
		t.Fatal(err)
	}
	state := DevelopmentTargetStatus{TargetID: "local-linux", Status: "upgrade_failed", CurrentReleaseID: pkg.ID, PreviousReleaseID: "old", URL: "http://127.0.0.1:18080", BackupAvailable: true}
	if err := saveDevelopmentTarget(target, state); err != nil {
		t.Fatal(err)
	}
	got, err := GetDevelopmentTarget(context.Background(), scope, "local-linux")
	if err != nil || got.BackupAvailable {
		t.Fatalf("trusted flag without backup: %+v %v", got, err)
	}
	backup := filepath.Join(shared, "backups", "upgrade-old-new")
	if err := os.MkdirAll(backup, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backup, "database.db"), []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	journal := map[string]string{"backupId": "upgrade-old-new", "state": "applied"}
	if err := writeDevelopmentPrivateJSON(filepath.Join(shared, "config.yaml.upgrade-"+pkg.ID+".json"), journal); err != nil {
		t.Fatal(err)
	}
	got, err = GetDevelopmentTarget(context.Background(), scope, "local-linux")
	if err != nil || !got.BackupAvailable {
		t.Fatalf("missing actual backup: %+v %v", got, err)
	}
	if err := os.WriteFile(filepath.Join(shared, "target-version.json"), []byte(`{"status":"anything"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := GetDevelopmentTarget(context.Background(), scope, "local-linux"); err == nil {
		t.Fatal("invalid state became an empty target")
	}
}

func TestDevelopmentRollbackRetryReturnsVerifiedRestoredVersion(t *testing.T) {
	pkg, _, _, _ := releasePackageFixture(t)
	scope := setupReleaseControl(t, pkg)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ready" {
			fmt.Fprint(w, `{"status":"ready","releaseId":"restored-version"}`)
			return
		}
		fmt.Fprint(w, "<html>restored</html>")
	}))
	defer server.Close()
	target, _ := developmentTargetDirectory(scope, "local-linux")
	if err := os.MkdirAll(filepath.Join(target, "shared"), 0o700); err != nil {
		t.Fatal(err)
	}
	state := DevelopmentTargetStatus{TargetID: "local-linux", Status: "deployed", CurrentReleaseID: "restored-version", RestoredFromReleaseID: pkg.ID, URL: server.URL}
	if err := saveDevelopmentTarget(target, state); err != nil {
		t.Fatal(err)
	}
	got, err := RollbackDevelopmentRelease(context.Background(), scope, pkg.ID, "local-linux", pkg.ID, true)
	if err != nil || got.ReleaseID != "restored-version" || got.Credentials != nil {
		t.Fatalf("rollback retry: %+v %v", got, err)
	}
}

func TestDevelopmentUpgradeCommandCleansFailedRuntimeAndKeepsNetworkClosed(t *testing.T) {
	original := developmentDockerCommand
	t.Cleanup(func() { developmentDockerCommand = original })
	removed, failed := 0, false
	developmentDockerCommand = func(_ context.Context, _ string, args ...string) (string, error) {
		if args[0] == "rm" {
			removed++
			return "", nil
		}
		failed = true
		if !containsAdjacent(args, "--network", "none") || !containsAdjacent(args, "--from-manifest", "/previous/manifest.json") {
			t.Fatal("upgrade runtime command lost its fixed isolation or baseline")
		}
		return "", errors.New("injected migration failure")
	}
	if err := developmentVersionCommand(context.Background(), t.TempDir(), "owned-target", "/release-new", "/release-old", "/shared", "upgrade"); err == nil || !failed || removed != 2 {
		t.Fatalf("runtime failed without cleanup: %v removed=%d", err, removed)
	}
}

func containsAdjacent(args []string, key, value string) bool {
	for i := 1; i < len(args); i++ {
		if args[i-1] == key && args[i] == value {
			return true
		}
	}
	return false
}

func TestDevelopmentResumeRequiresCurrentDatabaseVerification(t *testing.T) {
	original := developmentDockerCommand
	t.Cleanup(func() { developmentDockerCommand = original })
	started := false
	developmentDockerCommand = func(_ context.Context, _ string, args ...string) (string, error) {
		if args[0] == "run" {
			if args[1] == "--detach" {
				started = true
			}
			return "", errors.New("database belongs to a different release")
		}
		return "", nil
	}
	if _, err := resumeDevelopmentVersion(t.TempDir(), "target", "/candidate", "/shared", "http://127.0.0.1:18080", "new-version"); err == nil || started {
		t.Fatal("unverified database was restarted")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ready" {
			fmt.Fprint(w, `{"status":"ready","releaseId":"new-version"}`)
			return
		}
		fmt.Fprint(w, "<html>verified</html>")
	}))
	defer server.Close()
	developmentDockerCommand = func(_ context.Context, _ string, args ...string) (string, error) {
		if args[0] == "port" {
			return strings.TrimPrefix(server.URL, "http://"), nil
		}
		if args[0] == "run" && args[1] == "--detach" {
			started = true
		}
		return "", nil
	}
	endpoint, err := resumeDevelopmentVersion(t.TempDir(), "target", "/candidate", "/shared", server.URL, "new-version")
	if err != nil || endpoint != server.URL || !started {
		t.Fatalf("verified version failed to resume: %q %v", endpoint, err)
	}
}
