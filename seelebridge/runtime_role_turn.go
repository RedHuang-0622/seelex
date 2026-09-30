package seelebridge

// runtime_role_turn.go — 「角色（员工/评审者）回合执行体」的 seelebridge 落点。
//
// 生态位：application/core 只声明"谁有座位、谁先谁后"（goal_coordinator.go 的
// RoleTurnRunner / RoleTurnRequest），真正的执行体在这里：**给角色开一个自己的
// 会话（引擎），开的同时把这个角色的权限（主体 × 路由组 × 位）分配到权责表，
// 然后按权责口径跑一轮带工具的回合**。
//
// 两个消费者共用同一套原语（runRoleRound）：
//   - **员工回合**（RunRoleTurn）：V 模型团队循环里 pm/exec/test_case 各自做工；
//   - **ADVISOR 评审回合**（runtime_goal_tl.go 的 goalLLMEvaluator）：tl 座位带
//     **只读**工具跑一轮，把裁决从"观点"变成"证据"（A2A-VALUE-REVIEW §2.5/§3.3）。
//     两者共用会话生命周期/权限分配/项目根绑定/回合闸门——差别只在系统提示、
//     输入与循环上限。
//
// 为什么必须有这一层（而不是让 application 直接调模型）：
//   - 轮到一个角色发言时，它需要的是**一次真的会被权限门管辖的工具回合**——
//     有会话（有历史、有项目根）、有工具面（readonly 的角色看不到写工具）、有
//     主体（判定落在 emp_<角色> 上）。缺任何一样，角色就只是"发言权"而不是
//     "做工权"（见 docs/2026-09-16-ring-escape-permission-bearing/A2A-VALUE-REVIEW.md §1）。
//   - 权责落地必须是**起点条件**而不是事后解析：本文件在起手把角色主体按构造
//     放进 ctx（seeltools.WithEmployeeGrant），因此即使角色会话 → 权责的反向
//     索引冷启动没查到，角色回合的工具调用照样按角色口径拦（"按构造授权"永远比
//     "按反查授权"可靠）。
//
// 边界（诚实标注，不假装）：
//   - 角色会话的引擎是**进程内**的（不接 DurableHistory）：角色历史与主会话/角色
//     draft 存储是两件事，谁也不是谁的持久化面；
//   - Progress 是"本轮产出了非空结论"的保守近似，不是"目标真的推进了"的度量；
//   - 未装配 agent（无账号/completer）时显式报错，不静默返回空回合。

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/RedHuang-0622/Seele/session"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	goaldomain "github.com/RedHuang-0622/seelex/application/core/goal"
	seeltools "github.com/RedHuang-0622/seelex/seelebridge/tools"
)

// roleTurnMaxLoops 是员工回合的 ReAct 循环上限：员工回合必须**有界**——一轮员工
// 座位不该把一个 goal 的预算烧在一个循环里，收口交给评审者（ADVISOR）。
const roleTurnMaxLoops = 12

// roleTurnNoteLimit 是回合结论进面板摘要的字符上限（面板是一句话，不是正文）。
const roleTurnNoteLimit = 400

// roleEngine 是一个角色会话的引擎最小面。*session.Session 满足它；测试可注入
// 假实现（角色回合的验收不需要真实 LLM）。
type roleEngine interface {
	SessionID() string
	SetSystemPrompt(prompt string)
	SetMaxLoops(n int)
	ClearHistory()
	ChatStream(ctx context.Context, input string, onChunk func(string)) (string, error)
}

// roleSessionHandle 是一个角色会话的运行时槽：引擎 + 回合准入闸门。
//
// 锁纪律（2026-09-29 锁面审计 §2.2）：
//   - roundGate 只包"跑一轮"，是**叶子锁**：持它期间不得再取任何上层锁
//     （Supervisor.mu / Core.ViewMu / 会话与存储模块锁），回合内的代码也不得再取它；
//   - 它**不保护任何字段**：引擎在 handle 发布进状态表之前写入、之后只读，因此命名
//     上刻意不叫 mu/bundleMu——把"闸门"当成"句柄锁"用，就会重演"宽临界区顺带串行"
//     的老路（当年归档器只是"读 app.Snapshot()"，契约一漂移就是永久自锁）；
//   - 同 goroutine 重入由 ctx 在飞标记拦下（roleRoundInFlightFromContext：显式错误、
//     不排队）。非重入锁上排队等待就是永久挂死——2026-09-23"迭代边界注入撞会话锁"
//     是同一族的真实挂死现场。
type roleSessionHandle struct {
	id        string
	engine    roleEngine
	roundGate sync.Mutex
}

