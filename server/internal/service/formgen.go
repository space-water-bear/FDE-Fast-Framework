package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"gorm.io/gorm"

	"fde-admin/internal/global"
	"fde-admin/internal/model"
)

var (
	formgenNameRe       = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	formgenReservedCols = map[string]bool{"id": true, "created_at": true, "updated_at": true, "deleted_at": true}
	formgenFieldTypes   = map[string]bool{"string": true, "text": true, "int": true, "float": true, "bool": true, "datetime": true, "enum": true, "relation": true}
	formgenControls     = map[string]bool{"input": true, "textarea": true, "number": true, "switch": true, "datetime": true, "select": true, "radio": true, "table-select": true}
	formgenRelationKind = map[string]bool{"many_to_one": true, "one_to_many": true}
)

const (
	formgenSchemaURI = "https://fde-admin/schema/form-v1"
	formgenErrCode   = "FORM_INVALID_SCHEMA"
)

// FormgenField 工具/接口入参中的字段定义
type FormgenField struct {
	Name       string         `json:"name"`
	Title      string         `json:"title"`
	Type       string         `json:"type"`
	Required   bool           `json:"required"`
	Searchable bool           `json:"searchable"`
	InList     bool           `json:"inList"`
	InForm     bool           `json:"inForm"`
	Rules      map[string]any `json:"rules"`
	XUI        map[string]any `json:"x-ui"`
	Sort       int            `json:"sort"`
}

// FormgenRelation 业务对象关系定义
type FormgenRelation struct {
	Field        string `json:"field"`
	TargetObject string `json:"targetObject"`
	Kind         string `json:"kind"`
}

// FormgenLinkage 表单联动定义
type FormgenLinkage struct {
	When map[string]any `json:"when"`
	Then map[string]any `json:"then"`
}

// FormgenObjectInput 业务对象入参
type FormgenObjectInput struct {
	ObjectKey       string            `json:"objectKey"`
	Name            string            `json:"name"`
	TableName       string            `json:"tableName"`
	Remark          string            `json:"remark"`
	Fields          []FormgenField    `json:"fields"`
	Relations       []FormgenRelation `json:"relations"`
	Linkages        []FormgenLinkage  `json:"linkages"`
	Mode            string            `json:"mode"`
	DryRun          *bool             `json:"dryRun"`
	ExpectedVersion *int              `json:"expectedVersion,omitempty"`
	Schema          map[string]any    `json:"schema,omitempty"`
}

// FormgenError 工具失败结构(设计文档 6.1)
type FormgenError struct {
	Code    int    `json:"code"`
	ErrCode string `json:"errCode"`
	Msg     string `json:"msg"`
}

// FormgenResult generate_form_schema 返回结构(设计文档 6.1)
type FormgenResult struct {
	OK       bool           `json:"ok"`
	Schema   map[string]any `json:"schema,omitempty"`
	Warnings []string       `json:"warnings"`
	Diff     map[string]any `json:"diff,omitempty"`
	Error    *FormgenError  `json:"error,omitempty"`
}

// GenerateFormSchema permits writes only with trusted engineering scope from the caller.
func GenerateFormSchema(ctx context.Context, args string) (any, error) {
	var in FormgenObjectInput
	if err := json.Unmarshal([]byte(args), &in); err != nil {
		return formgenFail("参数不是合法 JSON: " + err.Error()), nil
	}
	dryRun := in.DryRun == nil || *in.DryRun
	scope, scopeErr := RequireEngineeringScope(ctx)
	projectID := model.DefaultProjectID
	if scopeErr == nil {
		projectID = scope.ProjectID
	}
	if scopeErr != nil && !dryRun {
		preview, err := buildFormgenResult(&in, true, 0, projectID)
		if err != nil || !preview.OK {
			return preview, err
		}
		return formgenFailCode("ENGINEERING_SCOPE_REQUIRED", "保存业务对象需要已认证的项目作用域"), nil
	}
	return buildFormgenResult(&in, dryRun, scope.ActorID, projectID)
}

