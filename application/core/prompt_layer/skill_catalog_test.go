package prompt_layer_test

import (
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract"
	"github.com/RedHuang-0622/seelex/application/core/internal/state"
	pl "github.com/RedHuang-0622/seelex/application/core/prompt_layer"
	"github.com/RedHuang-0622/seelex/application/core/task_context"
	"github.com/RedHuang-0622/seelex/application/model"
	"github.com/RedHuang-0622/seelex/application/prompt"
)

// ── 桩：只读 Skill 端口（按当前插件返回技能表，模拟 skill.Registry 隔离） ──
type catalogSkillsStub struct{ all func() []model.SkillInfo }

func (s catalogSkillsStub) All() []model.SkillInfo {
	if s.all == nil {
		return nil
	}
	return s.all()
}
func (catalogSkillsStub) Get(string) (model.SkillInfo, bool) { return model.SkillInfo{}, false }

// ── 桩：无任务/无 plan 的 TaskContextView（只测目录段自身） ──
type catalogTasksStub struct {
	state *task_context.TaskExecutionState
}

func (s catalogTasksStub) CurrentTaskExecution() *task_context.TaskExecutionState { return s.state }
func (s catalogTasksStub) CurrentTaskExecutionFor(string) *task_context.TaskExecutionState {
	return s.state
}
func (catalogTasksStub) ActivePlanID() string          { return "" }
func (catalogTasksStub) ActivePlanIDFor(string) string { return "" }
func (catalogTasksStub) PlanSequence() uint64          { return 0 }
func (catalogTasksStub) PlanSequenceFor(string) uint64 { return 0 }

func newCatalogCoordinator(t testing.TB, skills contract.SkillPort, tasks *task_context.TaskExecutionState) *pl.Coordinator {
	t.Helper()
	core := state.New(contract.Dependencies{Skills: skills})
	ps := prompt.NewPromptStack()
	ps.Push("instructions", "instructions", "BASE-INSTRUCTIONS")
	return pl.NewCoordinator(pl.Deps{Core: core, PromptStack: ps, Tasks: catalogTasksStub{state: tasks}})
}

func systemPromptFor(t testing.TB, c *pl.Coordinator) string {
	t.Helper()
	return c.SystemPromptForActiveTaskLockedFor("session-1")
}

// TestRenderSkillCatalogEmpty：无技能表 → 不占 system 字节（返回空）。
func TestRenderSkillCatalogEmpty(t *testing.T) {
	if got := pl.RenderSkillCatalog(nil); got != "" {
		t.Fatalf("nil skills must render empty, got %q", got)
	}
	if got := pl.RenderSkillCatalog([]model.SkillInfo{}); got != "" {
		t.Fatalf("empty skills must render empty, got %q", got)
	}
	if got := pl.RenderSkillCatalog([]model.SkillInfo{{Name: "  "}}); got != "" {
		t.Fatalf("blank-name skills must render empty, got %q", got)
	}
}

// TestRenderSkillCatalogStableAndSorted：字节稳定（同输入同输出）+ 按 name 排序。
func TestRenderSkillCatalogStableAndSorted(t *testing.T) {
	input := []model.SkillInfo{
		{Name: "zebra", Description: "z desc"},
		{Name: "alpha", Description: "a desc"},
		{Name: "mid", Description: ""},
	}
	first := pl.RenderSkillCatalog(input)
	second := pl.RenderSkillCatalog([]model.SkillInfo{
		{Name: "alpha", Description: "a desc"},
		{Name: "zebra", Description: "z desc"},
		{Name: "mid", Description: ""},
	})
	if first != second {
		t.Fatalf("catalog must be byte-stable for identical content:\n%q\nvs\n%q", first, second)
	}
	if !strings.Contains(first, "## Available Skills") {
		t.Fatalf("missing catalog header: %q", first)
	}
	idxAlpha := strings.Index(first, "- alpha: a desc")
	idxMid := strings.Index(first, "- mid")
	idxZebra := strings.Index(first, "- zebra: z desc")
	if idxAlpha < 0 || idxMid < 0 || idxZebra < 0 {
		t.Fatalf("catalog must list every skill: %q", first)
	}
	if !(idxAlpha < idxMid && idxMid < idxZebra) {
		t.Fatalf("catalog must be sorted by name: %q", first)
	}
	if strings.Count(first, "## Available Skills") != 1 {
		t.Fatalf("catalog header must appear exactly once: %q", first)
	}
}

// TestRenderSkillCatalogNeverLeaksPrompt：目录只含 name/description，指令正文
// （Prompt）绝不进入目录段（发现不泄全文）。
func TestRenderSkillCatalogNeverLeaksPrompt(t *testing.T) {
	out := pl.RenderSkillCatalog([]model.SkillInfo{
		{Name: "review", Description: "review code", Prompt: "TOP-SECRET review prompt body"},
	})
	if strings.Contains(out, "TOP-SECRET") {
		t.Fatalf("catalog must not embed skill Prompt body: %q", out)
	}
	if !strings.Contains(out, "- review: review code") {
		t.Fatalf("catalog must carry name+description: %q", out)
	}
	// 空 description → 仅 "- <name>"。
	solo := pl.RenderSkillCatalog([]model.SkillInfo{{Name: "solo"}})
	if !strings.Contains(solo, "- solo") || strings.Contains(solo, "- solo:") {
		t.Fatalf("empty-description line shape wrong: %q", solo)
	}
}

