package utils

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"reflect"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
)

// BindBusinessJSON enforces the same frozen field constraints used by FormRender.
func BindBusinessJSON(c *gin.Context, destination any, schemaJSON string) error {
	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 {
		return errors.New("请求体过大或无法读取")
	}
	var values map[string]any
	if json.Unmarshal(raw, &values) != nil || values == nil {
		return errors.New("业务数据必须为 JSON 对象")
	}
	if err := ValidateBusinessValues(schemaJSON, values); err != nil {
		return err
	}
	return json.Unmarshal(raw, destination)
}

func ValidateBusinessValues(schemaJSON string, values map[string]any) error {
	var schema struct {
		Fields map[string]map[string]any `json:"fields"`
	}
	if json.Unmarshal([]byte(schemaJSON), &schema) != nil || len(schema.Fields) == 0 {
		return errors.New("业务校验定义无效")
	}
	for name, field := range schema.Fields {
		value, exists := values[name]
		required, _ := field["required"].(bool)
		if !exists || value == nil {
			if required {
				return fmt.Errorf("字段 %s 必填", name)
			}
			continue
		}
		kind, _ := field["type"].(string)
		switch kind {
		case "string":
			text, ok := value.(string)
			if !ok {
				return fmt.Errorf("字段 %s 必须是文本", name)
			}
			if required && strings.TrimSpace(text) == "" {
				return fmt.Errorf("字段 %s 不能为空", name)
			}
			length := float64(utf8.RuneCountInString(text))
			if limit, ok := field["minLength"].(float64); ok && length < limit {
				return fmt.Errorf("字段 %s 长度不足", name)
			}
			if limit, ok := field["maxLength"].(float64); ok && length > limit {
				return fmt.Errorf("字段 %s 过长", name)
			}
			if pattern, ok := field["pattern"].(string); ok {
				match, err := regexp.MatchString(pattern, text)
				if err != nil || !match {
					return fmt.Errorf("字段 %s 格式错误", name)
				}
			}
			if field["format"] == "date-time" && text != "" {
				if _, err := time.Parse(time.RFC3339, text); err != nil {
					return fmt.Errorf("字段 %s 时间格式错误", name)
				}
			}
		case "integer", "number":
			number, ok := value.(float64)
			if !ok || math.IsNaN(number) || math.IsInf(number, 0) || (kind == "integer" && math.Trunc(number) != number) {
				return fmt.Errorf("字段 %s 数值类型错误", name)
			}
			if limit, ok := field["minimum"].(float64); ok && number < limit {
				return fmt.Errorf("字段 %s 低于下限", name)
			}
			if limit, ok := field["maximum"].(float64); ok && number > limit {
				return fmt.Errorf("字段 %s 超过上限", name)
			}
		case "boolean":
			if _, ok := value.(bool); !ok {
				return fmt.Errorf("字段 %s 必须是布尔值", name)
			}
		default:
			return fmt.Errorf("字段 %s 的类型不受支持", name)
		}
		if options, ok := field["enum"].([]any); ok {
			found := false
			for _, option := range options {
				if reflect.DeepEqual(option, value) {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("字段 %s 不是合法选项", name)
			}
		}
	}
	return nil
}