// ErrRoleRoundReentrant 表示同一角色会话在**本轮之内**被再次驱动（同 goroutine
// 重入）。串行闸门不可重入：这种调用若排队就是永久挂死，因此显式失败。
var ErrRoleRoundReentrant = errors.New("角色回合：同一角色会话在本轮内被再次驱动（闸门不可重入）")

// roleRoundInFlightKey 携带"本 ctx 链上正在跑的角色会话 ID"。
//
// 它的作用域**就是这一轮**：标记只挂在回合自己的 turnCtx 上（随 ChatStream 进入
// 引擎、再随工具调用向下传），调用方的 ctx 不受影响，因此回合结束后的下一次调用
// 不会被误判为重入。
type roleRoundInFlightKey struct{}

func withRoleRoundInFlight(ctx context.Context, roleSessionID string) context.Context {
	return context.WithValue(ctx, roleRoundInFlightKey{}, roleSessionID)
}

func roleRoundInFlightFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	id, _ := ctx.Value(roleRoundInFlightKey{}).(string)
	return id
}

// roleTurnState 是角色回合执行面的运行时状态（懒建；Runtime 零值可用）。
type roleTurnState struct {
	mu        sync.Mutex
	sessions  map[string]*roleSessionHandle
	newEngine func(sessionID string) (roleEngine, error)
}

// roleRoundSpec 描述"在某个角色会话上跑一轮带工具回合"所需的全部输入。
//
// 它是员工回合与 ADVISOR 评审回合的**公共形态**：主会话（项目根/归属）、角色身份
// （角色名 + 角色会话 + 权责口径 + 逐格权限）、系统提示、本轮正文、循环上限。
type roleRoundSpec struct {
	MainSessionID    string
	RoleName         string
	RoleSessionID    string
	ToolsPolicy      string
	PermissionGroups map[string]uint8
	SystemPrompt     string
	Input            string
	MaxLoops         int
	// FreshContext 表示"本轮的模型上下文完全由 Input 自带"，因此每次回合前清空引擎
	// 历史（回合之间不共享隐式记忆）。
	//
	// 为什么需要它：**员工**要连续的历史（同一个员工的多轮工作是一个连续过程），
	// 而 **ADVISOR 评审**恰恰相反——b 的上下文是显式构造的（锚点 + 帧账本 + 自身
	// 回合记忆渲染进 prompt），而且 goal 收口后会 reap peer 重新 bind。若引擎把
	// "上一个 goal 的评审对话"留在历史里，跨 goal 的上下文隔离就被绕过（这正是
	// A2A-VALUE-REVIEW §2.4 认为本设计最站得住的那一点）。清历史 = 隔离由构造保证，
	// 而不是靠"每轮都把完整上下文重新渲染一遍"的巧合。
	FreshContext bool
	// OnDelta 是可选的**进行中**观察回调：本回合的流式分片逐段回调给它。
	//
	// 为什么要有它：执行面（引擎 ChatStream）本来就是流式的，但调用方此前把 onChunk
	// 传成 nil，分片被丢掉——评审"正在写什么"在上层完全不可见，前端只能等终局裁决
	// （用户看到的"渲染不及时"）。它只把**后端产生的分片单向**送出去，不接收任何
	// 输入，也不参与回合结果（结果仍以返回值/裁决为准）。
	OnDelta func(delta string)
}

