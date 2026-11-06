package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"fde-admin/internal/config"
	"fde-admin/internal/delivery"
	"fde-admin/internal/global"
	"fde-admin/internal/model"
	"fde-admin/internal/runner"
	"gopkg.in/yaml.v3"
)

var developmentDeliveryLocks sync.Map

func CreateDevelopmentRelease(ctx context.Context, scope EngineeringScope, runKey string) (*model.DevRelease, error) {
	run, _, err := GetDevRun(scope, runKey)
	if err != nil {
		return nil, err
	}
	value, _ := developmentDeliveryLocks.LoadOrStore("pack:"+run.Key, &sync.Mutex{})
	lock := value.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()
	if run.State != "succeeded" || run.SourceHash == "" {
		return nil, errors.New("只有检查通过的任务可以构建发布包")
	}
	var report runner.Report
	if json.Unmarshal([]byte(run.CheckResult), &report) != nil || !report.Success || report.SnapshotHash != run.SourceHash {
		return nil, errors.New("任务缺少匹配的真实检查报告")
	}
	var existing model.DevRelease
	if global.DB.Where("run_id = ?", run.ID).First(&existing).Error == nil {
		if _, err := VerifiedDevelopmentRelease(scope, existing.Key); err != nil {
			return nil, err
		}
		return &existing, nil
	}
	root := global.Config.Development.ReleaseDir
	if root == "" {
		root = "./data/releases"
	}
	pkg, err := delivery.Pack(ctx, delivery.PackRequest{Workspace: run.WorkspacePath, CheckReportPath: report.ReportPath, OutputDir: root, SourceCommit: run.BaseCommit})
	if err != nil {
		return nil, err
	}
	hash, err := runner.FileHash(filepath.Join(pkg.Path, "manifest.json"))
	if err != nil {
		return nil, err
	}
	manifest, _ := json.Marshal(pkg.Manifest)
	release := &model.DevRelease{Key: pkg.ID, RunID: run.ID, ProjectID: run.ProjectID, ActorID: run.ActorID, SourceHash: run.SourceHash, ArtifactHash: hash, Status: "ready", PackagePath: pkg.Path, Manifest: string(manifest)}
	if err := global.DB.Create(release).Error; err != nil {
		return nil, err
	}
	_ = global.DB.Create(&model.DevRunEvent{RunID: run.ID, Kind: "release_ready", Message: "已验证同一制品，发布包 " + release.Key + "；尚未部署"}).Error
	return release, nil
}

func VerifiedDevelopmentRelease(scope EngineeringScope, key string) (*model.DevRelease, error) {
	if _, err := EngineeringProject(scope); err != nil {
		return nil, err
	}
	var release model.DevRelease
	if err := global.DB.Where("key = ? AND project_id = ? AND actor_id = ?", key, scope.ProjectID, scope.ActorID).First(&release).Error; err != nil {
		return nil, errors.New("发布包不存在或无权访问")
	}
	if release.Status != "ready" {
		return nil, errors.New("发布包未验证")
	}
	pkg, err := delivery.Inspect(release.PackagePath)
	if err != nil {
		return nil, err
	}
	hash, err := runner.FileHash(filepath.Join(release.PackagePath, "manifest.json"))
	if err != nil || hash != release.ArtifactHash || pkg.ID != release.Key || pkg.Manifest.SourceHash != release.SourceHash {
		return nil, errors.New("发布包与审批候选不一致")
	}
	return &release, nil
}

type DevelopmentDeploymentResult struct {
	Status      string            `json:"status"`
	ReleaseID   string            `json:"releaseId"`
	TargetID    string            `json:"targetId"`
	URL         string            `json:"url"`
	Credentials map[string]string `json:"credentials,omitempty"`
}

