package codegen

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"text/template"

	"go/format"
)

// FieldTypes 支持的字段类型 -> Go/TS 类型与表单控件
var FieldTypes = map[string]FieldType{
	"string":   {GoType: "string", TsType: "string", GormTag: "size:128", FormControl: "input", Searchable: true},
	"text":     {GoType: "string", TsType: "string", GormTag: "type:text", FormControl: "textarea", Searchable: true},
	"int":      {GoType: "int", TsType: "number", GormTag: "default:0", FormControl: "number", Searchable: true},
	"float":    {GoType: "float64", TsType: "number", GormTag: "default:0", FormControl: "number", Searchable: true},
	"bool":     {GoType: "bool", TsType: "boolean", GormTag: "default:false", FormControl: "switch"},
	"datetime": {GoType: "time.Time", TsType: "string", GormTag: "type:datetime", FormControl: "datetime"},
}

type FieldType struct {
	GoType      string
	TsType      string
	GormTag     string
	FormControl string
	Searchable  bool
}

var (
	reservedSnakes  = map[string]bool{"user": true, "role": true, "menu": true, "auth": true, "codegen": true, "dashboard": true, "system": true}
	reservedColumns = map[string]bool{"id": true, "created_at": true, "updated_at": true, "deleted_at": true}
	goKeywords      = map[string]bool{"type": true, "func": true, "range": true, "map": true, "select": true, "chan": true, "defer": true, "go": true, "if": true, "else": true, "for": true, "var": true, "const": true, "return": true, "break": true, "continue": true, "switch": true, "case": true, "default": true, "struct": true, "interface": true, "package": true, "import": true, "fallthrough": true, "goto": true, "string": true, "int": true, "bool": true, "error": true, "nil": true, "true": true, "false": true}
	nameRe          = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	structNameRe    = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*$`)
)

type GenField struct {
	Name     string         `json:"name"`
	Title    string         `json:"title"`
	Type     string         `json:"type"`
	Search   bool           `json:"search"`
	InList   bool           `json:"inList"`
	InForm   bool           `json:"inForm"`
	Required bool           `json:"required"`
	Rules    map[string]any `json:"rules,omitempty"`
}

type GenRequest struct {
	ObjectKey    string     `json:"objectKey"`
	TableName    string     `json:"tableName"`
	Business     string     `json:"business"`
	StructName   string     `json:"structName"`
	ParentMenuID uint       `json:"parentMenuId"`
	Fields       []GenField `json:"fields"`
}

// FileResult 生成的一个文件(路径相对于 server 目录, web/ 前缀代表前端)
type FileResult struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// Data 模板渲染数据
type Data struct {
	SchemaDriven  bool
	ObjectKey     string
	SchemaLiteral string
	SchemaJSON    string
	TableName     string
	StructName    string
	Business      string
	Snake         string
	HasTime       bool
	ParentMenuID  uint
	Fields        []FieldData
	SearchFields  []FieldData
	ListFields    []FieldData
	FormFields    []FieldData
}

type FieldData struct {
	ColumnName   string
	FieldName    string
	JSONName     string
	Title        string
	GoType       string
	TsType       string
	TsDefault    string
	GormTag      string
	GoSearchType string
	FormControl  string
	Required     bool
}

// Prepare 校验请求并推导模板渲染数据
func Prepare(req GenRequest) (*Data, error) {
	if !nameRe.MatchString(req.TableName) {
		return nil, errors.New("表名不合法: 仅支持小写字母/数字/下划线且以字母开头")
	}
	if strings.TrimSpace(req.Business) == "" {
		return nil, errors.New("请填写业务名称(中文描述)")
	}
	if len(req.Fields) == 0 {
		return nil, errors.New("至少需要一个字段")
	}

	structName := strings.TrimSpace(req.StructName)
	if structName == "" {
		structName = snakeUpper(stripTablePrefix(req.TableName))
	}
	if !structNameRe.MatchString(structName) {
		return nil, errors.New("结构体名不合法: 仅支持字母开头、字母数字组成")
	}
	structName = strings.ToUpper(structName[:1]) + structName[1:]
	if goKeywords[strings.ToLower(structName)] {
		return nil, errors.New("结构体名不能是 Go 关键字: " + structName)
	}

	snake := camelToSnake(structName)
	if reservedSnakes[snake] {
		return nil, errors.New("业务名与内置模块冲突: " + snake)
	}

	d := &Data{
		TableName:    req.TableName,
		StructName:   structName,
		Business:     strings.TrimSpace(req.Business),
		Snake:        snake,
		ParentMenuID: req.ParentMenuID,
	}

	seen := map[string]bool{}
	hasForm := false
	for _, f := range req.Fields {
		if !nameRe.MatchString(f.Name) {
			return nil, errors.New("字段名不合法: " + f.Name)
		}
		if reservedColumns[f.Name] {
			return nil, errors.New("字段名与内置字段冲突: " + f.Name + " (id/created_at/updated_at 已内置)")
		}
		if seen[f.Name] {
			return nil, errors.New("字段名重复: " + f.Name)
		}
		seen[f.Name] = true
		if strings.TrimSpace(f.Title) == "" {
			return nil, errors.New("请填写字段 " + f.Name + " 的中文名")
		}
		ft, ok := FieldTypes[f.Type]
		if !ok {
			return nil, fmt.Errorf("不支持的字段类型: %s (可选 string/text/int/float/bool/datetime)", f.Type)
		}
		if f.Search && !ft.Searchable {
			return nil, errors.New("字段 " + f.Name + " 的类型不支持作为查询条件(仅 string/text/int/float 支持)")
		}
		fd := FieldData{
			ColumnName:  f.Name,
			FieldName:   snakeUpper(f.Name),
			JSONName:    snakeLower(f.Name),
			Title:       strings.TrimSpace(f.Title),
			GoType:      ft.GoType,
			TsType:      ft.TsType,
			GormTag:     ft.GormTag + ";comment:" + strings.TrimSpace(f.Title),
			FormControl: ft.FormControl,
			Required:    f.Required,
		}
		if goKeywords[strings.ToLower(fd.FieldName)] || goKeywords[fd.JSONName] {
			return nil, errors.New("字段名 " + f.Name + " 生成的 Go 标识符是关键字，请更换")
		}
		switch ft.GoType {
		case "int", "float64":
			fd.TsDefault = "null"
		case "bool":
			fd.TsDefault = "false"
		case "time.Time":
			fd.TsDefault = "''"
			d.HasTime = true
		default:
			fd.TsDefault = "''"
		}
		if f.Search {
			switch ft.GoType {
			case "int":
				fd.GoSearchType = "*int"
			case "float64":
				fd.GoSearchType = "*float64"
			default:
				fd.GoSearchType = "string"
			}
			d.SearchFields = append(d.SearchFields, fd)
		}
		if f.InList {
			d.ListFields = append(d.ListFields, fd)
		}
		if f.InForm {
			hasForm = true
			d.FormFields = append(d.FormFields, fd)
		}
		d.Fields = append(d.Fields, fd)
	}
	if !hasForm {
		return nil, errors.New("至少需要一个表单字段")
	}
	d.ObjectKey = req.ObjectKey
	if d.ObjectKey == "" {
		d.ObjectKey = req.TableName
	}
	fields := map[string]any{}
	for i, f := range req.Fields {
		fd := d.Fields[i]
		kind := "string"
		switch f.Type {
		case "int":
			kind = "integer"
		case "float":
			kind = "number"
		case "bool":
			kind = "boolean"
		}
		field := map[string]any{"type": kind, "title": f.Title, "required": f.Required, "x-ui": map[string]any{"control": fd.FormControl}, "x-fde": map[string]any{"type": f.Type, "sort": i, "inForm": f.InForm, "inList": f.InList, "column": f.Name}}
		for _, key := range []string{"minLength", "maxLength", "minimum", "maximum", "pattern", "enum", "default"} {
			if value, ok := f.Rules[key]; ok {
				field[key] = value
			}
		}
		if f.Type == "datetime" {
			field["format"] = "date-time"
		}
		fields[fd.JSONName] = field
	}
	schema := map[string]any{"objectKey": d.ObjectKey, "name": d.Business, "version": 1, "fields": fields}
	raw, err := json.Marshal(schema)
	if err != nil {
		return nil, err
	}
	d.SchemaJSON = string(raw)
	d.SchemaLiteral = strconv.Quote(string(raw))
	return d, nil
}

func stripTablePrefix(s string) string {
	for _, p := range []string{"sys_", "biz_", "t_"} {
		if strings.HasPrefix(s, p) {
			return strings.TrimPrefix(s, p)
		}
	}
	return s
}

func snakeUpper(s string) string {
	parts := strings.Split(s, "_")
	var b strings.Builder
	for _, p := range parts {
		if p == "" {
			continue
		}
		b.WriteString(strings.ToUpper(p[:1]))
		b.WriteString(p[1:])
	}
	return b.String()
}

func snakeLower(s string) string {
	upper := snakeUpper(s)
	if upper == "" {
		return ""
	}
	return strings.ToLower(upper[:1]) + upper[1:]
}

func camelToSnake(s string) string {
	var b strings.Builder
	for i, r := range s {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				b.WriteByte('_')
			}
			b.WriteRune(r - 'A' + 'a')
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Diff 比较磁盘旧文件与新生成内容: 文件不存在返回 (false, nil), 内容不同返回 (true, nil)
func Diff(oldPath, newContent string) (bool, error) {
	old, err := os.ReadFile(oldPath)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return string(old) != newContent, nil
}

// Render 渲染全部模板, 后端 .go 文件过 gofmt 校验语法
func Render(d *Data) ([]FileResult, error) {
	tpls := []struct {
		path string
		name string
		text string
	}{
		{"internal/model/" + d.Snake + ".go", "model", modelTpl},
		{"internal/service/" + d.Snake + ".go", "service", serviceTpl},
		{"internal/api/" + d.Snake + ".go", "api", apiTpl},
		{"internal/router/" + d.Snake + ".go", "router", routerTpl},
		{"web/src/api/" + d.Snake + ".ts", "api.ts", apiTsTpl},
		{"web/src/views/generated/" + d.Snake + "/index.vue", "index.vue", vueTpl},
	}
	files := make([]FileResult, 0, len(tpls))
	for _, t := range tpls {
		tpl, err := template.New(t.name).Delims("[[", "]]").Parse(t.text)
		if err != nil {
			return nil, fmt.Errorf("模板解析失败 %s: %w", t.name, err)
		}
		var buf bytes.Buffer
		if err := tpl.Execute(&buf, d); err != nil {
			return nil, fmt.Errorf("模板渲染失败 %s: %w", t.name, err)
		}
		content := buf.String()
		if strings.HasSuffix(t.name, ".go") || t.name == "model" || t.name == "service" || t.name == "api" || t.name == "router" {
			formatted, err := format.Source([]byte(content))
			if err != nil {
				return nil, fmt.Errorf("生成代码语法错误 %s: %w", t.path, err)
			}
			content = string(formatted)
		}
		files = append(files, FileResult{Path: t.path, Content: content})
	}
	return files, nil
}

// RenderBusiness creates managed pages using the shared runtime FormRender.
func RenderBusiness(d *Data) ([]FileResult, error) {
	copy := *d
	copy.SchemaDriven = true
	return Render(&copy)
}