// PreviewFormSchema is read-only and remains available without engineering identity.
func PreviewFormSchema(args string) (*FormgenResult, error) {
	return PreviewFormSchemaInProject(model.DefaultProjectID, args)
}
func PreviewFormSchemaInProject(projectID uint, args string) (*FormgenResult, error) {
	var in FormgenObjectInput
	if err := json.Unmarshal([]byte(args), &in); err != nil {
		return formgenFail("参数不是合法 JSON: " + err.Error()), nil
	}
	return buildFormgenResult(&in, true, 0, projectID)
}

func CreateBizObject(in FormgenObjectInput, createdBy uint) (*FormgenResult, error) {
	return CreateBizObjectInProject(model.DefaultProjectID, in, createdBy)
}
func CreateBizObjectInProject(projectID uint, in FormgenObjectInput, createdBy uint) (*FormgenResult, error) {
	in.Mode = "create"
	return buildFormgenResult(&in, false, createdBy, projectID)
}
func UpdateBizObject(key string, in FormgenObjectInput, createdBy uint) (*FormgenResult, error) {
	return UpdateBizObjectInProject(model.DefaultProjectID, key, in, createdBy)
}
func UpdateBizObjectInProject(projectID uint, key string, in FormgenObjectInput, createdBy uint) (*FormgenResult, error) {
	if strings.TrimSpace(key) == "" {
		return formgenFail("对象键不能为空"), nil
	}
	in.ObjectKey, in.Mode = key, "update"
	return buildFormgenResult(&in, false, createdBy, projectID)
}

func ListBizObjects(page, pageSize int, keyword string) ([]model.BizObject, int64, error) {
	return ListBizObjectsInProject(model.DefaultProjectID, page, pageSize, keyword)
}
func ListBizObjectsInProject(projectID uint, page, pageSize int, keyword string) ([]model.BizObject, int64, error) {
	var list []model.BizObject
	var total int64
	q := global.DB.Model(&model.BizObject{}).Where("project_id = ?", projectID)
	if keyword != "" {
		like := "%" + keyword + "%"
		q = q.Where("(key LIKE ? OR name LIKE ?)", like, like)
	}
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if err := q.Order("id desc").Offset((page - 1) * pageSize).Limit(pageSize).Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return list, total, nil
}
func GetBizObject(id uint) (*model.BizObject, error) {
	return GetBizObjectInProject(model.DefaultProjectID, id)
}
func GetBizObjectInProject(projectID, id uint) (*model.BizObject, error) {
	var obj model.BizObject
	if err := global.DB.Where("project_id = ? AND id = ?", projectID, id).First(&obj).Error; err != nil {
		return nil, err
	}
	return &obj, nil
}
func GetBizObjectByKey(key string) (*model.BizObject, []model.BizField, error) {
	return GetBizObjectByKeyInProject(model.DefaultProjectID, key)
}
func GetBizObjectByKeyInProject(projectID uint, key string) (*model.BizObject, []model.BizField, error) {
	var obj model.BizObject
	if err := global.DB.Where("project_id = ? AND key = ?", projectID, key).First(&obj).Error; err != nil {
		return nil, nil, err
	}
	var fields []model.BizField
	if err := global.DB.Where("object_id = ?", obj.ID).Order("sort asc, id asc").Find(&fields).Error; err != nil {
		return nil, nil, err
	}
	return &obj, fields, nil
}

