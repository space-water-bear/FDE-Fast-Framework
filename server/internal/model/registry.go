package model

// tables 代码生成器产出的模型注册表, 由生成文件的 init() 自动注册
var tables []any

func RegisterTable(m any) {
	tables = append(tables, m)
}

func RegistryTables() []any {
	return tables
}
