package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"fde-admin/internal/config"
	"fde-admin/internal/global"
	"fde-admin/internal/model"
	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// Continue the retained real-model V1 fixture through a second real task and upgrade.
func TestDevelopmentRealModelIncrementalDelivery(t *testing.T) {
	if os.Getenv("FDE_RUN_REAL_MODEL_ACCEPTANCE") != "1" || os.Getenv("FDE_REAL_MODEL_CONTINUE_ROOT") == "" {
		t.Skip("requires explicit real-provider opt-in and a retained successful V1 fixture")
	}
	root := os.Getenv("FDE_REAL_MODEL_CONTINUE_ROOT")
	var firstEvidence struct {
		ProjectID uint   `json:"projectId"`
		RunKey    string `json:"runKey"`
		ReleaseID string `json:"releaseId"`
	}
	data, err := os.ReadFile(filepath.Join(root, "acceptance.json"))
	if err != nil || json.Unmarshal(data, &firstEvidence) != nil || firstEvidence.ProjectID == 0 || firstEvidence.RunKey == "" {
		t.Fatal("a successful real-model first-delivery fixture is required")
	}
	db, err := gorm.Open(sqlite.Open(filepath.Join(root, "control.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	global.DB, global.Logger = db, zap.NewNop()
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	global.Config = &config.Config{App: config.App{Mode: "debug"}, Codegen: config.Codegen{ServerDir: filepath.Join(repo, "server"), WebDir: filepath.Join(repo, "web")}, Development: config.Development{TemplateRepo: repo, TemplateRef: "HEAD", WorkspaceDir: filepath.Join(root, "workspaces"), ReleaseDir: filepath.Join(root, "releases"), TargetDir: filepath.Join(root, "targets"), TargetArch: runtime.GOARCH}}
	var project model.DevProject
	if err := db.First(&project, firstEvidence.ProjectID).Error; err != nil {
		t.Fatal(err)
	}
	scope := EngineeringScope{ActorID: project.OwnerID, ProjectID: project.ID}
	first, _, err := GetDevRun(scope, firstEvidence.RunKey)
	if err != nil || first.State != "succeeded" {
		t.Fatal("V1 task is unavailable", err)
	}
	var selected model.AiAgent
	if err := db.First(&selected, first.AgentID).Error; err != nil {
		t.Fatal(err)
	}
	var settings struct {
		Provider model.AiProvider `json:"provider"`
	}
	input, err := os.ReadFile(os.Getenv("FDE_REAL_MODEL_INPUT"))
	if err != nil || json.Unmarshal(input, &settings) != nil || settings.Provider.APIKey == "" {
		t.Fatal("private real provider input is required")
	}
	if err := db.Model(&model.AiProvider{}).Where("id = ?", selected.ProviderID).Updates(map[string]any{"base_url": settings.Provider.BaseURL, "api_key": settings.Provider.APIKey, "model": settings.Provider.Model, "api_mode": settings.Provider.APIMode}).Error; err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "targets", fmt.Sprint(project.ID), "local-linux")
	private, err := readDevelopmentPrivateFile(filepath.Join(target, "shared", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := developmentPrivateConfig(private)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	state, err := GetDevelopmentTarget(ctx, scope, "local-linux")
	if err != nil || state.CurrentReleaseID != firstEvidence.ReleaseID || state.Status != "deployed" {
		t.Fatal("fixture is not on the original V1", err)
	}
	token := acceptanceLogin(t, state.URL, cfg.Seed.AdminUsername, cfg.Seed.AdminPassword)
	initial := acceptanceRequest(t, "GET", state.URL+"/api/repair_ticket/list?pageSize=100", token, nil, 200, 0)
	initialCount := len(initial["data"].(map[string]any)["list"].([]any))
	for i := initialCount; i < 20; i++ {
		acceptanceRequest(t, "POST", state.URL+"/api/repair_ticket", token, map[string]any{"ticketNo": fmt.Sprintf("REAL-P2-%02d", i), "description": fmt.Sprintf("真实模型历史工单 %02d", i), "status": "open", "owner": "演示负责人", "reportedAt": time.Now().UTC().Format(time.RFC3339)}, 200, 0)
	}
	before := acceptanceRequest(t, "GET", state.URL+"/api/repair_ticket/list?pageSize=100", token, nil, 200, 0)
	oldRecords := before["data"].(map[string]any)["list"].([]any)
	if len(oldRecords) != 20 {
		t.Fatal("fixture must contain exactly 20 records")
	}
	if _, err := PromoteDevRun(ctx, scope, first.Key, true); err != nil {
		t.Fatal(err)
	}
	requirement := `在已有objectKey=biz_repair_ticket上执行第二次开发。先读取项目上下文，使用generate_business_code的mode=update、baseSpecVersion=1，fields仅新增priority，title=优先级，type=enum，required/inForm/inList=true，rules.enum=[normal,urgent]，rules.default=normal。旧字段和数据保持原语义。随后读取server/internal/service/repair_ticket.go，在CreateRepairTicket和UpdateRepairTicket中加入业务规则：m.Priority等于urgent且m.Status等于closed时返回error，其他情况保留原逻辑。按工具返回的真实SHA256用apply_patch修改。实现后返回交给宿主检查，收到失败反馈时按实际诊断修复。`
	run, err := CreateDevRun(ctx, scope, requirement, selected.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Log("real incremental fixture:", root, "run:", run.Key)
	if claimed, err := ProcessNextDevelopmentRun(ctx); err != nil || !claimed {
		t.Fatalf("worker: %v %v", claimed, err)
	}
	finished, _, err := GetDevRun(scope, run.Key)
	if err != nil || finished.State != "succeeded" {
		t.Fatalf("real incremental task state=%s error=%s %v", finished.State, finished.Error, err)
	}
	release, err := CreateDevelopmentRelease(ctx, scope, run.Key)
	if err != nil {
		t.Fatal(err)
	}
	upgraded, err := UpgradeDevelopmentRelease(ctx, scope, release.Key, "local-linux", firstEvidence.ReleaseID, true)
	if err != nil {
		t.Fatal(err)
	}
	if upgraded.URL != state.URL || upgraded.Credentials != nil {
		t.Fatal("upgrade changed the target or credentials")
	}
	after := acceptanceRequest(t, "GET", state.URL+"/api/repair_ticket/list?pageSize=100", token, nil, 200, 0)
	newRecords := after["data"].(map[string]any)["list"].([]any)
	if len(newRecords) != len(oldRecords) {
		t.Fatal("upgrade lost records")
	}
	for i, item := range newRecords {
		row := item.(map[string]any)
		if row["priority"] != "normal" {
			t.Fatal("historical priority was not backfilled")
		}
		for name, value := range oldRecords[i].(map[string]any) {
			if row[name] != value {
				t.Fatalf("upgrade changed historical field %s", name)
			}
		}
	}
	acceptanceRequest(t, "POST", state.URL+"/api/repair_ticket", token, map[string]any{"ticketNo": "REAL-STATE", "description": "urgent guard", "status": "closed", "owner": "演示负责人", "reportedAt": time.Now().UTC().Format(time.RFC3339), "priority": "urgent"}, 200, 40001)
	acceptanceRequest(t, "POST", state.URL+"/api/repair_ticket", token, map[string]any{"ticketNo": "REAL-ENUM", "description": "enum guard", "status": "open", "reportedAt": time.Now().UTC().Format(time.RFC3339), "priority": "invalid"}, 200, 40001)
	if _, err := UpgradeDevelopmentRelease(ctx, scope, release.Key, "local-linux", firstEvidence.ReleaseID, true); err != nil {
		t.Fatal("upgrade retry", err)
	}
	var deployment model.DevDeployment
	if err := db.Where("release_id = ? AND target_id = ?", release.ID, "local-linux").First(&deployment).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := developmentDocker(ctx, root, "restart", deployment.ContainerID); err != nil {
		t.Fatal(err)
	}
	if err := verifyDevelopmentHTTP(ctx, state.URL, release.Key); err != nil {
		t.Fatal(err)
	}
	restored, err := RollbackDevelopmentRelease(ctx, scope, release.Key, "local-linux", release.Key, true)
	if err != nil {
		t.Fatal(err)
	}
	if restored.ReleaseID != firstEvidence.ReleaseID {
		t.Fatal("rollback restored another version")
	}
	final := acceptanceRequest(t, "GET", state.URL+"/api/repair_ticket/list?pageSize=100", token, nil, 200, 0)
	oldJSON, _ := json.Marshal(oldRecords)
	finalJSON, _ := json.Marshal(final["data"].(map[string]any)["list"])
	if string(oldJSON) != string(finalJSON) {
		t.Fatal("rollback changed historical business values")
	}
	evidence := map[string]any{"model": settings.Provider.Model, "v1Release": firstEvidence.ReleaseID, "v2Release": release.Key, "v2SourceHash": release.SourceHash, "v2ManifestHash": release.ArtifactHash, "projectTemplateCommit": project.SourceCommit, "runKey": run.Key, "attempts": finished.Attempts, "preservedRecords": 20, "priorityBackfill": "normal", "newStatusRule": "urgent cannot close", "upgradeRetry": true, "restart": true, "rollback": true, "container": deployment.ContainerID}
	raw, _ := json.MarshalIndent(evidence, "", "  ")
	if err := os.WriteFile(filepath.Join(root, "incremental-acceptance.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Log("real incremental evidence:", filepath.Join(root, "incremental-acceptance.json"))
}
