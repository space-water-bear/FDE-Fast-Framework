package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/format"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"fde-admin/internal/codegen"
	"fde-admin/internal/global"
	"fde-admin/internal/model"
	"fde-admin/internal/runner"
	"github.com/microsoft/agent-framework-go/tool"
)

var runWorkspaceLocks sync.Map
var developmentVerifiedChecks sync.Map

type developmentTool struct {
	name, description string
	schema            map[string]any
	run               model.DevRun
	call              func(context.Context, model.DevRun, string) (any, error)
}

func (t *developmentTool) Name() string        { return t.name }
func (t *developmentTool) Description() string { return t.description }
func (t *developmentTool) Schema() any         { return t.schema }
func (t *developmentTool) ReturnSchema() any   { return map[string]any{"type": "object"} }
func (t *developmentTool) Call(ctx context.Context, args string) (any, error) {
	scope, err := RequireDevelopmentRunScope(ctx)
	if err != nil {
		return nil, err
	}
	if scope.RunID != t.run.ID || scope.ActorID != t.run.ActorID || scope.ProjectID != t.run.ProjectID || scope.WorkspacePath != t.run.WorkspacePath {
		return nil, errors.New("开发任务上下文不匹配")
	}
	if _, err := EngineeringProject(EngineeringScope{ActorID: scope.ActorID, ProjectID: scope.ProjectID}); err != nil {
		return nil, err
	}
	var current model.DevRun
	if err := global.DB.First(&current, t.run.ID).Error; err != nil {
		return nil, err
	}
	if current.State != "running" || current.CancelRequested {
		return nil, errors.New("开发任务已停止")
	}
	value, _ := runWorkspaceLocks.LoadOrStore(t.run.ID, &sync.Mutex{})
	lock := value.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return t.call(ctx, current, args)
}

func developmentSchema(properties map[string]any, required ...string) map[string]any {
	if required == nil {
		required = []string{}
	}
	return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
}
func stringProperty(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

func DevelopmentAgentTools(run model.DevRun) []tool.Tool {
	definitions := []*developmentTool{
		{name: "read_project_context", description: "读取本任务需求、项目基线、源码文件清单；需求为本次验收依据。", schema: developmentSchema(map[string]any{}), call: readDevelopmentContext},
		{name: "read_file", description: "读取工作区源码片段与文件SHA256；不能读取配置凭证或运行数据。", schema: developmentSchema(map[string]any{"path": stringProperty("工作区相对文件路径"), "startLine": map[string]any{"type": "integer"}, "endLine": map[string]any{"type": "integer"}}, "path"), call: readDevelopmentFile},
		{name: "search_code", description: "在工作区源码中查找文本，返回有限行与路径。", schema: developmentSchema(map[string]any{"query": stringProperty("需要搜索的字面文本")}, "query"), call: searchDevelopmentCode},
		{name: "generate_business_code", description: "在本次已授权隔离任务工作区创建CRUD、表单Schema和安装资产；不会写工作台源码或运行数据库。新模块直接生成，字段类型string/text/int/float/bool/datetime/enum。增量mode=update必须给objectKey、baseSpecVersion，fields仅为新增字段，枚举给rules.enum/default。", schema: developmentSchema(map[string]any{"objectKey": stringProperty("可选：当前项目已保存的对象键"), "tableName": stringProperty("新业务表名，如biz_repair_ticket"), "mode": map[string]any{"type": "string", "enum": []string{"create", "update"}}, "baseSpecVersion": map[string]any{"type": "integer", "minimum": 1}, "business": stringProperty("业务名称"), "structName": stringProperty("可选Go类型名称"), "fields": map[string]any{"type": "array", "items": map[string]any{"type": "object", "properties": map[string]any{"name": stringProperty("snake_case列名"), "title": stringProperty("字段标题"), "type": map[string]any{"type": "string", "enum": []string{"string", "text", "int", "float", "bool", "datetime", "enum"}}, "required": map[string]any{"type": "boolean"}, "search": map[string]any{"type": "boolean"}, "inList": map[string]any{"type": "boolean"}, "inForm": map[string]any{"type": "boolean"}, "rules": map[string]any{"type": "object"}}, "required": []string{"name", "title", "type", "inForm"}}}}), call: generateDevelopmentBusiness},
		{name: "apply_patch", description: "按完整旧文件hash原子更新源码；新文件expectedHash=missing。禁止修改已有验收测试、配置/锁文件或运行数据。changes为path/expectedHash/content数组，修复时先read_file。", schema: developmentSchema(map[string]any{"changes": map[string]any{"type": "array", "items": map[string]any{"type": "object", "properties": map[string]any{"path": stringProperty("源码相对路径"), "expectedHash": stringProperty("read_file返回hash；新文件missing"), "content": stringProperty("文件完整新内容")}, "required": []string{"path", "expectedHash", "content"}}}}, "changes"), call: patchDevelopmentFiles},
		{name: "run_checks", description: "执行固定Go测试/编译、前端类型/构建和Linux制品检查。返回真实步骤结果，失败后只修源码；成功仍由host复核。", schema: developmentSchema(map[string]any{}), call: func(ctx context.Context, run model.DevRun, _ string) (any, error) {
			return RunDevelopmentChecks(ctx, run)
		}},
	}
	tools := make([]tool.Tool, 0, len(definitions))
	for _, definition := range definitions {
		definition.run = run
		tools = append(tools, definition)
	}
	return tools
}

func developmentFiles(root string) ([]string, error) {
	var result []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, _ := filepath.Rel(root, path)
		relative = filepath.ToSlash(relative)
		if entry.IsDir() {
			if relative != "." && (entry.Name() == ".git" || entry.Name() == "node_modules" || entry.Name() == "dist" || entry.Name() == "data" || entry.Name() == "uploads" || entry.Name() == "log" || entry.Name() == "bin") {
				return filepath.SkipDir
			}
			return nil
		}
		if allowedDevelopmentPath(relative, false) == nil && entry.Type().IsRegular() {
			result = append(result, relative)
		}
		return nil
	})
	return result, err
}

