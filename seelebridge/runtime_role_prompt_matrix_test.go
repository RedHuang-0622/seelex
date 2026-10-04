package seelebridge

// runtime_role_prompt_matrix_test.go — 补证 E：**非空集**下的 system prompt 每轮重设行为，
// 并把「空集 / 1 个 / 2 个 / 3 个插件」放进**同一张预期表**，由**同一个断言函数**跑。
//
// 缺口（为什么这一条必须补）：`runtime_role_turn.go` 的 runRoleRound 在进入回合闸门后
// **每轮重设** system prompt（`engine.SetSystemPrompt(spec.SystemPrompt)`），而这一轮的
// spec.SystemPrompt 是"提示词 + 装配集合的技能目录段"（`appendSkillCatalog`）。空集那一侧
// 已被钉住（目录段为空串 ⇒ 字节逐字不变），**非空集那一侧此前没被钉**：没人证明过
//   ① 第二轮及以后目录段**仍在**（没被"重设"成未加目录的那一份）；
//   ② 目录段**恰好出现一次**（每轮是"替换"而不是"读当前 prompt 再追加"⇒ 不随轮数增长，
//      每轮常驻的 system 字节不因多轮而膨胀）；
//   ③ 每轮被设置上去的**每一份** prompt 都与 `base + "\n\n" + catalog` 逐字节相等。
//
// 本用例钉的**不变式**（一句话）：
//
//	同一个角色会话上跑 N 轮（N=3），非空装配的每一轮 system 字节 == base + "\n\n" +
//	catalog，且目录段恰好出现一次、base 部分逐字未被改写；空装配的每一轮 system 字节
//	== base，且目录段出现 0 次。
//
// "每轮重设"因此被钉成**幂等替换**：不是"只第一轮带上目录"，也不是"每轮再加一遍"。
//
// 边界：本文件只加用例，不改产品代码。同里程碑的 wi-fix-core 会改
// seelebridge/runtime_role_plugins.go 的读数与失灵可见化——若本文件因它的改动变红，
// 那是证据（见交付正文里记的版本号）。

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/RedHuang-0622/Seele/types"

	"github.com/RedHuang-0622/seelex/seelebridge/tools"
	"github.com/RedHuang-0622/seelex/skill"
)

// promptMatrixCatalogHeader 是目录段标题（口径来自 application/core/prompt_layer/
// skill_catalog.go 的 skillCatalogHeader，同值；本文件只把它当**检测串**用）。
const promptMatrixCatalogHeader = "## Available Skills"

// promptMatrixSkillNote 是目录段尾"员工面纠正句"的稳定片段：目录段**重复**时它必然
// 也重复，用它做第二条检测串（比只用标题更难被"改写文案"骗过）。
const promptMatrixSkillNote = "cannot switch plugins"

// promptMatrixRoundCount 是每个用例在同一会话上跑的轮数：≥2 才有"第二轮及以后"可言，
// 取 3 是为了让"累积型退化"（每轮 +1 份目录）在第 3 轮必然与预期不等。
const promptMatrixRoundCount = 3

// promptMatrixCase 是这张表的一行：**装配数 × 预期**。
//
// wantCtx 为 nil 与 wantSkills 为空的组合就是"空集（继承宿主、目录不注入）"；有插件时
// 工具面必须**只做减法**（并集收窄），目录段必须带上这些技能。
type promptMatrixCase struct {
	name string
	// plugins 是这一轮 roleRoundSpec.Plugins（空集 = nil，即"没声明"）。
	plugins []string
	// wantCtx 是本轮 ctx 里的装配集合（空集 = nil，不是"空切片"）。
	wantCtx []string
	// wantVisible 是本轮**真正可见的工具面**（装配集合 ∩ 全量工具；宿主全局激活 cad）。
	wantVisible []string
	// wantSkills 是必须出现在目录段里的技能名（空 = 不该有目录段）。
	wantSkills []string
	// wantCatalog 是"这一轮该不该有目录段"的显式声明——不对称在这里被**声明**，
	// 而不是靠"没测到"。
	wantCatalog bool
}

