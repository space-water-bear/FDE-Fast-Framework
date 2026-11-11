package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"sort"
	"strconv"

	"fde-admin/internal/codegen"
	"fde-admin/internal/model"
)

// updateDevelopmentBusiness only adds fields to an owned module. Custom service,
// API and page code remains available for explicit hash-bound patches.
func updateDevelopmentBusiness(_ context.Context, run model.DevRun, request codegen.GenRequest, baseSpecVersion int) (any, error) {
	if request.ObjectKey == "" || baseSpecVersion < 1 || len(request.Fields) == 0 {
		return nil, errors.New("增量生成需要objectKey、baseSpecVersion和新增fields")
	}
	raw, err := readRootedFile(run.WorkspacePath, "fde/assets/business.json")
	if err != nil {
		return nil, err
	}
	var assets BusinessInstallationAssets
	if json.Unmarshal(raw, &assets) != nil {
		return nil, errors.New("业务安装资产无效")
	}
	index := -1
	for i, object := range assets.Objects {
		if object.ObjectKey == request.ObjectKey {
			index = i
			break
		}
	}
	if index < 0 {
		return nil, errors.New("业务对象尚未生成或接管")
	}
	old := assets.Objects[index]
	version, ok := developmentSchemaVersion(old.Schema["version"])
	if !ok || version != int64(baseSpecVersion) {
		return nil, errors.New("业务规格版本已变化，请重新读取当前规格")
	}
	if request.TableName != "" && request.TableName != old.TableName {
		return nil, errors.New("增量生成不能改名或替换已有表")
	}
	if request.StructName != "" && request.StructName != old.Menu.Name {
		return nil, errors.New("增量生成不能替换已有Go类型")
	}
	previousFields, err := developmentFieldsFromSchema(old.Schema)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, field := range previousFields {
		seen[field.Name] = true
	}
	for _, field := range request.Fields {
		if seen[field.Name] {
			return nil, fmt.Errorf("增量fields仅接受新增字段，不能覆盖已有字段: %s", field.Name)
		}
		seen[field.Name] = true
		if field.Search {
			return nil, errors.New("新增查询条件需要对已接管服务/API显式patch，生成器不会改写自定义查询")
		}
		if field.Required {
			if _, exists := field.Rules["default"]; !exists {
				return nil, errors.New("新增必填字段必须声明可回填的default")
			}
		}
	}
	combined := codegen.GenRequest{ObjectKey: old.ObjectKey, TableName: old.TableName, Business: old.Name, StructName: old.Menu.Name, Fields: append(previousFields, request.Fields...)}
	prepared, err := codegen.Prepare(combined)
	if err != nil {
		return nil, err
	}
	var projected map[string]any
	if err := json.Unmarshal([]byte(prepared.SchemaJSON), &projected); err != nil {
		return nil, err
	}
	schemaBytes, _ := json.Marshal(old.Schema)
	var updated map[string]any
	if err := json.Unmarshal(schemaBytes, &updated); err != nil {
		return nil, err
	}
	oldEntries, ok := updated["fields"].(map[string]any)
	if !ok {
		return nil, errors.New("业务字段规格无效")
	}
	newEntries := projected["fields"].(map[string]any)
	newFields := prepared.Fields[len(previousFields):]
	for _, field := range newFields {
		oldEntries[field.JSONName] = newEntries[field.JSONName]
	}
	updated["version"] = baseSpecVersion + 1
	updatedJSON, err := json.Marshal(updated)
	if err != nil {
		return nil, err
	}
	modelPath := "server/internal/model/" + prepared.Snake + ".go"
	routerPath := "server/internal/router/" + prepared.Snake + ".go"
	manifest, err := readDevelopmentOwnership(run.WorkspacePath)
	if err != nil {
		return nil, err
	}
	for _, path := range []string{modelPath, routerPath} {
		owner, owned := manifest.Files[path]
		if !owned || owner.ObjectKey != old.ObjectKey {
			return nil, fmt.Errorf("增量文件没有明确的业务所有权: %s", path)
		}
	}
	modelSource, err := readRootedFile(run.WorkspacePath, modelPath)
	if err != nil {
		return nil, err
	}
	newModel, err := appendDevelopmentModelFields(modelSource, prepared.StructName, newFields)
	if err != nil {
		return nil, err
	}
	routerSource, err := readRootedFile(run.WorkspacePath, routerPath)
	if err != nil {
		return nil, err
	}
	newRouter, err := replaceDevelopmentCompiledSchema(routerSource, old.ObjectKey, old.Schema, string(updatedJSON))
	if err != nil {
		return nil, err
	}
	specPath := "fde/spec/" + old.ObjectKey + ".json"
	specSource, err := readRootedFile(run.WorkspacePath, specPath)
	if err != nil {
		return nil, err
	}
	var currentSpec map[string]any
	if json.Unmarshal(specSource, &currentSpec) != nil || !sameDevelopmentJSON(currentSpec, old.Schema) {
		return nil, errors.New("源码规格与安装资产已漂移，增量已停止")
	}
	assets.Objects[index].Schema = updated
	assetJSON, err := json.MarshalIndent(assets, "", "  ")
	if err != nil {
		return nil, err
	}
	changes := []developmentChange{
		{Path: modelPath, ExpectedHash: sha256Hex(string(modelSource)), Content: string(newModel)},
		{Path: routerPath, ExpectedHash: sha256Hex(string(routerSource)), Content: string(newRouter)},
		{Path: specPath, ExpectedHash: sha256Hex(string(specSource)), Content: string(updatedJSON)},
		{Path: "fde/assets/business.json", ExpectedHash: sha256Hex(string(raw)), Content: string(assetJSON)},
	}
	if _, err := applyDevelopmentChanges(run, changes); err != nil {
		return nil, err
	}
	return map[string]any{"objectKey": old.ObjectKey, "mode": "update", "baseSpecVersion": baseSpecVersion, "specVersion": baseSpecVersion + 1, "schema": updated, "written": true, "preservedCustomFiles": []string{"service", "api", "frontendApi", "page"}}, nil
}