func allowedDevelopmentPath(path string, write bool) error {
	if !fs.ValidPath(path) || strings.Contains(path, "\\") || strings.HasPrefix(path, ".") || strings.Contains(path, "/.git") || strings.Contains(path, "/.env") {
		return errors.New("不是允许的源码路径")
	}
	allowed := strings.HasPrefix(path, "server/internal/") || strings.HasPrefix(path, "web/src/") || strings.HasPrefix(path, "fde/spec/") || strings.HasPrefix(path, "fde/assets/") || (!write && (path == "README.md" || strings.HasPrefix(path, "docs/") || path == "server/go.mod" || path == "web/package.json" || path == "server/main.go" || strings.HasPrefix(path, "fde/requirements/")))
	if write {
		allowed = strings.HasPrefix(path, "server/internal/model/") || strings.HasPrefix(path, "server/internal/service/") || strings.HasPrefix(path, "server/internal/api/") || strings.HasPrefix(path, "server/internal/router/") || strings.HasPrefix(path, "web/src/api/") || strings.HasPrefix(path, "web/src/views/generated/") || strings.HasPrefix(path, "web/src/components/business/") || strings.HasPrefix(path, "fde/spec/") || strings.HasPrefix(path, "fde/assets/")
	}
	ext := filepath.Ext(path)
	if !allowed || (ext != ".go" && ext != ".ts" && ext != ".vue" && ext != ".css" && ext != ".json" && ext != ".md" && path != "server/go.mod") {
		return errors.New("任务仅能访问源码和业务规格文件")
	}
	return nil
}

func readDevelopmentContext(_ context.Context, run model.DevRun, _ string) (any, error) {
	files, err := developmentFiles(run.WorkspacePath)
	if len(files) > 300 {
		files = files[:300]
	}
	return map[string]any{"requirement": run.Requirement, "projectId": run.ProjectID, "baseCommit": run.BaseCommit, "files": files, "conventions": "基本字段新模块读本上下文后直接generate_business_code，不必通读框架/模板/安装器。只有自定义规则或失败修复才按需读业务文件。增量先读fde/assets/business.json，用mode=update和baseSpecVersion追加字段，保留自定义服务/页面。最终run_checks必须成功，不能删除或降低已有验收测试。"}, err
}

func readDevelopmentFile(_ context.Context, run model.DevRun, args string) (any, error) {
	var in struct {
		Path      string `json:"path"`
		StartLine int    `json:"startLine"`
		EndLine   int    `json:"endLine"`
	}
	if json.Unmarshal([]byte(args), &in) != nil {
		return nil, errors.New("参数错误")
	}
	if err := allowedDevelopmentPath(in.Path, false); err != nil {
		return nil, err
	}
	data, err := readRootedFile(run.WorkspacePath, in.Path)
	if err != nil {
		return nil, err
	}
	if len(data) > 2<<20 {
		return nil, errors.New("源码文件过大")
	}
	lines := strings.Split(string(data), "\n")
	start := in.StartLine
	if start < 1 {
		start = 1
	}
	end := in.EndLine
	if end < start || end > start+299 {
		end = start + 299
	}
	if end > len(lines) {
		end = len(lines)
	}
	if start > len(lines) {
		start = len(lines) + 1
	}
	return map[string]any{"path": in.Path, "sha256": sha256Hex(string(data)), "startLine": start, "endLine": end, "content": strings.Join(lines[start-1:end], "\n")}, nil
}

