// Seelex assembles the Seele agent framework with product-level plugins,
// skills, session storage, and the terminal UI.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/RedHuang-0622/Seele/engine"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/RedHuang-0622/seelex/plugin"
	"github.com/RedHuang-0622/seelex/seelebridge"
	"github.com/RedHuang-0622/seelex/session"
	"github.com/RedHuang-0622/seelex/skill"
	"github.com/RedHuang-0622/seelex/tui"
	tuiApprove "github.com/RedHuang-0622/seelex/tui/approve"
)

var (
	configPath   = flag.String("c", "config/account-openai.yaml", "LLM 配置路径")
	storePath    = flag.String("store", "", "持久化存储路径（空=当前目录）")
	skillsPaths  = flag.String("skills", "skills,cmd/repl/skills", "Skill 加载路径（逗号分隔）")
	pluginsPaths = flag.String("plugins", "plugins", "Plugin 加载路径（逗号分隔）")
)

func main() {
	flag.Parse()
	runtime := initRuntime()
	defer runtime.Shutdown()

	runtime.RegisterBuiltins()
	skillRegistry := initSkillSystem()
	pluginManager := initPluginSystem(runtime, skillRegistry)
	store := initStore()
	eng := initEngine(runtime, store)
	registerProductTools(runtime, pluginManager, eng)
	activateDefaultPlugin(pluginManager, eng)
	sessionManager := initSessionManager(store, eng)
	model := initTUI(eng, runtime, pluginManager, sessionManager, skillRegistry)
	initCommands(eng, runtime, pluginManager, sessionManager, skillRegistry, model)
	startTUI(model)
}

func initRuntime() *seelebridge.Runtime {
	runtime, err := seelebridge.NewRuntime(seelebridge.RuntimeConfig{
		AccountsPath: *configPath, ToolCallTimeout: 120 * time.Second,
	})
	if err != nil {
		fatalf("初始化 Seele Runtime 失败: %v", err)
	}
	return runtime
}

func initSkillSystem() *skill.Registry {
	registry := skill.NewRegistry()
	loader := skill.NewLoader(splitPaths(*skillsPaths)...)
	if err := registry.AddLoader(loader); err != nil {
		fmt.Fprintf(os.Stderr, "⚠ Skill 加载警告: %v\n", err)
	}
	return registry
}

func initPluginSystem(
	runtime *seelebridge.Runtime,
	skills *skill.Registry,
) *plugin.Manager {
	loader := plugin.NewLoader(splitPaths(*pluginsPaths)...)
	manager := plugin.NewManager(loader, runtime, runtime, skills)
	if err := manager.Load(); err != nil {
		fatalf("加载 Plugin 失败: %v", err)
	}
	return manager
}

func activateDefaultPlugin(manager *plugin.Manager, eng *engine.Engine) {
	if _, err := pluginByName(manager.All(), "default"); err != nil {
		return
	}
	if err := manager.Activate(context.Background(), "default"); err != nil {
		fatalf("激活 default Plugin 失败: %v", err)
	}
	applyPluginPrompt(eng, manager)
}

func registerProductTools(runtime *seelebridge.Runtime, plugins *plugin.Manager, eng *engine.Engine) {
	registerTimeTool(runtime)
	registerPluginSwitchTools(runtime, plugins, eng)
	registerAskApprove(runtime)
}

func registerTimeTool(runtime *seelebridge.Runtime) {
	runtime.RegisterTool(
		"get_time",
		"获取当前日期和时间",
		map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		func(context.Context, string) (string, error) {
			return fmt.Sprintf(`"%s"`, time.Now().Format("2006-01-02 15:04:05")), nil
		},
	)
}

func registerPluginSwitchTools(
	runtime *seelebridge.Runtime,
	plugins *plugin.Manager,
	eng *engine.Engine,
) {
	names := make([]interface{}, 0, len(plugins.All())+1)
	for _, p := range plugins.All() {
		names = append(names, p.Name)
	}
	names = append(names, "off")
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"plugin": map[string]interface{}{
				"type": "string", "enum": names, "description": "目标插件",
			},
		},
		"required": []string{"plugin"},
	}
	handler := func(ctx context.Context, argsJSON string) (string, error) {
		var input struct {
			Plugin string `json:"plugin"`
			Mode   string `json:"mode"`
		}
		if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
			return "", fmt.Errorf("switch_plugin: %w", err)
		}
		name := strings.ToLower(strings.TrimSpace(input.Plugin))
		if name == "" {
			name = strings.ToLower(strings.TrimSpace(input.Mode))
		}
		if name == "off" || name == "none" || name == "" {
			if err := plugins.Deactivate(ctx); err != nil {
				return "", err
			}
		} else if err := plugins.Activate(ctx, name); err != nil {
			return "", err
		}
		applyPluginPrompt(eng, plugins)
		result := map[string]interface{}{
			"plugin":        runtime.ActivePlugin(),
			"visible_tools": len(runtime.VisibleTools(ctx)),
			"total_tools":   len(runtime.AllTools()),
		}
		encoded, err := json.Marshal(result)
		return string(encoded), err
	}
	runtime.RegisterTool("switch_plugin", "切换 Seelex Plugin 及其工具、Skill 和 MCP", schema, handler)

	legacySchema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"mode": map[string]interface{}{
				"type": "string", "enum": names, "description": "目标插件（兼容 mode 名称）",
			},
		},
		"required": []string{"mode"},
	}
	runtime.RegisterTool("switch_mode", "兼容工具：等价于 switch_plugin", legacySchema, handler)
}

