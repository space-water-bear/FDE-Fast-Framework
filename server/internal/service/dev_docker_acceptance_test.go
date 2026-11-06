package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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

// Explicit opt-in: exercises real Docker checks and Linux first installation.
// The model gateway is scripted; MAF dispatch, source generation, checks and deployment are real.
func TestDevelopmentDockerFirstDelivery(t *testing.T) {
	if os.Getenv("FDE_RUN_DOCKER_ACCEPTANCE") != "1" {
		t.Skip("set FDE_RUN_DOCKER_ACCEPTANCE=1 for real isolated Linux delivery acceptance")
	}
	root, err := os.MkdirTemp("", "fde-p1-acceptance-")
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
		var request struct {
			Messages []map[string]any `json:"messages"`
		}
		if json.NewDecoder(r.Body).Decode(&request) != nil {
			http.Error(w, "bad request", 400)
			return
		}
		hasTool, repair := false, false
		var lastTool map[string]any
		for _, message := range request.Messages {
			if message["role"] == "tool" {
				hasTool = true
				lastTool = message
			}
			if content, _ := message["content"].(string); strings.Contains(content, "宿主检查失败") {
				repair = true
			}
		}
		message := map[string]any{"role": "assistant", "content": "模块已生成，等待宿主验证。"}
		finish := "stop"
		if !hasTool && !repair {
			args := map[string]any{"tableName": "biz_repair_ticket", "business": "维修登记", "fields": []map[string]any{{"name": "ticket_no", "title": "登记号", "type": "string", "required": true, "inForm": true, "inList": true, "search": true, "rules": map[string]any{"maxLength": 64}}, {"name": "description", "title": "问题描述", "type": "text", "required": true, "inForm": true, "inList": true, "rules": map[string]any{"maxLength": 200}}, {"name": "status", "title": "处理状态", "type": "string", "required": true, "inForm": true, "inList": true}, {"name": "owner", "title": "负责人", "type": "string", "inForm": true, "inList": true}, {"name": "reported_at", "title": "登记时间", "type": "datetime", "required": true, "inForm": true, "inList": true}}}
			raw, _ := json.Marshal(args)
			message = map[string]any{"role": "assistant", "content": nil, "tool_calls": []any{map[string]any{"id": "create-repair", "type": "function", "function": map[string]any{"name": "generate_business_code", "arguments": string(raw)}}}}
			finish = "tool_calls"
		}

		if hasTool {
			callID, _ := lastTool["tool_call_id"].(string)
			nextName := ""
			var nextArgs any
			switch callID {
			case "create-repair":
				nextName = "read_file"
				nextArgs = map[string]any{"path": "server/internal/service/repair_ticket.go"}
			case "read-generated-service":
				content, _ := lastTool["content"].(string)
				var file map[string]any
				if json.Unmarshal([]byte(content), &file) != nil {
					t.Error("read_file returned malformed result")
				}
				code, _ := file["content"].(string)
				if repair {
					code = strings.ReplaceAll(code, "var injectedCompileFailure UndefinedAcceptanceType", "")
				} else {
					code += "\nvar injectedCompileFailure UndefinedAcceptanceType\n"
				}
				nextName = "apply_patch"
				nextArgs = map[string]any{"changes": []map[string]any{{"path": "server/internal/service/repair_ticket.go", "expectedHash": file["sha256"], "content": code}}}
			}
			if nextName != "" {
				arguments, _ := json.Marshal(nextArgs)
				id := "patch-generated-service"
				if nextName == "read_file" {
					id = "read-generated-service"
				}
				message = map[string]any{"role": "assistant", "content": nil, "tool_calls": []any{map[string]any{"id": id, "type": "function", "function": map[string]any{"name": nextName, "arguments": string(arguments)}}}}
				finish = "tool_calls"
			}
		} else if repair {
			arguments, _ := json.Marshal(map[string]any{"path": "server/internal/service/repair_ticket.go"})
			message = map[string]any{"role": "assistant", "content": nil, "tool_calls": []any{map[string]any{"id": "read-generated-service", "type": "function", "function": map[string]any{"name": "read_file", "arguments": string(arguments)}}}}
			finish = "tool_calls"
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "qa", "object": "chat.completion", "created": 1, "model": "mock-development", "choices": []any{map[string]any{"index": 0, "message": message, "finish_reason": finish}}})
	}))
	defer gateway.Close()
	if err := db.Model(&provider).Update("base_url", gateway.URL+"/v1").Error; err != nil {
		t.Fatal(err)
	}
	project, err := CreateDevProject(context.Background(), admin.ID, "repair-qa", "维修业务验收")
	if err != nil {
		t.Fatal(err)
	}
	scope := EngineeringScope{ActorID: admin.ID, ProjectID: project.ID}
	run, err := CreateDevRun(context.Background(), scope, "构建维修登记，包含登记号、问题描述、处理状态、负责人、登记时间，描述必填且不超过200字；发布后完成CRUD和权限验收。", agent.ID)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	t.Log("acceptance root:", root)
	if claimed, err := ProcessNextDevelopmentRun(ctx); err != nil || !claimed {
		t.Fatalf("worker failed: %v %v; evidence=%s", claimed, err, root)
	}
	finished, _, err := GetDevRun(scope, run.Key)
	if err != nil || finished.State != "succeeded" || finished.Attempts != 2 {
		t.Fatalf("task did not pass: state=%s attempts=%d error=%s (%v); evidence=%s", finished.State, finished.Attempts, finished.Error, err, root)
	}
	release, err := CreateDevelopmentRelease(ctx, scope, run.Key)
	if err != nil {
		t.Fatal(err)
	}
	deployed, err := DeployDevelopmentRelease(ctx, scope, release.Key, "local-linux", true, false)
	if err != nil {
		t.Fatal(err)
	}
	if deployed.Status != "deployed" || deployed.Credentials["password"] == "" {
		t.Fatal("missing actual deployment/first login")
	}
	token := acceptanceLogin(t, deployed.URL, deployed.Credentials["username"], deployed.Credentials["password"])
	request := map[string]any{"ticketNo": "QA-001", "description": "验收维修记录", "status": "open", "owner": "演示负责人", "reportedAt": time.Now().UTC().Format(time.RFC3339)}
	created := acceptanceRequest(t, "POST", deployed.URL+"/api/repair_ticket", token, request, 200, 0)
	id := created["data"].(map[string]any)["id"].(float64)
	request["description"] = "已修改维修记录"
	acceptanceRequest(t, "PUT", fmt.Sprintf("%s/api/repair_ticket/%d", deployed.URL, int(id)), token, request, 200, 0)
	request["description"] = ""
	acceptanceRequest(t, "POST", deployed.URL+"/api/repair_ticket", token, request, 200, 40001)
	roles := acceptanceRequest(t, "GET", deployed.URL+"/api/role/list", token, nil, 200, 0)
	var operatorID float64
	for _, value := range roles["data"].(map[string]any)["list"].([]any) {
		role := value.(map[string]any)
		if role["code"] == "operator" {
			operatorID = role["id"].(float64)
		}
	}
	if operatorID == 0 {
		t.Fatal("business role not installed")
	}
	operatorPassword := "qa-only-operator-password"
	acceptanceRequest(t, "POST", deployed.URL+"/api/user", token, map[string]any{"username": "qa_operator", "password": operatorPassword, "nickname": "演示操作员", "enabled": true, "roleIds": []float64{operatorID}}, 200, 0)
	filtered := acceptanceRequest(t, "GET", deployed.URL+"/api/repair_ticket/list?ticketNo=does-not-exist", token, nil, 200, 0)
	if len(filtered["data"].(map[string]any)["list"].([]any)) != 0 {
		t.Fatal("HTTP query names differ from frontend JSON field names")
	}
	operatorToken := acceptanceLogin(t, deployed.URL, "qa_operator", operatorPassword)
	acceptanceRequest(t, "GET", deployed.URL+"/api/repair_ticket/list", operatorToken, nil, 200, 0)
	acceptanceRequest(t, "GET", deployed.URL+"/api/biz/schema/biz_repair_ticket", operatorToken, nil, 200, 0)
	acceptanceRequest(t, "GET", deployed.URL+"/api/user/list", operatorToken, nil, 403, 40003)
	var deployment model.DevDeployment
	if err := db.Where("release_id = ? AND target_id = ?", release.ID, "local-linux").First(&deployment).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := developmentDocker(ctx, root, "restart", deployment.ContainerID); err != nil {
		t.Fatal(err)
	}
	if err := verifyDevelopmentHTTP(ctx, deployed.URL, release.Key); err != nil {
		t.Fatal(err)
	}
	list := acceptanceRequest(t, "GET", deployed.URL+"/api/repair_ticket/list", operatorToken, nil, 200, 0)
	items := list["data"].(map[string]any)["list"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["description"] != "已修改维修记录" {
		t.Fatal("restart did not preserve business data")
	}
	request["description"] = "待删除记录"
	second := acceptanceRequest(t, "POST", deployed.URL+"/api/repair_ticket", token, request, 200, 0)
	secondID := second["data"].(map[string]any)["id"].(float64)
	acceptanceRequest(t, "DELETE", fmt.Sprintf("%s/api/repair_ticket/%d", deployed.URL, int(secondID)), token, nil, 200, 0)
	evidence := map[string]any{"root": root, "projectId": project.ID, "runKey": run.Key, "releaseId": release.Key, "sourceHash": release.SourceHash, "artifactHash": release.ArtifactHash, "url": deployed.URL, "container": deployment.ContainerID, "checks": "real Docker Go/Node/TypeScript/build", "model": "local scripted gateway, real MAF dispatch", "business": "CRUD,required validation,operator RBAC,restart persistence passed"}
	raw, _ := json.MarshalIndent(evidence, "", "  ")
	if err := os.WriteFile(filepath.Join(root, "acceptance.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Log("acceptance evidence:", filepath.Join(root, "acceptance.json"))
}

func acceptanceLogin(t *testing.T, url, username, password string) string {
	t.Helper()
	response := acceptanceRequest(t, "POST", url+"/api/auth/login", "", map[string]any{"username": username, "password": password}, 200, 0)
	return response["data"].(map[string]any)["token"].(string)
}
func acceptanceRequest(t *testing.T, method, url, token string, payload any, status, code int) map[string]any {
	t.Helper()
	var body io.Reader
	if payload != nil {
		raw, _ := json.Marshal(payload)
		body = strings.NewReader(string(raw))
	}
	request, err := http.NewRequest(method, url, body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("x-token", token)
	}
	response, err := (&http.Client{Timeout: 15 * time.Second}).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var result map[string]any
	if json.NewDecoder(response.Body).Decode(&result) != nil {
		t.Fatalf("nonJSON response from %s", url)
	}
	if response.StatusCode != status || result["code"] != float64(code) {
		t.Fatalf("%s %s expected HTTP%d/code%d got HTTP%d/code%v msg=%v", method, url, status, code, response.StatusCode, result["code"], result["msg"])
	}
	return result
}