func searchDevelopmentCode(_ context.Context, run model.DevRun, args string) (any, error) {
	var in struct {
		Query string `json:"query"`
	}
	if json.Unmarshal([]byte(args), &in) != nil || in.Query == "" || len(in.Query) > 200 {
		return nil, errors.New("查询文本无效")
	}
	files, err := developmentFiles(run.WorkspacePath)
	if err != nil {
		return nil, err
	}
	matches := []map[string]any{}
	for _, path := range files {
		data, err := readRootedFile(run.WorkspacePath, path)
		if err != nil || len(data) > 1<<20 {
			continue
		}
		for i, line := range strings.Split(string(data), "\n") {
			if strings.Contains(strings.ToLower(line), strings.ToLower(in.Query)) {
				if len(line) > 500 {
					line = line[:500]
				}
				matches = append(matches, map[string]any{"path": path, "line": i + 1, "text": line})
				if len(matches) >= 60 {
					return map[string]any{"matches": matches, "truncated": true}, nil
				}
			}
		}
	}
	return map[string]any{"matches": matches}, nil
}

type developmentChange struct {
	Path         string `json:"path"`
	ExpectedHash string `json:"expectedHash"`
	Content      string `json:"content"`
}

func applyDevelopmentChanges(run model.DevRun, changes []developmentChange) (any, error) {
	return applyDevelopmentChangesWithOwnership(run, changes, nil)
}

func applyDevelopmentChangesWithOwnership(run model.DevRun, changes []developmentChange, generated map[string]developmentFileOwnership) (any, error) {
	manifest, err := readDevelopmentOwnership(run.WorkspacePath)
	if err != nil {
		return nil, err
	}
	if err := validateDevelopmentOwnership(run, manifest); err != nil {
		return nil, err
	}
	baselineOwners, err := baselineDevelopmentOwnership(context.Background(), run)
	if err != nil {
		return nil, err
	}
	if len(changes) == 0 || len(changes) > 20 {
		return nil, errors.New("一次最多更新20个文件")
	}
	old := map[string][]byte{}
	seen := map[string]bool{}
	for index, change := range changes {
		if seen[change.Path] {
			return nil, errors.New("重复文件路径")
		}
		seen[change.Path] = true
		if err := allowedDevelopmentPath(change.Path, true); err != nil {
			return nil, err
		}
		if change.Path == developmentOwnershipPath {
			return nil, errors.New("文件所有权清单由宿主维护")
		}
		if developmentPathInBaseline(run, change.Path) {
			if _, ok := baselineOwners.Files[change.Path]; !ok {
				return nil, fmt.Errorf("任务不能修改未接管的项目基线文件: %s", change.Path)
			}
		}

		if strings.HasSuffix(change.Path, ".go") {
			formatted, err := format.Source([]byte(change.Content))
			if err != nil {
				return nil, fmt.Errorf("Go语法不合法 %s: %w", change.Path, err)
			}
			changes[index].Content = string(formatted)
			change = changes[index]
		}
		if len(change.Content) > 2<<20 {
			return nil, errors.New("文件内容过大")
		}
		data, err := readRootedFile(run.WorkspacePath, change.Path)
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		if err == nil {
			if sha256Hex(string(data)) != change.ExpectedHash {
				return nil, fmt.Errorf("文件基线已变化: %s", change.Path)
			}
			if strings.HasSuffix(change.Path, "_test.go") || strings.Contains(change.Path, "/tests/") {
				return nil, errors.New("不能修改已有验收测试")
			}
			old[change.Path] = data
		} else {
			if change.ExpectedHash != "missing" {
				return nil, errors.New("新文件需要expectedHash=missing")
			}
			old[change.Path] = nil
		}
	}
	written := []string{}
	for _, change := range changes {
		if err := writeRootedFile(run.WorkspacePath, change.Path, []byte(change.Content)); err != nil {
			for _, path := range written {
				if old[path] == nil {
					root, openErr := openCheckedRoot(run.WorkspacePath, path)
					if openErr == nil {
						_ = root.Remove(path)
						_ = root.Close()
					}
				} else {
					_ = writeRootedFile(run.WorkspacePath, path, old[path])
				}
			}
			return nil, err
		}
		written = append(written, change.Path)
	}
	for _, change := range changes {
		owner, ok := manifest.Files[change.Path]
		if override, exists := generated[change.Path]; exists {
			owner = override
		} else if !ok {
			owner = developmentFileOwnership{Kind: "custom"}
		}
		owner.CurrentHash = sha256Hex(change.Content)
		manifest.Files[change.Path] = owner
	}
	if err := saveDevelopmentOwnership(run.WorkspacePath, manifest); err != nil {
		for _, path := range written {
			if old[path] == nil {
				r, openErr := openCheckedRoot(run.WorkspacePath, path)
				if openErr == nil {
					_ = r.Remove(path)
					r.Close()
				}
			} else {
				_ = writeRootedFile(run.WorkspacePath, path, old[path])
			}
		}
		return nil, err
	}
	return map[string]any{"written": written}, nil
}
func patchDevelopmentFiles(_ context.Context, run model.DevRun, args string) (any, error) {
	var in struct {
		Changes []developmentChange `json:"changes"`
	}
	if json.Unmarshal([]byte(args), &in) != nil {
		return nil, errors.New("参数错误")
	}
	return applyDevelopmentChanges(run, in.Changes)
}