func developmentFieldsFromSchema(schema map[string]any) ([]codegen.GenField, error) {
	entries, ok := schema["fields"].(map[string]any)
	if !ok || len(entries) == 0 {
		return nil, errors.New("冻结规格没有业务字段")
	}
	type orderedField struct {
		order float64
		field codegen.GenField
	}
	ordered := make([]orderedField, 0, len(entries))
	for jsonName, value := range entries {
		field, ok := value.(map[string]any)
		if !ok {
			return nil, errors.New("冻结字段规格无效")
		}
		meta, _ := field["x-fde"].(map[string]any)
		column, _ := meta["column"].(string)
		if column == "" {
			return nil, fmt.Errorf("字段缺少原始列名: %s", jsonName)
		}
		kind, _ := meta["type"].(string)
		title, _ := field["title"].(string)
		required, _ := field["required"].(bool)
		inForm, _ := meta["inForm"].(bool)
		inList, _ := meta["inList"].(bool)
		searchable, _ := meta["searchable"].(bool)
		rules := map[string]any{}
		for _, key := range []string{"minLength", "maxLength", "minimum", "maximum", "pattern", "enum", "default"} {
			if value, exists := field[key]; exists {
				rules[key] = value
			}
		}
		order, _ := meta["sort"].(float64)
		ordered = append(ordered, orderedField{order: order, field: codegen.GenField{Name: column, Title: title, Type: kind, Search: searchable, InList: inList, InForm: inForm, Required: required, Rules: rules}})
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].order == ordered[j].order {
			return ordered[i].field.Name < ordered[j].field.Name
		}
		return ordered[i].order < ordered[j].order
	})
	fields := make([]codegen.GenField, 0, len(ordered))
	for _, entry := range ordered {
		fields = append(fields, entry.field)
	}
	return fields, nil
}

