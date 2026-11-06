package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"fde-admin/internal/config"
	"fde-admin/internal/delivery"
	"fde-admin/internal/global"
	"fde-admin/internal/model"
	"fde-admin/internal/runner"
	"gopkg.in/yaml.v3"
	"gorm.io/gorm"
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
	lookupErr := global.DB.Where("run_id = ?", run.ID).First(&existing).Error
	if lookupErr != nil && !errors.Is(lookupErr, gorm.ErrRecordNotFound) {
		return nil, lookupErr
	}
	if lookupErr == nil {
		if _, err := VerifiedDevelopmentRelease(scope, existing.Key); err != nil {
			return nil, err
		}
		return &existing, nil
	}
	root := global.Config.Development.ReleaseDir
	if root == "" {
		root = "./data/releases"
	}
	pkg, err := developmentPackageCandidate(ctx, *run, report, root)
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

// developmentDockerCommand is the fixed Docker adapter; tests replace it without executing a host shell.
var developmentDockerCommand = developmentDocker

func DeployDevelopmentRelease(ctx context.Context, scope EngineeringScope, key, targetID string, approve, preview bool) (result *DevelopmentDeploymentResult, retErr error) {
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
	target = filepath.Dir(installed.SharedDir)
	name := developmentContainerName(target)
	deployment := model.DevDeployment{ReleaseID: release.ID, TargetID: targetID, Status: "deploying"}
	if err := global.DB.Where("release_id = ? AND target_id = ?", release.ID, targetID).FirstOrCreate(&deployment).Error; err != nil {
		return nil, err
	}
	fail := func(cause error) (*DevelopmentDeploymentResult, error) {
		if updateErr := global.DB.Model(&deployment).Updates(map[string]any{"status": "failed", "error": cause.Error()}).Error; updateErr != nil {
			return nil, errors.Join(cause, updateErr)
		}
		return nil, cause
	}

	wasDeployed := deployment.Status == "deployed"
	operationCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	priorConfigHash, _ := runner.FileHash(filepath.Join(installed.SharedDir, "config.yaml"))
	priorAcceptance := developmentBusinessAcceptance{ProjectID: scope.ProjectID, ReleaseID: release.Key, ManifestHash: release.ArtifactHash, ConfigHash: priorConfigHash}
	if wasDeployed && deployment.URL != "" && deployment.ContainerID == name && priorConfigHash != "" && hasDevelopmentBusinessAcceptance(installed.SharedDir, priorAcceptance) {
		if err := verifyDevelopmentHTTP(operationCtx, deployment.URL, release.Key); err == nil {
			return &DevelopmentDeploymentResult{Status: "deployed", ReleaseID: release.Key, TargetID: targetID, URL: deployment.URL}, nil
		}
		if err := operationCtx.Err(); err != nil {
			return nil, err
		}
	}
	credentials, err := ensureDevelopmentRuntimeConfig(installed.SharedDir, scope.ProjectID, release.Key)
	if err != nil {
		return fail(err)
	}
	if err := global.DB.Model(&deployment).Updates(map[string]any{"status": "deploying", "error": ""}).Error; err != nil {
		return nil, err
	}
	cleanupNeeded, keepServe := false, false
	defer func() {
		if cleanupNeeded && !keepServe {
			if err := removeDevelopmentContainer(target, name); err != nil {
				retErr = errors.Join(retErr, fmt.Errorf("临时服务容器清理未确认: %w", err))
				_ = global.DB.Model(&deployment).Updates(map[string]any{"status": "failed", "error": retErr.Error()}).Error
			}
		}
	}()
	common := []string{"--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()), "--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--pids-limit", "256", "--memory", "512m", "--cpus", "1", "--tmpfs", "/tmp:rw,nosuid,size=64m", "--mount", "type=bind,src=" + installed.ReleasePath + ",dst=/release,readonly", "--mount", "type=bind,src=" + installed.SharedDir + ",dst=/shared"}
	for _, command := range []string{"bootstrap", "verify"} {
		container := name + "-" + command
		if err := removeDevelopmentContainer(target, container); err != nil {
			return fail(err)
		}
		args := append([]string{"run", "--rm", "--name", container, "--network", "none"}, common...)
		args = append(args, "alpine:3.22", "/release/server", command, "--config", "/shared/config.yaml", "--manifest", "/release/manifest.json")
		if command == "bootstrap" {
			args = append(args, "--assets", "/release/assets")
		}
		_, runErr := developmentDockerCommand(operationCtx, target, args...)
		cleanupErr := removeDevelopmentContainer(target, container)
		if runErr != nil || cleanupErr != nil {
			return fail(fmt.Errorf("%s未完成: %w", command, errors.Join(runErr, cleanupErr)))
		}
	}
	if err := removeDevelopmentContainer(target, name); err != nil {
		return fail(err)
	}
	args := append([]string{"run", "--detach", "--name", name, "-p", "127.0.0.1::8080"}, common...)
	args = append(args, "alpine:3.22", "/release/server", "serve", "--config", "/shared/config.yaml", "--manifest", "/release/manifest.json", "--web", "/release/web")
	cleanupNeeded = true // A canceled Docker client may have already started the named container.
	if _, err := developmentDockerCommand(operationCtx, target, args...); err != nil {
		return fail(err)
	}
	port, err := developmentDockerCommand(operationCtx, target, "port", name, "8080/tcp")
	if err != nil {
		return fail(err)
	}
	address := strings.TrimSpace(strings.Split(port, "\n")[0])
	host, portNumber, err := net.SplitHostPort(address)
	parsedPort, portErr := strconv.Atoi(portNumber)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() || portErr != nil || parsedPort < 1 || parsedPort > 65535 {
		return fail(errors.New("测试端口未限制到本机"))
	}
	endpoint := "http://" + address
	if err := verifyDevelopmentHTTP(operationCtx, endpoint, release.Key); err != nil {
		return fail(err)
	}
	checkpoint := developmentBusinessAcceptance{ProjectID: scope.ProjectID, ReleaseID: release.Key, ManifestHash: release.ArtifactHash}
	configHash, err := runner.FileHash(filepath.Join(installed.SharedDir, "config.yaml"))
	if err != nil {
		return fail(err)
	}
	checkpoint.ConfigHash = configHash
	if !hasDevelopmentBusinessAcceptance(installed.SharedDir, checkpoint) {
		if err := verifyDevelopmentBusinessHTTP(operationCtx, endpoint, installed.SharedDir, installed.ReleasePath); err != nil {
			return fail(err)
		}
		checkpoint.CheckedAt = time.Now().UTC()
		if err := writeDevelopmentPrivateJSON(filepath.Join(installed.SharedDir, "business-acceptance.json"), checkpoint); err != nil {
			return fail(err)
		}
	}
	if err := operationCtx.Err(); err != nil {
		return fail(err)
	}
	update := global.DB.Model(&deployment).Updates(map[string]any{"status": "deployed", "url": endpoint, "container_id": name, "error": ""})
	if update.Error != nil {
		return fail(update.Error)
	}
	if update.RowsAffected != 1 {
		return fail(errors.New("部署状态未保存"))
	}
	keepServe = true
	_ = global.DB.Create(&model.DevRunEvent{RunID: release.RunID, Kind: "deployment_verified", Message: "测试目标 " + targetID + " 已完成初始化、HTTP就绪与业务路由/活动Schema验收；版本 " + release.Key}).Error
	if wasDeployed {
		credentials = nil
	}
	return &DevelopmentDeploymentResult{Status: "deployed", ReleaseID: release.Key, TargetID: targetID, URL: endpoint, Credentials: credentials}, nil
}

func developmentContainerName(target string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(target)))
	return "fde-p1-target-" + hex.EncodeToString(sum[:])[:20]
}

