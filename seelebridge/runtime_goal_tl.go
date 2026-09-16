package seelebridge

// runtime_goal_tl.go — seelebridge → goal 域的真实 TL 评估器（P1 装配）。
//
// goal/govern 保持叶子包：TLEvaluator 由装配侧（seelebridge）实现，输出经 goal 域
// 校验的 TLDirective。组合根（main.go）在首次会话启动前经 app.SetGoalTLEvaluator 注入。
//
// 2026-09-16 追加：ADVISOR 回合从"一次无工具的 completer 调用"升级为**一次带只读
// 工具的角色回合**（复用 runtime_role_turn.go 的 runRoleRound：角色会话 + 员工主体
// 按构造落地 + 项目根绑定）。理由与证据见
// docs/2026-09-16-ring-escape-permission-bearing/A2A-VALUE-REVIEW.md §2.5/§3.3：
// 增益的真实来源是"可执行验证接地"——评审者能读文件/搜索，裁决才是证据判断；
// 没有读手段时它只能做似真性判断。
//
// 边界（比改动本身重要）：
//   - 只读 = ro 路由组（读文件 / 搜索 / glob / 读工具结果）。**跑测试要 bash（rw
//     组），不在只读面内**——"能跑测试的评审者"是下一步（需要给评审者一把更细的
//     执行位），本轮不宣称；
//   - 没有工作区（embed 无主会话 id：老宿主/单测）时退回**无工具的一次 completer
//     评审**，并在系统提示里如实说明"本回合没有工具"，不让模型以为自己有读手段；
//   - 裁决仍必须落在 TLDirective JSON 上（输出契约不因有工具而改变）：govern 环
//     与 gate 依赖它解析。

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/RedHuang-0622/Seele/agent"
	"github.com/RedHuang-0622/Seele/types"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	goaldomain "github.com/RedHuang-0622/seelex/application/core/goal"
)

// goalLLMEvaluator 实现 goaldomain.TLEvaluator：真实 LLM 评审 goal 域
// 嵌入（锚点 + 帧账本 + b 自身回合记忆），要求输出 TLDirective JSON。
//
// 两条执行面：
//   - round != nil 且 embed 带主会话 id → **带只读工具的角色回合**（首选）；
//   - 否则 → completer 的一次无工具调用（兜底，保留 B4 语义：没有工作区就没有证据）。
type goalLLMEvaluator struct {
	completer agent.Completer
	// promptProvider 读"已装配的 ADVISOR 提示词"（Agent Team 员工入职时登记）；
	// nil 或返回空串 = 用内置角色设定。输出契约不受它影响。
	promptProvider func(roleName string) string
	// round 是角色回合执行原语（Runtime.runRoleRound）。nil = 只走无工具兜底。
	round func(ctx context.Context, spec roleRoundSpec) (string, error)
}

// advisorRoleSessionPrefix 是 ADVISOR 评审会话号的命名空间前缀。
//
// 为什么不是注册表里的角色会话（`<teamID>-tl`）：那是 tl **员工做工**的会话坐标
// （消息/草稿存储的键），而评审回合是 DS-A2A 的 b 侧执行面——b 的上下文刻意与
// a 隔离、引擎进程内、不接 DurableHistory（见 advisor.go 的模型说明）。两者是
// 两个座位，共用一个会话号会让"评审上下文"和"员工工作上下文"互相污染。
// 前缀 + 主会话 id 保证：同一主会话的评审会话稳定复用（缓存前缀稳定），不同主会话
// 互不串味。
const advisorRoleSessionPrefix = "advisor:"

// advisorRoleSessionID 派生某主会话的 ADVISOR 评审会话号。
func advisorRoleSessionID(mainSessionID string) string {
	return advisorRoleSessionPrefix + strings.TrimSpace(mainSessionID)
}

// advisorRoundMaxLoops 是评审回合的 ReAct 循环上限：评审要读几份证据，但一轮评审
// 不该把一个 goal 的预算烧在探索上（比员工回合更紧）。
const advisorRoundMaxLoops = 8

// goalAdvisorRolePrompt 是 ADVISOR(TL) 的内置角色设定（未登记员工提示词时使用）。
// 写法按 Claude 官方提示词规范组织：先角色、再任务、再约束，每段一个 XML 标签
// （模型能准确定位"哪一段在讲什么"，也便于人审时逐段替换）。它只是"角色设定"那
// 一段；输出契约由 goalAdvisorOutputContract 追加，员工提示词改不掉它。
const goalAdvisorRolePrompt = `<role>
你是 Seelex 的 TechLeader（ADVISOR，逻辑角色名 tl）：在 goal 治理回合里做**一次**有界评审。
</role>

<task>
判断当前 goal 是否达成，并给出下一步动作（继续 / 收口 / 转人工）。不扩大范围、不替执行者干活。
</task>

<constraints>
- 证据不足就不要猜：先用手上的手段核对（见 <tooling>），仍不足则按输出契约给 escalate_human 或先要检查点；
- 不重复裁决已经裁过的事，除非上下文里有新证据；
- 依据必须落在仓库内可核对的事实上（refs 用相对路径）；
- 语言跟随上下文（中文上下文用中文）。
</constraints>`

