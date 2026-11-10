package initialize

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.uber.org/zap"

	"fde-admin/internal/global"
	"fde-admin/internal/model"
	"fde-admin/internal/service"
)

func setupGenerationSkillDB(t *testing.T) {
	t.Helper()
	setupSeedDB(t)
	sqlDB, err := global.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	global.Logger = zap.NewNop()
	if err := global.DB.AutoMigrate(
		&model.AiSkill{}, &model.AiAgent{}, &model.AiProvider{}, &model.AiMcpServer{},
		&model.AiSession{}, &model.AiMessage{}, &model.AiUsage{},
	); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureGenerationSkillsInstallsMissingAndPreservesBindings(t *testing.T) {
	setupGenerationSkillDB(t)
	custom := model.AiSkill{Name: "customer-skill", Prompt: "客户现有规范"}
	if err := global.DB.Create(&custom).Error; err != nil {
		t.Fatal(err)
	}
	agent := model.AiAgent{Name: "现有智能体", SystemPrompt: "已有系统说明", Skills: []model.AiSkill{custom}, Enabled: true}
	if err := global.DB.Create(&agent).Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := ensureGenerationSkills(); err != nil {
			t.Fatal(err)
		}
		if err := ensureChartBuilderSkill(); err != nil {
			t.Fatal(err)
		}
	}
	skills, err := service.ListAiSkills("")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"fde-page-builder": pageBuilderPrompt, "fde-business-logic": businessLogicPrompt,
		"fde-chart-builder": chartBuilderPrompt, "customer-skill": custom.Prompt,
	}
	if len(skills) != len(want) {
		t.Fatalf("missing or duplicated skills: %+v", skills)
	}
	for _, skill := range skills {
		prompt, found := want[skill.Name]
		if !found || prompt == "" || skill.Prompt != prompt {
			t.Fatalf("incorrect installed skill: %s", skill.Name)
		}
	}
	if err := global.DB.Preload("Skills").First(&agent, agent.ID).Error; err != nil {
		t.Fatal(err)
	}
	if agent.SystemPrompt != "已有系统说明" || len(agent.Skills) != 1 || agent.Skills[0].ID != custom.ID {
		t.Fatalf("initialization changed existing agent configuration: %+v", agent)
	}
}

func TestEnsureGenerationSkillsPreservesCustomizedSkills(t *testing.T) {
	for _, name := range []string{"fde-page-builder", "fde-business-logic"} {
		t.Run(name, func(t *testing.T) {
			setupGenerationSkillDB(t)
			custom := model.AiSkill{Name: name, Description: "客户说明", Prompt: "人工修改后的规范", Remark: "客户备注"}
			if err := global.DB.Create(&custom).Error; err != nil {
				t.Fatal(err)
			}
			if err := ensureGenerationSkills(); err != nil {
				t.Fatal(err)
			}
			if err := ensureGenerationSkills(); err != nil {
				t.Fatal(err)
			}
			var rows []model.AiSkill
			if err := global.DB.Where("name = ?", name).Find(&rows).Error; err != nil {
				t.Fatal(err)
			}
			if len(rows) != 1 || rows[0].ID != custom.ID || rows[0].Prompt != custom.Prompt || rows[0].Description != custom.Description || rows[0].Remark != custom.Remark {
				t.Fatalf("overwrote or duplicated customized skill: %+v", rows)
			}
		})
	}
}

func TestGenerationSkillsReachOnlyBoundAgentModelRequests(t *testing.T) {
	setupGenerationSkillDB(t)
	if err := ensureGenerationSkills(); err != nil {
		t.Fatal(err)
	}
	if err := ensureChartBuilderSkill(); err != nil {
		t.Fatal(err)
	}
	skills, err := service.ListAiSkills("fde-")
	if err != nil {
		t.Fatal(err)
	}
	var skillIDs []uint
	for _, skill := range skills {
		skillIDs = append(skillIDs, skill.ID)
	}
	requests := make(chan string, 2)
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		var request struct {
			Messages []struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var instructions strings.Builder
		for _, message := range request.Messages {
			if message.Role != "system" && message.Role != "developer" {
				continue
			}
			var text string
			if json.Unmarshal(message.Content, &text) == nil {
				instructions.WriteString(text)
				continue
			}
			var parts []struct {
				Text string `json:"text"`
			}
			if json.Unmarshal(message.Content, &parts) == nil {
				for _, part := range parts {
					instructions.WriteString(part.Text)
				}
			}
		}
		requests <- instructions.String()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"skill-test","object":"chat.completion","created":1,"model":"mock","choices":[{"index":0,"message":{"role":"assistant","content":"方案已生成，尚未执行代码变更"},"finish_reason":"stop"}]}`))
	}))
	defer gateway.Close()
	provider := model.AiProvider{Name: "技能测试网关", BaseURL: gateway.URL + "/v1", APIKey: "mock-key", APIMode: "chat", Model: "mock", Enabled: true}
	if err := global.DB.Create(&provider).Error; err != nil {
		t.Fatal(err)
	}
	if err := service.CreateAiAgent("组合开发助手", "原有系统提示词", provider.ID, 0.5, true, "", skillIDs, nil, nil); err != nil {
		t.Fatal(err)
	}
	agents, err := service.ListAiAgents("组合开发助手")
	if err != nil || len(agents) != 1 {
		t.Fatalf("load agent: count=%d err=%v", len(agents), err)
	}
	agent := agents[0]
	session, err := service.CreateAiSession("", agent.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ChatAi(session.ID, "生成含列表、表单、图表和提交规则的工单模块"); err != nil {
		t.Fatal(err)
	}
	boundPrompt := <-requests
	if !strings.Contains(boundPrompt, agent.SystemPrompt) {
		t.Fatal("existing instructions were lost")
	}
	for _, skill := range skills {
		if !strings.Contains(boundPrompt, strings.TrimSpace(skill.Prompt)) {
			t.Errorf("bound skill did not reach model request: %s", skill.Name)
		}
	}
	if err := service.UpdateAiAgent(agent.ID, agent.Name, agent.SystemPrompt, provider.ID, 0.5, true, "", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ChatAi(session.ID, "继续处理"); err != nil {
		t.Fatal(err)
	}
	unboundPrompt := <-requests
	if unboundPrompt != agent.SystemPrompt {
		t.Fatalf("unbound skills leaked into model instructions: %s", unboundPrompt)
	}
}
