package seelebridge

// runtime_role_turn_test.go — 「角色回合执行体」的验收面。
//
// 三件事必须同时成立，角色才不是"发言权"而是"做工权"：
//  1. 轮到一个 agent 角色时，它真的跑了一轮（在**自己的会话**上，不是主会话）；
//  2. 开角色会话的那一刻，这个员工（角色）的权限被分配到权责表（emp_<角色>）；
//  3. 该回合的工具调用按**员工口径**解析主体（工具面被权责收窄），而不是落回 root。
//
// 假引擎替代真实 LLM：角色回合的正确性不该依赖一次真实 API 调用（那是"真实 API
// 冒烟"那一件事），这里钉的是执行体自己的装配与授权语义。

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	seetelemetry "github.com/RedHuang-0622/seelex/seelebridge/internal/telemetry"
	seeltools "github.com/RedHuang-0622/seelex/seelebridge/tools"
)

// fakeRoleEngine 是角色会话引擎的桩：记录系统提示/循环上限/输入/ctx，不碰网络。
type fakeRoleEngine struct {
	mu       sync.Mutex
	id       string
	prompt   string
	maxLoops int
	inputs   []string
	ctxs     []context.Context
	output   string
	err      error
	clears   int
	// deltas 是本桩"模型"输出的流式分片：非空时逐段回调 onChunk（钉 OnDelta 的
	// 转发——回合执行面本来就是流式的，旧实现把 onChunk 传成 nil 会丢掉这些分片）。
	deltas []string
}

func (engine *fakeRoleEngine) SessionID() string { return engine.id }

func (engine *fakeRoleEngine) SetSystemPrompt(prompt string) {
	engine.mu.Lock()
	engine.prompt = prompt
	engine.mu.Unlock()
}

// ClearHistory 是 FreshContext 回合的隔离手段（评审回合每轮前清历史）。
func (engine *fakeRoleEngine) ClearHistory() {
	engine.mu.Lock()
	engine.clears++
	engine.mu.Unlock()
}

// clearCount 返回被要求清历史的次数。
func (engine *fakeRoleEngine) clearCount() int {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	return engine.clears
}

func (engine *fakeRoleEngine) SetMaxLoops(n int) {
	engine.mu.Lock()
	engine.maxLoops = n
	engine.mu.Unlock()
}

func (engine *fakeRoleEngine) ChatStream(ctx context.Context, input string, onChunk func(string)) (string, error) {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	engine.inputs = append(engine.inputs, input)
	engine.ctxs = append(engine.ctxs, ctx)
	if engine.err != nil {
		return "", engine.err
	}
	for _, delta := range engine.deltas {
		if onChunk != nil {
			onChunk(delta)
		}
	}
	return engine.output, nil
}

func (engine *fakeRoleEngine) snapshot() (prompt string, maxLoops, calls int, ctxs []context.Context) {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	return engine.prompt, engine.maxLoops, len(engine.inputs), append([]context.Context(nil), engine.ctxs...)
}

// newRoleTurnRuntime 造一个装了假引擎工厂的 Runtime，权限面按生产默认配置装配。
func newRoleTurnRuntime(t *testing.T) (*Runtime, *fakeRoleEngine, *int) {
	t.Helper()
	runtime := newTestRuntime(t)
	runtime.SetPermissionConfig(seeltools.DefaultPermissionConfig(), nil)
	engine := &fakeRoleEngine{output: "拆出 3 个子任务，下一步验证边界"}
	created := 0
	runtime.SetRoleEngineFactory(func(sessionID string) (roleEngine, error) {
		created++
		engine.id = sessionID
		return engine, nil
	})
	return runtime, engine, &created
}

