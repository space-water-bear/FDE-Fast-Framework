package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"fde-admin/internal/config"
	"fde-admin/internal/global"
	"fde-admin/internal/model"
	"fde-admin/internal/utils"
	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func TestDevelopmentDockerIncrementalDelivery(t *testing.T) {
	if os.Getenv("FDE_RUN_DOCKER_INCREMENTAL_ACCEPTANCE") != "1" {
		t.Skip("set FDE_RUN_DOCKER_INCREMENTAL_ACCEPTANCE=1 for real isolated Linux delivery acceptance")
	}
	root, err := os.MkdirTemp("", "fde-p2-acceptance-")
	if err != nil {
		t.Fatal(err)
	}
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(sqlite.Open(filepath.Join(root, "control.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	global.DB = db
	global.Logger = zap.NewNop()
	if err := db.AutoMigrate(&model.User{}, &model.Role{}, &model.Menu{}, &model.AiProvider{}, &model.AiAgent{}, &model.AiSkill{}, &model.AiTool{}, &model.AiMcpServer{}, &model.AiSession{}, &model.AiMessage{}, &model.AiUsage{}, &model.DevProject{}, &model.DevRun{}, &model.DevRunEvent{}, &model.DevRelease{}, &model.DevDeployment{}); err != nil {
		t.Fatal(err)
	}
	global.Config = &config.Config{App: config.App{Mode: "debug"}, Codegen: config.Codegen{ServerDir: filepath.Join(repo, "server"), WebDir: filepath.Join(repo, "web")}, Development: config.Development{TargetArch: runtime.GOARCH, TemplateRepo: repo, TemplateRef: "HEAD", WorkspaceDir: filepath.Join(root, "workspaces"), ReleaseDir: filepath.Join(root, "releases"), TargetDir: filepath.Join(root, "targets")}}
	password, _ := utils.HashPassword("123456")
	adminRole := model.Role{Name: "管理员", Code: "admin", Enabled: true}
	if err := db.Create(&adminRole).Error; err != nil {
		t.Fatal(err)
	}
	admin := model.User{Username: "admin", Password: password, Enabled: true, Roles: []model.Role{adminRole}}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatal(err)
	}
	for _, menu := range []model.Menu{{Path: "system", Name: "System", Title: "系统管理", Type: 0, Icon: "Setting"}, {Path: "dashboard", Name: "Dashboard", Title: "首页", Component: "dashboard/index", Type: 1, Icon: "Odometer", Affix: true}} {
		if err := db.Create(&menu).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&adminRole).Association("Menus").Append(&menu); err != nil {
			t.Fatal(err)
		}
	}
	defaultProject := model.DevProject{Base: model.Base{ID: 1}, Key: "default", Name: "默认项目"}
	if err := db.Create(&defaultProject).Error; err != nil {
		t.Fatal(err)
	}
	provider := model.AiProvider{Name: "本地验收网关", Model: "mock-development", APIKey: "test-only-placeholder", Enabled: true, APIMode: "chat"}
	if err := db.Create(&provider).Error; err != nil {
		t.Fatal(err)
	}
	agent := model.AiAgent{Name: "验收开发智能体", ProviderID: provider.ID, Enabled: true}
	if err := db.Create(&agent).Error; err != nil {
		t.Fatal(err)
	}

	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Messages []map[string]any `json:"messages"`
		}
		if json.NewDecoder(r.Body).Decode(&input) != nil {
			http.Error(w, "bad input", 400)
			return
		}
		var lastTool map[string]any
		update := false
		repair := false
		for _, m := range input.Messages {
			if m["role"] == "tool" {
				lastTool = m
			}
			if content, ok := m["content"].(string); ok && strings.Contains(content, "P2_PRIORITY") {
				update = true
			}
			if content, ok := m["content"].(string); ok && strings.Contains(content, "宿主检查失败") {
				repair = true
			}
		}
		message := map[string]any{"role": "assistant", "content": "业务候选已生成，等待宿主检查。"}
		finish := "stop"
		call := func(id, name string, args any) {
			raw, _ := json.Marshal(args)
			message = map[string]any{"role": "assistant", "content": nil, "tool_calls": []any{map[string]any{"id": id, "type": "function", "function": map[string]any{"name": name, "arguments": string(raw)}}}}
			finish = "tool_calls"
		}
		if lastTool == nil && !repair {
			if update {
				call("update-priority", "generate_business_code", map[string]any{"objectKey": "biz_repair_ticket", "mode": "update", "baseSpecVersion": 1, "fields": []map[string]any{{"name": "priority", "title": "优先级", "type": "enum", "required": true, "inForm": true, "inList": true, "rules": map[string]any{"enum": []string{"normal", "urgent"}, "default": "normal"}}}})
			} else {
				call("create-repair", "generate_business_code", map[string]any{"tableName": "biz_repair_ticket", "business": "维修登记", "fields": []map[string]any{{"name": "ticket_no", "title": "登记号", "type": "string", "required": true, "inForm": true, "inList": true, "search": true}, {"name": "description", "title": "问题描述", "type": "text", "required": true, "inForm": true, "inList": true}, {"name": "status", "title": "状态", "type": "string", "required": true, "inForm": true, "inList": true}}})
			}
		} else if update {
			switch lastTool["tool_call_id"] {
			case "update-priority":
				call("read-service-update", "read_file", map[string]any{"path": "server/internal/service/repair_ticket.go"})
			case "read-service-update":
				var file map[string]any
				content, _ := lastTool["content"].(string)
				if json.Unmarshal([]byte(content), &file) != nil {
					t.Error("invalid update read result")
				}
				code, _ := file["content"].(string)
				rule := `
 if m.Priority == "urgent" && m.Status == "closed" { return errors.New("urgent ticket requires review") }
`
				for _, signature := range []string{"func CreateRepairTicket(m *model.RepairTicket) error {", "func UpdateRepairTicket(id uint, m *model.RepairTicket) error {"} {
					code = strings.Replace(code, signature, signature+rule, 1)
				}
				call("new-status-rule", "apply_patch", map[string]any{"changes": []map[string]any{{"path": "server/internal/service/repair_ticket.go", "expectedHash": file["sha256"], "content": code}}})
			}
		} else {
			switch lastTool["tool_call_id"] {
			case "create-repair":
				call("read-service", "read_file", map[string]any{"path": "server/internal/service/repair_ticket.go"})
			case "read-service":
				var file map[string]any
				content, _ := lastTool["content"].(string)
				if json.Unmarshal([]byte(content), &file) != nil {
					t.Error("invalid read result")
				}
				code, _ := file["content"].(string)
				code = strings.Replace(code, "import (", `import (
"errors"`, 1)
				code = strings.Replace(code, "func CreateRepairTicket(m *model.RepairTicket) error {", `func CreateRepairTicket(m *model.RepairTicket) error {
 if m.TicketNo == "BLOCKED" { return errors.New("blocked ticket") }`, 1)
				call("custom-rule", "apply_patch", map[string]any{"changes": []map[string]any{{"path": "server/internal/service/repair_ticket.go", "expectedHash": file["sha256"], "content": code}}})
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "p2", "object": "chat.completion", "created": 1, "model": "mock-development", "choices": []any{map[string]any{"index": 0, "message": message, "finish_reason": finish}}})
	}))
	defer gateway.Close()
	if err := db.Model(&provider).Update("base_url", gateway.URL+"/v1").Error; err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	project, err := CreateDevProject(ctx, admin.ID, "repair-p2", "维修增量验收")
	if err != nil {
		t.Fatal(err)
	}
	scope := EngineeringScope{ActorID: admin.ID, ProjectID: project.ID}
	t.Log("acceptance root:", root)
	first, err := CreateDevRun(ctx, scope, "构建维修登记，保留登记号、问题描述和状态，登记号BLOCKED应由自定义服务校验拒绝。", agent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if claimed, err := ProcessNextDevelopmentRun(ctx); err != nil || !claimed {
		t.Fatalf("first worker: %v %v", claimed, err)
	}
	first, _, err = GetDevRun(scope, first.Key)
	if err != nil || first.State != "succeeded" {
		t.Fatalf("V1 task: %+v %v", first, err)
	}
	release1, err := CreateDevelopmentRelease(ctx, scope, first.Key)
	if err != nil {
		t.Fatal(err)
	}
	deployed, err := DeployDevelopmentRelease(ctx, scope, release1.Key, "local-linux", true, false)
	if err != nil {
		t.Fatal(err)
	}
	token := acceptanceLogin(t, deployed.URL, deployed.Credentials["username"], deployed.Credentials["password"])
	var oldIDs []float64
	for i := 0; i < 20; i++ {
		result := acceptanceRequest(t, "POST", deployed.URL+"/api/repair_ticket", token, map[string]any{"ticketNo": fmt.Sprintf("P2-%02d", i), "description": fmt.Sprintf("历史记录 %02d", i), "status": "open"}, 200, 0)
		oldIDs = append(oldIDs, result["data"].(map[string]any)["id"].(float64))
	}
	acceptanceRequest(t, "POST", deployed.URL+"/api/repair_ticket", token, map[string]any{"ticketNo": "BLOCKED", "description": "校验", "status": "open"}, 200, 40001)
	sourceBefore, err := os.ReadFile(filepath.Join(first.WorkspacePath, "server/internal/service/repair_ticket.go"))
	if err != nil {
		t.Fatal(err)
	}
	promoted, err := PromoteDevRun(ctx, scope, first.Key, true)
	if err != nil {
		t.Fatal(err)
	}
	second, err := CreateDevRun(ctx, scope, "P2_PRIORITY 在已有维修登记上新增优先级normal/urgent，历史记录回填normal，保留自定义BLOCKED登记号校验，新增规则为urgent登记不能直接设置closed状态。", agent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if second.BaseCommit != promoted.CurrentCommit {
		t.Fatal("second task ignored promoted baseline")
	}
	if claimed, err := ProcessNextDevelopmentRun(ctx); err != nil || !claimed {
		t.Fatalf("second worker: %v %v", claimed, err)
	}
	second, _, err = GetDevRun(scope, second.Key)
	if err != nil || second.State != "succeeded" {
		t.Fatalf("V2 task: state=%s error=%s %v", second.State, second.Error, err)
	}
	sourceAfter, err := os.ReadFile(filepath.Join(second.WorkspacePath, "server/internal/service/repair_ticket.go"))
	if err != nil || !strings.Contains(string(sourceBefore), `m.TicketNo == "BLOCKED"`) || !strings.Contains(string(sourceAfter), `m.TicketNo == "BLOCKED"`) || !strings.Contains(string(sourceAfter), `m.Priority == "urgent"`) {
		t.Fatal("update overwrote custom business service", err)
	}
	release2, err := CreateDevelopmentRelease(ctx, scope, second.Key)
	if err != nil {
		t.Fatal(err)
	}
	upgraded, err := UpgradeDevelopmentRelease(ctx, scope, release2.Key, "local-linux", release1.Key, true)
	if err != nil {
		t.Fatal(err)
	}
	if upgraded.URL != deployed.URL || upgraded.Credentials != nil {
		t.Fatal("upgrade changed target or returned credentials")
	}
	list := acceptanceRequest(t, "GET", upgraded.URL+"/api/repair_ticket/list?pageSize=100", token, nil, 200, 0)
	rows := list["data"].(map[string]any)["list"].([]any)
	if len(rows) != 20 {
		t.Fatal("upgrade lost records")
	}
	for i, item := range rows {
		row := item.(map[string]any)
		if row["id"] != oldIDs[i] || row["priority"] != "normal" || row["description"] != fmt.Sprintf("历史记录 %02d", i) {
			t.Fatalf("changed legacy record %d", i)
		}
	}
	schema := acceptanceRequest(t, "GET", upgraded.URL+"/api/biz/schema/biz_repair_ticket", token, nil, 200, 0)
	priority := schema["data"].(map[string]any)["fields"].(map[string]any)["priority"].(map[string]any)
	if len(priority["enum"].([]any)) != 2 {
		t.Fatal("priority enum missing")
	}
	acceptanceRequest(t, "POST", upgraded.URL+"/api/repair_ticket", token, map[string]any{"ticketNo": "BLOCKED", "description": "custom", "status": "open", "priority": "normal"}, 200, 40001)
	acceptanceRequest(t, "POST", upgraded.URL+"/api/repair_ticket", token, map[string]any{"ticketNo": "INVALID", "description": "enum", "status": "open", "priority": "invalid"}, 200, 40001)
	acceptanceRequest(t, "POST", upgraded.URL+"/api/repair_ticket", token, map[string]any{"ticketNo": "STATE", "description": "status rule", "status": "closed", "priority": "urgent"}, 200, 40001)
	preview, err := DeployDevelopmentRelease(ctx, scope, release2.Key, "", true, true)
	if err != nil {
		t.Fatal("V2 fresh preview", err)
	}
	previewToken := acceptanceLogin(t, preview.URL, preview.Credentials["username"], preview.Credentials["password"])
	acceptanceRequest(t, "POST", preview.URL+"/api/repair_ticket", previewToken, map[string]any{"ticketNo": "URGENT", "description": "valid priority", "status": "open", "priority": "urgent"}, 200, 0)
	if _, err := UpgradeDevelopmentRelease(ctx, scope, release2.Key, "local-linux", release1.Key, true); err != nil {
		t.Fatal("upgrade retry", err)
	}
	targetState, err := GetDevelopmentTarget(ctx, scope, "local-linux")
	if err != nil || !targetState.BackupAvailable {
		t.Fatal("missing target backup evidence", err)
	}
	var deployedRow model.DevDeployment
	if err := db.Where("release_id = ? AND target_id = ?", release2.ID, "local-linux").First(&deployedRow).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := developmentDocker(ctx, root, "restart", deployedRow.ContainerID); err != nil {
		t.Fatal(err)
	}
	if err := verifyDevelopmentHTTP(ctx, upgraded.URL, release2.Key); err != nil {
		t.Fatal(err)
	}
	newRecord := acceptanceRequest(t, "POST", upgraded.URL+"/api/repair_ticket", token, map[string]any{"ticketNo": "POST-UPGRADE", "description": "回退保护验收记录", "status": "open", "priority": "urgent"}, 200, 0)
	newID := newRecord["data"].(map[string]any)["id"].(float64)
	if _, err := RollbackDevelopmentRelease(ctx, scope, release2.Key, "local-linux", release2.Key, true); err == nil {
		t.Fatal("rollback discarded new business data")
	}
	if err := verifyDevelopmentHTTP(ctx, upgraded.URL, release2.Key); err != nil {
		t.Fatal("rejected rollback did not restore the verified V2 service", err)
	}
	protected := acceptanceRequest(t, "GET", upgraded.URL+"/api/repair_ticket/list?pageSize=100", token, nil, 200, 0)
	if len(protected["data"].(map[string]any)["list"].([]any)) != 21 {
		t.Fatal("rollback protection lost a record")
	}
	// Remove only the explicitly created guard fixture before testing snapshot restoration.
	acceptanceRequest(t, "DELETE", fmt.Sprintf("%s/api/repair_ticket/%d", upgraded.URL, int(newID)), token, nil, 200, 0)
	rolledBack, err := RollbackDevelopmentRelease(ctx, scope, release2.Key, "local-linux", release2.Key, true)
	if err != nil {
		t.Fatal(err)
	}
	if rolledBack.ReleaseID != release1.Key || rolledBack.URL != deployed.URL {
		t.Fatal("rollback selected wrong version")
	}
	if _, err := RollbackDevelopmentRelease(ctx, scope, release2.Key, "local-linux", release2.Key, true); err != nil {
		t.Fatal("rollback retry", err)
	}
	restored := acceptanceRequest(t, "GET", rolledBack.URL+"/api/repair_ticket/list?pageSize=100", token, nil, 200, 0)
	restoredRows := restored["data"].(map[string]any)["list"].([]any)
	if len(restoredRows) != 20 {
		t.Fatal("rollback lost records")
	}
	for i, item := range restoredRows {
		row := item.(map[string]any)
		if row["id"] != oldIDs[i] || row["description"] != fmt.Sprintf("历史记录 %02d", i) {
			t.Fatalf("rollback changed record %d", i)
		}
	}
	acceptanceRequest(t, "POST", rolledBack.URL+"/api/repair_ticket", token, map[string]any{"ticketNo": "BLOCKED", "description": "custom", "status": "open"}, 200, 40001)
	evidence := map[string]any{"templateCommit": project.SourceCommit, "v1Release": release1.Key, "v2Release": release2.Key, "v1SourceHash": release1.SourceHash, "v2SourceHash": release2.SourceHash, "v1ArtifactHash": release1.ArtifactHash, "v2ArtifactHash": release2.ArtifactHash, "preservedRecords": 20, "customServicePreserved": true, "upgradeRetry": true, "rollbackRetry": true, "priorityBackfill": "normal", "model": "scripted gateway, real MAF worker", "container": deployedRow.ContainerID}
	data, _ := json.MarshalIndent(evidence, "", "  ")
	if err := os.WriteFile(filepath.Join(root, "acceptance.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Log("acceptance evidence:", filepath.Join(root, "acceptance.json"))
}
