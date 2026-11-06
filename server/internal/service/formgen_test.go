package service

import (
	"context"
	"testing"

	"fde-admin/internal/global"
	"fde-admin/internal/model"
)

func setupFormgenDB(t *testing.T) {
	t.Helper()
	setupTestDB(t)
	if err := global.DB.AutoMigrate(&model.BizObject{}, &model.BizField{}, &model.FormSchema{}, &model.GenAudit{}); err != nil {
		t.Fatalf("migrate formgen: %v", err)
	}
}

func TestGenerateFormSchemaValid(t *testing.T) {
	setupFormgenDB(t)
	args := `{"objectKey":"biz_customer","name":"客户","fields":[{"name":"name","title":"客户名称","type":"string","required":true,"rules":{"minLength":2,"maxLength":64}},{"name":"level","title":"等级","type":"enum","rules":{"enum":["A","B","C"]}},{"name":"balance","title":"余额","type":"float","rules":{"minimum":0}}],"relations":[],"linkages":[{"when":{"field":"level","eq":"A"},"then":{"field":"balance","required":true,"visible":true}}],"dryRun":false}`
	got, err := GenerateFormSchema(context.Background(), args)
	if err != nil {
		t.Fatalf("GenerateFormSchema: %v", err)
	}
	res, ok := got.(*FormgenResult)
	if !ok {
		t.Fatalf("unexpected result type: %T", got)
	}
	if !res.OK {
		t.Fatalf("expected ok, got error: %+v", res.Error)
	}
	fields, ok := res.Schema["fields"].(map[string]any)
	if !ok || len(fields) != 3 {
		t.Fatalf("schema fields missing: %v", res.Schema["fields"])
	}
	if _, ok := res.Schema["x-linkages"]; !ok {
		t.Fatalf("x-linkages missing")
	}
	nameEntry, _ := fields["name"].(map[string]any)
	if nameEntry["x-ui"] == nil {
		t.Fatalf("field x-ui missing")
	}
	var obj model.BizObject
	if err := global.DB.Where("key = ?", "biz_customer").First(&obj).Error; err != nil {
		t.Fatalf("object not persisted: %v", err)
	}
	if obj.Schema == "" || obj.Version != 1 {
		t.Fatalf("object schema/version invalid: version=%d schemaLen=%d", obj.Version, len(obj.Schema))
	}
	var bfs []model.BizField
	if err := global.DB.Where("object_id = ?", obj.ID).Find(&bfs).Error; err != nil {
		t.Fatalf("list fields: %v", err)
	}
	if len(bfs) != 3 {
		t.Fatalf("fields count = %d", len(bfs))
	}
	var fs model.FormSchema
	if err := global.DB.Where("object_id = ?", obj.ID).First(&fs).Error; err != nil {
		t.Fatalf("schema history not persisted: %v", err)
	}
	if fs.Version != obj.Version || !fs.Published {
		t.Fatalf("schema history invalid: version=%d published=%v", fs.Version, fs.Published)
	}
}

func TestGenerateFormSchemaInvalidFieldName(t *testing.T) {
	setupFormgenDB(t)
	args := `{"objectKey":"biz_bad","name":"坏对象","fields":[{"name":"BadName","title":"坏字段","type":"string"}],"dryRun":false}`
	got, err := GenerateFormSchema(context.Background(), args)
	if err != nil {
		t.Fatalf("GenerateFormSchema: %v", err)
	}
	res := got.(*FormgenResult)
	if res.OK || res.Error == nil || res.Error.ErrCode != "FORM_INVALID_SCHEMA" {
		t.Fatalf("expected FORM_INVALID_SCHEMA, got ok=%v err=%+v", res.OK, res.Error)
	}
}

func TestGenerateFormSchemaLinkageCycle(t *testing.T) {
	setupFormgenDB(t)
	args := `{"objectKey":"biz_cycle","name":"循环对象","fields":[{"name":"field_a","title":"A","type":"string"},{"name":"field_b","title":"B","type":"string"}],"linkages":[{"when":{"field":"field_a","eq":"1"},"then":{"field":"field_b","visible":true}},{"when":{"field":"field_b","eq":"1"},"then":{"field":"field_a","visible":true}}],"dryRun":false}`
	got, err := GenerateFormSchema(context.Background(), args)
	if err != nil {
		t.Fatalf("GenerateFormSchema: %v", err)
	}
	res := got.(*FormgenResult)
	if res.OK || res.Error == nil || res.Error.ErrCode != "FORM_INVALID_SCHEMA" {
		t.Fatalf("expected FORM_INVALID_SCHEMA, got ok=%v err=%+v", res.OK, res.Error)
	}
}
