package initialize

import (
	_ "embed"

	"fde-admin/internal/model"
)

//go:embed skills/fde-chart-builder.md
var chartBuilderPrompt string

func ensureChartDemoMenu() error {
	return ensureSystemMenu("chart-demo", "SystemChartDemo", "demo/charts/index", "图表组件示例", "DataAnalysis", 11, []struct {
		title  string
		api    string
		method string
	}{{"图表示例-查询", "/api/demo/charts/sales", "GET"}})
}

func ensureChartBuilderSkill() error {
	return ensureBuiltinAiSkill(model.AiSkill{
		Name: "fde-chart-builder", Description: "生成或修改 Apache ECharts 统计图表、聚合接口和筛选交互，适用于趋势、对比、构成分析。",
		Prompt: chartBuilderPrompt, Remark: "内置图表生成技能；在智能体配置中按需绑定",
	})
}