// Runtime reads only explicitly activated schema; draft edits remain invisible here.
func GetFormSchemaByKey(key string) (map[string]any, error) {
	return GetFormSchemaByKeyInProject(model.DefaultProjectID, key)
}
func GetFormSchemaByKeyInProject(projectID uint, key string) (map[string]any, error) {
	obj, _, err := GetBizObjectByKeyInProject(projectID, key)
	if err != nil {
		return nil, fmt.Errorf("对象不存在: %s", key)
	}
	if obj.ActiveVersion == 0 {
		return nil, fmt.Errorf("对象尚未激活 schema: %s", key)
	}
	return schemaVersionForObject(obj, obj.ActiveVersion)
}
func GetDraftFormSchemaByKeyInProject(projectID uint, key string) (map[string]any, error) {
	obj, _, err := GetBizObjectByKeyInProject(projectID, key)
	if err != nil {
		return nil, err
	}
	return schemaVersionForObject(obj, obj.Version)
}
func GetFormSchemaVersionInProject(projectID uint, key string, version int) (map[string]any, error) {
	obj, _, err := GetBizObjectByKeyInProject(projectID, key)
	if err != nil {
		return nil, err
	}
	return schemaVersionForObject(obj, version)
}
func schemaVersionForObject(obj *model.BizObject, version int) (map[string]any, error) {
	var fs model.FormSchema
	if err := global.DB.Where("object_id = ? AND version = ?", obj.ID, version).First(&fs).Error; err != nil {
		return nil, fmt.Errorf("schema 版本不存在: %d", version)
	}
	var schema map[string]any
	if err := json.Unmarshal([]byte(fs.Schema), &schema); err != nil {
		return nil, err
	}
	// Legacy schemas omitted business metadata. Enrich only the latest draft,
	// because current BizFields cannot describe an older immutable version.
	if version == obj.Version {
		var fields []model.BizField
		if err := global.DB.Where("object_id = ?", obj.ID).Find(&fields).Error; err != nil {
			return nil, err
		}
		entries, _ := schema["fields"].(map[string]any)
		for _, field := range fields {
			entry, _ := entries[field.Name].(map[string]any)
			if entry == nil {
				continue
			}
			if _, exists := entry["x-fde"]; !exists {
				entry["x-fde"] = map[string]any{"type": field.Type, "searchable": field.Searchable, "inList": field.InList, "inForm": field.InForm, "sort": field.Sort}
			}
		}
		if _, exists := schema["tableName"]; !exists {
			schema["tableName"] = obj.Table
		}
		if _, exists := schema["remark"]; !exists {
			schema["remark"] = obj.Remark
		}
	}
	schema["projectId"] = obj.ProjectID
	schema["activeVersion"] = obj.ActiveVersion
	return schema, nil
}

// Activating requires the current draft and an unchanged active-version baseline.
func ActivateFormSchemaInProject(projectID uint, key string, version, expectedActiveVersion int, actorID uint) (*FormgenResult, error) {
	var activated map[string]any
	err := global.DB.Transaction(func(tx *gorm.DB) error {
		var obj model.BizObject
		if err := tx.Where("project_id = ? AND key = ?", projectID, key).First(&obj).Error; err != nil {
			return err
		}
		if version <= 0 || version != obj.Version || expectedActiveVersion != obj.ActiveVersion {
			return errFormgenStale
		}
		var fs model.FormSchema
		if err := tx.Where("object_id = ? AND version = ?", obj.ID, version).First(&fs).Error; err != nil {
			return err
		}
		if err := json.Unmarshal([]byte(fs.Schema), &activated); err != nil {
			return err
		}
		result := tx.Model(&model.BizObject{}).Where("id = ? AND version = ? AND active_version = ?", obj.ID, version, expectedActiveVersion).Update("active_version", version)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errFormgenStale
		}
		if err := tx.Model(&model.FormSchema{}).Where("object_id = ?", obj.ID).Update("published", false).Error; err != nil {
			return err
		}
		return tx.Model(&fs).Updates(map[string]any{"published": true, "activated_by": actorID}).Error
	})
	if errors.Is(err, errFormgenStale) {
		return formgenFailCode("FORM_VERSION_STALE", "版本已变化，请重新加载草稿后激活"), nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return formgenFail("对象或 schema 版本不存在"), nil
	}
	if err != nil {
		return nil, err
	}
	activated["activeVersion"] = version
	return &FormgenResult{OK: true, Schema: activated, Warnings: []string{}}, nil
}

var errFormgenStale = errors.New("form schema version stale")

func formgenFail(msg string) *FormgenResult { return formgenFailCode(formgenErrCode, msg) }
func formgenFailCode(code, msg string) *FormgenResult {
	return &FormgenResult{OK: false, Error: &FormgenError{Code: 40001, ErrCode: code, Msg: msg}}
}

