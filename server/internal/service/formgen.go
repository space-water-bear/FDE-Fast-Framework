package service

import (
	"context"
	"encoding/json"
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
	ObjectKey string            `json:"objectKey"`
	Name      string            `json:"name"`
	TableName string            `json:"tableName"`
	Remark    string            `json:"remark"`
	Fields    []FormgenField    `json:"fields"`
	Relations []FormgenRelation `json:"relations"`
	Linkages  []FormgenLinkage  `json:"linkages"`
	Mode      string            `json:"mode"`
	DryRun    *bool             `json:"dryRun"`
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

// GenerateFormSchema generate_form_schema 工具执行函数(AiToolFunc 签名)
func GenerateFormSchema(ctx context.Context, args string) (any, error) {
	res, err := generateFormSchema(args, false, 0)
	if err != nil {
		return nil, err
	}
	return res, nil
}

// PreviewFormSchema HTTP 预览: 强制 dryRun=true, 不落库
func PreviewFormSchema(args string) (*FormgenResult, error) {
	return generateFormSchema(args, true, 0)
}

// CreateBizObject 保存业务对象(校验 + 生成 schema + 落库), 供 POST /api/formgen/objects 使用
func CreateBizObject(in FormgenObjectInput, createdBy uint) (*FormgenResult, error) {
	dryRun := false
	in.DryRun = &dryRun
	raw, err := json.Marshal(in)
	if err != nil {
		return nil, err
	}
	return generateFormSchema(string(raw), false, createdBy)
}

// UpdateBizObject 更新业务对象(校验 + 生成 schema + 落库), 供 PUT /api/formgen/objects/:objectKey 使用。
// 仅允许更新已存在的对象, 对象不存在返回 40001 业务错误(与 POST 创建语义区分)。
func UpdateBizObject(objectKey string, in FormgenObjectInput, createdBy uint) (*FormgenResult, error) {
	if strings.TrimSpace(objectKey) == "" {
		return formgenFail("对象键不能为空"), nil
	}
	var existing model.BizObject
	err := global.DB.Where("key = ?", objectKey).First(&existing).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return formgenFail("对象不存在: " + objectKey), nil
		}
		return nil, err
	}
	in.ObjectKey = objectKey
	return buildFormgenResult(&in, false, createdBy)
}

