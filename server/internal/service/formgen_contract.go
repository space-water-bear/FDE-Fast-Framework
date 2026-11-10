package service

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
)

// normalizeFormgenSchemaInput accepts the complete schema as the editing contract.
// The array input remains supported for tools creating a schema for the first time.
func normalizeFormgenSchemaInput(in *FormgenObjectInput) error {
	if in.Schema == nil {
		for _, f := range in.Fields {
			for _, key := range []string{"title", "type", "required", "x-ui", "x-fde", "relation"} {
				if _, ok := f.Rules[key]; ok {
					return fmt.Errorf("rules 不能覆盖字段结构 %s.%s", f.Name, key)
				}
			}
		}
		if strings.TrimSpace(in.TableName) == "" {
			in.TableName = in.ObjectKey
		}
		return nil
	}
	schema := in.Schema
	if key, ok := schema["objectKey"].(string); ok && key != "" {
		if in.ObjectKey != "" && in.ObjectKey != key {
			return fmt.Errorf("schema.objectKey 与对象键不一致")
		}
		in.ObjectKey = key
	}
	if name, ok := schema["name"].(string); ok && in.Name == "" {
		in.Name = name
	}
	if value, ok := schema["tableName"].(string); ok && in.TableName == "" {
		in.TableName = value
	}
	if value, ok := schema["remark"].(string); ok && in.Remark == "" {
		in.Remark = value
	}
	if in.TableName == "" {
		in.TableName = in.ObjectKey
	}
	rawFields, ok := schema["fields"].(map[string]any)
	if !ok {
		return fmt.Errorf("schema.fields 必须为字段对象")
	}
	fields := make([]FormgenField, 0, len(rawFields))
	for name, raw := range rawFields {
		entry, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("schema 字段 %s 必须为对象", name)
		}
		meta := map[string]any{}
		if rawMeta, exists := entry["x-fde"]; exists {
			var ok bool
			meta, ok = rawMeta.(map[string]any)
			if !ok {
				return fmt.Errorf("字段 %s.x-fde 必须为对象", name)
			}
		}
		f := FormgenField{Name: name, Rules: map[string]any{}, XUI: map[string]any{}}
		f.Title, _ = entry["title"].(string)
		if rawRequired, exists := entry["required"]; exists {
			var ok bool
			f.Required, ok = rawRequired.(bool)
			if !ok {
				return fmt.Errorf("字段 %s.required 必须为布尔值", name)
			}
		}
		if rawUI, exists := entry["x-ui"]; exists {
			var ok bool
			f.XUI, ok = rawUI.(map[string]any)
			if !ok {
				return fmt.Errorf("字段 %s.x-ui 必须为对象", name)
			}
		}
		f.Type, _ = meta["type"].(string)
		if f.Type == "" {
			f.Type = inferFormgenBusinessType(entry)
		}
		for key, target := range map[string]*bool{"searchable": &f.Searchable, "inList": &f.InList, "inForm": &f.InForm} {
			if value, exists := meta[key]; exists {
				b, ok := value.(bool)
				if !ok {
					return fmt.Errorf("字段 %s.x-fde.%s 必须为布尔值", name, key)
				}
				*target = b
			}
		}
		if value, exists := meta["sort"]; exists {
			b, err := json.Marshal(value)
			if err != nil {
				return err
			}
			if err := json.Unmarshal(b, &f.Sort); err != nil {
				return fmt.Errorf("字段 %s.x-fde.sort 必须为整数", name)
			}
		}
		for key, value := range entry {
			switch key {
			case "title", "type", "required", "x-ui", "x-fde", "relation":
				continue
			}
			f.Rules[key] = value
		}
		if f.Type == "enum" {
			values, _ := f.Rules["enum"].([]any)
			valueType := formgenEnumJSONType(values)
			declared, _ := entry["type"].(string)
			if declared != "enum" && declared != valueType && !(declared == "number" && valueType == "integer") {
				return fmt.Errorf("字段 %s 的 JSON 类型与 enum 值类型不一致", name)
			}
		}
		fields = append(fields, f)
	}
	// JSON objects have no order: use the explicit metadata order with a stable tie-break.
	sort.Slice(fields, func(i, j int) bool {
		if fields[i].Sort != fields[j].Sort {
			return fields[i].Sort < fields[j].Sort
		}
		return fields[i].Name < fields[j].Name
	})
	in.Fields = fields
	if raw, exists := schema["x-linkages"]; exists {
		if err := decodeFormgenPart(raw, &in.Linkages); err != nil {
			return fmt.Errorf("x-linkages 不合法: %w", err)
		}
	}
	if raw, exists := schema["x-relations"]; exists {
		if err := decodeFormgenPart(raw, &in.Relations); err != nil {
			return fmt.Errorf("x-relations 不合法: %w", err)
		}
	} else {
		// Support existing schemas which carry relations on individual fields only.
		in.Relations = nil
		for name, raw := range rawFields {
			entry, _ := raw.(map[string]any)
			if rel, ok := entry["relation"].(map[string]any); ok {
				kind, _ := rel["kind"].(string)
				target, _ := rel["target"].(string)
				in.Relations = append(in.Relations, FormgenRelation{Field: name, Kind: kind, TargetObject: target})
			}
		}
	}
	return nil
}