// buildFormgenResult 校验入参、构建 FDE Form Schema, 并按 dryRun 决定是否落库
func buildFormgenResult(in *FormgenObjectInput, dryRun bool, createdBy, projectID uint) (*FormgenResult, error) {
	if projectID == 0 {
		return formgenFail("项目作用域不能为空"), nil
	}
	if err := normalizeFormgenSchemaInput(in); err != nil {
		return formgenFail(err.Error()), nil
	}
	if !formgenNameRe.MatchString(in.ObjectKey) {
		return formgenFail("对象键不合法: " + in.ObjectKey), nil
	}
	if strings.TrimSpace(in.Name) == "" {
		return formgenFail("对象名称不能为空"), nil
	}
	if len(in.Fields) == 0 {
		return formgenFail("至少需要一个字段"), nil
	}

	relByField := map[string]FormgenRelation{}
	for _, r := range in.Relations {
		if r.Field == "" || !formgenRelationKind[r.Kind] || !formgenNameRe.MatchString(r.TargetObject) {
			return formgenFail("关系定义不合法: " + r.Field), nil
		}
		if _, exists := relByField[r.Field]; exists {
			return formgenFail("关系字段重复: " + r.Field), nil
		}
		relByField[r.Field] = r
	}

	seen := map[string]bool{}
	fieldEntries := map[string]any{}
	warnings := []string{}
	for _, f := range in.Fields {
		if !formgenNameRe.MatchString(f.Name) {
			return formgenFail("字段名不合法: " + f.Name), nil
		}
		if formgenReservedCols[f.Name] {
			return formgenFail("字段名与内置字段冲突: " + f.Name), nil
		}
		if seen[f.Name] {
			return formgenFail("字段名重复: " + f.Name), nil
		}
		seen[f.Name] = true
		if !formgenFieldTypes[f.Type] {
			return formgenFail("字段类型不合法: " + f.Name + " type=" + f.Type), nil
		}
		if f.Type == "enum" {
			arr, ok := f.Rules["enum"].([]any)
			if !ok || len(arr) == 0 || formgenEnumJSONType(arr) == "" {
				return formgenFail("enum 字段必须提供非空 rules.enum: " + f.Name), nil
			}
		}
		if f.Type == "relation" {
			rel := relByField[f.Name]
			if rel.Field == "" || rel.TargetObject == "" || !formgenRelationKind[rel.Kind] {
				return formgenFail("relation 字段缺少合法关系定义: " + f.Name), nil
			}
		}
		if ctrl, ok := f.XUI["control"].(string); ok && ctrl != "" && !formgenControls[ctrl] {
			return formgenFail("x-ui 控件不合法: " + ctrl), nil
		}
		if f.Type == "enum" || f.Type == "relation" {
			warnings = append(warnings, f.Type+" 字段不参与代码生成")
		}
		fieldEntries[f.Name] = buildFieldEntry(f, relByField[f.Name])
	}

	for _, r := range in.Relations {
		if !seen[r.Field] {
			return formgenFail("关系字段不存在: " + r.Field), nil
		}
	}
	for _, l := range in.Linkages {
		whenField, _ := l.When["field"].(string)
		thenField, _ := l.Then["field"].(string)
		if whenField == "" || thenField == "" {
			return formgenFail("x-linkages 必须包含 when.field 与 then.field"), nil
		}
		if !seen[whenField] {
			return formgenFail("x-linkages when 字段不存在: " + whenField), nil
		}
		if !seen[thenField] {
			return formgenFail("x-linkages then 字段不存在: " + thenField), nil
		}
	}
	if err := checkFormgenLinkageCycle(in.Linkages); err != nil {
		return formgenFail("x-linkages 存在循环依赖: " + err.Error()), nil
	}

	version := 1
	var existing model.BizObject
	err := global.DB.Where("project_id = ? AND key = ?", projectID, in.ObjectKey).First(&existing).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		return nil, err
	}
	if in.Mode == "" {
		in.Mode = "create"
	}
	if in.Mode != "create" && in.Mode != "update" {
		return formgenFail("mode 仅支持 create 或 update"), nil
	}
	if in.Mode == "create" && existing.ID > 0 {
		return formgenFailCode("FORM_OBJECT_EXISTS", "对象已存在，请读取草稿后更新"), nil
	}
	if in.Mode == "update" && existing.ID == 0 {
		return formgenFail("对象不存在: " + in.ObjectKey), nil
	}
	if in.Mode == "update" && (in.ExpectedVersion == nil || *in.ExpectedVersion != existing.Version) {
		return formgenFailCode("FORM_VERSION_STALE", "草稿版本已变化或缺少 expectedVersion，请重新加载"), nil
	}
	if existing.ID > 0 {
		version = existing.Version + 1
	}
	existingNames := map[string]bool{}
	if existing.ID > 0 {
		var oldFields []model.BizField
		if err := global.DB.Where("object_id = ?", existing.ID).Find(&oldFields).Error; err != nil {
			return nil, err
		}
		for _, of := range oldFields {
			existingNames[of.Name] = true
		}
	}
	added, updated := []string{}, []string{}
	for _, f := range in.Fields {
		if existingNames[f.Name] {
			updated = append(updated, f.Name)
		} else {
			added = append(added, f.Name)
		}
	}

	linkages := make([]any, 0, len(in.Linkages))
	for _, l := range in.Linkages {
		linkages = append(linkages, map[string]any{"when": l.When, "then": l.Then})
	}
	schemaMap := map[string]any{
		"$schema":     formgenSchemaURI,
		"objectKey":   in.ObjectKey,
		"name":        strings.TrimSpace(in.Name),
		"version":     version,
		"fields":      fieldEntries,
		"x-linkages":  linkages,
		"x-relations": in.Relations,
		"tableName":   in.TableName,
		"remark":      in.Remark,
		"projectId":   projectID,
	}
	preserveFormgenExtensions(in, schemaMap)
	res := &FormgenResult{OK: true, Schema: schemaMap, Warnings: warnings, Diff: map[string]any{"added": added, "updated": updated}}
	if dryRun {
		return res, nil
	}
	if err := persistFormgen(in, schemaMap, version, createdBy, projectID); err != nil {
		if errors.Is(err, errFormgenStale) {
			return formgenFailCode("FORM_VERSION_STALE", "草稿版本已变化，请重新加载"), nil
		}
		return nil, err
	}
	return res, nil
}