// TestCoordinatorInjectsPassiveCatalog：目录段自动进 system prompt（模型零
// 调用），且位置在 base（instructions）之后、激活技能正文之前；无激活技能
// 时不出现 "Trusted Active Skill" 段。
func TestCoordinatorInjectsPassiveCatalog(t *testing.T) {
	c := newCatalogCoordinator(t, catalogSkillsStub{all: func() []model.SkillInfo {
		return []model.SkillInfo{
			{Name: "review", Description: "review code"},
			{Name: "plan", Description: "plan tasks"},
		}
	}}, nil)
	promptText := systemPromptFor(t, c)
	if !strings.Contains(promptText, "BASE-INSTRUCTIONS") {
		t.Fatalf("base must remain first: %q", promptText)
	}
	if !strings.Contains(promptText, "## Available Skills") ||
		!strings.Contains(promptText, "- plan: plan tasks") ||
		!strings.Contains(promptText, "- review: review code") {
		t.Fatalf("catalog section missing: %q", promptText)
	}
	if strings.Contains(promptText, "## Trusted Active Skill") {
		t.Fatalf("no active skill → trusted section must not appear: %q", promptText)
	}
	if strings.Contains(promptText, "review prompt") {
		t.Fatalf("unactivated skill body must not leak: %q", promptText)
	}
}

// TestCoordinatorSystemOmitsActiveSkillBody：激活技能存在时，system 也只含
// 目录段，绝不包含技能正文（Trusted Active Skill 段已移出 system；正文在
// 激活时作为 internal 事件 append 进 transcript，跟随对话 append-only——
// 见 task_context.ensureActiveSkillEventsLocked 与 plan_transcript.go；
// 目录段仍须跟在 base 之后）。
func TestCoordinatorSystemOmitsActiveSkillBody(t *testing.T) {
	task := &task_context.TaskExecutionState{
		RequestID: "req-1",
		TrustedSkillLayers: []prompt.PromptLayer{
			{Kind: "skill", Name: "review", Text: "review prompt body"},
		},
	}
	c := newCatalogCoordinator(t, catalogSkillsStub{all: func() []model.SkillInfo {
		return []model.SkillInfo{{Name: "review", Description: "review code"}}
	}}, task)
	promptText := systemPromptFor(t, c)
	baseAt := strings.Index(promptText, "BASE-INSTRUCTIONS")
	catalogAt := strings.Index(promptText, "## Available Skills")
	if baseAt < 0 || catalogAt < 0 {
		t.Fatalf("missing segments: %q", promptText)
	}
	if !(baseAt < catalogAt) {
		t.Fatalf("order must be base < catalog: %q", promptText)
	}
	if strings.Contains(promptText, "## Trusted Active Skill") {
		t.Fatalf("activated skill body must NOT be embedded in system: %q", promptText)
	}
	if strings.Contains(promptText, "review prompt body") {
		t.Fatalf("activated skill body must NOT be embedded in system: %q", promptText)
	}
}

// TestCoordinatorCatalogFollowsPluginSwitch：目录内容随"当前插件技能表"变化
// 而切换（插件激活 → 新目录；切回 → 与旧目录字节一致，前缀缓存友好）。
func TestCoordinatorCatalogFollowsPluginSwitch(t *testing.T) {
	current := []model.SkillInfo{{Name: "plan", Description: "default plan skill"}}
	c := newCatalogCoordinator(t, catalogSkillsStub{all: func() []model.SkillInfo {
		return current
	}}, nil)
	before := systemPromptFor(t, c)

	current = []model.SkillInfo{{Name: "cad", Description: "freecad skill"}}
	after := systemPromptFor(t, c)
	if before == after {
		t.Fatal("catalog must change when the active plugin skill set changes")
	}
	if !strings.Contains(after, "- cad: freecad skill") || strings.Contains(after, "- plan:") {
		t.Fatalf("switch must replace the catalog: %q", after)
	}

	current = []model.SkillInfo{{Name: "plan", Description: "default plan skill"}}
	back := systemPromptFor(t, c)
	if back != before {
		t.Fatalf("identical plugin skill set must reproduce identical catalog bytes:\n%q\nvs\n%q", before, back)
	}
}

// TestCoordinatorEmptyCatalogOmitsSection：插件无技能 → 目录段整体不出现，
// 不给固定开销。
func TestCoordinatorEmptyCatalogOmitsSection(t *testing.T) {
	c := newCatalogCoordinator(t, catalogSkillsStub{all: func() []model.SkillInfo { return nil }}, nil)
	promptText := systemPromptFor(t, c)
	if strings.Contains(promptText, "## Available Skills") {
		t.Fatalf("empty catalog must not render a section: %q", promptText)
	}
}
