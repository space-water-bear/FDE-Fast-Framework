package service

import (
	"context"
	"encoding/json"
	"reflect"
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
	got, err := GenerateFormSchema(WithEngineeringScope(context.Background(), EngineeringScope{ActorID: 7, ProjectID: model.DefaultProjectID}), args)
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
	if fs.Version != obj.Version || fs.Published {
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

func TestGenerateFormSchemaWithoutIdentityCannotPersist(t *testing.T) {
	setupFormgenDB(t)
	got, err := GenerateFormSchema(context.Background(), `{"objectKey":"biz_unscoped","name":"Unscoped","fields":[{"name":"name","type":"string"}],"dryRun":false}`)
	if err != nil {
		return
	}
	res := got.(*FormgenResult)
	if res.OK {
		t.Fatal("unauthenticated tool unexpectedly saved a business object")
	}
	var n int64
	if err := global.DB.Model(&model.BizObject{}).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("unauthenticated tool persisted %d objects", n)
	}
}

func TestSavingDraftDoesNotPublishRuntimeSchema(t *testing.T) {
	setupFormgenDB(t)
	res, err := CreateBizObject(FormgenObjectInput{ObjectKey: "biz_draft", Name: "Draft", Fields: []FormgenField{{Name: "name", Type: "string"}}}, 7)
	if err != nil || !res.OK {
		t.Fatalf("save draft: %v %+v", err, res)
	}
	if _, err := GetFormSchemaByKey("biz_draft"); err == nil {
		t.Fatal("saving a draft unexpectedly published runtime schema")
	}
}

func TestDraftLifecycleAndProjectIsolation(t *testing.T) {
	setupFormgenDB(t)
	input := FormgenObjectInput{ObjectKey: "biz_customer", Name: "Original", Fields: []FormgenField{{Name: "name", Type: "string", InList: true, InForm: true}}}
	first, err := CreateBizObjectInProject(1, input, 7)
	if err != nil || !first.OK {
		t.Fatalf("first draft: %v %+v", err, first)
	}
	other, err := CreateBizObjectInProject(2, input, 8)
	if err != nil || !other.OK {
		t.Fatalf("same key in another project: %v %+v", err, other)
	}
	if _, err := GetFormSchemaByKeyInProject(1, input.ObjectKey); err == nil {
		t.Fatal("draft leaked into active runtime")
	}
	activated, err := ActivateFormSchemaInProject(1, input.ObjectKey, 1, 0, 7)
	if err != nil || !activated.OK {
		t.Fatalf("activate first: %v %+v", err, activated)
	}
	expected := 1
	input.Name, input.ExpectedVersion = "Second draft", &expected
	updated, err := UpdateBizObjectInProject(1, input.ObjectKey, input, 7)
	if err != nil || !updated.OK {
		t.Fatalf("save second draft: %v %+v", err, updated)
	}
	active, err := GetFormSchemaByKeyInProject(1, input.ObjectKey)
	if err != nil || active["name"] != "Original" {
		t.Fatalf("draft changed active schema: %v %+v", err, active)
	}
	draft, err := GetDraftFormSchemaByKeyInProject(1, input.ObjectKey)
	if err != nil || draft["name"] != "Second draft" || draft["activeVersion"] != 1 {
		t.Fatalf("draft state: %v %+v", err, draft)
	}
	stale, err := UpdateBizObjectInProject(1, input.ObjectKey, input, 7)
	if err != nil || stale.OK || stale.Error.ErrCode != "FORM_VERSION_STALE" {
		t.Fatalf("stale editor not rejected: %v %+v", err, stale)
	}
	stale, err = ActivateFormSchemaInProject(1, input.ObjectKey, 1, 1, 7)
	if err != nil || stale.OK || stale.Error.ErrCode != "FORM_VERSION_STALE" {
		t.Fatalf("stale draft activation not rejected: %v %+v", err, stale)
	}
	stale, err = ActivateFormSchemaInProject(1, input.ObjectKey, 2, 0, 7)
	if err != nil || stale.OK {
		t.Fatalf("stale active baseline not rejected: %v %+v", err, stale)
	}
	activated, err = ActivateFormSchemaInProject(1, input.ObjectKey, 2, 1, 7)
	if err != nil || !activated.OK {
		t.Fatalf("activate second: %v %+v", err, activated)
	}
	active, err = GetFormSchemaByKeyInProject(1, input.ObjectKey)
	if err != nil || active["name"] != "Second draft" {
		t.Fatalf("activated state: %v %+v", err, active)
	}
	if _, err := GetFormSchemaByKeyInProject(2, input.ObjectKey); err == nil {
		t.Fatal("another project's draft was activated")
	}
	list, total, err := ListBizObjectsInProject(2, 1, 20, "")
	if err != nil || total != 1 || len(list) != 1 || list[0].Name != "Original" {
		t.Fatalf("project list leak: %v %d %+v", err, total, list)
	}
}

func TestCompleteSchemaRoundTripPreservesBusinessContract(t *testing.T) {
	setupFormgenDB(t)
	raw := `{"objectKey":"biz_roundtrip","name":"Roundtrip","metadata":{"domain":"customer","revision":3},"x-custom":{"keep":true},"fields":{"name":{"type":"string","title":"Name","required":true,"minLength":2,"x-ui":{"control":"input","placeholder":"Name","colSpan":8,"custom":{"a":1}},"x-fde":{"type":"string","searchable":true,"inList":true,"inForm":true,"sort":1,"custom":"keep"}},"notes":{"type":"string","maxLength":200,"x-ui":{"control":"textarea"},"x-fde":{"type":"text","inForm":true,"sort":2},"x-customer-hint":{"hidden":false}},"score":{"type":"integer","minimum":0,"maximum":100,"x-fde":{"type":"int","inList":true,"sort":3}},"balance":{"type":"number","multipleOf":0.01,"x-fde":{"type":"float","sort":4}},"enabled":{"type":"boolean","default":false,"x-fde":{"type":"bool","sort":5}},"when":{"type":"string","format":"date-time","x-fde":{"type":"datetime","sort":6}},"level":{"type":"string","enum":["A","B"],"x-ui":{"control":"select","options":[{"label":"Alpha","value":"A"},{"label":"Beta","value":"B"}]},"x-fde":{"type":"enum","sort":7}},"owner":{"type":"integer","relation":{"kind":"many_to_one","target":"biz_owner","onDelete":"restrict"},"x-ui":{"control":"select","dict":"biz_owner"},"x-fde":{"type":"relation","sort":8}}},"x-linkages":[{"id":"level-link","when":{"field":"level","eq":"A"},"then":{"field":"balance","required":true,"visible":false}}],"x-relations":[{"field":"owner","kind":"many_to_one","targetObject":"biz_owner","custom":"keep"}]}`
	var schema map[string]any
	if err := json.Unmarshal([]byte(raw), &schema); err != nil {
		t.Fatal(err)
	}
	created, err := CreateBizObjectInProject(1, FormgenObjectInput{Schema: schema}, 7)
	if err != nil || !created.OK {
		t.Fatalf("save schema: %v %+v", err, created)
	}
	before, err := GetDraftFormSchemaByKeyInProject(1, "biz_roundtrip")
	if err != nil {
		t.Fatal(err)
	}
	obj, fields, err := GetBizObjectByKeyInProject(1, "biz_roundtrip")
	if err != nil {
		t.Fatal(err)
	}
	types := map[string]string{}
	for _, f := range fields {
		types[f.Name] = f.Type
		if f.Name == "name" && (f.XUI == "" || !f.InList || !f.InForm || !f.Searchable) {
			t.Fatalf("field metadata lost: %+v", f)
		}
	}
	if types["notes"] != "text" || types["level"] != "enum" || types["owner"] != "relation" || types["score"] != "int" || types["when"] != "datetime" {
		t.Fatalf("business types changed: %+v", types)
	}
	version := obj.Version
	updated, err := UpdateBizObjectInProject(1, "biz_roundtrip", FormgenObjectInput{Schema: before, ExpectedVersion: &version}, 7)
	if err != nil || !updated.OK {
		t.Fatalf("roundtrip save: %v %+v", err, updated)
	}
	after, err := GetDraftFormSchemaByKeyInProject(1, "biz_roundtrip")
	if err != nil {
		t.Fatal(err)
	}
	delete(before, "version")
	delete(after, "version")
	if !reflect.DeepEqual(before, after) {
		a, _ := json.Marshal(before)
		b, _ := json.Marshal(after)
		t.Fatalf("schema roundtrip changed contract: before=%s after=%s", a, b)
	}
	f, _ := after["fields"].(map[string]any)
	meta, _ := f["name"].(map[string]any)["x-fde"].(map[string]any)
	links := after["x-linkages"].([]any)
	if links[0].(map[string]any)["id"] != "level-link" {
		t.Fatal("linkage metadata lost")
	}
	if meta["custom"] != "keep" || after["metadata"] == nil || after["x-custom"] == nil {
		t.Fatal("unknown metadata lost")
	}
}

func TestLegacyDraftReadRecoversMetadataBeforeEditing(t *testing.T) {
	setupFormgenDB(t)
	obj := model.BizObject{ProjectID: 1, Key: "biz_legacy", Name: "Legacy", Table: "biz_legacy", Version: 1}
	if err := global.DB.Create(&obj).Error; err != nil {
		t.Fatal(err)
	}
	if err := global.DB.Create(&model.BizField{ObjectID: obj.ID, Name: "description", Type: "text", InForm: true, InList: true, Searchable: true, Sort: 4, UIRules: `{"maxLength":80}`, XUI: `{"control":"textarea"}`}).Error; err != nil {
		t.Fatal(err)
	}
	if err := global.DB.Create(&model.FormSchema{ObjectID: obj.ID, Version: 1, Schema: `{"objectKey":"biz_legacy","name":"Legacy","version":1,"fields":{"description":{"type":"string","title":"Description","maxLength":80,"x-ui":{"control":"textarea"}}}}`}).Error; err != nil {
		t.Fatal(err)
	}
	schema, err := GetDraftFormSchemaByKeyInProject(1, "biz_legacy")
	if err != nil {
		t.Fatal(err)
	}
	expected := 1
	res, err := UpdateBizObjectInProject(1, "biz_legacy", FormgenObjectInput{Schema: schema, ExpectedVersion: &expected}, 7)
	if err != nil || !res.OK {
		t.Fatalf("legacy edit: %v %+v", err, res)
	}
	_, fields, err := GetBizObjectByKeyInProject(1, "biz_legacy")
	if err != nil {
		t.Fatal(err)
	}
	if len(fields) != 1 || fields[0].Type != "text" || !fields[0].InForm || !fields[0].InList || !fields[0].Searchable || fields[0].Sort != 4 {
		t.Fatalf("legacy metadata lost during editing: %+v", fields)
	}
}

func TestNumericEnumSchemaRetainsJSONAndOptionValueTypes(t *testing.T) {
	setupFormgenDB(t)
	var schema map[string]any
	if err := json.Unmarshal([]byte(`{"objectKey":"biz_numeric_enum","name":"Enums","fields":{"level":{"type":"integer","enum":[1,2],"x-ui":{"control":"select","options":[{"label":"One","value":1},{"label":"Two","value":2}]},"x-fde":{"type":"enum"}}}}`), &schema); err != nil {
		t.Fatal(err)
	}
	res, err := CreateBizObjectInProject(1, FormgenObjectInput{Schema: schema}, 7)
	if err != nil || !res.OK {
		t.Fatalf("save enum: %v %+v", err, res)
	}
	draft, err := GetDraftFormSchemaByKeyInProject(1, "biz_numeric_enum")
	if err != nil {
		t.Fatal(err)
	}
	entry := draft["fields"].(map[string]any)["level"].(map[string]any)
	if entry["type"] != "integer" {
		t.Fatalf("numeric enum changed JSON type to %v", entry["type"])
	}
	options := entry["x-ui"].(map[string]any)["options"].([]any)
	if options[0].(map[string]any)["value"] != float64(1) {
		t.Fatal("numeric option value was coerced")
	}
}