func decodeFormgenPart(value any, out any) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

func inferFormgenBusinessType(entry map[string]any) string {
	if _, ok := entry["relation"]; ok {
		return "relation"
	}
	if _, ok := entry["enum"]; ok {
		return "enum"
	}
	if entry["format"] == "date-time" {
		return "datetime"
	}
	t, _ := entry["type"].(string)
	switch t {
	case "integer":
		return "int"
	case "number":
		return "float"
	case "boolean":
		return "bool"
	}
	if t == "string" {
		if ui, ok := entry["x-ui"].(map[string]any); ok && ui["control"] == "textarea" {
			return "text"
		}
	}
	return t
}

func preserveFormgenExtensions(in *FormgenObjectInput, out map[string]any) {
	if in.Schema == nil {
		return
	}
	for key, value := range in.Schema {
		switch key {
		case "$schema", "objectKey", "name", "version", "activeVersion", "projectId", "fields", "tableName", "remark":
			continue
		}
		out[key] = value
	}
	oldFields, _ := in.Schema["fields"].(map[string]any)
	newFields, _ := out["fields"].(map[string]any)
	for name, newRaw := range newFields {
		oldEntry, _ := oldFields[name].(map[string]any)
		newEntry, _ := newRaw.(map[string]any)
		// Keep the numeric enum's declared JSON type as well as its typed values.
		if meta, ok := newEntry["x-fde"].(map[string]any); ok && meta["type"] == "enum" {
			if oldType, ok := oldEntry["type"].(string); ok && oldType != "enum" {
				newEntry["type"] = oldType
			}
		}
		// Keep unknown metadata next to the canonical fields; recognized values are server normalized.
		if oldMeta, ok := oldEntry["x-fde"].(map[string]any); ok {
			meta, _ := newEntry["x-fde"].(map[string]any)
			for key, value := range oldMeta {
				if _, known := meta[key]; !known {
					meta[key] = value
				}
			}
		}
		if oldRel, ok := oldEntry["relation"].(map[string]any); ok {
			if rel, ok := newEntry["relation"].(map[string]any); ok {
				for key, value := range oldRel {
					if _, known := rel[key]; !known {
						rel[key] = value
					}
				}
			}
		}
	}
}

// Enum values retain primitive JSON types; mixed strings/numbers would have no
// single valid schema type and are rejected rather than coerced.
func formgenEnumJSONType(values []any) string {
	kind := ""
	for _, value := range values {
		current := ""
		switch n := value.(type) {
		case string:
			current = "string"
		case bool:
			current = "boolean"
		case float64:
			current = "number"
			if math.Trunc(n) == n {
				current = "integer"
			}
		case int, int32, int64, uint, uint32, uint64:
			current = "integer"
		default:
			return ""
		}
		if kind == "" {
			kind = current
			continue
		}
		if (kind == "number" || kind == "integer") && (current == "number" || current == "integer") {
			if current == "number" {
				kind = "number"
			}
			continue
		}
		if current != kind {
			return ""
		}
	}
	return kind
}
