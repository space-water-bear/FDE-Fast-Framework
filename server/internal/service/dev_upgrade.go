package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"fde-admin/internal/delivery"
	"fde-admin/internal/global"
	"fde-admin/internal/model"
	"fde-admin/internal/runner"
)

type DevelopmentTargetStatus struct {
	TargetID              string `json:"targetId"`
	Status                string `json:"status"`
	CurrentReleaseID      string `json:"currentReleaseId"`
	PreviousReleaseID     string `json:"previousReleaseId"`
	URL                   string `json:"url"`
	BackupAvailable       bool   `json:"backupAvailable"`
	OperationActive       bool   `json:"operationActive"`
	RestoredFromReleaseID string `json:"restoredFromReleaseId,omitempty"`
}

func developmentTargetDirectory(scope EngineeringScope, targetID string) (string, error) {
	if _, err := EngineeringProject(scope); err != nil {
		return "", err
	}
	if targetID != "local-linux" || global.Config == nil {
		return "", errors.New("请选择本机 Linux 测试目标")
	}
	root := global.Config.Development.TargetDir
	if root == "" {
		root = "./data/test-targets"
	}
	return filepath.Abs(filepath.Join(root, fmt.Sprint(scope.ProjectID), targetID))
}

func GetDevelopmentTarget(ctx context.Context, scope EngineeringScope, targetID string) (*DevelopmentTargetStatus, error) {
	target, err := developmentTargetDirectory(scope, targetID)
	if err != nil {
		return nil, err
	}
	state := &DevelopmentTargetStatus{TargetID: targetID, Status: "empty"}
	defer func() {
		value, _ := developmentDeliveryLocks.LoadOrStore(fmt.Sprintf("target:%d:%s", scope.ProjectID, targetID), &sync.Mutex{})
		lock := value.(*sync.Mutex)
		if lock.TryLock() {
			state.OperationActive = false
			lock.Unlock()
		} else {
			state.OperationActive = true
		}
	}()
	data, err := readDevelopmentPrivateFile(filepath.Join(target, "shared", "target-version.json"))
	if err == nil {
		if err := decodeDevelopmentTarget(data, state); err != nil || state.TargetID != targetID {
			return nil, errors.New("目标版本记录无效")
		}
		state.BackupAvailable = developmentUpgradeBackupAvailable(target, state.CurrentReleaseID)
		return state, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	// Existing P1 targets have a deployment row and no P2 target record.
	query := global.DB.WithContext(ctx).Table("sys_dev_deployments AS deployments").
		Select("releases.key AS current_release_id, deployments.url, deployments.status").
		Joins("JOIN sys_dev_releases AS releases ON releases.id = deployments.release_id").
		Where("releases.project_id = ? AND releases.actor_id = ? AND deployments.target_id = ? AND deployments.status = ?", scope.ProjectID, scope.ActorID, targetID, "deployed").
		Order("deployments.updated_at DESC, deployments.id DESC").Limit(1).Scan(state)
	if query.Error != nil {
		return nil, query.Error
	}
	return state, nil
}

func UpgradeDevelopmentRelease(ctx context.Context, scope EngineeringScope, key, targetID, expectedCurrentReleaseID string, approve bool) (*DevelopmentDeploymentResult, error) {
	return changeDevelopmentTarget(ctx, scope, key, targetID, expectedCurrentReleaseID, approve, false)
}

func RollbackDevelopmentRelease(ctx context.Context, scope EngineeringScope, key, targetID, expectedCurrentReleaseID string, approve bool) (*DevelopmentDeploymentResult, error) {
	return changeDevelopmentTarget(ctx, scope, key, targetID, expectedCurrentReleaseID, approve, true)
}

func changeDevelopmentTarget(ctx context.Context, scope EngineeringScope, key, targetID, expected string, approve, rollback bool) (*DevelopmentDeploymentResult, error) {
	if !approve || expected == "" {
		return nil, errors.New("请明确批准操作并确认目标当前版本")
	}
	candidate, err := VerifiedDevelopmentRelease(scope, key)
	if err != nil {
		return nil, err
	}
	target, err := developmentTargetDirectory(scope, targetID)
	if err != nil {
		return nil, err
	}
	value, _ := developmentDeliveryLocks.LoadOrStore(fmt.Sprintf("target:%d:%s", scope.ProjectID, targetID), &sync.Mutex{})
	lock := value.(*sync.Mutex)
	if !lock.TryLock() {
		return nil, errors.New("目标正在执行交付操作，请等待当前操作完成")
	}
	defer lock.Unlock()
	state, err := GetDevelopmentTarget(ctx, scope, targetID)
	if err != nil {
		return nil, err
	}
	if rollback && state.Status == "deployed" && state.RestoredFromReleaseID == key && expected == key {
		if err := verifyDevelopmentHTTP(ctx, state.URL, state.CurrentReleaseID); err != nil {
			return nil, err
		}
		return &DevelopmentDeploymentResult{Status: state.Status, ReleaseID: state.CurrentReleaseID, TargetID: targetID, URL: state.URL}, nil
	}
	if state.CurrentReleaseID == key && !rollback && state.Status == "deployed" && (expected == key || expected == state.PreviousReleaseID) {
		if err := verifyDevelopmentHTTP(ctx, state.URL, key); err != nil {
			return nil, err
		}
		return &DevelopmentDeploymentResult{Status: state.Status, ReleaseID: key, TargetID: targetID, URL: state.URL}, nil
	}
	resuming := !rollback && state.CurrentReleaseID == key && state.PreviousReleaseID != "" && (state.Status == "upgrading" || state.Status == "upgrade_failed")
	if (state.CurrentReleaseID != expected && !(resuming && state.PreviousReleaseID == expected)) || state.CurrentReleaseID == "" {
		return nil, errors.New("目标当前版本已变化，请重新读取并确认")
	}
	previousID := state.CurrentReleaseID
	if resuming {
		previousID = state.PreviousReleaseID
	}
	if rollback {
		if key != state.CurrentReleaseID || state.PreviousReleaseID == "" {
			return nil, errors.New("此目标没有匹配的上一版本可恢复")
		}
		previousID = state.PreviousReleaseID
	} else if !resuming && (state.Status != "deployed" || key == previousID) {
		return nil, errors.New("目标需要先完成当前操作或恢复")
	}
	previous, err := VerifiedDevelopmentRelease(scope, previousID)
	if err != nil {
		return nil, err
	}
	if !rollback {
		if err := requireDevelopmentUpgradeLineage(scope, *candidate, *previous); err != nil {
			return nil, err
		}
		if _, err := delivery.InstallUpgrade(ctx, previous.PackagePath, candidate.PackagePath, target); err != nil {
			return nil, err
		}
	}
	shared := filepath.Join(target, "shared")
	previousPath := filepath.Join(target, "releases", previous.Key)
	candidatePath := filepath.Join(target, "releases", candidate.Key)
	for _, release := range []*model.DevRelease{previous, candidate} {
		path := filepath.Join(target, "releases", release.Key)
		pkg, err := delivery.Inspect(path)
		hash, hashErr := runner.FileHash(filepath.Join(path, "manifest.json"))
		if err != nil || hashErr != nil || pkg.ID != release.Key || hash != release.ArtifactHash {
			return nil, errors.New("目标中的发布文件与已验证版本不一致")
		}
	}
	if _, err := readDevelopmentPrivateFile(filepath.Join(shared, "config.yaml")); err != nil {
		return nil, errors.New("目标缺少现有私有运行配置")
	}
	if !rollback {
		state.CurrentReleaseID, state.PreviousReleaseID = candidate.Key, previous.Key
		state.RestoredFromReleaseID = ""
	}
	state.Status = "upgrading"
	if rollback {
		state.Status = "rolling_back"
	}
	if err := saveDevelopmentTarget(target, *state); err != nil {
		return nil, err
	}
	operationCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	name := developmentContainerName(target)
	writerStopped := false
	fail := func(cause error) (*DevelopmentDeploymentResult, error) {
		state.Status = "upgrade_failed"
		if rollback {
			state.Status = "rollback_failed"
			if writerStopped {
				endpoint, recoveryErr := resumeDevelopmentVersion(target, name, candidatePath, shared, state.URL, candidate.Key)
				if recoveryErr == nil {
					state.Status, state.CurrentReleaseID, state.URL = "deployed", candidate.Key, endpoint
				} else {
					cause = errors.Join(cause, fmt.Errorf("当前版本恢复未通过验证: %w", recoveryErr))
				}
			}
		}
		failureKind := "upgrade_failed"
		if rollback {
			failureKind = "rollback_failed"
		}
		if err := saveDevelopmentTarget(target, *state); err != nil {
			cause = errors.Join(cause, err)
		}
		_ = appendDevelopmentEvent(candidate.RunID, failureKind, developmentSummary(map[string]any{"targetId": targetID, "targetStatus": state.Status, "error": cause.Error()}))
		return nil, cause
	}
	// Stop the sole writer before the runtime creates its database/upload backup.
	if err := removeDevelopmentContainer(target, name); err != nil {
		return fail(err)
	}
	writerStopped = true
	command := "upgrade"
	if rollback {
		command = "rollback"
	}
	if err := developmentVersionCommand(operationCtx, target, name, candidatePath, previousPath, shared, command); err != nil {
		return fail(err)
	}
	active := candidate
	activePath := candidatePath
	if rollback {
		active, activePath = previous, previousPath
	} else {
		state.BackupAvailable = true
	}
	if err := developmentVersionCommand(operationCtx, target, name, activePath, "", shared, "verify"); err != nil {
		return fail(err)
	}
	endpoint, err := startDevelopmentVersion(operationCtx, target, name, activePath, shared, state.URL)
	if err != nil {
		return fail(err)
	}
	if err := verifyDevelopmentHTTP(operationCtx, endpoint, active.Key); err != nil {
		_ = removeDevelopmentContainer(target, name)
		return fail(err)
	}
	if err := verifyDevelopmentBusinessHTTP(operationCtx, endpoint, shared, activePath); err != nil {
		_ = removeDevelopmentContainer(target, name)
		return fail(err)
	}
	if rollback {
		if err := delivery.RollbackInstall(operationCtx, previous.PackagePath, candidate.PackagePath, target); err != nil {
			return fail(err)
		}
	} else if err := delivery.ActivateUpgrade(operationCtx, previous.PackagePath, candidate.PackagePath, target); err != nil {
		return fail(err)
	}
	completed := *state
	completed.Status, completed.URL, completed.CurrentReleaseID = "deployed", endpoint, active.Key
	if rollback {
		completed.PreviousReleaseID, completed.BackupAvailable = "", false
		completed.RestoredFromReleaseID = candidate.Key
	}
	deployment := model.DevDeployment{ReleaseID: active.ID, TargetID: targetID}
	if err := global.DB.Where("release_id = ? AND target_id = ?", active.ID, targetID).FirstOrCreate(&deployment).Error; err != nil {
		return fail(err)
	}
	if err := global.DB.Model(&deployment).Updates(map[string]any{"status": "deployed", "url": endpoint, "container_id": name, "error": ""}).Error; err != nil {
		return fail(err)
	}
	if err := appendDevelopmentEvent(candidate.RunID, command+"_verified", developmentJSON(completed)); err != nil {
		return fail(err)
	}
	if err := saveDevelopmentTarget(target, completed); err != nil {
		return fail(err)
	}
	return &DevelopmentDeploymentResult{Status: completed.Status, ReleaseID: active.Key, TargetID: targetID, URL: endpoint}, nil
}

func requireDevelopmentUpgradeLineage(scope EngineeringScope, candidate, previous model.DevRelease) error {
	var run model.DevRun
	if err := global.DB.Where("id = ? AND project_id = ? AND actor_id = ?", candidate.RunID, scope.ProjectID, scope.ActorID).First(&run).Error; err != nil {
		return err
	}
	var prior model.DevRun
	if global.DB.First(&prior, previous.RunID).Error != nil || prior.SourceHash == "" {
		return errors.New("上一版本缺少可核对的源码基线")
	}
	if run.BaseCommit == "" || run.BaseCommit != prior.PromotedCommit {
		return errors.New("升级候选必须从已提升的目标源码基线创建")
	}
	return nil
}

func decodeDevelopmentTarget(data []byte, target *DevelopmentTargetStatus) error {
	if json.Unmarshal(data, target) != nil {
		return errors.New("目标状态格式错误")
	}
	switch target.Status {
	case "deployed", "upgrading", "upgrade_failed", "rolling_back", "rollback_failed":
		if target.CurrentReleaseID == "" || target.URL == "" {
			return errors.New("目标缺少版本或地址")
		}
	default:
		return errors.New("目标操作状态无效")
	}
	return nil
}

func saveDevelopmentTarget(target string, state DevelopmentTargetStatus) error {
	state.OperationActive = false
	return writeDevelopmentPrivateJSON(filepath.Join(target, "shared", "target-version.json"), state)
}

func developmentUpgradeBackupAvailable(target, releaseID string) bool {
	if releaseID == "" || filepath.Base(releaseID) != releaseID {
		return false
	}
	raw, err := readDevelopmentPrivateFile(filepath.Join(target, "shared", "config.yaml.upgrade-"+releaseID+".json"))
	var journal struct {
		BackupID string `json:"backupId"`
		State    string `json:"state"`
	}
	if err != nil || json.Unmarshal(raw, &journal) != nil || journal.State == "rolled_back" || journal.BackupID == "" || filepath.Base(journal.BackupID) != journal.BackupID {
		return false
	}
	st, err := os.Lstat(filepath.Join(target, "shared", "backups", journal.BackupID, "database.db"))
	return err == nil && st.Mode().IsRegular()
}

func developmentVersionCommand(ctx context.Context, target, name, release, previous, shared, command string) error {
	container := name + "-" + command
	if err := removeDevelopmentContainer(target, container); err != nil {
		return err
	}
	args := []string{"run", "--rm", "--name", container, "--network", "none"}
	args = append(args, developmentRuntimeMounts(release, shared)...)
	if previous != "" {
		args = append(args, "--mount", "type=bind,src="+previous+",dst=/previous,readonly")
	}
	args = append(args, "alpine:3.22", "/release/server", command, "--config", "/shared/config.yaml", "--manifest", "/release/manifest.json")
	if previous != "" {
		args = append(args, "--from-manifest", "/previous/manifest.json")
	}
	_, runErr := developmentDockerCommand(ctx, target, args...)
	cleanupErr := removeDevelopmentContainer(target, container)
	return errors.Join(runErr, cleanupErr)
}

func developmentRuntimeMounts(release, shared string) []string {
	return []string{"--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()), "--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--pids-limit", "256", "--memory", "512m", "--cpus", "1", "--tmpfs", "/tmp:rw,nosuid,size=64m", "--mount", "type=bind,src=" + release + ",dst=/release,readonly", "--mount", "type=bind,src=" + shared + ",dst=/shared"}
}

func startDevelopmentVersion(ctx context.Context, target, name, release, shared, previousURL string) (string, error) {
	port, err := developmentRuntimePort(previousURL)
	if err != nil {
		return "", err
	}
	args := append([]string{"run", "--detach", "--name", name, "-p", "127.0.0.1:" + port + ":8080"}, developmentRuntimeMounts(release, shared)...)
	args = append(args, "alpine:3.22", "/release/server", "serve", "--config", "/shared/config.yaml", "--manifest", "/release/manifest.json", "--web", "/release/web")
	if _, err := developmentDockerCommand(ctx, target, args...); err != nil {
		_ = removeDevelopmentContainer(target, name)
		return "", err
	}
	address, err := developmentDockerCommand(ctx, target, "port", name, "8080/tcp")
	if err != nil {
		_ = removeDevelopmentContainer(target, name)
		return "", err
	}
	address = strings.TrimSpace(strings.Split(address, "\n")[0])
	host, reportedPort, err := net.SplitHostPort(address)
	number, portErr := strconv.Atoi(reportedPort)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() || portErr != nil || number < 1 || number > 65535 || reportedPort != port {
		_ = removeDevelopmentContainer(target, name)
		return "", errors.New("运行端口与本机目标不一致")
	}
	return "http://" + address, nil
}

func resumeDevelopmentVersion(target, name, release, shared, previousURL, releaseID string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if err := developmentVersionCommand(ctx, target, name, release, "", shared, "verify"); err != nil {
		return "", err
	}
	if err := removeDevelopmentContainer(target, name); err != nil {
		return "", err
	}
	endpoint, err := startDevelopmentVersion(ctx, target, name, release, shared, previousURL)
	if err != nil {
		return "", err
	}
	if err := verifyDevelopmentHTTP(ctx, endpoint, releaseID); err != nil {
		_ = removeDevelopmentContainer(target, name)
		return "", err
	}
	return endpoint, nil
}