// TestRoleTurnRunsOnRoleSessionAndAssignsEmployeePermission 钉住"开角色会话的同时
// 分配该员工的权限"，并确认回合真的落在角色会话上（按角色提示 + 有界循环）。
func TestRoleTurnRunsOnRoleSessionAndAssignsEmployeePermission(t *testing.T) {
	runtime, engine, created := newRoleTurnRuntime(t)
	outcome, err := runtime.RunRoleTurn(context.Background(), dto.RoleTurnRequest{
		SessionID:     "sess-main",
		RoleName:      "pm",
		RoleSessionID: "goal-a2a-pm",
		ToolsPolicy:   dto.ToolPolicyReadonly,
		Input:         "本轮工作正文：把目标拆成可验证的子任务",
	})
	if err != nil {
		t.Fatalf("RunRoleTurn: %v", err)
	}
	if !outcome.Ran || !outcome.Progress {
		t.Fatalf("回合结论 = %+v，应有产出", outcome)
	}
	if outcome.Note != "拆出 3 个子任务，下一步验证边界" {
		t.Fatalf("Note = %q", outcome.Note)
	}

	prompt, maxLoops, calls, _ := engine.snapshot()
	if calls != 1 {
		t.Fatalf("角色回合调用次数 = %d", calls)
	}
	if strings.TrimSpace(prompt) == "" {
		t.Fatal("角色会话必须装系统提示（未登记员工提示词时也要有最小角色框架）")
	}
	if !strings.Contains(prompt, "pm") {
		t.Fatalf("未登记提示词时的最小框架应点明角色名，得到 %q", prompt)
	}
	if maxLoops != roleTurnMaxLoops {
		t.Fatalf("角色回合必须有界：maxLoops = %d, want %d", maxLoops, roleTurnMaxLoops)
	}
	if *created != 1 {
		t.Fatalf("角色会话引擎构造次数 = %d, want 1", *created)
	}
	if ids := runtime.RoleSessionIDs(); len(ids) != 1 || ids[0] != "goal-a2a-pm" {
		t.Fatalf("已开角色会话 = %v", ids)
	}

	// 开的同时分配权限：一条主体条目 emp_pm（与用户权限同一张表）。
	permissions := runtime.EmployeePermissions()
	if len(permissions) != 1 {
		t.Fatalf("装配期员工权限 = %+v, want 一条 emp_pm", permissions)
	}
	if permissions[0].RoleName != "pm" || permissions[0].Policy != dto.ToolPolicyReadonly {
		t.Fatalf("员工权限 = %+v, want {pm readonly}", permissions[0])
	}
}

// TestRoleTurnContextYieldsEmployeeToolFace 钉住"按构造授权"：角色回合的 ctx 一
// 进执行面就带着员工主体，工具面因此按**这个员工自己的主体**收窄——readonly 的
// 员工看不到写工具，readwrite 的看得到。这条不依赖任何"角色会话 → 权责"反查
// （那个索引可能冷启动没查到）。
func TestRoleTurnContextYieldsEmployeeToolFace(t *testing.T) {
	runtime, engine, _ := newRoleTurnRuntime(t)
	cases := []struct {
		role      string
		policy    string
		wantWrite bool
	}{
		{role: "reviewer", policy: dto.ToolPolicyReadonly, wantWrite: false},
		{role: "worker", policy: dto.ToolPolicyReadWrite, wantWrite: true},
	}
	for _, item := range cases {
		engine.output = "本轮结论"
		if _, err := runtime.RunRoleTurn(context.Background(), dto.RoleTurnRequest{
			SessionID:     "sess-main",
			RoleName:      item.role,
			RoleSessionID: "goal-a2a-" + item.role,
			ToolsPolicy:   item.policy,
			Input:         "工作正文",
		}); err != nil {
			t.Fatalf("RunRoleTurn(%s): %v", item.role, err)
		}
	}
	engine.mu.Lock()
	ctxs := append([]context.Context(nil), engine.ctxs...)
	engine.mu.Unlock()
	if len(ctxs) != 2 {
		t.Fatalf("回合数 = %d", len(ctxs))
	}

	if !runtime.permission.ToolFaceForContext(ctxs[0], "read_file") {
		t.Fatal("readonly 员工的读工具应在面上")
	}
	if runtime.permission.ToolFaceForContext(ctxs[0], "write_file") {
		t.Fatal("readonly 员工的写工具不该在面上（工具面就是授权范围）")
	}
	if !runtime.permission.ToolFaceForContext(ctxs[1], "write_file") {
		t.Fatal("readwrite 员工的写工具应在面上")
	}
	// telemetry 会话标签写成角色会话：角色回合的 llm/tool 事件可按角色归因。
	if got := seetelemetry.SessionIDFromContext(ctxs[1]); got != "goal-a2a-worker" {
		t.Fatalf("角色回合的会话标签 = %q, want goal-a2a-worker", got)
	}
}

// TestRoleTurnReusesRoleSessionAcrossTurns：同一角色会话复用同一个引擎（员工有
// 连续的历史），而不是每轮重开一个（重开 = 每轮都失忆）。
func TestRoleTurnReusesRoleSessionAcrossTurns(t *testing.T) {
	runtime, engine, created := newRoleTurnRuntime(t)
	request := dto.RoleTurnRequest{
		SessionID: "sess-main", RoleName: "pm", RoleSessionID: "goal-a2a-pm",
		ToolsPolicy: dto.ToolPolicyReadonly, Input: "第一轮",
	}
	if _, err := runtime.RunRoleTurn(context.Background(), request); err != nil {
		t.Fatalf("第一轮: %v", err)
	}
	request.Input = "第二轮"
	if _, err := runtime.RunRoleTurn(context.Background(), request); err != nil {
		t.Fatalf("第二轮: %v", err)
	}
	if *created != 1 {
		t.Fatalf("同一角色会话应复用引擎：构造次数 = %d", *created)
	}
	_, _, calls, _ := engine.snapshot()
	if calls != 2 {
		t.Fatalf("回合数 = %d, want 2", calls)
	}
}

