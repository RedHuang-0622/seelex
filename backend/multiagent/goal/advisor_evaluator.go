package goal

// advisor_evaluator.go 是 **ADVISOR(TL) 的真实评审评估器**（`TLEvaluator` 的生产实现）。
//
// 落点沿革（P21-13「域方法随类型走」）：从 `backend/engine/assemble/runtime_goal_tl.go` 整文件
// 归位——plan §2.5 把 `seelebridge/runtime_goal_tl.go` 列为 `backend/multiagent/goal/` 的目标
// （"搬 + 反向依赖改端口"）。判据（提示词分段与输出契约追加、工具边界与实际执行面同源、
// 裁决解析与域校验）随类型走；装配层只剩三格宿主读数。
//
// 2026-09-16 追加：ADVISOR 回合从"一次无工具的 completer 调用"升级为**一次带只读工具的
// 角色回合**（宿主用 `runRoleRound`：角色会话 + 员工主体按构造落地 + 项目根绑定）。理由与
// 证据见 docs/2026-09-16-ring-escape-permission-bearing/A2A-VALUE-REVIEW.md §2.5/§3.3：
// 增益的真实来源是"可执行验证接地"——评审者能读文件/搜索，裁决才是证据判断；没有读手段
// 时它只能做似真性判断。
//
// 边界（比改动本身重要）：
//   - 只读 = ro 路由组（读文件 / 搜索 / glob / 读工具结果）。**跑测试要 bash（rw 组），
//     不在只读面内**——"能跑测试的评审者"是下一步（需要给评审者一把更细的执行位），
//     本轮不宣称；
//   - 没有工作区（embed 无主会话 id：老宿主/单测）时退回**无工具的一次 completer 评审**，
//     并在系统提示里如实说明"本回合没有工具"，不让模型以为自己有读手段；
//   - 裁决仍必须落在 TLDirective JSON 上（输出契约不因有工具而改变）：终态 gate
//     与审批预筛依赖它解析。

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/internal/jsonobj"
)

// AdvisorRoleName 是 ADVISOR 的逻辑角色名（与 `store.RoleTL` 一致）。
const AdvisorRoleName = "tl"

// AdvisorRoundMaxLoops 是评审回合的 ReAct 循环上限：评审要读几份证据，但一轮评审
// 不该把一个 goal 的预算烧在探索上（比员工回合更紧）。
const AdvisorRoundMaxLoops = 8

// advisorRoleSessionPrefix 是 ADVISOR 评审会话号的命名空间前缀。
//
// 为什么不是注册表里的角色会话（`<teamID>-tl`）：那是 tl **员工做工**的会话坐标
// （消息/草稿存储的键），而评审回合是 DS-A2A 的 b 侧执行面——b 的上下文刻意与
// a 隔离、引擎进程内、不接 DurableHistory（见 advisor.go 的模型说明）。两者是
// 两个执行面（不是一个 b 座位与一个员工座位）——共用一个会话号会让"评审上下文"和
// "员工工作上下文"互相污染，所以评审会话号走独立命名空间。
// 前缀 + 主会话 id 保证：同一主会话的评审会话稳定复用（缓存前缀稳定），不同主会话
// 互不串味。
const advisorRoleSessionPrefix = "advisor:"

// AdvisorRoleSessionID 派生某主会话的 ADVISOR 评审会话号。
func AdvisorRoleSessionID(mainSessionID string) string {
	return advisorRoleSessionPrefix + strings.TrimSpace(mainSessionID)
}

