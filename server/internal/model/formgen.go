package model

// BizObject 业务对象(表单与代码生成的统一事实源)
// 注意: 设计文档中的 Go 字段名 TableName 与 GORM 的 TableName() 方法同名会编译冲突,
// 故字段改名 Table, JSON 名与数据库列名保持设计文档约定(tableName / table_name)。
type BizObject struct {
	Base
	Key     string `json:"key" gorm:"size:64;uniqueIndex;comment:业务键"`
	Name    string `json:"name" gorm:"size:128;comment:中文名"`
	Table   string `json:"tableName" gorm:"column:table_name;size:128;comment:落库表名"`
	Remark  string `json:"remark" gorm:"size:255"`
	Schema  string `json:"schema" gorm:"type:text;comment:最新 FDE Form Schema(JSON)"`
	Version int    `json:"version" gorm:"default:1"`
}

func (BizObject) TableName() string { return "sys_biz_objects" }

// BizField 业务对象字段元数据
type BizField struct {
	Base
	ObjectID   uint   `json:"objectId" gorm:"index"`
	Name       string `json:"name" gorm:"size:128"`
	Title      string `json:"title" gorm:"size:128"`
	Type       string `json:"type" gorm:"size:32"`
	Required   bool   `json:"required" gorm:"default:false"`
	Searchable bool   `json:"searchable" gorm:"default:false"`
	InList     bool   `json:"inList" gorm:"default:false"`
	InForm     bool   `json:"inForm" gorm:"default:false"`
	UIRules    string `json:"uiRules" gorm:"type:text;comment:校验+UI 提示 JSON"`
	Relation   string `json:"relation" gorm:"type:text;comment:关系定义 JSON"`
	Sort       int    `json:"sort" gorm:"default:0"`
}

func (BizField) TableName() string { return "sys_biz_fields" }

// FormSchema 表单 schema 版本历史
type FormSchema struct {
	Base
	ObjectID  uint   `json:"objectId" gorm:"index"`
	Schema    string `json:"schema" gorm:"type:text"`
	Version   int    `json:"version" gorm:"default:1"`
	Published bool   `json:"published" gorm:"default:false"`
	CreatedBy uint   `json:"createdBy" gorm:"default:0"`
}

func (FormSchema) TableName() string { return "sys_form_schemas" }

// GenAudit 生成操作审计
type GenAudit struct {
	Base
	UserID     uint   `json:"userId" gorm:"index"`
	Tool       string `json:"tool" gorm:"size:64"`
	Action     string `json:"action" gorm:"size:32;comment:dry_run|apply"`
	ObjectKey  string `json:"objectKey" gorm:"size:64"`
	BatchID    string `json:"batchId" gorm:"size:64;index;comment:dry-run 批次号"`
	Status     string `json:"status" gorm:"size:32;default:pending;comment:pending|applied|rolled_back"`
	ArgsHash   string `json:"argsHash" gorm:"size:128"`
	Result     string `json:"result" gorm:"type:text"`
	Files      string `json:"files" gorm:"type:text"`
	SnapshotID string `json:"snapshotId" gorm:"size:128"`
}

func (GenAudit) TableName() string { return "sys_gen_audits" }