// TestRoleTurnPropagatesEngineError：执行面出错必须向上抛。吞成"没产出"会让治理
// 循环按无进展逃生，把真正的故障（模型/权限/存储）掩盖成"团队不干活"。
func TestRoleTurnPropagatesEngineError(t *testing.T) {
	runtime, engine, _ := newRoleTurnRuntime(t)
	sentinel := errors.New("模型连接断了")
	engine.err = sentinel
	if _, err := runtime.RunRoleTurn(context.Background(), dto.RoleTurnRequest{
		SessionID: "sess-main", RoleName: "pm", RoleSessionID: "goal-a2a-pm", ToolsPolicy: dto.ToolPolicyReadonly,
	}); !errors.Is(err, sentinel) {
		t.Fatalf("角色回合错误必须可被 errors.Is 归类，得到 %v", err)
	}
}

// TestRoleTurnRejectsMissingIdentity：角色名与角色会话是权限落地的锚点，缺一不跑。
func TestRoleTurnRejectsMissingIdentity(t *testing.T) {
	runtime, _, created := newRoleTurnRuntime(t)
	cases := []dto.RoleTurnRequest{
		{RoleName: "pm"},
		{RoleSessionID: "goal-a2a-pm"},
	}
	for _, request := range cases {
		if _, err := runtime.RunRoleTurn(context.Background(), request); err == nil {
			t.Fatalf("缺身份应显式失败：%+v", request)
		}
	}
	if *created != 0 {
		t.Fatal("身份不全时不该开任何角色会话")
	}
}

// TestRoleTurnInheritedPolicyAssignsNothing：空/full 口径是"继承宿主默认"，
// 必须真的是继承——不写一条自造的员工主体条目冒充继承。
func TestRoleTurnInheritedPolicyAssignsNothing(t *testing.T) {
	runtime, _, _ := newRoleTurnRuntime(t)
	for _, policy := range []string{dto.ToolPolicyInherit, dto.ToolPolicyFull} {
		if _, err := runtime.RunRoleTurn(context.Background(), dto.RoleTurnRequest{
			SessionID: "sess-main", RoleName: "owner", RoleSessionID: "goal-a2a-owner", ToolsPolicy: policy,
		}); err != nil {
			t.Fatalf("RunRoleTurn(%q): %v", policy, err)
		}
	}
	if permissions := runtime.EmployeePermissions(); len(permissions) != 0 {
		t.Fatalf("继承宿主默认的员工不该有自造主体条目：%+v", permissions)
	}
}

// TestReleaseRoleSessionsDropsEngines：角色会话是派生执行面，释放后重开即正确
// （不释放就等于角色与进程同寿）。
func TestReleaseRoleSessionsDropsEngines(t *testing.T) {
	runtime, _, created := newRoleTurnRuntime(t)
	request := dto.RoleTurnRequest{
		SessionID: "sess-main", RoleName: "pm", RoleSessionID: "goal-a2a-pm", ToolsPolicy: dto.ToolPolicyReadonly,
	}
	if _, err := runtime.RunRoleTurn(context.Background(), request); err != nil {
		t.Fatalf("第一轮: %v", err)
	}
	runtime.ReleaseRoleSessions()
	if ids := runtime.RoleSessionIDs(); len(ids) != 0 {
		t.Fatalf("释放后仍有角色会话：%v", ids)
	}
	if _, err := runtime.RunRoleTurn(context.Background(), request); err != nil {
		t.Fatalf("释放后重开: %v", err)
	}
	if *created != 2 {
		t.Fatalf("释放后重开应重建引擎：构造次数 = %d", *created)
	}
}

// TestRoleEngineDefaultKeepsRoleSessionIdentity：默认引擎（未注入构造器）必须用
// **角色会话号**建框架 Session——引擎身份错了，权限归属（按会话解析）与 telemetry
// 归因就都错了。这条不碰网络：只造会话，不跑 ChatStream。
func TestRoleEngineDefaultKeepsRoleSessionIdentity(t *testing.T) {
	runtime := newTestRuntime(t)
	engine, err := runtime.newRoleEngine("goal-a2a-tl")
	if err != nil {
		t.Fatalf("newRoleEngine: %v", err)
	}
	if engine == nil {
		t.Fatal("默认引擎不应为 nil")
	}
	if got := engine.SessionID(); got != "goal-a2a-tl" {
		t.Fatalf("角色会话引擎身份 = %q, want goal-a2a-tl", got)
	}
}