func removeDevelopmentContainer(target, name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := developmentDockerCommand(ctx, target, "rm", "-f", name)
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "no such container") {
		return nil
	}
	return err
}

func developmentPackageCandidate(ctx context.Context, run model.DevRun, report runner.Report, root string) (delivery.Package, error) {
	hash, err := runner.SnapshotHash(run.WorkspacePath)
	if err != nil || hash != run.SourceHash {
		return delivery.Package{}, errors.New("源码已在检查后变化，请重新验证")
	}
	if report.ID == "" || filepath.Base(report.ID) != report.ID {
		return delivery.Package{}, errors.New("检查报告标识无效")
	}
	path := filepath.Join(root, report.ID)
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		return delivery.Pack(ctx, delivery.PackRequest{Workspace: run.WorkspacePath, CheckReportPath: report.ReportPath, OutputDir: root, SourceCommit: run.BaseCommit})
	} else if err != nil {
		return delivery.Package{}, err
	}
	pkg, err := delivery.Inspect(path)
	if err != nil {
		return delivery.Package{}, fmt.Errorf("已有候选目录未通过校验: %w", err)
	}
	checkHash, err := runner.FileHash(report.ReportPath)
	if err != nil {
		return delivery.Package{}, err
	}
	if pkg.ID != report.ID || pkg.Manifest.SourceHash != run.SourceHash || pkg.Manifest.ChecksHash != checkHash || pkg.Manifest.SourceCommit != run.BaseCommit {
		return delivery.Package{}, errors.New("已有候选不属于当前源码/检查报告")
	}
	return pkg, nil
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