func appendDevelopmentModelFields(source []byte, structName string, fields []codegen.FieldData) ([]byte, error) {
	files := token.NewFileSet()
	file, err := parser.ParseFile(files, "model.go", source, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	var target *ast.StructType
	for _, declaration := range file.Decls {
		gen, ok := declaration.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range gen.Specs {
			named, ok := spec.(*ast.TypeSpec)
			if ok && named.Name.Name == structName {
				target, _ = named.Type.(*ast.StructType)
			}
		}
	}
	if target == nil {
		return nil, errors.New("业务模型结构已改变，需人工审阅patch")
	}
	names := map[string]bool{}
	for _, field := range target.Fields.List {
		for _, name := range field.Names {
			names[name.Name] = true
		}
	}
	for _, field := range fields {
		if names[field.FieldName] {
			return nil, fmt.Errorf("模型已有同名自定义字段: %s", field.FieldName)
		}
		typ, err := parser.ParseExpr(field.GoType)
		if err != nil {
			return nil, err
		}
		if field.GoType == "time.Time" {
			found := false
			for _, spec := range file.Imports {
				path, _ := strconv.Unquote(spec.Path.Value)
				if path == "time" {
					found = true
					if spec.Name != nil && spec.Name.Name != "time" {
						return nil, errors.New("模型time导入使用自定义别名，需显式patch")
					}
				}
			}
			if !found {
				declaration := &ast.GenDecl{Tok: token.IMPORT, Specs: []ast.Spec{&ast.ImportSpec{Path: &ast.BasicLit{Kind: token.STRING, Value: `"time"`}}}}
				file.Decls = append([]ast.Decl{declaration}, file.Decls...)
			}
		}
		tag := fmt.Sprintf("json:%q gorm:%q", field.JSONName, field.GormTag)
		target.Fields.List = append(target.Fields.List, &ast.Field{Names: []*ast.Ident{ast.NewIdent(field.FieldName)}, Type: typ, Tag: &ast.BasicLit{Kind: token.STRING, Value: strconv.Quote(tag)}})
	}
	var output bytes.Buffer
	if err := format.Node(&output, files, file); err != nil {
		return nil, err
	}
	return format.Source(output.Bytes())
}

func replaceDevelopmentCompiledSchema(source []byte, objectKey string, old map[string]any, updated string) ([]byte, error) {
	files := token.NewFileSet()
	file, err := parser.ParseFile(files, "router.go", source, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	replaced := 0
	var conflict error
	ast.Inspect(file, func(node ast.Node) bool {
		literal, ok := node.(*ast.CompositeLit)
		if !ok {
			return true
		}
		selector, ok := literal.Type.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "BusinessRegistration" {
			return true
		}
		var key string
		var schema *ast.KeyValueExpr
		for _, element := range literal.Elts {
			pair, ok := element.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			name, ok := pair.Key.(*ast.Ident)
			if !ok {
				continue
			}
			if name.Name == "ObjectKey" {
				if text, ok := pair.Value.(*ast.BasicLit); ok {
					key, _ = strconv.Unquote(text.Value)
				}
			}
			if name.Name == "SchemaJSON" {
				schema = pair
			}
		}
		if key != objectKey {
			return true
		}
		if schema == nil {
			conflict = errors.New("业务注册缺少编译Schema")
			return false
		}
		text, ok := schema.Value.(*ast.BasicLit)
		if !ok {
			conflict = errors.New("自定义Schema表达式不能被生成器覆盖")
			return false
		}
		original, err := strconv.Unquote(text.Value)
		var originalSchema map[string]any
		if err != nil || json.Unmarshal([]byte(original), &originalSchema) != nil || !sameDevelopmentJSON(originalSchema, old) {
			conflict = errors.New("编译Schema与冻结规格已漂移")
			return false
		}
		schema.Value = &ast.BasicLit{Kind: token.STRING, Value: strconv.Quote(updated)}
		replaced++
		return false
	})
	if conflict != nil {
		return nil, conflict
	}
	if replaced != 1 {
		return nil, errors.New("业务编译注册数量不明确，增量已停止")
	}
	var output bytes.Buffer
	if err := format.Node(&output, files, file); err != nil {
		return nil, err
	}
	return format.Source(output.Bytes())
}
func sameDevelopmentJSON(left, right any) bool {
	a, _ := json.Marshal(left)
	b, _ := json.Marshal(right)
	return string(a) == string(b)
}