// SetRoleEngineFactory 注入角色会话引擎的构造器（nil = 回退默认的框架 Session）。
// 存在的理由有两个：① 自动化验收不需要真实 LLM；② 未来要给角色回合换更严格的
// 隔离面（worktree / 独立工具子集）时，改的是这一个构造点。
func (r *Runtime) SetRoleEngineFactory(factory func(sessionID string) (roleEngine, error)) {
	if r == nil {
		return
	}
	state := r.roleTurnState()
	state.mu.Lock()
	state.newEngine = factory
	state.mu.Unlock()
}

// roleTurnState 返回（必要时创建）角色回合状态。
func (r *Runtime) roleTurnState() *roleTurnState {
	r.roleTurnsMu.Lock()
	defer r.roleTurnsMu.Unlock()
	if r.roleTurns == nil {
		r.roleTurns = &roleTurnState{}
	}
	return r.roleTurns
}

// RunRoleTurn 实现 contract.RoleTurnPort：跑一个员工角色的一轮带工具回合。
//
// 顺序即语义见 runRoleRound（开角色会话即分配权限 → 绑项目根 → 按构造带主体 →
// 跑一轮有界回合），本方法只负责把回合结论收敛成面板可读的一句话。
func (r *Runtime) RunRoleTurn(ctx context.Context, request dto.RoleTurnRequest) (dto.RoleTurnOutcome, error) {
	roleName := strings.TrimSpace(request.RoleName)
	note, err := r.runRoleRound(ctx, roleRoundSpec{
		MainSessionID:    request.SessionID,
		RoleName:         roleName,
		RoleSessionID:    strings.TrimSpace(request.RoleSessionID),
		ToolsPolicy:      request.ToolsPolicy,
		PermissionGroups: request.PermissionGroups,
		SystemPrompt:     r.roleTurnSystemPrompt(roleName),
		Input:            roleTurnInput(roleName, request.Input),
		MaxLoops:         roleTurnMaxLoops,
	})
	if err != nil {
		return dto.RoleTurnOutcome{}, err
	}
	return dto.RoleTurnOutcome{
		Ran: true,
		// Progress 是保守近似：**有非空结论**即记一次进展。它的用途是喂环的
		// no_progress 逃生记账（连续无结论 → 收束），不是"目标真的推进了"的度量。
		Progress: note != "",
		Note:     truncateRunes(note, roleTurnNoteLimit),
	}, nil
}