type generatedDevelopmentConfig struct {
	ProjectID      uint   `json:"projectId"`
	ReleaseID      string `json:"releaseId"`
	ConfigHash     string `json:"configHash"`
	State          string `json:"state"`
	PreparedConfig []byte `json:"preparedConfig,omitempty"`
}

// The private marker proves which initial credential this workbench generated.
// A pre-existing or edited configuration is never treated as an owned secret.
func ensureDevelopmentRuntimeConfig(shared string, projectID uint, releaseID string) (map[string]string, error) {
	if projectID == 0 || releaseID == "" {
		return nil, errors.New("缺少自动配置所属项目/版本")
	}
	path := filepath.Join(shared, "config.yaml")
	markerPath := filepath.Join(shared, "config.generated.json")
	current, configErr := readDevelopmentPrivateFile(path)
	markerBytes, markerErr := readDevelopmentPrivateFile(markerPath)
	var marker generatedDevelopmentConfig
	if markerErr == nil {
		if json.Unmarshal(markerBytes, &marker) != nil || marker.ProjectID != projectID || marker.ReleaseID != releaseID {
			return nil, errors.New("自动配置标记不属于当前项目/版本")
		}
	} else if !os.IsNotExist(markerErr) {
		return nil, markerErr
	}
	if configErr == nil {
		if os.IsNotExist(markerErr) {
			return nil, nil
		}
		if sha256Hex(string(current)) != marker.ConfigHash {
			return nil, nil
		}
		cfg, err := developmentPrivateConfig(current)
		if err != nil {
			return nil, err
		}
		if marker.State == "prepared" {
			marker.State = "completed"
			marker.PreparedConfig = nil
			if err := writeDevelopmentPrivateJSON(markerPath, marker); err != nil {
				return nil, err
			}
		}
		if marker.State != "completed" {
			return nil, errors.New("自动配置来源状态无效")
		}
		return map[string]string{"username": cfg.Seed.AdminUsername, "password": cfg.Seed.AdminPassword}, nil
	}
	if !os.IsNotExist(configErr) {
		return nil, configErr
	}
	var data []byte
	if markerErr == nil {
		if marker.State != "prepared" || len(marker.PreparedConfig) == 0 || sha256Hex(string(marker.PreparedConfig)) != marker.ConfigHash {
			return nil, errors.New("已有配置被移除，不能自动覆盖恢复；请明确恢复私有配置")
		}
		data = marker.PreparedConfig
	} else {
		secret := func() (string, error) {
			v := make([]byte, 32)
			if _, err := rand.Read(v); err != nil {
				return "", err
			}
			return hex.EncodeToString(v), nil
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
		data, configErr = yaml.Marshal(cfg)
		if configErr != nil {
			return nil, configErr
		}
		marker = generatedDevelopmentConfig{ProjectID: projectID, ReleaseID: releaseID, ConfigHash: sha256Hex(string(data)), State: "prepared", PreparedConfig: data}
		raw, _ := json.MarshalIndent(marker, "", "  ")
		if err := writeDevelopmentPrivateExclusive(markerPath, raw); err != nil {
			return nil, err
		}
	}
	if err := writeDevelopmentPrivateExclusive(path, data); err != nil {
		return nil, err
	}
	marker.State = "completed"
	marker.PreparedConfig = nil
	if err := writeDevelopmentPrivateJSON(markerPath, marker); err != nil {
		return nil, err
	}
	cfg, err := developmentPrivateConfig(data)
	if err != nil {
		return nil, err
	}
	return map[string]string{"username": cfg.Seed.AdminUsername, "password": cfg.Seed.AdminPassword}, nil
}

func developmentPrivateConfig(data []byte) (config.Config, error) {
	var cfg config.Config
	if yaml.Unmarshal(data, &cfg) != nil || cfg.Seed.AdminUsername == "" || cfg.Seed.AdminPassword == "" {
		return cfg, errors.New("私有运行配置缺少管理员初始化凭据")
	}
	return cfg, nil
}
func readDevelopmentPrivateFile(path string) ([]byte, error) {
	st, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() || st.Size() > 256<<10 {
		return nil, errors.New("私有配置/标记必须是有限大小的普通文件")
	}
	return os.ReadFile(path)
}
func writeDevelopmentPrivateExclusive(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".private-")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Link(tmp, path)
}
func writeDevelopmentPrivateJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".private-")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

