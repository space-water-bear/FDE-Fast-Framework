package initialize

import (
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"fde-admin/internal/global"
	"fde-admin/internal/model"
	"fde-admin/internal/service"
)

func setupFormgenUpdateDB(t *testing.T) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	global.DB = db
	if err := db.AutoMigrate(&model.BizObject{}, &model.BizField{}, &model.FormSchema{}, &model.GenAudit{}, &model.Menu{}, &model.Role{}); err != nil {
		t.Fatalf("migrate formgen update: %v", err)
	}
}

func TestUpdateBizObjectVersionAndFields(t *testing.T) {
	setupFormgenUpdateDB(t)
	created, err := service.CreateBizObject(service.FormgenObjectInput{
		ObjectKey: "biz_customer",
		Name:      "客户",
		TableName: "biz_customer",
		Fields: []service.FormgenField{
			{Name: "name", Title: "名称", Type: "string", Required: true, InList: true, InForm: true},
			{Name: "code", Title: "编码", Type: "string", InList: true, InForm: true},
		},
	}, 0)
	if err != nil || !created.OK {
		t.Fatalf("create object: err=%v ok=%v errInfo=%+v", err, created != nil && created.OK, createdError(created))
	}
	var before model.BizObject
	if err := global.DB.Where("key = ?", "biz_customer").First(&before).Error; err != nil {
		t.Fatalf("load before: %v", err)
	}
	if before.Version != 1 {
		t.Fatalf("before version = %d, want 1", before.Version)
	}
	updated, err := service.UpdateBizObject("biz_customer", service.FormgenObjectInput{
		ObjectKey: "biz_customer",
		Name:      "客户V2",
		TableName: "biz_customer",
		Fields: []service.FormgenField{
			{Name: "name", Title: "名称", Type: "string", Required: true, InList: true, InForm: true},
			{Name: "balance", Title: "余额", Type: "float", InList: true, InForm: true},
		},
	}, 0)
	if err != nil || !updated.OK {
		t.Fatalf("update object: err=%v ok=%v errInfo=%+v", err, updated != nil && updated.OK, createdError(updated))
	}
	if v, ok := updated.Schema["version"].(int); !ok || v != 2 {
		t.Fatalf("updated schema version = %v(%T), want 2", updated.Schema["version"], updated.Schema["version"])
	}
	var after model.BizObject
	if err := global.DB.Where("key = ?", "biz_customer").First(&after).Error; err != nil {
		t.Fatalf("load after: %v", err)
	}
	if after.Version != 2 || after.Name != "客户V2" || after.Schema == "" {
		t.Fatalf("after object invalid: version=%d name=%s schemaLen=%d", after.Version, after.Name, len(after.Schema))
	}
	var fields []model.BizField
	if err := global.DB.Where("object_id = ?", after.ID).Find(&fields).Error; err != nil {
		t.Fatalf("load fields: %v", err)
	}
	if len(fields) != 2 {
		t.Fatalf("fields count = %d, want 2 (full replace)", len(fields))
	}
	names := map[string]bool{}
	for _, f := range fields {
		names[f.Name] = true
	}
	if !names["name"] || !names["balance"] || names["code"] {
		t.Fatalf("fields not replaced correctly: %v", names)
	}
}

func TestUpdateBizObjectNotFound(t *testing.T) {
	setupFormgenUpdateDB(t)
	res, err := service.UpdateBizObject("biz_missing", service.FormgenObjectInput{
		ObjectKey: "biz_missing",
		Name:      "不存在",
		Fields:    []service.FormgenField{{Name: "name", Type: "string"}},
	}, 0)
	if err != nil {
		t.Fatalf("update missing: %v", err)
	}
	if res.OK || res.Error == nil || res.Error.Code != 40001 {
		t.Fatalf("expected 40001 business error, got ok=%v err=%+v", res.OK, res.Error)
	}
}

func TestEnsureFormgenUpdateButtonIdempotent(t *testing.T) {
	setupFormgenUpdateDB(t)
	formgen := &model.Menu{Path: "formgen", Name: "Formgen", Component: "system/formgen/index", Title: "表单生成", Type: 1, Sort: 10}
	if err := global.DB.Create(formgen).Error; err != nil {
		t.Fatalf("create formgen menu: %v", err)
	}
	admin := &model.Role{Name: "超级管理员", Code: "admin", Enabled: true}
	if err := global.DB.Create(admin).Error; err != nil {
		t.Fatalf("create admin role: %v", err)
	}
	for i := 0; i < 2; i++ {
		if err := ensureFormgenUpdateButton(); err != nil {
			t.Fatalf("round %d: %v", i, err)
		}
	}
	var n int64
	if err := global.DB.Model(&model.Menu{}).Where("parent_id = ? AND api_path = ?", formgen.ID, "/api/formgen/objects/:objectKey").Count(&n).Error; err != nil {
		t.Fatalf("count update button: %v", err)
	}
	if n != 1 {
		t.Fatalf("update button count = %d, want 1", n)
	}
}

func createdError(res *service.FormgenResult) string {
	if res == nil || res.Error == nil {
		return ""
	}
	return res.Error.Msg
}
