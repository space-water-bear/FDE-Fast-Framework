package initialize

import (
	_ "embed"

	"fde-admin/internal/global"
	"fde-admin/internal/model"
)

//go:embed skills/fde-page-builder.md
var pageBuilderPrompt string

//go:embed skills/fde-business-logic.md
var businessLogicPrompt string

func ensureGenerationSkills() error {
	for _, skill := range []model.AiSkill{
		{
			Name: "fde-page-builder", Description: "生成或修改后台列表、表单、详情与统计组合页面，复用 TablePage、FormRender、Element Plus 和 ECharts。",
			Prompt: pageBuilderPrompt, Remark: "内置页面生成技能；与业务逻辑技能按需绑定",
		},
		{
			Name: "fde-business-logic", Description: "实现业务校验、接口编排、状态流转、权限归属及事务规则，协同页面与统计组件。",
			Prompt: businessLogicPrompt, Remark: "内置业务逻辑技能；与页面生成技能按需绑定",
		},
	} {
		if err := ensureBuiltinAiSkill(skill); err != nil {
			return err
		}
	}
	return nil
}

// Install missing skills without changing existing prompts or agent bindings.
func ensureBuiltinAiSkill(skill model.AiSkill) error {
	var count int64
	if err := global.DB.Model(&model.AiSkill{}).Where("name = ?", skill.Name).Count(&count).Error; err != nil {
		return err
	}
	if count != 0 {
		return nil
	}
	return global.DB.Create(&skill).Error
}