// promptMatrixCases 是交付的那张表本体（顺序 = 空集 → 1 → 2 → 3 个插件，都在上限内）。
func promptMatrixCases() []promptMatrixCase {
	return []promptMatrixCase{
		{
			name:        "空集（继承宿主、目录不注入）",
			plugins:     nil,
			wantCtx:     nil,
			wantVisible: []string{"cad_draw"},
			wantSkills:  nil,
			wantCatalog: false,
		},
		{
			name:        "1 个插件（docs）",
			plugins:     []string{"docs"},
			wantCtx:     []string{"docs"},
			wantVisible: []string{"doc_read", "doc_edit"},
			wantSkills:  []string{"docs-guide"},
			wantCatalog: true,
		},
		{
			name:        "2 个插件（docs+ops，include 并集）",
			plugins:     []string{"docs", "ops"},
			wantCtx:     []string{"docs", "ops"},
			wantVisible: []string{"doc_read", "doc_edit", "get_time"},
			wantSkills:  []string{"docs-guide", "ops-runbook"},
			wantCatalog: true,
		},
		{
			name:        "3 个插件（cad+docs+ops，恰好到上限）",
			plugins:     []string{"cad", "docs", "ops"},
			wantCtx:     []string{"cad", "docs", "ops"},
			wantVisible: []string{"cad_draw", "doc_read", "doc_edit", "get_time"},
			wantSkills:  []string{"cad-tips", "docs-guide", "ops-runbook"},
			wantCatalog: true,
		},
	}
}

// TestRoleRoundPromptIsResetIdempotentlyAcrossRounds 是补证 E 的主用例：同一张表、
// 同一个断言函数，逐个跑 3 轮。
//
// 它排掉了什么错：
//   - "每轮重设"被实现成**累积**（读当前 prompt 再追加目录）⇒ 第 2/3 轮的 system 字节
//     比第 1 轮多一份目录段（每轮常驻字节随轮数膨胀，且模型的目录里出现重复条目）；
//   - "每轮重设"被实现成**只第一轮带目录**（例如把目录段挂在建会话那一次、回合内重设
//     成未加目录的原文）⇒ 第 2 轮起员工看不见自己装配出来的技能；
//   - 追加时**改写了 base**（例如 TrimSpace / 换行归一化动了提示词本体）⇒ 员工提示词
//     被静默改写，prompt 前缀缓存也失效；
//   - 空集与非空集走上**同一张表**后，"空集不注入"不再是"只测了空集"的孤立断言：
//     同一断言函数要求空集那一行的 wantCatalog=false 与非空行的 wantCatalog=true 都成立。
func TestRoleRoundPromptIsResetIdempotentlyAcrossRounds(t *testing.T) {
	runtime := newPromptMatrixRuntime(t)
	defer runtime.Shutdown()

	// 生产路径给的就是 roleTurnSystemPrompt(role)（见 workerRoleRoundSpec）：
	// 用它当 base，"每轮重设后的字节"才有生产含义。
	const role = "exec"
	base := runtime.roleTurnSystemPrompt(role)
	if strings.Contains(base, promptMatrixCatalogHeader) {
		t.Fatalf("基座前提不成立：员工提示词本体里本来就带目录段标题：%q", base)
	}
	limit := runtime.maxPluginsPerTeammate()
	if limit != 3 {
		t.Fatalf("本表的 3 插件行依赖出厂上限 3，得 %d", limit)
	}

	seenCatalog := make(map[string]string, len(promptMatrixCases()))
	for _, tc := range promptMatrixCases() {
		// 上限内是这张表的**前提**（超限走编排入口的显式拒绝，不在本用例范围）。
		if len(tc.plugins) > limit {
			t.Fatalf("%s：%d 个插件超出上限 %d，本表只覆盖上限内", tc.name, len(tc.plugins), limit)
		}
		catalog := runtime.roleSkillCatalog(tc.plugins)
		if tc.wantCatalog && catalog == "" {
			t.Fatalf("%s：装配了 %v 却没渲染出目录段（基座前提不成立）", tc.name, tc.plugins)
		}
		if !tc.wantCatalog && catalog != "" {
			t.Fatalf("%s：空集不该有目录段，得 %q", tc.name, catalog)
		}
		// 目录段必须**逐行可辨**：目录内容随装配集合变，否则"装配数 × 预期"这张表
		// 的后三行会退化成同一份期望。
		if tc.wantCatalog {
			if other, ok := seenCatalog[catalog]; ok {
				t.Fatalf("%s：目录段与「%s」逐字相同 ⇒ 表里这几行没有判别力", tc.name, other)
			}
			seenCatalog[catalog] = tc.name
		}

		probe := newPromptMatrixProbe(runtime, "role-"+tc.name)
		runtime.SetRoleEngineFactory(promptMatrixEngineFactory(map[string]roleEngine{probe.id: probe}))

		// 同一个会话上跑 N 轮：这是"每轮重设"的观测面。
		for round := 0; round < promptMatrixRoundCount; round++ {
			if _, err := runtime.runRoleRound(context.Background(), roleRoundSpec{
				MainSessionID: "main-prompt-matrix", RoleName: role, RoleSessionID: probe.id,
				SystemPrompt: base, Plugins: tc.plugins,
				Input: fmt.Sprintf("第 %d 轮", round+1),
			}); err != nil {
				t.Fatalf("%s 第 %d 轮失败：%v", tc.name, round+1, err)
			}
		}

		assertPromptMatrixCase(t, runtime, tc, base, catalog, probe.snapshot())
	}
}