// goalAdvisorToolingWithTools 是"本回合有只读工具"时的 <tooling> 段：把评审者的
// 能力边界与用法讲清楚（能力边界也是提示词的一部分——模型不知道边界就会去试越权）。
const goalAdvisorToolingWithTools = `<tooling>
本回合你有**只读**工具：read_file / grep_search / glob（以及读工具结果、读计划）。
用它们把"看起来对"变成"核对过"：涉及具体文件的结论，先读一眼再写 refs。
写文件、执行命令不在你的权限面内——需要动手改的事交回执行者，不要试图越权。
</tooling>`

// goalAdvisorToolingWithoutTools 是"本回合没有工具"时的 <tooling> 段。如实说明，
// 免得模型假装自己核对过文件（那会让似真性判断伪装成证据判断）。
const goalAdvisorToolingWithoutTools = `<tooling>
本回合你没有工具：只能依据 <review_context> 里给出的证据判定。
凡是需要看仓库内容才能确认的结论，一律按证据不足处理（escalate_human 或 checkpoint_ok），
不要凭名字猜测文件内容。
</tooling>`

// goalAdvisorOutputContract 是 ADVISOR 回合的输出契约（登记提示词也改不掉它，
// 否则 goal 域解析不出指令）。
const goalAdvisorOutputContract = `<output_contract>
只输出一个 JSON 对象，不要 markdown 围栏、不要任何解释文字：
{"kind":"verdict_done|verdict_not_done|checkpoint_ok|correct|escalate_human","content":"结论与下一步建议","refs":["仓库内相对路径"],"severity":"P0|P1|P2"}
规则：
1. kind 只能取上面五个值之一；
2. content ≤1200 字符，写结论 + 下一步建议，不写推理过程；
3. refs 只放仓库内相对路径，最多 16 条；
4. 无法判定或越权时用 escalate_human。
</output_contract>`

// advisorRoleName 是 ADVISOR 的逻辑角色名（与 sessionstore.RoleTL 一致）。
const advisorRoleName = "tl"

// advisorSystemPrompt 组装 ADVISOR 回合的 system 提示词：**已登记的员工提示词
// 优先**（Agent Team 面板"员工入职 → 提示词"），未登记时用内置角色设定；无论
// 哪种情况都追加工具边界与输出契约。各段用 XML 标签分开——角色设定可被人替换，
// 工具边界与输出契约是治理的运行前提，标签让边界一眼可见（也是 Claude 规范推荐的
// 分段方式）。
func (e *goalLLMEvaluator) advisorSystemPrompt(withTools bool) string {
	role := ""
	if e.promptProvider != nil {
		role = strings.TrimSpace(e.promptProvider(advisorRoleName))
	}
	if role == "" {
		role = goalAdvisorRolePrompt
	}
	tooling := goalAdvisorToolingWithoutTools
	if withTools {
		tooling = goalAdvisorToolingWithTools
	}
	return "<role_prompt>\n" + role + "\n</role_prompt>\n\n" + tooling + "\n\n" + goalAdvisorOutputContract
}

// toolRoundEnvelope 报告本回合会不会走"带只读工具"的评审回合。
//
// 两个条件缺一不可：注入了执行原语（round）**且** embed 带主会话 id（有工作区
// 可读、项目根可绑）。它只回答"这一轮有没有读手段"，不回答"权限够不够"——只读
// 是评审者的既定口径（按构造分配 readonly，不读注册表）。
//
// 它必须与 review 的分支判定**同源**：提示词说"你有工具"而实际没有（或反之）
// 会让模型的行为与实际能力错位，比没有工具更糟。
func (e *goalLLMEvaluator) toolRoundEnvelope(embed goaldomain.TLSessionEmbed) bool {
	return e != nil && e.round != nil && strings.TrimSpace(embed.SessionID) != ""
}

func (e *goalLLMEvaluator) Evaluate(ctx context.Context, embed goaldomain.TLSessionEmbed) (goaldomain.TLDirective, error) {
	if e == nil || (e.completer == nil && e.round == nil) {
		return goaldomain.TLDirective{}, goaldomain.ErrTLDisabled
	}
	withTools := e.toolRoundEnvelope(embed)
	systemPrompt := e.advisorSystemPrompt(withTools)
	content := "<review_context>\n" + embed.RenderText() + "\n</review_context>" +
		"\n\n<task>\n按系统提示词里的角色设定、工具边界与输出契约评审以上上下文，只输出 TLDirective JSON。\n</task>"

	raw, err := e.review(ctx, embed, systemPrompt, content)
	if err != nil {
		return goaldomain.TLDirective{}, fmt.Errorf("goal TL 回合失败（B4 缺席矩阵）: %w", err)
	}
	directive, err := parseGoalDirective(raw)
	if err != nil {
		return goaldomain.TLDirective{}, err
	}
	if err := directive.Validate(); err != nil {
		return goaldomain.TLDirective{}, fmt.Errorf("%w: goal TL 输出未通过域校验: %v（原文 %q）",
			goaldomain.ErrBadDirective, err, truncateRunes(raw, 200))
	}
	return directive, nil
}