// runRoleRound 是角色回合的公共执行原语：一轮角色回合的**顺序即语义**。
//
//  1. **开角色会话**（没有就开）：见 roleSessionFor——开的同时把该角色的权限
//     分配到权责表（"新开一个 session 的同时给引擎分配好权限"）；
//  2. **绑项目根**：角色会话按主会话的项目根解析工具路径（角色读的是同一个项目）；
//  3. **按构造带上角色主体**：ctx 里放角色名 + 权责档 + 逐格权限，判定与工具面都
//     落到这个角色自己的主体上；
//  4. **跑一轮有界回合**，返回原始输出（调用方决定怎么解释它：员工回合 = 面板一句话，
//     ADVISOR = TLDirective JSON）。
func (r *Runtime) runRoleRound(ctx context.Context, spec roleRoundSpec) (string, error) {
	if r == nil {
		return "", errors.New("角色回合：runtime 未装配")
	}
	roleName := strings.TrimSpace(spec.RoleName)
	roleSessionID := strings.TrimSpace(spec.RoleSessionID)
	if roleSessionID == "" {
		return "", errors.New("角色回合：角色会话 ID 是必填项")
	}
	if roleName == "" {
		return "", errors.New("角色回合：角色名是必填项（权限与提示词都按角色名落地）")
	}
	spec.RoleName, spec.RoleSessionID = roleName, roleSessionID

	// 同 goroutine 重入检测放在**任何副作用之前**（不开角色会话、不分配权限）：同一
	// 角色会话在本轮之内被再次驱动时（工具/回调里同步再发起一轮，用的是本轮向下传的
	// ctx），回合闸门不可重入——排队即永久挂死，所以显式失败。
	if inFlight := roleRoundInFlightFromContext(ctx); inFlight == roleSessionID {
		return "", fmt.Errorf("角色回合 %s（会话 %s）：%w", roleName, roleSessionID, ErrRoleRoundReentrant)
	}

	handle, err := r.roleSessionFor(spec)
	if err != nil {
		return "", err
	}
	r.bindRoleProjectRoot(strings.TrimSpace(spec.MainSessionID), roleSessionID)

	// 按构造把角色主体放进 ctx：不依赖任何反查（roleSessionClass 的索引可能冷），
	// 且工具面按这个角色自己的主体算（同档位的两个角色可以有不同能力面）。
	turnCtx := seeltools.WithEmployeeGrant(ctx, roleName, spec.ToolsPolicy, spec.PermissionGroups)
	// telemetry 标签写成角色会话：角色回合的 llm/tool 事件可按角色归因（审计面）。
	turnCtx = WithTelemetrySessionID(turnCtx, roleSessionID)

	// 在飞标记挂到**本轮**的 ctx 上：检测在上方（进函数处），标记在这里——ChatStream
	// 与它派生的工具调用因此都能看到"这一轮正在跑哪个角色会话"，本轮之内的重入请求
	// 才会被拦下（调用方自己的 ctx 不受影响）。
	turnCtx = withRoleRoundInFlight(turnCtx, roleSessionID)

	// 闸门只包"跑一轮"这一段（准入在内、执行在外）。引擎读在闸门外：handle 经
	// state.mu 发布、engine 在发布前写入，因此是安全的只读。
	engine := handle.engine
	handle.roundGate.Lock()
	if spec.FreshContext {
		// 隔离由构造保证：本轮的上下文完全由 spec.Input 自带（见 roleRoundSpec.FreshContext）。
		engine.ClearHistory()
	}
	output, err := engine.ChatStream(turnCtx, spec.Input, spec.OnDelta)
	handle.roundGate.Unlock()
	if err != nil {
		// 执行面出错向上抛（治理循环透传）：不能吞成"没产出"，否则环会把它记成
		// 无进展并逃生，把模型/权限/存储的故障掩盖成"团队不干活"。
		return "", fmt.Errorf("角色回合 %s 失败: %w", roleName, err)
	}
	return strings.TrimSpace(output), nil
}

// roleSessionFor 取（必要时开）角色会话的引擎，并在**开的那一刻**把这个角色的权限
// 分配到权责表。
//
// "开的同时分配"不是顺序上的巧合：角色是在这一刻进入执行面的（谁在编、什么权责
// 到这一刻才随请求确定），权限分配必须与它同一个动作——否则会出现"已经在跑回合、
// 权限还没分配"的窗口，该窗口里角色回合按什么判都没有依据。
func (r *Runtime) roleSessionFor(spec roleRoundSpec) (*roleSessionHandle, error) {
	state := r.roleTurnState()
	roleSessionID := spec.RoleSessionID

	state.mu.Lock()
	if handle := state.sessions[roleSessionID]; handle != nil {
		state.mu.Unlock()
		return handle, nil
	}
	state.mu.Unlock()

	// 分配角色权限（路径与装配期同一个：AssignEmployeePermissions）。口径不可分配
	// （空/full/未识别且没有显式格子）时不写条目 = 真的继承宿主默认，不拿自造条目
	// 冒充继承。
	if err := r.AssignEmployeePermissions([]dto.RoleSpec{{
		RoleName:         spec.RoleName,
		ToolsPolicy:      spec.ToolsPolicy,
		PermissionGroups: spec.PermissionGroups,
	}}); err != nil {
		return nil, fmt.Errorf("角色回合：分配角色权限失败: %w", err)
	}

	engine, err := r.newRoleEngine(roleSessionID)
	if err != nil {
		return nil, err
	}
	engine.SetSystemPrompt(spec.SystemPrompt)
	if spec.MaxLoops > 0 {
		engine.SetMaxLoops(spec.MaxLoops)
	}

	handle := &roleSessionHandle{id: roleSessionID, engine: engine}
	state.mu.Lock()
	if state.sessions == nil {
		state.sessions = make(map[string]*roleSessionHandle)
	}
	// 并发下可能已被别人开好：保留先到的那一个（幂等，不泄漏第二个引擎）。
	if existing := state.sessions[roleSessionID]; existing != nil {
		state.mu.Unlock()
		return existing, nil
	}
	state.sessions[roleSessionID] = handle
	state.mu.Unlock()
	return handle, nil
}