type BusinessInstallationAsset struct {
	ObjectKey string              `json:"objectKey"`
	Name      string              `json:"name"`
	TableName string              `json:"tableName"`
	Schema    map[string]any      `json:"schema"`
	Menu      CodegenMenuPlanItem `json:"menu"`
}
type BusinessInstallationAssets struct {
	Objects []BusinessInstallationAsset `json:"objects"`
}

func generateDevelopmentBusiness(ctx context.Context, run model.DevRun, args string) (any, error) {
	var input struct {
		codegen.GenRequest
		DryRun          *bool  `json:"dryRun"`
		Mode            string `json:"mode"`
		BaseSpecVersion int    `json:"baseSpecVersion"`
	}
	if err := json.Unmarshal([]byte(args), &input); err != nil {
		return nil, err
	}
	req := input.GenRequest
	if input.Mode == "update" {
		return updateDevelopmentBusiness(ctx, run, req, input.BaseSpecVersion)
	}
	if input.Mode != "" && input.Mode != "create" {
		return nil, errors.New("生成模式必须是create或update")
	}
	if req.ObjectKey != "" && len(req.Fields) == 0 {
		obj, fields, err := GetBizObjectByKeyInProject(run.ProjectID, req.ObjectKey)
		if err != nil {
			return nil, err
		}
		var draft map[string]any
		if json.Unmarshal([]byte(obj.Schema), &draft) != nil {
			return nil, errors.New("业务草稿Schema无效")
		}
		if linkages, ok := draft["x-linkages"].([]any); ok && len(linkages) > 0 {
			return nil, errors.New("当前基础单表生成不支持草稿联动规则，已停止生成以保留业务语义")
		}
		if relations, ok := draft["x-relations"].([]any); ok && len(relations) > 0 {
			return nil, errors.New("当前基础单表生成不支持对象关系")
		}
		req.TableName = obj.Table
		if req.TableName == "" {
			req.TableName = obj.Key
		}
		req.Business = obj.Name
		for _, field := range fields {
			if field.Type == "relation" {
				return nil, errors.New("当前业务生成不支持关联字段")
			}
			var ui map[string]any
			_ = json.Unmarshal([]byte(field.XUI), &ui)
			if dictionary, ok := ui["dict"]; ok && dictionary != nil && dictionary != "" && dictionary != float64(0) {
				return nil, errors.New("当前生成不支持带字典依赖的字段")
			}
			var rules map[string]any
			_ = json.Unmarshal([]byte(field.UIRules), &rules)
			req.Fields = append(req.Fields, codegen.GenField{Name: field.Name, Title: field.Title, Type: field.Type, Search: field.Searchable, InList: field.InList, InForm: field.InForm, Required: field.Required, Rules: rules})
		}
	}
	if req.ObjectKey == "" {
		req.ObjectKey = req.TableName
	}
	if !strings.HasPrefix(req.TableName, "biz_") || !strings.HasPrefix(req.ObjectKey, "biz_") {
		return nil, errors.New("受管理业务对象和数据表必须使用biz_命名空间")
	}
	d, err := codegen.Prepare(req)
	if err != nil {
		return nil, err
	}
	files, err := codegen.RenderBusiness(d)
	if err != nil {
		return nil, err
	}
	changes := []developmentChange{}
	paths := []string{}
	owners := map[string]developmentFileOwnership{}
	for _, file := range files {
		path := file.Path
		if !strings.HasPrefix(path, "web/") {
			path = "server/" + path
		}
		if _, err := readRootedFile(run.WorkspacePath, path); !os.IsNotExist(err) {
			return nil, fmt.Errorf("模块文件已存在，请通过read_file/apply_patch修改: %s", path)
		}
		changes = append(changes, developmentChange{Path: path, ExpectedHash: "missing", Content: file.Content})
		kind := "custom"
		if strings.HasPrefix(path, "server/internal/model/") || strings.HasPrefix(path, "server/internal/router/") {
			kind = "projection"
		}
		owners[path] = developmentFileOwnership{ObjectKey: d.ObjectKey, Kind: kind, TemplateHash: sha256Hex(file.Content)}
		paths = append(paths, path)
	}
	var schema map[string]any
	_ = json.Unmarshal([]byte(d.SchemaJSON), &schema)
	assetPath := "fde/assets/business.json"
	assets := BusinessInstallationAssets{}
	expected := "missing"
	existing, err := readRootedFile(run.WorkspacePath, assetPath)
	if err == nil {
		expected = sha256Hex(string(existing))
		if json.Unmarshal(existing, &assets) != nil {
			return nil, errors.New("业务安装资产格式错误")
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	for _, object := range assets.Objects {
		if object.ObjectKey == d.ObjectKey {
			return nil, errors.New("业务对象已生成，使用增量patch修改")
		}
	}
	assets.Objects = append(assets.Objects, BusinessInstallationAsset{ObjectKey: d.ObjectKey, Name: d.Business, TableName: d.TableName, Schema: schema, Menu: CodegenMenuPlanItem{Path: d.Snake, Name: d.StructName, Title: d.Business, Component: "generated/" + d.Snake + "/index"}})
	assetJSON, _ := json.MarshalIndent(assets, "", "  ")
	changes = append(changes, developmentChange{Path: assetPath, ExpectedHash: expected, Content: string(assetJSON)})
	specPath := "fde/spec/" + d.ObjectKey + ".json"
	changes = append(changes, developmentChange{Path: specPath, ExpectedHash: "missing", Content: d.SchemaJSON})
	if _, err := applyDevelopmentChangesWithOwnership(run, changes, owners); err != nil {
		return nil, err
	}
	return map[string]any{"objectKey": d.ObjectKey, "files": paths, "assets": assetPath, "schema": schema, "written": true}, nil
}

func DevelopmentRunSnapshot(ctx context.Context, run model.DevRun) (string, string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", run.WorkspacePath, "add", "--all")
	if err := cmd.Run(); err != nil {
		return "", "", err
	}
	diff, err := exec.CommandContext(ctx, "git", "-C", run.WorkspacePath, "diff", "--cached", "--no-ext-diff", run.WorkspaceBaseCommit, "--", "server/internal", "web/src", "fde/spec", "fde/assets").Output()
	if err != nil {
		return "", "", err
	}
	if len(diff) > 2<<20 {
		diff = append(diff[:2<<20], []byte("\n[diff truncated]")...)
	}
	hash, err := runner.SnapshotHash(run.WorkspacePath)
	return string(diff), hash, err
}

func RunDevelopmentChecks(ctx context.Context, run model.DevRun) (any, error) {
	hash, err := runner.SnapshotHash(run.WorkspacePath)
	if err != nil {
		return nil, err
	}
	if value, ok := developmentVerifiedChecks.Load(run.ID); ok {
		cached := value.(runner.Report)
		if cached.Success && cached.SnapshotHash == hash {
			artifacts, err := runner.HashTree(cached.ArtifactsDir)
			if err == nil && len(artifacts) == len(cached.Artifacts) {
				same := true
				for path, digest := range artifacts {
					if cached.Artifacts[path] != digest {
						same = false
						break
					}
				}
				if same {
					return cached, nil
				}
			}
		}
	}
	output := filepath.Join(filepath.Dir(run.WorkspacePath), "checks", run.Key)
	report, err := runner.Check(ctx, runner.Request{Workspace: run.WorkspacePath, OutputDir: output, Profile: runner.Profile, ExpectedSnapshotHash: hash, GOARCH: global.Config.Development.TargetArch})
	if err == nil && report.Success {
		developmentVerifiedChecks.Store(run.ID, report)
	}
	if err == nil && !report.Success {
		err = errors.New("固定检查未通过")
	}
	return report, err
}