// assertPromptMatrixCase 是这张表**唯一**的断言函数：空集与非空集都走它，差别只在
// 期望值（wantCatalog / wantSkills / wantPrompt）。不对称因此是被声明的行为。
func assertPromptMatrixCase(t *testing.T, runtime *Runtime, tc promptMatrixCase, base, catalog string, snap promptMatrixSnapshot) {
	t.Helper()

	// ① ctx 里的装配集合：空集必须是 nil（"没装配"与"装配了零个"同一形态）。
	if tc.wantCtx == nil {
		if snap.ctxPlugins != nil {
			t.Fatalf("%s：空集不得往 ctx 里放集合，得 %v", tc.name, snap.ctxPlugins)
		}
	} else if !promptMatrixSameStrings(snap.ctxPlugins, tc.wantCtx) {
		t.Fatalf("%s：本轮 ctx 的装配集合 = %v，want %v", tc.name, snap.ctxPlugins, tc.wantCtx)
	}

	// ② 工具面：装配只做减法（写死的全量工具里，write_file 不属于任何插件）。
	if !promptMatrixSameStrings(snap.visible, tc.wantVisible) {
		t.Fatalf("%s：本轮可见工具 = %v，want %v", tc.name, snap.visible, tc.wantVisible)
	}
	for _, name := range snap.visible {
		if name == "write_file" {
			t.Fatalf("%s：装配只收窄不放宽，却看见了未装配进任何插件的 write_file", tc.name)
		}
	}

	// ③ 期望字节：**独立复述**追加纪律（只追加、不改写 base），不调用
	// appendSkillCatalog 自己当期望——否则断言会跟着实现一起变。
	wantPrompt := base
	if tc.wantCatalog {
		wantPrompt = base + "\n\n" + catalog
	}

	// ④ 每一份被设置上去的 prompt 都必须逐字节等于期望：建会话那一次 + 每轮起手那次。
	//    这一条同时覆盖"第二轮起目录丢了"与"第 N 轮多了一份"两种方向。
	if len(snap.prompts) < promptMatrixRoundCount {
		t.Fatalf("%s：只观测到 %d 次 system prompt 设置（少于轮数 %d）", tc.name, len(snap.prompts), promptMatrixRoundCount)
	}
	for index, got := range snap.prompts {
		if got != wantPrompt {
			t.Fatalf("%s：第 %d 次设置的系统提示与预期不等（每轮重设必须是**幂等替换**）：\n got %q\nwant %q",
				tc.name, index, got, wantPrompt)
		}
	}

	// ⑤ 目录段恰好出现一次（含"尾句"检测串）：这是"不累积"的直接读数。
	wantOccurrences := 0
	if tc.wantCatalog {
		wantOccurrences = 1
	}
	for index, got := range snap.prompts {
		if count := strings.Count(got, promptMatrixCatalogHeader); count != wantOccurrences {
			t.Fatalf("%s：第 %d 次设置的 prompt 里目录段标题出现 %d 次，want %d（每轮重设不得累积）",
				tc.name, index, count, wantOccurrences)
		}
		if count := strings.Count(got, promptMatrixSkillNote); count != wantOccurrences {
			t.Fatalf("%s：第 %d 次设置的 prompt 里目录尾句出现 %d 次，want %d（每轮重设不得累积）",
				tc.name, index, count, wantOccurrences)
		}
	}

	last := snap.lastPrompt()
	if tc.wantCatalog {
		// ⑥ base 部分逐字未被改写：提示词本体是前缀，剩下的**正好**是空行 + 目录段。
		if !strings.HasPrefix(last, base) {
			t.Fatalf("%s：追加不得改写提示词本体，得 %q", tc.name, last)
		}
		if rest := last[len(base):]; rest != "\n\n"+catalog {
			t.Fatalf("%s：目录段必须原样追加在末尾（\n\n + 目录段），得 %q", tc.name, rest)
		}
		for _, want := range tc.wantSkills {
			if !strings.Contains(last, want) {
				t.Fatalf("%s：目录段缺少技能 %q：%q", tc.name, want, last)
			}
		}
		// 目录段只宣告 name+description：正文（Prompt）永不进目录。
		if strings.Contains(last, "PROMPT-BODY") {
			t.Fatalf("%s：技能正文不得进目录段", tc.name)
		}
	} else {
		// 空集那一侧：字节逐字等于 base（连一个换行都不许多）。
		if last != base {
			t.Fatalf("%s：空集的 system 字节必须逐字不变：\n got %q\nwant %q", tc.name, last, base)
		}
	}

	// ⑦ 判别力对照：把目录段**再追加一遍**（累积型退化的形态），检测串必须能识别出来。
	//    没有这一条，上面 ⑤ 的 `want 1` 就可能是"检测串根本不会命中"式的空断言。
	if tc.wantCatalog {
		degraded := appendSkillCatalog(wantPrompt, catalog)
		if strings.Count(degraded, promptMatrixCatalogHeader) != 2 {
			t.Fatalf("%s：检测串对累积型退化没有判别力：%q", tc.name, degraded)
		}
		if degraded == wantPrompt {
			t.Fatalf("%s：累积型退化与预期逐字相同 ⇒ 期望值选错了", tc.name)
		}
	}
}