// newRoleEngine 造一个角色会话的引擎：优先用注入的构造器（测试/严格隔离面），
// 否则用框架 Session（own loop + **节点级**上下文组件 + 独立 SessionID）。
//
// 为什么用节点级（而不是主会话级）上下文组件：主会话级的上下文控制器把压缩栈
// 锚在"当前活跃会话"（r.sessionContextStore() / r.MainSessionID）。角色回合一旦
// 触发压缩，帧就会写进**主会话**的压缩栈——那是跨会话写，不是隔离面。节点级
// 控制器用内存压缩栈 + "node" 段 ID，角色回合的上下文因此完全自成一体。
//
// 刻意**不接 DurableHistory**：角色历史与主会话存储是两件事；把角色回合写进主
// 会话的 provider 历史会污染主会话上下文（而且角色会话号不含主会话身份）。
func (r *Runtime) newRoleEngine(sessionID string) (roleEngine, error) {
	state := r.roleTurnState()
	state.mu.Lock()
	factory := state.newEngine
	state.mu.Unlock()
	if factory != nil {
		return factory(sessionID)
	}
	if r.agt == nil {
		return nil, errors.New("角色回合：agent 未装配（无账号/completer 时角色回合不可用）")
	}
	context := r.nodeContextComponents()
	if r.node == nil {
		// 节点协调器缺失（部分装配/测试基座）时退回主会话组件：功能可用，但
		// 隔离性降级（压缩栈会锚到活跃会话）。生产装配里 r.node 始终就绪。
		context = r.mainContextComponents()
	}
	sess, err := session.NewSession(session.SessionComponents{
		Agent:     r.agt,
		Context:   context,
		Telemetry: r.hook,
		// ReAct 钩子把角色回合的**工具步骤**接到 goal 域的过程观察面（tl_steps.go）：
		// ADVISOR 评审的只读工具调用因此能被前端看见（"评审过程"），而不是只活在
		// 这个进程内会话里。钩子按 ctx 取 sink：员工回合没有 sink（取到 nil），
		// 因此这条路径对员工回合是零成本 no-op。
		Hooks:     roleLoopHooks(),
		SessionID: sessionID,
		ModelName: r.model,
		Config:    session.SessionConfig{MaxLoops: roleTurnMaxLoops},
	})
	if err != nil {
		return nil, fmt.Errorf("角色回合：创建角色会话 %q 失败: %w", sessionID, err)
	}
	return sess, nil
}

// roleLoopHooks 返回角色回合的 ReAct 钩子集合：只做一件事——把工具调用与返回
// 转成 goal 域的过程步骤（TLStep），送到**本轮 ctx 上挂的 sink**。
//
// 为什么用 ctx 而不是给 Session 传固定回调：角色会话引擎是**按角色会话缓存**的
// （一个角色开一次），而 sink 是**按回合**的（每轮 Supervisor 都会重新挂）。把
// sink 放 ctx 正好让"引擎活得久、回调只活一轮"两件事各归其位；流程结束 sink 失效，
// 不会把上一轮的观察面带到下一轮。
//
// 为什么只送工具步骤、不送模型正文：正文分片已经由 OnDelta 通道负责（见
// runtime_goal_tl.go）；两句分开可以让面板分别控制粒度——工具步骤是"可核对的事实"，
// 模型正文是"正在写什么"。
func roleLoopHooks() *session.LoopHooks {
	return &session.LoopHooks{
		OnToolStart: func(ctx context.Context, info session.ToolCallInfo) {
			sink := goaldomain.TLStepSinkFrom(ctx)
			if sink == nil {
				return
			}
			sink(goaldomain.TLStep{
				Kind: "tool", Turn: info.Turn, Name: info.Name,
				Args: truncateRunes(info.Arguments, goaldomain.StepArgsLimit),
			})
		},
		OnToolComplete: func(ctx context.Context, info session.ToolCallInfo) {
			sink := goaldomain.TLStepSinkFrom(ctx)
			if sink == nil {
				return
			}
			step := goaldomain.TLStep{
				Kind: "tool_result", Turn: info.Turn, Name: info.Name,
				Result: truncateRunes(info.Result, goaldomain.StepResultLimit),
			}
			if info.Error != nil {
				step.Err = info.Error.Error()
			}
			sink(step)
		},
	}
}