// AdvisorRoundRequest 是一轮"带只读工具的角色回合"的**域侧请求**：装配层把它翻译成
// 宿主自己的角色回合原语（`roleRoundSpec`），域包因此不认识宿主类型。
type AdvisorRoundRequest struct {
	// MainSessionID 是这一轮所属的主会话（有工作区就有它）。
	MainSessionID string
	// RoleSessionID 是评审会话号（`AdvisorRoleSessionID`）。
	RoleSessionID string
	// RoleName 是角色名（`AdvisorRoleName`）。
	RoleName string
	// ToolsPolicy 是本轮的权限口径——**只读是评审者的构造口径**（不是从注册表读来的
	// 配置）：评审者不该有写权限，也不该因为"tl 这个角色在注册表里被填了 readwrite"
	// 就变成能改执行者的代码。要看更宽的面，改的是本常量所在的域文件。
	ToolsPolicy string
	// SystemPrompt / Input 是本轮的消息面（评审上下文按帧渲染，见 review）。
	SystemPrompt string
	Input        string
	// MaxLoops 是本轮 ReAct 循环上限。
	MaxLoops int
	// OnDelta 是本轮的流式观察回调（可为 nil）。
	OnDelta TLDeltaSink
	// FreshContext 表示"本轮的模型上下文完全由 Input 自带"（评审口径，见宿主
	// roleRoundSpec.FreshContext 的说明）。
	FreshContext bool
}

// AdvisorEvaluatorDeps 是评估器的三格宿主读数（都是函数值，域包因此不认识 Runtime、
// 引擎与角色回合执行面）。
type AdvisorEvaluatorDeps struct {
	// Complete 是无工具兜底：一次 completer 调用（system + user → 原文）。
	Complete func(ctx context.Context, systemPrompt, userContent string) (string, error)
	// PromptProvider 读"已装配的 ADVISOR 提示词"（Agent Team 员工入职时登记）；
	// nil 或返回空串 = 用内置角色设定。输出契约不受它影响。
	PromptProvider func(roleName string) string
	// Round 是角色回合执行原语（宿主 `Runtime.runRoleRound`）。nil = 只走无工具兜底。
	Round func(ctx context.Context, request AdvisorRoundRequest) (string, error)
}

// AdvisorEvaluator 实现 `TLEvaluator`：真实 LLM 评审 goal 域嵌入（锚点 + 帧账本 +
// b 自身回合记忆），要求输出 TLDirective JSON。
//
// 两条执行面：
//   - `Round != nil` 且 embed 带主会话 id → **带只读工具的角色回合**（首选）；
//   - 否则 → completer 的一次无工具调用（兜底，保留 B4 语义：没有工作区就没有证据）。
type AdvisorEvaluator struct {
	deps AdvisorEvaluatorDeps
}

// NewAdvisorEvaluator 装配评估器（装配点是组合根的 `Runtime.GoalTLEvaluator`）。
func NewAdvisorEvaluator(deps AdvisorEvaluatorDeps) *AdvisorEvaluator {
	return &AdvisorEvaluator{deps: deps}
}

// AdvisorSystemPrompt 组装 ADVISOR 回合的 system 提示词：**已登记的员工提示词
// 优先**（Agent Team 面板"员工入职 → 提示词"），未登记时用内置角色设定；无论
// 哪种情况都追加工具边界与输出契约。各段用 XML 标签分开——角色设定可被人替换，
// 工具边界与输出契约是治理的运行前提，标签让边界一眼可见（也是 Claude 规范推荐的
// 分段方式）。
func (e *AdvisorEvaluator) AdvisorSystemPrompt(withTools bool) string {
	role := ""
	if e != nil && e.deps.PromptProvider != nil {
		role = strings.TrimSpace(e.deps.PromptProvider(AdvisorRoleName))
	}
	if role == "" {
		role = advisorRolePrompt
	}
	tooling := advisorToolingWithoutTools
	if withTools {
		tooling = advisorToolingWithTools
	}
	return "<role_prompt>\n" + role + "\n</role_prompt>\n\n" + tooling + "\n\n" + advisorOutputContract
}

