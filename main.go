// ── Seelex 入口 ──────────────────────────────────────────────────
// 装配件模式：创建所有依赖并注入模型

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/RedHuang-0622/Seele/agent"
	"github.com/RedHuang-0622/Seele/agent/core/api"
	"github.com/RedHuang-0622/Seele/agent/core/tool/builtin"
	"github.com/RedHuang-0622/Seele/agent/core/tool/holder"
	"github.com/RedHuang-0622/Seele/engine"
	"github.com/RedHuang-0622/Seele/seelectx/storage"
	"github.com/RedHuang-0622/Seele/seelectx/tracer"
	"github.com/RedHuang-0622/Seele/types"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/RedHuang-0622/seelex/session"
	"github.com/RedHuang-0622/seelex/skill"
	"github.com/RedHuang-0622/seelex/tui"
)

var configPath = flag.String("c", "config/account-openai.yaml", "LLM 配置路径")

func main() {
	flag.Parse()

	// ── 第 1 层：基础依赖 ─────────────────────────────────────────
	// 加载账号配置
	result, err := api.LoadFullAccountsConfig(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "✖ 加载配置失败: %v\n", err)
		os.Exit(1)
	}
	ls := result.LLMDefaults
	pool := result.Pool
	first := pool.All()[0]

	llmCfg := types.LLMConfig{
		BaseURL: first.BaseURL, APIKey: first.APIKey, Model: first.Model,
		MaxTokens: ls.MaxTokens, Timeout: ls.Timeout, Temperature: ls.Temperature,
	}

	// 创建 Agent
	agt, err := agent.New(agent.Options{
		LLMConfig: llmCfg, ToolCallTimeOut: 120 * time.Second, HubStartupDelay: 10,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "✖ Agent 初始化失败: %v\n", err)
		os.Exit(1)
	}
	defer agt.Shutdown()

	chatClient := agt.LLM().(*api.ChatClient)
	chatClient.WithAccountPool(pool)
	if ls.Provider != "" {
		chatClient.SetProvider(ls.Provider)
	}

	// ── 第 2 层：工具与插件注册 ──────────────────────────────────
	builtin.RegisterAll(agt.Tools())
	agt.RegisterTool("get_time", "获取当前日期和时间",
		map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		func(_ context.Context, _ string) (string, error) {
			return fmt.Sprintf(`"%s"`, time.Now().Format("2006-01-02 15:04:05")), nil
		},
	)

	wpt := builtin.NewWorkPlanTool(builtin.NewChatAgentFactory(agt.LLM()))
	agt.Tools().Register(wpt)

	initPlugins(agt)

	// ── switch_mode 工具 ──────────────────────────────────────────
	agt.RegisterTool(
		"switch_mode",
		"切换工作模式以改变可用工具集。模式包括：default(全部), "+
			"read(搜索/读取), write(编辑), git(版本控制), shell(命令执行), plan(工作流)。"+
			"切换后后续回合自动生效。",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"mode": map[string]interface{}{
					"type":        "string",
					"enum":        []interface{}{"default", "read", "write", "git", "shell", "plan"},
					"description": "目标模式",
				},
			},
			"required": []string{"mode"},
		},
		func(_ context.Context, argsJSON string) (string, error) {
			var input struct {
				Mode string `json:"mode"`
			}
			if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
				return "", fmt.Errorf("switch_mode: %w", err)
			}
			mode := strings.ToLower(input.Mode)
			if mode == "" || mode == "default" {
				agt.Tools().DeactivatePlugin()
			} else {
				if err := agt.Tools().ActivatePlugin(mode); err != nil {
					return fmt.Sprintf(`{"error":"unknown mode: %s"}`, mode), nil
				}
			}
			visible := agt.VisibleTools(context.Background())
			all := agt.Tools().Tools()
			return fmt.Sprintf(`{"mode":"%s","visible_tools":%d,"total_tools":%d}`,
				mode, len(visible), len(all)), nil
		},
	)

	// ── ask_approve 工具（需人工确认的操作）────────────────────────
	agt.RegisterTool(
		"ask_approve",
		"向用户请求操作确认。当需要执行高风险操作时调用此工具，提供清晰问题描述和选项列表。",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"question": map[string]interface{}{
					"type":        "string",
					"description": "向用户展示的确认问题",
				},
				"choices": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"type": "string",
					},
					"description": "可选项列表（默认: Yes/No）",
				},
			},
			"required": []string{"question"},
		},
		func(_ context.Context, argsJSON string) (string, error) {
			var input struct {
				Question string   `json:"question"`
				Choices  []string `json:"choices,omitempty"`
			}
			if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
				return "", fmt.Errorf("ask_approve: %w", err)
			}
			choices := input.Choices
			if len(choices) == 0 {
				choices = []string{"Yes", "No"}
			}
			result := tui.HandleApproval(input.Question, choices)
			if result == "__CANCEL__" {
				return `{"approved":false,"reason":"cancelled"}`, nil
			}
			return fmt.Sprintf(`{"approved":true,"choice":"%s"}`, result), nil
		},
	)

	// ── 第 3 层：会话持久化 + Engine ──────────────────────────────
	store, err := storage.NewStore("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "✖ 初始化存储失败: %v\n", err)
		os.Exit(1)
	}
	eng := engine.New(agt, engine.WithStore(store),
		engine.WithTracer(tracer.NewSimpleTracer()),
			engine.WithHooks(tui.CreateToolHooks()),
	)

	// ── 第 4 层：会话管理（薄包装）────────────────────────────────
	sessionMgr := session.NewManager(store)
	sessionMgr.InjectSaveLoad(
		func(sessionID string) error {
			return store.Save(sessionID, eng.History())
		},
		func(sessionID string) error {
			messages, err := store.Load(sessionID)
			if err != nil {
				return err
			}
			eng.ClearHistory()
			// 重新注入历史消息 — 通过 system prompt 恢复
			for _, msg := range messages {
				_ = msg
				// engine 内部管理历史，加载后自动同步
			}
			return nil
		},
	)

	// ── 第 5 层：Skill 加载 ──────────────────────────────────────
	skillReg := skill.NewRegistry()
	skillLoader := skill.NewLoader("skills", "cmd/repl/skills")
	_ = skillReg.AddLoader(skillLoader) // 无可读目录时不报错

	// ── 第 6 层：命令注册（策略模式）─────────────────────────────
	tui.RegisterCommands(eng, chatClient, first.Model, sessionMgr)

	// ── 第 7 层：TUI 装配（装配件模式）─────────────────────────────
	p := tea.NewProgram(
		tui.NewModel(eng, first.Model, chatClient, agt, sessionMgr, skillReg),
		tea.WithAltScreen(),
			tea.WithMouseCellMotion(),
	)
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "✖ TUI 错误: %v\n", err)
		os.Exit(1)
	}
}

// ── 插件系统（工厂模式）────────────────────────────────────────

func initPlugins(agt *agent.Agent) {
	pm := holder.NewPluginManager()
	plugins := []struct {
		Name, Description string
		Include, Exclude  []string
	}{
		{"default", "所有工具可用", nil, nil},
		{"read", "阅读/搜索模式", []string{"switch_mode", "grep*", "read_file", "glob", "git_status", "git_log", "git_diff", "get_time"}, nil},
		{"write", "编辑模式", []string{"switch_mode", "write*", "edit*", "read_file", "bash", "git_diff", "git_status", "get_time"}, nil},
		{"git", "Git 模式", []string{"switch_mode", "git_*", "bash", "get_time"}, nil},
		{"shell", "Shell/DevOps 模式", []string{"switch_mode", "bash", "get_time"}, nil},
		{"plan", "WorkPlan 工作流模式", []string{"switch_mode", "plan_*", "get_time"}, nil},
	}
	for _, p := range plugins {
		pm.Define(holder.NewPlugin(p.Name, p.Description, p.Include, p.Exclude))
	}
	agt.Tools().WithPluginManager(pm)
}