// ── 基座 ────────────────────────────────────────────────────────────────────

// promptMatrixToolNames 是写死的全量工具面：cad 只有 cad_*，docs 只有 doc_*，
// ops 只有 get_*，write_file 谁都没装配到（装配只做减法，且空集不等于"零工具面"）。
var promptMatrixToolNames = []string{"cad_draw", "doc_read", "doc_edit", "write_file", "get_time"}

// newPromptMatrixRuntime：宿主全局激活 cad；docs / ops 只有按会话装配才拿得到；
// 三个插件各带一条技能（目录段因此逐行不同）。
func newPromptMatrixRuntime(t *testing.T) *Runtime {
	t.Helper()
	runtime := newTestRuntime(t)
	for _, name := range promptMatrixToolNames {
		name := name
		runtime.RegisterTool(name, name, map[string]interface{}{"type": "object"},
			func(context.Context, string) (string, error) { return "ok", nil })
	}
	for _, spec := range []struct {
		name    string
		include []string
	}{
		{"cad", []string{"cad_*"}},
		{"docs", []string{"doc_*"}},
		{"ops", []string{"get_*"}},
	} {
		if err := runtime.DefinePlugin(spec.name, strings.ToUpper(spec.name), spec.include, nil); err != nil {
			t.Fatalf("DefinePlugin(%s): %v", spec.name, err)
		}
	}
	if err := runtime.ActivatePlugin("cad"); err != nil {
		t.Fatalf("ActivatePlugin: %v", err)
	}

	registry := skill.NewRegistry()
	// 技能正文（Prompt）故意带一个标记串：目录段只许宣告 name+description。
	registry.SetPluginSkills("cad", []skill.Skill{{Name: "cad-tips", Description: "draw precisely"}})
	registry.SetPluginSkills("docs", []skill.Skill{{Name: "docs-guide", Description: "how to read the docs", Prompt: "PROMPT-BODY"}})
	registry.SetPluginSkills("ops", []skill.Skill{{Name: "ops-runbook", Description: "short runbook"}})
	// 全局激活态故意指向 cad：员工面的目录**不许**读它（否则断言的"逐行不同"会串味）。
	registry.ActivatePlugin("cad")
	runtime.SetSkillRegistry(registry)
	return runtime
}