// review 跑一次评审回合：优先带只读工具（角色会话，权限按构造落地），没有工作区
// 时退回一次无工具的 completer 调用。
func (e *goalLLMEvaluator) review(ctx context.Context, embed goaldomain.TLSessionEmbed, systemPrompt, content string) (string, error) {
	if e.toolRoundEnvelope(embed) {
		return e.round(ctx, roleRoundSpec{
			MainSessionID: embed.SessionID,
			RoleName:      advisorRoleName,
			RoleSessionID: advisorRoleSessionID(embed.SessionID),
			// 只读是评审者的**构造口径**（不是从注册表读来的配置）：评审者不该有
			// 写权限，也不该因为"tl 这个角色在注册表里被填了 readwrite"就变成能改
			// 执行者的代码。要看更宽的面，改的是这个常量所在的装配点。
			ToolsPolicy:  dto.ToolPolicyReadonly,
			SystemPrompt: systemPrompt,
			Input:        content,
			MaxLoops:     advisorRoundMaxLoops,
			// 评审的流式分片交给挂在 ctx 上的观察回调（goal 域在回合开始处挂）：
			// 旧实现这里是 nil，分片被丢掉 → 前端只能等终局裁决（"渲染不及时"）。
			// 单向：只有后端 → 观察面的推送，没有任何回写路径。
			OnDelta: goaldomain.TLDeltaSinkFrom(ctx),
			// 评审上下文按帧渲染（锚点 + 帧 + 自身回合记忆），回合之间不共享引擎历史：
			// goal 收口后 peer 会被 reap，引擎历史若留着就会把上一个 goal 的评审带进来。
			FreshContext: true,
		})
	}
	if e.completer == nil {
		return "", goaldomain.ErrTLDisabled
	}
	message, err := e.completer.Complete(ctx, []types.Message{
		{Role: "system", Content: stringPtr(systemPrompt)},
		{Role: "user", Content: stringPtr(content)},
	}, nil)
	if err != nil {
		return "", err
	}
	if message.Content == nil {
		return "", nil
	}
	return *message.Content, nil
}

// parseGoalDirective 从 b 回合原文里取出 TLDirective。
//
// 解析失败一律包 goaldomain.ErrBadDirective：这类失败是"b 已作答、裁决不可用"，
// 不是"b 缺席（429/超时）"。gate 依赖这个区分给用户如实的收口说明——把转义细节
// 说成"缺席"会让人看到与实际相反的 goal 状态（事故见 json_object.go 的说明）。
//
// 容错只修语法（未转义引号 / 裸控制字符 / 非法转义），不改语义：kind 取值范围、
// goal 漂移、refs 合法性仍由 TLDirective.Validate() 判定。
func parseGoalDirective(raw string) (goaldomain.TLDirective, error) {
	raw = strings.TrimSpace(raw)
	var directive goaldomain.TLDirective
	if err := decodeJSONObjectLenient(raw, &directive); err != nil {
		if errors.Is(err, ErrNoJSONObject) {
			return goaldomain.TLDirective{}, fmt.Errorf("%w: goal TL 输出缺少 JSON 对象（原文 %q）",
				goaldomain.ErrBadDirective, truncateRunes(raw, 300))
		}
		return goaldomain.TLDirective{}, fmt.Errorf("%w: goal TL 输出非 JSON: %v（原文 %q）",
			goaldomain.ErrBadDirective, err, truncateRunes(raw, 300))
	}
	return directive, nil
}

func stringPtr(value string) *string { return &value }

func truncateRunes(value string, max int) string {
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max-1]) + "…"
}

// GoalTLEvaluator 返回真实 TL 评估器（用 Runtime 主 completer；未装配
// completer 时返回 nil → Supervisor 按 TL 未启用处理）。
//
// ADVISOR 的角色提示词取自 `SetRolePromptProvider` 注入的读面（装配根从 Agent
// Team 角色注册表读）：用户登记了提示词就用它，否则用内置角色设定；工具边界与
// 输出契约永远追加。
//
// 评审回合的执行面是 `runRoleRound`：**有工作区时 tl 带只读工具跑一轮**（读文件/
// 搜索），无工作区时退回无工具的一次调用。
func (r *Runtime) GoalTLEvaluator() goaldomain.TLEvaluator {
	if r == nil || r.completer == nil {
		return nil
	}
	return &goalLLMEvaluator{
		completer:      r.completer,
		promptProvider: r.rolePromptFor,
		round:          r.runRoleRound,
	}
}