// ToolRoundEnvelope 报告本回合会不会走"带只读工具"的评审回合。
//
// 两个条件缺一不可：注入了执行原语（`Round`）**且** embed 带主会话 id（有工作区
// 可读、项目根可绑）。它只回答"这一轮有没有读手段"，不回答"权限够不够"——只读
// 是评审者的既定口径（按构造分配 readonly，不读注册表）。
//
// 它必须与 review 的分支判定**同源**：提示词说"你有工具"而实际没有（或反之）
// 会让模型的行为与实际能力错位，比没有工具更糟。
func (e *AdvisorEvaluator) ToolRoundEnvelope(embed TLSessionEmbed) bool {
	return e != nil && e.deps.Round != nil && strings.TrimSpace(embed.SessionID) != ""
}

// Evaluate 跑一轮评审并给出裁决：提示词 → 回合 → 解析 → 域校验。
func (e *AdvisorEvaluator) Evaluate(ctx context.Context, embed TLSessionEmbed) (TLDirective, error) {
	if e == nil || (e.deps.Complete == nil && e.deps.Round == nil) {
		return TLDirective{}, ErrTLDisabled
	}
	withTools := e.ToolRoundEnvelope(embed)
	systemPrompt := e.AdvisorSystemPrompt(withTools)
	content := "<review_context>\n" + embed.RenderText() + "\n</review_context>" +
		"\n\n<task>\n按系统提示词里的角色设定、工具边界与输出契约评审以上上下文，只输出 TLDirective JSON。\n</task>"

	raw, err := e.review(ctx, embed, systemPrompt, content)
	if err != nil {
		return TLDirective{}, fmt.Errorf("goal TL 回合失败（B4 缺席矩阵）: %w", err)
	}
	directive, err := parseGoalDirective(raw)
	if err != nil {
		return TLDirective{}, err
	}
	if err := directive.Validate(); err != nil {
		return TLDirective{}, fmt.Errorf("%w: goal TL 输出未通过域校验: %v（原文 %q）",
			ErrBadDirective, err, truncateAdvisorRunes(raw, 200))
	}
	return directive, nil
}

// review 跑一次评审回合：优先带只读工具（角色会话，权限按构造落地），没有工作区
// 时退回一次无工具的 completer 调用。
func (e *AdvisorEvaluator) review(ctx context.Context, embed TLSessionEmbed, systemPrompt, content string) (string, error) {
	if e.ToolRoundEnvelope(embed) {
		return e.deps.Round(ctx, AdvisorRoundRequest{
			MainSessionID: embed.SessionID,
			RoleSessionID: AdvisorRoleSessionID(embed.SessionID),
			RoleName:      AdvisorRoleName,
			ToolsPolicy:   dto.ToolPolicyReadonly,
			SystemPrompt:  systemPrompt,
			Input:         content,
			MaxLoops:      AdvisorRoundMaxLoops,
			// 评审的流式分片交给挂在 ctx 上的观察回调（goal 域在回合开始处挂）：
			// 旧实现这里是 nil，分片被丢掉 → 前端只能等终局裁决（"渲染不及时"）。
			// 单向：只有后端 → 观察面的推送，没有任何回写路径。
			OnDelta: TLDeltaSinkFrom(ctx),
			// 评审上下文按帧渲染（锚点 + 帧 + 自身回合记忆），回合之间不共享引擎历史：
			// goal 收口后 peer 会被 reap，引擎历史若留着就会把上一个 goal 的评审带进来。
			FreshContext: true,
		})
	}
	if e.deps.Complete == nil {
		return "", ErrTLDisabled
	}
	return e.deps.Complete(ctx, systemPrompt, content)
}

