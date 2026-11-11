package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fde-admin/internal/config"
	"fde-admin/internal/global"
	"fde-admin/internal/model"
	"fde-admin/internal/utils"
	"fmt"
	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestDevelopmentRealModelFirstDelivery(t *testing.T) {
	if os.Getenv("FDE_RUN_REAL_MODEL_ACCEPTANCE") != "1" {
		t.Skip("set FDE_RUN_REAL_MODEL_ACCEPTANCE=1 for real isolated Linux delivery acceptance")
	}
	root, err := os.MkdirTemp("", "fde-real-model-acceptance-")
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

	input, err := os.ReadFile(os.Getenv("FDE_REAL_MODEL_INPUT"))
	if err != nil {
		t.Fatal("read private provider input:", err)
	}
	var settings struct {
		Provider model.AiProvider `json:"provider"`
		Skills   []model.AiSkill  `json:"skills"`
	}
	if err := json.Unmarshal(input, &settings); err != nil {
		t.Fatal("invalid private provider input")
	}
	provider := settings.Provider
	provider.Base = model.Base{}
	upstream, err := url.Parse(provider.BaseURL)
	if err != nil {
		t.Fatal("invalid configured upstream")
	}
	secret := provider.APIKey
	var requestCount atomic.Int64
	proxy := httputil.NewSingleHostReverseProxy(upstream)
	director := proxy.Director
	proxy.Director = func(request *http.Request) {
		director(request)
		request.Host = upstream.Host
		requestCount.Add(1)
	}
	proxy.ModifyResponse = func(response *http.Response) error {
		if response.StatusCode >= 400 || (!strings.Contains(response.Header.Get("Content-Type"), "json") && !strings.Contains(response.Header.Get("Content-Type"), "event-stream")) {
			raw, _ := io.ReadAll(io.LimitReader(response.Body, 65536))
			response.Body.Close()
			response.Body = io.NopCloser(bytes.NewReader(raw))
			t.Log("upstream diagnostic", response.StatusCode, strings.ReplaceAll(string(raw), secret, "<redacted>"))
		}
		return nil
	}
	observer := httptest.NewServer(proxy)
	defer observer.Close()
	provider.BaseURL = observer.URL

	if provider.APIKey == "" || provider.Model == "" || !provider.Enabled {
		t.Fatal("an enabled configured real provider is required")
	}
	if err := db.Create(&provider).Error; err != nil {
		t.Fatal(err)
	}
	for i := range settings.Skills {
		settings.Skills[i].Base = model.Base{}
		if err := db.Create(&settings.Skills[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	agent := model.AiAgent{Name: "真实模型工程验收", ProviderID: provider.ID, Enabled: true,
		SystemPrompt: "本任务是标准模板CRUD。首先read_project_context，随后直接generate_business_code实现需求，框架和工具已由宿主提供。生成后返回，由宿主运行检查。收到宿主失败反馈后，只读报错对应的生成文件并按hash用apply_patch修复，然后返回。优先使用当前工具定义，不需要重新理解模板、安装器和路由注册的实现。",
		Skills:       settings.Skills}
	if err := db.Create(&agent).Error; err != nil {
		t.Fatal(err)
	}
	project, err := CreateDevProject(context.Background(), admin.ID, "repair-qa", "维修业务验收")
	if err != nil {
		t.Fatal(err)
	}
	scope := EngineeringScope{ActorID: admin.ID, ProjectID: project.ID}
	run, err := CreateDevRun(context.Background(), scope, "使用generate_business_code创建标准CRUD维修登记，tableName=biz_repair_ticket，structName=RepairTicket。字段为ticket_no(string，必填，maxLength=64，列表与查询)，description(text，必填，maxLength=200)，status(string，必填)，owner(string，可选)，reported_at(datetime，必填)。全部字段inForm=true、inList=true。生成后交给宿主检查，收到错误反馈时修复对应生成文件。", agent.ID)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	t.Log("acceptance root:", root)
	services := defaultDevelopmentRunServices()
	injected := false
	services.snapshot = func(ctx context.Context, current model.DevRun) (string, string, error) {
		if !injected {
			path := "server/internal/service/repair_ticket.go"
			code, err := readRootedFile(current.WorkspacePath, path)
			if err != nil {
				return "", "", err
			}
			injection := developmentChange{Path: path, ExpectedHash: sha256Hex(string(code)), Content: string(code) + "\nvar injectedRealModelCompileFailure UndefinedRealModelAcceptanceType\n"}
			if _, err := applyDevelopmentChanges(current, []developmentChange{injection}); err != nil {
				return "", "", err
			}
			injected = true
		}
		return DevelopmentRunSnapshot(ctx, current)
	}
	if claimed, err := processNextDevelopmentRun(ctx, services); err != nil || !claimed {
		t.Fatalf("worker failed: %v %v; evidence=%s", claimed, err, root)
	}
	finished, _, err := GetDevRun(scope, run.Key)
	if err != nil || finished.State != "succeeded" || finished.Attempts < 2 {
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
	client := &http.Client{Timeout: 5 * time.Second}
	for _, path := range []string{"/api/dev/projects", "/api/ai/bot/list", "/api/demo/charts/sales"} {
		probe, err := http.NewRequestWithContext(ctx, http.MethodGet, deployed.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		probe.Header.Set("x-token", token)
		response, err := client.Do(probe)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusNotFound {
			t.Fatalf("workbench endpoint %s exposed by release: HTTP %d", path, response.StatusCode)
		}
	}
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
	evidence := map[string]any{"root": root, "projectId": project.ID, "runKey": run.Key, "releaseId": release.Key, "sourceHash": release.SourceHash, "artifactHash": release.ArtifactHash, "url": deployed.URL, "container": deployment.ContainerID, "checks": "real Docker Go/Node/TypeScript/build", "model": provider.Model, "apiMode": provider.APIMode, "templateCommit": project.SourceCommit, "attempts": finished.Attempts, "injectedCompileFailure": injected, "business": "CRUD,required validation,operator RBAC,restart persistence passed"}
	var usage []model.AiUsage
	if err := db.Where("agent_id = ?", agent.ID).Find(&usage).Error; err != nil {
		t.Fatal(err)
	}
	inputTokens, outputTokens := int64(0), int64(0)
	for _, item := range usage {
		inputTokens += item.PromptTokens
		outputTokens += item.CompletionTokens
	}
	evidence["usage"] = map[string]any{"usageRecords": len(usage), "inputTokensReported": inputTokens, "outputTokensReported": outputTokens}
	var events []model.DevRunEvent
	if err := db.Where("run_id = ?", run.ID).Order("id").Find(&events).Error; err != nil {
		t.Fatal(err)
	}
	var toolNames []string
	for _, event := range events {
		if event.Kind == "tool_started" {
			var call map[string]any
			if json.Unmarshal([]byte(event.Message), &call) == nil {
				if name, ok := call["tool"].(string); ok {
					toolNames = append(toolNames, name)
				}
			}
		}
	}
	evidence["toolCalls"] = toolNames
	evidence["providerRequests"] = requestCount.Load()
	raw, _ := json.MarshalIndent(evidence, "", "  ")
	if err := os.WriteFile(filepath.Join(root, "acceptance.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Log("acceptance evidence:", filepath.Join(root, "acceptance.json"))
}