func buildFieldEntry(f FormgenField, rel FormgenRelation) map[string]any {
	title := strings.TrimSpace(f.Title)
	if title == "" {
		title = f.Name
	}
	entry := map[string]any{"title": title, "type": formgenJSONType(f.Type, rel)}
	if f.Type == "enum" {
		values, _ := f.Rules["enum"].([]any)
		entry["type"] = formgenEnumJSONType(values)
	}
	if f.Required {
		entry["required"] = true
	}
	if f.Type == "datetime" {
		entry["format"] = "date-time"
	}
	if f.Rules != nil {
		for k, v := range f.Rules {
			if k == "enum" && f.Type != "enum" {
				continue
			}
			entry[k] = v
		}
	}
	xui := map[string]any{}
	if f.XUI != nil {
		for k, v := range f.XUI {
			xui[k] = v
		}
	}
	if _, ok := xui["control"]; !ok {
		xui["control"] = formgenDefaultControl(f.Type, rel)
	}
	if f.Type == "enum" {
		if _, ok := xui["options"]; !ok {
			if arr, ok := f.Rules["enum"].([]any); ok {
				opts := make([]map[string]any, 0, len(arr))
				for _, v := range arr {
					opts = append(opts, map[string]any{"label": fmt.Sprint(v), "value": v})
				}
				xui["options"] = opts
			}
		}
	}
	if f.Type == "relation" && rel.Field != "" {
		entry["relation"] = map[string]any{"kind": rel.Kind, "target": rel.TargetObject}
		if _, ok := xui["dict"]; !ok {
			xui["dict"] = rel.TargetObject
		}
	}
	entry["x-fde"] = map[string]any{"type": f.Type, "searchable": f.Searchable, "inList": f.InList, "inForm": f.InForm, "sort": f.Sort}
	entry["x-ui"] = xui
	return entry
}