// promptMatrixProbe 是最小角色引擎：记录**每份**被设上去的 system prompt、本轮 ctx 里的
// 装配集合、以及本轮真正可见的工具面（用运行期 Policy + 本轮 ctx 现算 = 生产那条路）。
type promptMatrixProbe struct {
	id      string
	runtime *Runtime
	all     []types.Tool

	mu         sync.Mutex
	prompts    []string
	ctx        context.Context
	ctxPlugins []string
	visible    []string
}

func newPromptMatrixProbe(runtime *Runtime, id string) *promptMatrixProbe {
	all := make([]types.Tool, 0, len(promptMatrixToolNames))
	for _, name := range promptMatrixToolNames {
		all = append(all, types.Tool{Type: "function", Function: types.ToolFunction{Name: name}})
	}
	return &promptMatrixProbe{id: id, runtime: runtime, all: all}
}

func (p *promptMatrixProbe) SessionID() string { return p.id }

func (p *promptMatrixProbe) SetSystemPrompt(prompt string) {
	p.mu.Lock()
	p.prompts = append(p.prompts, prompt)
	p.mu.Unlock()
}

func (p *promptMatrixProbe) SetMaxLoops(int)          {}
func (p *promptMatrixProbe) ClearHistory()            {}
func (p *promptMatrixProbe) History() []types.Message { return nil }

func (p *promptMatrixProbe) ChatStream(ctx context.Context, _ string, _ func(string)) (string, error) {
	names := make([]string, 0, len(p.all))
	for _, tool := range p.runtime.visibilityPolicy.Filter(ctx, p.all) {
		names = append(names, tool.Function.Name)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ctx = ctx
	p.ctxPlugins = tools.RolePluginsFromContext(ctx)
	p.visible = names
	return "done", nil
}

type promptMatrixSnapshot struct {
	prompts    []string
	ctx        context.Context
	ctxPlugins []string
	visible    []string
}

func (p *promptMatrixProbe) snapshot() promptMatrixSnapshot {
	p.mu.Lock()
	defer p.mu.Unlock()
	return promptMatrixSnapshot{
		prompts:    append([]string(nil), p.prompts...),
		ctx:        p.ctx,
		ctxPlugins: append([]string(nil), p.ctxPlugins...),
		visible:    append([]string(nil), p.visible...),
	}
}

func (s promptMatrixSnapshot) lastPrompt() string {
	if len(s.prompts) == 0 {
		return ""
	}
	return s.prompts[len(s.prompts)-1]
}

// promptMatrixEngineFactory 按会话号发引擎（工厂每个用例重建一次，句柄表随之新建）。
func promptMatrixEngineFactory(engines map[string]roleEngine) func(string) (roleEngine, error) {
	var mu sync.Mutex
	return func(sessionID string) (roleEngine, error) {
		mu.Lock()
		defer mu.Unlock()
		engine := engines[sessionID]
		if engine == nil {
			return nil, fmt.Errorf("prompt 矩阵探针没有为会话 %q 准备引擎", sessionID)
		}
		return engine, nil
	}
}

func promptMatrixSameStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}