func applyPluginPrompt(eng *engine.Engine, plugins *plugin.Manager) {
	current, ok := plugins.Current()
	if !ok {
		eng.SetSystemPrompt("")
		return
	}
	eng.SetSystemPrompt(strings.TrimSpace(current.Prompt))
}

func registerAskApprove(runtime *seelebridge.Runtime) {
	runtime.RegisterTool(
		"ask_approve",
		"向用户请求操作确认。当需要执行高风险操作时调用此工具。",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"question": map[string]interface{}{"type": "string"},
				"choices": map[string]interface{}{
					"type": "array", "items": map[string]interface{}{"type": "string"},
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
			options := make([]tuiApprove.ChoiceOption, len(choices))
			for i, choice := range choices {
				options[i] = tuiApprove.ChoiceOption{Key: choice, Label: choice}
				for _, builtinChoice := range tuiApprove.Choices(choice) {
					if builtinChoice.Key == choice {
						options[i] = builtinChoice
						break
					}
				}
			}
			question := tuiApprove.Question{
				ID:      fmt.Sprintf("ask_%d", time.Now().UnixNano()),
				Content: input.Question, Options: options,
			}
			result := tuiApprove.Ask(question, "low", "", "")
			if result == "__CANCEL__" {
				return `{"approved":false,"reason":"cancelled"}`, nil
			}
			encoded, err := json.Marshal(map[string]interface{}{"approved": true, "choice": result})
			return string(encoded), err
		},
	)
}

func initStore() *seelebridge.SessionStore {
	store, err := seelebridge.NewSessionStore(*storePath)
	if err != nil {
		fatalf("初始化存储失败: %v", err)
	}
	return store
}

func initEngine(runtime *seelebridge.Runtime, store *seelebridge.SessionStore) *engine.Engine {
	return engine.New(
		runtime.Agent(),
		engine.WithStore(store.FrameworkStore()),
		engine.WithTracer(seelebridge.NewTracer()),
		engine.WithHooks(tui.CreateToolHooks()),
	)
}

func initSessionManager(store *seelebridge.SessionStore, eng *engine.Engine) *session.Manager {
	manager := session.NewManager(store)
	manager.InjectSaveLoad(
		func(sessionID string) error { return store.Save(sessionID, eng.History()) },
		func(sessionID string) error {
			if _, err := store.Load(sessionID); err != nil {
				return err
			}
			return fmt.Errorf("session resume requires engine history replacement support")
		},
	)
	return manager
}

func initTUI(
	eng *engine.Engine,
	runtime *seelebridge.Runtime,
	plugins *plugin.Manager,
	sessions *session.Manager,
	skills *skill.Registry,
) tui.Model {
	return tui.NewModel(eng, runtime.Model(), runtime, plugins, sessions, skills)
}

func initCommands(
	eng *engine.Engine,
	runtime *seelebridge.Runtime,
	plugins *plugin.Manager,
	sessions *session.Manager,
	skills *skill.Registry,
	model tui.Model,
) {
	tui.RegisterCommands(eng, runtime, runtime.Model(), sessions, skills, nil, plugins)
	tui.SyncCommandSuggestions(model.SuggEng)
}

func startTUI(model tui.Model) {
	program := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := program.Run(); err != nil {
		fatalf("TUI 错误: %v", err)
	}
}

func splitPaths(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			result = append(result, part)
		}
	}
	return result
}

func pluginByName(plugins []plugin.Plugin, name string) (plugin.Plugin, error) {
	for _, p := range plugins {
		if p.Name == name {
			return p, nil
		}
	}
	return plugin.Plugin{}, fmt.Errorf("plugin %q not found", name)
}

func fatalf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "✖ "+format+"\n", args...)
	os.Exit(1)
}
