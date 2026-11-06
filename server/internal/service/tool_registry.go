package service

import "context"

// AiToolFunc 是 sys_ai_tools 名称对应的真实执行函数, args 为模型传入的原始 JSON 参数串
type AiToolFunc func(ctx context.Context, args string) (any, error)

var aiToolRegistry = map[string]AiToolFunc{}

// RegisterAiToolFunc 注册 AI 工具执行函数(代码生成器或扩展模块调用)
func RegisterAiToolFunc(name string, fn AiToolFunc) {
	if name != "" && fn != nil {
		aiToolRegistry[name] = fn
	}
}

func lookupAiToolFunc(name string) (AiToolFunc, bool) {
	fn, ok := aiToolRegistry[name]
	return fn, ok
}

func init() {
	// 内置示例工具: 与 sys_ai_tools 中名为 get_weather 的工具对应, 演示真实执行链路
	RegisterAiToolFunc("get_weather", func(ctx context.Context, args string) (any, error) {
		return map[string]any{"city": "北京", "weather": "晴", "temperature": 26}, nil
	})
	// 表单生成器 P1: 生成 FDE Form Schema
	RegisterAiToolFunc("generate_form_schema", GenerateFormSchema)
	// 代码生成器 P3: 统一业务对象模型生成 CRUD 与两段式确认写盘
	RegisterAiToolFunc("generate_business_code", GenerateBusinessCode)
	RegisterAiToolFunc("apply_files", ApplyFiles)
}