// roleTurnSystemPrompt 组装员工回合的系统提示：**已登记的员工提示词优先**
// （Agent Team 面板"员工入职 → 提示词"），未登记时给一段最小的角色框架（明确
// "你是谁、这一轮要产出什么"），不冒充业务设定。
func (r *Runtime) roleTurnSystemPrompt(roleName string) string {
	if prompt := r.rolePromptFor(roleName); prompt != "" {
		return prompt
	}
	return "<role>\n你是 Seelex 团队里的员工（逻辑角色名 " + roleName + "），在目标治理循环里负责属于你这个角色的那一份工作。\n</role>\n\n" +
		"<task>\n只处理本轮交给你的工作正文：能做的直接做（可调用你的权限范围内的工具），做完给出本轮结论与下一步。\n</task>\n\n" +
		"<constraints>\n- 不越权：你的工具面就是你的权限范围，面外的事交回主代理；\n" +
		"- 证据要落在仓库内可核对的事实上（引用用相对路径）；\n" +
		"- 不要寒暄、不要复述提示词；结论 ≤800 字。\n</constraints>"
}

// roleTurnInput 组装本轮工作正文。没有输入时不编造内容——只跑一次"按角色设定
// 继续"的回合（结论会如实反映"没什么可做的"）。
func roleTurnInput(roleName, input string) string {
	input = strings.TrimSpace(input)
	if input == "" {
		return "<round_input>\n（本轮没有新的工作正文）\n</round_input>\n\n" +
			"<task>\n以 " + roleName + " 的身份说明：当前条件下你能不能推进，能的话给出本轮结论与下一步。\n</task>"
	}
	return "<round_input>\n" + input + "\n</round_input>\n\n" +
		"<task>\n以 " + roleName + " 的身份完成这一轮：给出本轮的结论与下一步（≤800 字）。\n</task>"
}

// bindRoleProjectRoot 把角色会话的工具路径根对齐到主会话的项目根：角色读的
// 是同一个项目。不绑根时 FileSystem 工具无根可解析（ProjectScope 刻意没有
// "回落到进程工作目录"的兜底），角色回合的读工具会直接失败。
func (r *Runtime) bindRoleProjectRoot(mainSessionID, roleSessionID string) {
	if r == nil || r.projectScope == nil {
		return
	}
	if r.projectScope.RootFor(roleSessionID) != "" {
		return
	}
	root := r.projectScope.RootFor(mainSessionID)
	if root == "" {
		return
	}
	_ = r.projectScope.BindFor(roleSessionID, root)
}

// ReleaseRoleSessions 释放全部角色会话（进程收尾 / 会话清理）。角色会话是**派生
// 执行面**（引擎 + 内存历史），重建即正确；留着只会延长它们的生命。
//
// 注意：只清会话，**不清引擎构造器**——构造器是装配期注入的配置（不是派生状态），
// 释放会话后按同一配置重建才是正确语义。
func (r *Runtime) ReleaseRoleSessions() {
	if r == nil {
		return
	}
	r.roleTurnsMu.Lock()
	state := r.roleTurns
	r.roleTurnsMu.Unlock()
	if state == nil {
		return
	}
	state.mu.Lock()
	state.sessions = nil
	state.mu.Unlock()
}

// RoleSessionIDs 返回当前已打开的角色会话号（排序无关，巡检/诊断面）。
func (r *Runtime) RoleSessionIDs() []string {
	if r == nil {
		return nil
	}
	r.roleTurnsMu.Lock()
	state := r.roleTurns
	r.roleTurnsMu.Unlock()
	if state == nil {
		return nil
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	ids := make([]string, 0, len(state.sessions))
	for id := range state.sessions {
		ids = append(ids, id)
	}
	return ids
}