// parseGoalDirective 从 b 回合原文里取出 TLDirective。
//
// 解析失败一律包 `ErrBadDirective`：这类失败是"b 已作答、裁决不可用"，不是"b 缺席
// （429/超时）"。gate 依赖这个区分给用户如实的收口说明——把转义细节说成"缺席"会让人
// 看到与实际相反的 goal 状态（事故见 internal/jsonobj 的说明）。
//
// 容错只修语法（未转义引号 / 裸控制字符 / 非法转义），不改语义：kind 取值范围、
// goal 漂移、refs 合法性仍由 TLDirective.Validate() 判定。
func parseGoalDirective(raw string) (TLDirective, error) {
	raw = strings.TrimSpace(raw)
	var directive TLDirective
	if err := jsonobj.DecodeLenient(raw, &directive); err != nil {
		if errors.Is(err, jsonobj.ErrNoJSONObject) {
			return TLDirective{}, fmt.Errorf("%w: goal TL 输出缺少 JSON 对象（原文 %q）",
				ErrBadDirective, truncateAdvisorRunes(raw, 300))
		}
		return TLDirective{}, fmt.Errorf("%w: goal TL 输出非 JSON: %v（原文 %q）",
			ErrBadDirective, err, truncateAdvisorRunes(raw, 300))
	}
	return directive, nil
}

// truncateAdvisorRunes 把一段原文压到 max 个 rune 并加省略号（只用于错误文案的原文摘录）。
//
// 为什么不像别处同名助手那样并成一份：本仓另有四处 `truncateRunes`，判据**并不相同**
// （有没有省略号、要不要 TrimSpace、`<=0` 时返回空串还是原样返回）——按 AGENTS §8.2
// "只是长得像的不并"，这里保留本域的口径，并把 `max <= 0` 从"切片越界 panic"改成
// 原样返回（调用方只传字面量，此前不可达；改成不 panic 是为了让助手自身可被安全复用）。
func truncateAdvisorRunes(value string, max int) string {
	if max <= 0 {
		return value
	}
	if utf8.RuneCountInString(value) <= max {
		return value
	}
	runes := []rune(value)
	return string(runes[:max-1]) + "…"
}

// 提示词常量：内置角色设定（未登记员工提示词时使用）+ 两个互斥的工具边界段 +
// 永远追加的输出契约。
//
// 角色设定写法按 Claude 官方提示词规范组织：先角色、再任务、再约束，每段一个 XML 标签
// （模型能准确定位"哪一段在讲什么"，也便于人审时逐段替换）。它只是"角色设定"那
// 一段；输出契约由 advisorOutputContract 追加，员工提示词改不掉它。
const advisorRolePrompt = `<role>
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

// advisorToolingWithTools 是"本回合有只读工具"时的 <tooling> 段：把评审者的
// 能力边界与用法讲清楚（能力边界也是提示词的一部分——模型不知道边界就会去试越权）。
const advisorToolingWithTools = `<tooling>
本回合你有**只读**工具：read_file / grep_search / glob（以及读工具结果、读计划）。
用它们把"看起来对"变成"核对过"：涉及具体文件的结论，先读一眼再写 refs。
写文件、执行命令不在你的权限面内——需要动手改的事交回执行者，不要试图越权。
</tooling>`

// advisorToolingWithoutTools 是"本回合没有工具"时的 <tooling> 段。如实说明，
// 免得模型假装自己核对过文件（那会让似真性判断伪装成证据判断）。
const advisorToolingWithoutTools = `<tooling>
本回合你没有工具：只能依据 <review_context> 里给出的证据判定。
凡是需要看仓库内容才能确认的结论，一律按证据不足处理（escalate_human 或 checkpoint_ok），
不要凭名字猜测文件内容。
</tooling>`

// advisorOutputContract 是 ADVISOR 回合的输出契约（登记提示词也改不掉它，
// 否则 goal 域解析不出指令）。
const advisorOutputContract = `<output_contract>
只输出一个 JSON 对象，不要 markdown 围栏、不要任何解释文字：
{"kind":"verdict_done|verdict_not_done|checkpoint_ok|correct|escalate_human","content":"结论与下一步建议","refs":["仓库内相对路径"],"severity":"P0|P1|P2"}
规则：
1. kind 只能取上面五个值之一；
2. content ≤1200 字符，写结论 + 下一步建议，不写推理过程；
3. refs 只放仓库内相对路径，最多 16 条；
4. 无法判定或越权时用 escalate_human。
</output_contract>`
