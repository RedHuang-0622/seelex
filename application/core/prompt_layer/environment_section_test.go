package prompt_layer_test

import (
	"context"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract"
	"github.com/RedHuang-0622/seelex/application/core/internal/state"
	pl "github.com/RedHuang-0622/seelex/application/core/prompt_layer"
	"github.com/RedHuang-0622/seelex/application/model"
	"github.com/RedHuang-0622/seelex/application/prompt"
	"github.com/RedHuang-0622/seelex/internal/testutil"
)

// TestEnvironmentSectionIsInjectedAndStable：**执行面的环境事实**（这台机器上 bash 工具落到
// 哪个壳）必须进系统提示词（2026-10-15 审计 D4）；空值不占字节，非空时逐轮稳定（前缀缓存）。
//
// 这一段由组合根按契约注入（`contract.Dependencies.EnvironmentSection`），应用层不反向依赖
// 执行面：它只负责"有就推、没有就不推"。
func TestEnvironmentSectionIsInjectedAndStable(t *testing.T) {
	newCoordinator := func(environment string) *pl.Coordinator {
		engine := &environmentPromptEngine{}
		core := state.New(contract.Dependencies{Engine: engine, Plugins: environmentPluginStub{}})
		ps := prompt.NewPromptStack()
		return pl.NewCoordinator(pl.Deps{
			Core: core, PromptStack: ps, Tasks: catalogTasksStub{},
			EffortManager: prompt.NewEffortManager(ps, engine),
			Environment:   environment,
		})
	}

	const section = "## Environment\n\n- Host shell: **Windows PowerShell 5.x**"
	withSection := renderSystemPrompt(newCoordinator(section))
	if !strings.Contains(withSection, "Windows PowerShell 5.x") {
		t.Fatalf("注入的环境段必须出现在系统提示里：\n%s", withSection)
	}

	// 空值：这一格不出现（也不该留下空标题）。
	empty := renderSystemPrompt(newCoordinator("   "))
	if strings.Contains(empty, "## Environment") {
		t.Fatalf("没有环境事实时不该推这一段：\n%s", empty)
	}

	// 稳定：同一份事实两次组装逐字节相同（系统提示是稳定前缀）。
	again := renderSystemPrompt(newCoordinator(section))
	if again != withSection {
		t.Fatalf("环境段必须是稳定前缀（两次组装不一致）")
	}
}

// renderSystemPrompt 走**生产同一条路**：先 BuildSystemPrompt（推段 + 同步引擎），再读回。
func renderSystemPrompt(coordinator *pl.Coordinator) string {
	coordinator.BuildSystemPrompt()
	return coordinator.SystemPromptForActiveTaskLockedFor("session-1")
}

// environmentPromptEngine 只实现本用例真正走到的那两条同步口（其余继承 panic 底座）。
type environmentPromptEngine struct {
	testutil.EmbeddedChatEngine
	prompt string
}

func (engine *environmentPromptEngine) SetSystemPrompt(promptText string) { engine.prompt = promptText }
func (engine *environmentPromptEngine) SetMaxLoops(int)                   {}

// environmentPluginStub 报告"没有装 agent 插件"（本用例只关心环境段）。
type environmentPluginStub struct{}

func (environmentPluginStub) All() []model.PluginInfo { return nil }
func (environmentPluginStub) Activate(context.Context, string) error {
	return nil
}
func (environmentPluginStub) Deactivate(context.Context) error { return nil }
func (environmentPluginStub) Current() (model.PluginInfo, bool) {
	return model.PluginInfo{}, false
}