func formgenJSONType(t string, rel FormgenRelation) string {
	switch t {
	case "int":
		return "integer"
	case "float":
		return "number"
	case "bool":
		return "boolean"
	case "relation":
		if rel.Kind == "one_to_many" {
			return "array"
		}
		return "integer"
	default:
		return "string"
	}
}

func formgenDefaultControl(t string, rel FormgenRelation) string {
	switch t {
	case "string":
		return "input"
	case "text":
		return "textarea"
	case "int", "float":
		return "number"
	case "bool":
		return "switch"
	case "datetime":
		return "datetime"
	case "enum":
		return "select"
	case "relation":
		if rel.Kind == "one_to_many" {
			return "table-select"
		}
		return "select"
	default:
		return "input"
	}
}

func checkFormgenLinkageCycle(linkages []FormgenLinkage) error {
	edges := map[string][]string{}
	for _, l := range linkages {
		when, _ := l.When["field"].(string)
		then, _ := l.Then["field"].(string)
		if when == "" || then == "" {
			continue
		}
		edges[when] = append(edges[when], then)
	}
	state := map[string]int{} // 0 未访问, 1 访问中, 2 已完成
	var visit func(string) error
	visit = func(n string) error {
		if state[n] == 1 {
			return fmt.Errorf("字段 %s 参与循环", n)
		}
		if state[n] == 2 {
			return nil
		}
		state[n] = 1
		for _, m := range edges[n] {
			if err := visit(m); err != nil {
				return err
			}
		}
		state[n] = 2
		return nil
	}
	for n := range edges {
		if err := visit(n); err != nil {
			return err
		}
	}
	return nil
}

func persistFormgen(in *FormgenObjectInput, schemaMap map[string]any, version int, createdBy, projectID uint) error {
	schemaJSON, err := json.Marshal(schemaMap)
	if err != nil {
		return err
	}
	tableName := strings.TrimSpace(in.TableName)
	if tableName == "" {
		tableName = in.ObjectKey
	}
	relByField := map[string]FormgenRelation{}
	for _, r := range in.Relations {
		relByField[r.Field] = r
	}
	return global.DB.Transaction(func(tx *gorm.DB) error {
		var obj model.BizObject
		err := tx.Where("project_id = ? AND key = ?", projectID, in.ObjectKey).First(&obj).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if in.Mode == "update" {
				return errFormgenStale
			}
			obj = model.BizObject{ProjectID: projectID, Key: in.ObjectKey, Name: strings.TrimSpace(in.Name), Table: tableName, Remark: in.Remark, Schema: string(schemaJSON), Version: version}
			if err := tx.Create(&obj).Error; err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else {
			if in.Mode != "update" || in.ExpectedVersion == nil || *in.ExpectedVersion != obj.Version {
				return errFormgenStale
			}
			result := tx.Model(&model.BizObject{}).Where("id = ? AND version = ?", obj.ID, *in.ExpectedVersion).Updates(map[string]any{"name": strings.TrimSpace(in.Name), "table_name": tableName, "remark": in.Remark, "schema": string(schemaJSON), "version": version})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return errFormgenStale
			}
			if err := tx.Where("object_id = ?", obj.ID).Delete(&model.BizField{}).Error; err != nil {
				return err
			}
		}
		for _, f := range in.Fields {
			rules, err := json.Marshal(f.Rules)
			if err != nil {
				return err
			}
			xui, err := json.Marshal(f.XUI)
			if err != nil {
				return err
			}
			rel, err := json.Marshal(relByField[f.Name])
			if err != nil {
				return err
			}
			title := strings.TrimSpace(f.Title)
			if title == "" {
				title = f.Name
			}
			field := model.BizField{ObjectID: obj.ID, Name: f.Name, Title: title, Type: f.Type, Required: f.Required, Searchable: f.Searchable, InList: f.InList, InForm: f.InForm, UIRules: string(rules), XUI: string(xui), Relation: string(rel), Sort: f.Sort}
			if err := tx.Create(&field).Error; err != nil {
				return err
			}
		}
		return tx.Create(&model.FormSchema{ObjectID: obj.ID, Schema: string(schemaJSON), Version: version, Published: false, CreatedBy: createdBy}).Error
	})
}