// ListBizObjects 业务对象分页列表
func ListBizObjects(page, pageSize int, keyword string) ([]model.BizObject, int64, error) {
	var list []model.BizObject
	var total int64
	q := global.DB.Model(&model.BizObject{})
	if keyword != "" {
		like := "%" + keyword + "%"
		q = q.Where("key LIKE ? OR name LIKE ?", like, like)
	}
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err := q.Order("id desc").Offset((page - 1) * pageSize).Limit(pageSize).Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

// GetBizObject 按主键读取业务对象
func GetBizObject(id uint) (*model.BizObject, error) {
	var obj model.BizObject
	if err := global.DB.First(&obj, id).Error; err != nil {
		return nil, err
	}
	return &obj, nil
}

// GetBizObjectByKey 按业务键读取对象与字段
func GetBizObjectByKey(key string) (*model.BizObject, []model.BizField, error) {
	var obj model.BizObject
	if err := global.DB.Where("key = ?", key).First(&obj).Error; err != nil {
		return nil, nil, err
	}
	var fields []model.BizField
	if err := global.DB.Where("object_id = ?", obj.ID).Order("sort asc, id asc").Find(&fields).Error; err != nil {
		return nil, nil, err
	}
	return &obj, fields, nil
}

// GetFormSchemaByKey 返回对象最新的 FDE Form Schema
func GetFormSchemaByKey(key string) (map[string]any, error) {
	obj, _, err := GetBizObjectByKey(key)
	if err != nil {
		return nil, fmt.Errorf("对象不存在: %s", key)
	}
	if strings.TrimSpace(obj.Schema) == "" {
		return nil, fmt.Errorf("对象尚未生成 schema: %s", key)
	}
	var schema map[string]any
	if err := json.Unmarshal([]byte(obj.Schema), &schema); err != nil {
		return nil, err
	}
	return schema, nil
}

// generateFormSchema 解析工具入参并执行生成
func generateFormSchema(args string, forceDryRun bool, createdBy uint) (*FormgenResult, error) {
	var in FormgenObjectInput
	if err := json.Unmarshal([]byte(args), &in); err != nil {
		return formgenFail("参数不是合法 JSON: " + err.Error()), nil
	}
	dryRun := true
	if in.DryRun != nil {
		dryRun = *in.DryRun
	}
	if forceDryRun {
		dryRun = true
	}
	return buildFormgenResult(&in, dryRun, createdBy)
}

func formgenFail(msg string) *FormgenResult {
	return &FormgenResult{OK: false, Error: &FormgenError{Code: 40001, ErrCode: formgenErrCode, Msg: msg}}
}

// buildFormgenResult 校验入参、构建 FDE Form Schema, 并按 dryRun 决定是否落库
func buildFormgenResult(in *FormgenObjectInput, dryRun bool, createdBy uint) (*FormgenResult, error) {
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
			if !ok || len(arr) == 0 {
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
	err := global.DB.Where("key = ?", in.ObjectKey).First(&existing).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		return nil, err
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
		"$schema":    formgenSchemaURI,
		"objectKey":  in.ObjectKey,
		"name":       strings.TrimSpace(in.Name),
		"version":    version,
		"fields":     fieldEntries,
		"x-linkages": linkages,
	}
	res := &FormgenResult{OK: true, Schema: schemaMap, Warnings: warnings, Diff: map[string]any{"added": added, "updated": updated}}
	if dryRun {
		return res, nil
	}
	if err := persistFormgen(in, schemaMap, version, createdBy); err != nil {
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

func persistFormgen(in *FormgenObjectInput, schemaMap map[string]any, version int, createdBy uint) error {
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
		if err := tx.Where("key = ?", in.ObjectKey).First(&obj).Error; err != nil {
			if err != gorm.ErrRecordNotFound {
				return err
			}
			obj = model.BizObject{Key: in.ObjectKey, Name: strings.TrimSpace(in.Name), Table: tableName, Remark: in.Remark, Schema: string(schemaJSON), Version: version}
			if err := tx.Create(&obj).Error; err != nil {
				return err
			}
		} else {
			obj.Name = strings.TrimSpace(in.Name)
			obj.Table = tableName
			obj.Remark = in.Remark
			obj.Schema = string(schemaJSON)
			obj.Version = version
			if err := tx.Save(&obj).Error; err != nil {
				return err
			}
			if err := tx.Where("object_id = ?", obj.ID).Delete(&model.BizField{}).Error; err != nil {
				return err
			}
		}
		for _, f := range in.Fields {
			uiRules := "{}"
			if f.Rules != nil {
				if b, err := json.Marshal(f.Rules); err == nil {
					uiRules = string(b)
				}
			}
			relation := "{}"
			if rel, ok := relByField[f.Name]; ok {
				if b, err := json.Marshal(rel); err == nil {
					relation = string(b)
				}
			}
			title := strings.TrimSpace(f.Title)
			if title == "" {
				title = f.Name
			}
			bf := model.BizField{
				ObjectID:   obj.ID,
				Name:       f.Name,
				Title:      title,
				Type:       f.Type,
				Required:   f.Required,
				Searchable: f.Searchable,
				InList:     f.InList,
				InForm:     f.InForm,
				UIRules:    uiRules,
				Relation:   relation,
				Sort:       f.Sort,
			}
			if err := tx.Create(&bf).Error; err != nil {
				return err
			}
		}
		fs := model.FormSchema{ObjectID: obj.ID, Schema: string(schemaJSON), Version: version, Published: true, CreatedBy: createdBy}
		return tx.Create(&fs).Error
	})
}