type developmentBusinessAcceptance struct {
	ProjectID    uint      `json:"projectId"`
	ReleaseID    string    `json:"releaseId"`
	ManifestHash string    `json:"manifestHash"`
	ConfigHash   string    `json:"configHash"`
	CheckedAt    time.Time `json:"checkedAt"`
}

func hasDevelopmentBusinessAcceptance(shared string, expected developmentBusinessAcceptance) bool {
	data, err := readDevelopmentPrivateFile(filepath.Join(shared, "business-acceptance.json"))
	if err != nil {
		return false
	}
	var actual developmentBusinessAcceptance
	return json.Unmarshal(data, &actual) == nil && actual.ProjectID == expected.ProjectID && actual.ReleaseID == expected.ReleaseID && actual.ManifestHash == expected.ManifestHash && actual.ConfigHash == expected.ConfigHash && !actual.CheckedAt.IsZero()
}

func developmentHTTPClient() *http.Client {
	return &http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("验收请求不允许重定向") }}
}
func verifyDevelopmentHTTP(ctx context.Context, endpoint, releaseID string) error {
	client := developmentHTTPClient()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		req, _ := http.NewRequestWithContext(ctx, "GET", endpoint+"/ready", nil)
		response, err := client.Do(req)
		if err == nil {
			var ready struct{ Status, ReleaseID string }
			decodeErr := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&ready)
			_ = response.Body.Close()
			if response.StatusCode == 200 && decodeErr == nil && ready.ReleaseID == releaseID && ready.Status == "ready" {
				req, _ := http.NewRequestWithContext(ctx, "GET", endpoint+"/", nil)
				response, err := client.Do(req)
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

type developmentEnvelope struct {
	Code *int            `json:"code"`
	Data json.RawMessage `json:"data"`
}

func developmentAPI(ctx context.Context, client *http.Client, method, endpoint, token string, body any) (json.RawMessage, error) {
	var input io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		input = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, input)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("x-token", token)
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	var envelope developmentEnvelope
	if response.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(&envelope) != nil || envelope.Code == nil || *envelope.Code != 0 {
		return nil, errors.New("业务接口未返回成功响应")
	}
	return envelope.Data, nil
}

// This acceptance is read-only for business records: it exercises real login,
// list handlers and the published schema for every frozen installation object.
func verifyDevelopmentBusinessHTTP(ctx context.Context, endpoint, shared, releasePath string) error {
	data, err := readDevelopmentPrivateFile(filepath.Join(shared, "config.yaml"))
	if err != nil {
		return err
	}
	cfg, err := developmentPrivateConfig(data)
	if err != nil {
		return err
	}
	if cfg.Captcha.Enabled {
		return errors.New("首次自动业务验收需要明确关闭验证码的私有测试配置")
	}
	raw, err := os.ReadFile(filepath.Join(releasePath, "assets/business.json"))
	if err != nil {
		return err
	}
	var assets BusinessInstallationAssets
	if json.Unmarshal(raw, &assets) != nil || len(assets.Objects) == 0 {
		return errors.New("缺少冻结业务验收资产")
	}
	client := developmentHTTPClient()
	login, err := developmentAPI(ctx, client, "POST", endpoint+"/api/auth/login", "", map[string]string{"username": cfg.Seed.AdminUsername, "password": cfg.Seed.AdminPassword})
	if err != nil {
		return fmt.Errorf("首次业务验收登录失败: %w", err)
	}
	var auth struct{ Token, RefreshToken string }
	if json.Unmarshal(login, &auth) != nil || auth.Token == "" {
		return errors.New("首次业务验收未获得有效登录令牌")
	}
	defer func() {
		logout, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_, _ = developmentAPI(logout, client, "POST", endpoint+"/api/auth/logout", auth.Token, map[string]string{"refreshToken": auth.RefreshToken})
	}()
	for _, asset := range assets.Objects {
		if asset.Menu.Path == "" || asset.ObjectKey == "" || strings.ContainsAny(asset.Menu.Path, "/?#\\") {
			return errors.New("业务资产路由定义无效")
		}
		list, err := developmentAPI(ctx, client, "GET", endpoint+"/api/"+asset.Menu.Path+"/list?page=1&pageSize=1", auth.Token, nil)
		if err != nil {
			return fmt.Errorf("业务列表路由 %s 未验收: %w", asset.ObjectKey, err)
		}
		var page struct {
			List  json.RawMessage `json:"list"`
			Total *int64          `json:"total"`
		}
		if json.Unmarshal(list, &page) != nil || page.List == nil || page.Total == nil || *page.Total < 0 {
			return fmt.Errorf("业务列表 %s 响应结构不完整", asset.ObjectKey)
		}
		var items []json.RawMessage
		if json.Unmarshal(page.List, &items) != nil {
			return fmt.Errorf("业务列表 %s 数据格式无效", asset.ObjectKey)
		}
		schema, err := developmentAPI(ctx, client, "GET", endpoint+"/api/biz/schema/"+url.PathEscape(asset.ObjectKey), auth.Token, nil)
		if err != nil {
			return fmt.Errorf("活动表单 %s 未验收: %w", asset.ObjectKey, err)
		}
		var actual map[string]any
		if json.Unmarshal(schema, &actual) != nil || actual["objectKey"] != asset.ObjectKey {
			return fmt.Errorf("活动表单 %s 对象键不一致", asset.ObjectKey)
		}
		expectedVersion, ok := developmentSchemaVersion(asset.Schema["version"])
		actualVersion, actualOK := developmentSchemaVersion(actual["version"])
		activeVersion, activeOK := developmentSchemaVersion(actual["activeVersion"])
		if !ok || !actualOK || !activeOK || expectedVersion != actualVersion || activeVersion != expectedVersion {
			return fmt.Errorf("活动表单 %s 版本不一致", asset.ObjectKey)
		}
	}
	return nil
}
func developmentSchemaVersion(v any) (int64, bool) {
	switch n := v.(type) {
	case int:
		return int64(n), n > 0
	case int64:
		return n, n > 0
	case float64:
		i := int64(n)
		return i, i > 0 && float64(i) == n
	case json.Number:
		i, err := n.Int64()
		return i, err == nil && i > 0
	}
	return 0, false
}