func DeployDevelopmentRelease(ctx context.Context, scope EngineeringScope, key, targetID string, approve, preview bool) (*DevelopmentDeploymentResult, error) {
	release, err := VerifiedDevelopmentRelease(scope, key)
	if err != nil {
		return nil, err
	}
	if !preview && (!approve || targetID != "local-linux") {
		return nil, errors.New("请选择固定测试目标并明确批准此发布包")
	}
	if preview {
		targetID = "preview-" + release.Key
	}
	value, _ := developmentDeliveryLocks.LoadOrStore(fmt.Sprintf("target:%d:%s", scope.ProjectID, targetID), &sync.Mutex{})
	lock := value.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	root := global.Config.Development.TargetDir
	if root == "" {
		root = "./data/test-targets"
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	target := filepath.Join(root, fmt.Sprint(scope.ProjectID), targetID)
	installed, err := delivery.InstallContext(ctx, release.PackagePath, target)
	if err != nil {
		return nil, err
	}
	credentials, err := ensureDevelopmentRuntimeConfig(installed.SharedDir, release.Key)
	if err != nil {
		return nil, err
	}
	containerName := "fde-p1-" + strings.ReplaceAll(release.Key, "-", "")
	if !preview {
		containerName = fmt.Sprintf("fde-p1-project-%d", scope.ProjectID)
	}
	deployment := model.DevDeployment{ReleaseID: release.ID, TargetID: targetID, Status: "deploying"}
	if err := global.DB.Where("release_id = ? AND target_id = ?", release.ID, targetID).FirstOrCreate(&deployment).Error; err != nil {
		return nil, err
	}
	_ = global.DB.Model(&deployment).Updates(map[string]any{"status": "deploying", "error": ""}).Error
	fail := func(cause error) (*DevelopmentDeploymentResult, error) {
		_ = global.DB.Model(&deployment).Updates(map[string]any{"status": "failed", "error": cause.Error()}).Error
		return nil, cause
	}
	operationCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	common := []string{"--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--pids-limit", "256", "--memory", "512m", "--cpus", "1", "--tmpfs", "/tmp:rw,nosuid,size=64m", "--mount", "type=bind,src=" + installed.ReleasePath + ",dst=/release,readonly", "--mount", "type=bind,src=" + installed.SharedDir + ",dst=/shared"}
	for _, command := range []string{"bootstrap", "verify"} {
		name := containerName + "-" + command
		_, _ = developmentDocker(context.Background(), target, "rm", "-f", name)
		args := append([]string{"run", "--rm", "--name", name, "--network", "none"}, common...)
		args = append(args, "alpine:3.22", "/release/server", command, "--config", "/shared/config.yaml", "--manifest", "/release/manifest.json")
		if command == "bootstrap" {
			args = append(args, "--assets", "/release/assets")
		}
		_, runErr := developmentDocker(operationCtx, target, args...)
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		_, _ = developmentDocker(cleanupCtx, target, "rm", "-f", name)
		cleanupCancel()
		if runErr != nil {
			return fail(fmt.Errorf("%s未完成: %w", command, runErr))
		}
	}
	_, _ = developmentDocker(operationCtx, target, "rm", "-f", containerName)
	args := append([]string{"run", "--detach", "--name", containerName, "-p", "127.0.0.1::8080"}, common...)
	args = append(args, "alpine:3.22", "/release/server", "serve", "--config", "/shared/config.yaml", "--manifest", "/release/manifest.json", "--web", "/release/web")
	if _, err := developmentDocker(operationCtx, target, args...); err != nil {
		return fail(err)
	}
	port, err := developmentDocker(operationCtx, target, "port", containerName, "8080/tcp")
	if err != nil {
		return fail(err)
	}
	address := strings.TrimSpace(strings.Split(port, "\n")[0])
	if !strings.HasPrefix(address, "127.0.0.1:") {
		return fail(errors.New("测试端口未限制到本机"))
	}
	url := "http://" + address
	if err := verifyDevelopmentHTTP(operationCtx, url, release.Key); err != nil {
		_, _ = developmentDocker(context.Background(), target, "rm", "-f", containerName)
		return fail(err)
	}
	if err := global.DB.Model(&deployment).Updates(map[string]any{"status": "deployed", "url": url, "container_id": containerName, "error": ""}).Error; err != nil {
		return fail(err)
	}
	_ = global.DB.Create(&model.DevRunEvent{RunID: release.RunID, Kind: "deployment_verified", Message: "测试目标 " + targetID + " 已完成安装、初始化、结构校验与HTTP就绪验收；版本 " + release.Key}).Error
	return &DevelopmentDeploymentResult{Status: "deployed", ReleaseID: release.Key, TargetID: targetID, URL: url, Credentials: credentials}, nil
}

func developmentDocker(ctx context.Context, target string, args ...string) (string, error) {
	name := "docker-" + fmt.Sprint(time.Now().UnixNano()) + ".log"
	result, err := (runner.OSExecutor{}).Run(ctx, runner.Command{Args: args, LogPath: filepath.Join(target, "operation-logs", name), MaxLogBytes: 128 << 10})
	if err != nil {
		return "", err
	}
	if result.ExitCode != 0 {
		return "", fmt.Errorf("Docker操作失败(exit=%d): %s", result.ExitCode, result.Output)
	}
	return result.Output, nil
}

func ensureDevelopmentRuntimeConfig(shared, releaseID string) (map[string]string, error) {
	path := filepath.Join(shared, "config.yaml")
	if _, err := os.Stat(path); err == nil {
		return nil, nil
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	secret := func() (string, error) {
		value := make([]byte, 32)
		if _, err := rand.Read(value); err != nil {
			return "", err
		}
		return hex.EncodeToString(value), nil
	}
	jwt, err := secret()
	if err != nil {
		return nil, err
	}
	password, err := secret()
	if err != nil {
		return nil, err
	}
	cfg := config.Config{App: config.App{Name: "客户业务预览", Port: 8080, Mode: "release"}, DB: config.DB{Driver: "sqlite", DSN: "/shared/data/app.db"}, JWT: config.JWT{Secret: jwt, Issuer: "fde-runtime", AccessExpiresHours: 2, RefreshExpiresHours: 168, UseMultipoint: true}, Seed: config.Seed{AdminUsername: "admin", AdminPassword: password}, Captcha: config.Captcha{Enabled: false, ExpireMinutes: 5}, Upload: config.Upload{Dir: "/shared/uploads", MaxSizeMB: 10}, Zap: config.Zap{Level: "info", Director: "/shared/log", LogInConsole: true}}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return nil, err
	}
	return map[string]string{"username": "admin", "password": password}, nil
}

func verifyDevelopmentHTTP(ctx context.Context, url, releaseID string) error {
	client := &http.Client{Timeout: 3 * time.Second}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		req, _ := http.NewRequestWithContext(ctx, "GET", url+"/ready", nil)
		response, err := client.Do(req)
		if err == nil {
			var ready struct{ Status, ReleaseID string }
			decodeErr := json.NewDecoder(response.Body).Decode(&ready)
			_ = response.Body.Close()
			if response.StatusCode == 200 && decodeErr == nil && ready.ReleaseID == releaseID && ready.Status == "ready" {
				response, err := client.Get(url + "/")
				if err == nil {
					_ = response.Body.Close()
					if response.StatusCode == 200 {
						return nil
					}
				}
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(300 * time.Millisecond):
		}
	}
	return errors.New("发布后版本/数据库结构/前端入口就绪验收失败")
}
