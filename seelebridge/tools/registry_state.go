package tools

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	frameworktools "github.com/RedHuang-0622/Seele/tools"
	toolspermission "github.com/RedHuang-0622/Seele/tools/permission"
	"github.com/RedHuang-0622/Seele/types"
)

// RegistryState 包装 framework tools.Registry：内联工具 provider 由
// AddInline 累积维护，注册表快照在每次增删后重建。
type RegistryState struct {
	Registry *frameworktools.Registry
	inline   *InlineProvider
}

// NewRegistryState 构造带超时/权限门/事件与诊断中间件的工具注册表。
//
// 事件与权限门走 MetaMiddleware（权限判定要吃工具自带的簇属 ToolMeta），
// 诊断留在普通链上。框架先套普通中间件、再套 Meta 中间件，因此两种写法的
// 实际嵌套都是 事件 → 权限门 → 诊断 → handler，与历史顺序一致。
func NewRegistryState(timeout time.Duration, permission *PermissionGate, approvalTimeout time.Duration, eventMiddleware, diagnosticMiddleware frameworktools.Middleware) *RegistryState {
	return &RegistryState{Registry: frameworktools.NewRegistry(
		frameworktools.WithCallTimeout(timeout),
		frameworktools.WithMiddleware(diagnosticMiddleware),
		frameworktools.WithMetaMiddleware(asMetaMiddleware(eventMiddleware), permission.Middleware(approvalTimeout)),
	)}
}

// asMetaMiddleware 把不吃簇属的普通中间件提升为 MetaMiddleware（忽略 meta）。
func asMetaMiddleware(mw frameworktools.Middleware) frameworktools.MetaMiddleware {
	if mw == nil {
		return nil
	}
	return func(name string, _ frameworktools.ToolMeta, next frameworktools.ToolHandler) frameworktools.ToolHandler {
		return mw(name, next)
	}
}

// AddInline 注册一个普通产品工具（等价旧 holder.RegisterInline，重名覆盖）。
func (s *RegistryState) AddInline(
	name, description string,
	inputSchema map[string]interface{},
	handler func(context.Context, string) (string, error),
) {
	if s == nil || s.Registry == nil {
		return
	}
	if s.inline == nil {
		s.inline = &InlineProvider{}
		_ = s.Registry.Register(s.inline)
	}
	s.inline.upsert(frameworktools.ToolEntry{
		Definition: types.Tool{
			Type: "function",
			Function: types.ToolFunction{
				Name: name, Description: description, Parameters: inputSchema,
			},
		},
		Handler: frameworktools.HandlerFunc(handler),
	})
	// 重建快照使新工具立即可见（注册表只读锁调度，快照重建线程安全）。
	_ = s.Registry.Unregister(s.inline.ProviderName())
	_ = s.Registry.Register(s.inline)
}

// FindTool 按名称在注册表中查找工具（plan 工具面读取）。
func (s *RegistryState) FindTool(name string) (types.Tool, bool) {
	if s == nil || s.Registry == nil {
		return types.Tool{}, false
	}
	for _, tool := range s.Registry.Tools() {
		if tool.Function.Name == name {
			return tool, true
		}
	}
	return types.Tool{}, false
}

// InlineProvider 累积 RegisterTool 注册的普通产品工具。
// framework tools.Registry 不允许同名工具重复，AddInline 在添加前按名称去重。
type InlineProvider struct {
	mu      sync.Mutex
	entries []frameworktools.ToolEntry
}

// ProviderName 返回内联 provider 名称。
func (p *InlineProvider) ProviderName() string { return "seelex-inline" }

// Tools 返回全部内联工具（只读拷贝）。
func (p *InlineProvider) Tools() []frameworktools.ToolEntry {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]frameworktools.ToolEntry(nil), p.entries...)
}

func (p *InlineProvider) upsert(entry frameworktools.ToolEntry) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for index := range p.entries {
		if p.entries[index].Definition.Function.Name == entry.Definition.Function.Name {
			p.entries[index] = entry
			return
		}
	}
	p.entries = append(p.entries, entry)
}

// PermissionGate 是权限门控的可变状态：middleware 在注册表构造时闭包捕获
// 它，Set/SetFullAccess* 运行时原子更新。
//
// 全权（full access）是**会话级**的用户决定（application 侧归属进
// SessionUnit，G4）：执行面必须按工具调度 ctx 的会话归属解析，绝不能把
// 它放进程级布尔上——进程级布尔有两个真实后果：
//   - 污染：A 会话点全权 → B 会话的工具调用被静默放行（B 未同意）；
//   - 失灵：B 会话的 chat 起点同步（syncFullAccessFor）会把 A 的全权关掉，
//     表现为「点了全权仍弹审批/仍被拒」——多会话并行下必现（单飞时代只是
//     侥幸没暴露）。
type PermissionGate struct {
	mu      sync.RWMutex
	checker *toolspermission.PermissionChecker
	handler toolspermission.ApprovalHandler
	// cfg 是最近一次 Set 的权限配置快照：主体类策略（Enforcer）用它做路由与
	// 分封判断，保证"谁有权限"只有一份事实（分组表 + 主体授权表）。
	cfg toolspermission.PermissionConfig
	// RoleSessionPolicy 是"角色会话（员工）→ ToolsPolicy"的解析器，由组合根
	// （main.go，读角色注册表）注入；nil = 不做员工主体识别（全部按 root 判）。
	// 它只被非子代理调用使用，结果按 rolePolicyCacheTTL 缓存。
	RoleSessionPolicy func(sessionID string) (string, bool)
	// rolePolicyCache 缓存 RoleSessionPolicy 的结果（含未命中），由 mu 保护。
	rolePolicyCache map[string]rolePolicyCacheEntry
	// sessionFullAccess 是会话级全权选择（键 = 会话 ID）。空串键是**进程级
	// 默认**（CLI -permission full_access / 未做会话级选择的回退面）；未选择
	// 的会话回退进程默认，不继承别的会话的开关。
	//
	// middleware **先读它**、再读 checker：用户点击全权后，即使 checker 实例
	// 被替换（Set 重建）或 checker 尚未装配，也不会出现「权限检查还走旧
	// manual 规则 / 落在 nil checker 上」的窗口。
	sessionFullAccess map[string]bool
	// SessionFromContext 从工具调度 ctx 提取会话归属（seelebridge 根包
	// 注入 seelebridge 会话路由键；nil = 权限审批保持进程级空归属回退）。
	SessionFromContext func(ctx context.Context) string
}

// Set 装配权限配置与审批处理器。checker 只承载 manual 规则：全权由
// sessionFullAccess 在 middleware 最前面短路，因此 Set 重建 checker 不会
// 把任何会话的全权吃掉（历史缺陷：全权只记一个进程级布尔，Set 后被退回
// manual）。
func (state *PermissionGate) Set(cfg toolspermission.PermissionConfig, handler toolspermission.ApprovalHandler) {
	state.mu.Lock()
	state.checker = toolspermission.NewPermissionChecker(cfg)
	state.handler = handler
	state.cfg = cfg
	state.mu.Unlock()
}

// SetRoleSessionPolicyResolver 注入"角色会话 → 员工权限口径（ToolsPolicy）"解析器。
// 注入后，员工角色会话里的工具调用按 ToolsPolicy 判成 emp_ro / emp_rw 主体：
// 位齐 → 组默认/规则，位缺（违权）→ 执行选择页面提权。传 nil 关闭员工主体识别。
func (state *PermissionGate) SetRoleSessionPolicyResolver(resolver func(sessionID string) (string, bool)) {
	state.mu.Lock()
	state.RoleSessionPolicy = resolver
	state.rolePolicyCache = nil // 解析器换人 → 缓存作废
	state.mu.Unlock()
}

// SetFullAccess 设置**进程级默认**全权（CLI -permission full_access 与
// 装配期基线用；等价 SetFullAccessFor("", on)）。
func (state *PermissionGate) SetFullAccess(on bool) {
	state.SetFullAccessFor("", on)
}

// SetFullAccessFor 设置指定会话的全权选择（空会话 ID = 进程级默认）。
// 只影响该会话自己的工具调度，不触碰其它会话的选择。
func (state *PermissionGate) SetFullAccessFor(sessionID string, on bool) {
	state.mu.Lock()
	if state.sessionFullAccess == nil {
		state.sessionFullAccess = make(map[string]bool)
	}
	state.sessionFullAccess[sessionID] = on
	state.mu.Unlock()
}

// FullAccess 返回进程级默认全权（装配期捕获面；未做会话级选择的回退值）。
func (state *PermissionGate) FullAccess() bool {
	state.mu.RLock()
	defer state.mu.RUnlock()
	return state.sessionFullAccess[""]
}

// FullAccessFor 返回指定会话生效的全权模式：会话级选择优先，未选择回退
// 进程级默认（与 middleware 同源解析，供探针/诊断读取）。
func (state *PermissionGate) FullAccessFor(sessionID string) bool {
	state.mu.RLock()
	defer state.mu.RUnlock()
	return state.effectiveFullAccessLocked(sessionID)
}

// effectiveFullAccessLocked 解析生效的全权模式（调用方持锁）。
func (state *PermissionGate) effectiveFullAccessLocked(sessionID string) bool {
	if on, ok := state.sessionFullAccess[sessionID]; ok {
		return on
	}
	return state.sessionFullAccess[""]
}

// Middleware 把一次工具调用接到框架 permission.Gate 上：**判定完全交给框架**
// （工具自带簇属 → 位/组路由 → 规则），harness 只提供三件东西——授权表
// （checker）、执行选择页面（ApprovalHandler）、会话级提权（Enforce）。
//
// 会话归属与**主体**随调度 ctx 透出：会话 ID 走 WithSessionID（审批请求据此
// 路由回正确会话视图），主体走 WithEngine（框架据此查授权表）。主体由"谁在
// 调用"解析（见 permission_policy.go）：子代理 = sub、员工角色会话 = emp_ro /
// emp_rw、其余 = root。
//
// 全权短路同样按调用 ctx 的会话归属解析（A 会话的全权不替 B 会话放行；B 的
// 起点同步也关不掉 A）：全权是用户的显式决定，不允许任何"还没轮到规则"的
// 时序把它降级成询问或拒绝，也不允许它越会话传播。
func (state *PermissionGate) Middleware(approvalTimeout time.Duration) frameworktools.MetaMiddleware {
	return func(name string, meta frameworktools.ToolMeta, next frameworktools.ToolHandler) frameworktools.ToolHandler {
		return frameworktools.HandlerFunc(func(ctx context.Context, argsJSON string) (string, error) {
			sessionID := state.sessionFromContext(ctx)
			class := state.classFor(ctx)
			ctx = withSubjectClass(ctx, class)
			ctx = toolspermission.WithEngine(ctx, toolspermission.Engine(SubjectForClass(class)))
			if sessionID != "" {
				ctx = toolspermission.WithSessionID(ctx, sessionID)
			}
			if err := state.gate(approvalTimeout, class).Decide(ctx, name, meta, policyArgsFor(name, argsJSON)); err != nil {
				return "", err
			}
			return next.Execute(ctx, argsJSON)
		})
	}
}

// policyArgsFor 给判定换一份"模式匹配用参数"。命令行类工具（bash）的规则写的是
// **命令模式**（"git *" / "rm -rf /*"），而工具入参是 `{"command":"..."}` 的 JSON：
// 直接拿整串 JSON 去匹配模式，等于规则里的 patterns 全部失效（只留下"匹配不上"
// 的兜底规则）。这里把命令本体抽出来交给 checker，让 args 级能力白名单真正生效；
// 真正传给工具的仍是原始 argsJSON。
func policyArgsFor(name, argsJSON string) string {
	switch name {
	case "bash":
		var input struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal([]byte(argsJSON), &input); err == nil {
			if command := strings.TrimSpace(input.Command); command != "" {
				return command
			}
		}
	}
	return argsJSON
}

// gate 组装框架判定器：checker 与审批处理器在调用瞬间快照，因此 Set 重建
// checker 不会让在途调用落在旧表或 nil 上。
//
// DenyWithoutPrompt：seelex 的 deny 是用户显式配置的拒绝，维持"直接拒绝"的
// 语义（返回可 errors.Is 归类的英文错误：策略拒绝 / 不在该 engine 的命名
// 空间）；只有 ask（无命中规则）才呈现执行选择页面。框架默认的"拒绝也走选择
// 页面"是另一套产品语义，要采纳时去掉这个开关即可。
//
// 子代理（sub 主体）没有人类在环：Approval 置 nil，框架的 ask 分支直接返回
// 拒绝（`approve` 里 Approval==nil → DenialError），位缺（违权）本来就走
// denyOrPrompt 的拒绝分支——即"违权操作直接拒绝"，绝不挂起等一个不会有人回答
// 的选择页面。
func (state *PermissionGate) gate(approvalTimeout time.Duration, class SubjectClass) *toolspermission.Gate {
	state.mu.RLock()
	checker, handler := state.checker, state.handler
	state.mu.RUnlock()
	if class == SubjectClassSub {
		handler = nil
	}
	return &toolspermission.Gate{
		Checker:           checker,
		Approval:          handler,
		Enforcer:          state,
		Timeout:           approvalTimeout, // limits.approval_timeout；<=0 时框架取默认
		DenyWithoutPrompt: true,
	}
}

// Enforce 实现框架 permission.BitEnforcer：在**位/组/规则之前**做两件事。
//
//  1. 会话级全权短路放行（全权的语义就是"本次会话不做位与规则判定"）。子代理
//     不享全权：它自己的主体就有 ctl/adm 断位，会话的全权不该把一个无权主体
//     变成有权主体。
//  2. 主体类策略：子代理（位齐放行、位缺/未分封直接拒绝、无人类可问）与员工
//     （位齐交回框架、位缺走执行选择页面提权）见 permission_policy.go。
//
// 其余（root）返回 ok=false，完全落回框架判定。
func (state *PermissionGate) Enforce(ctx context.Context, subject toolspermission.Subject, meta frameworktools.ToolMeta, name, argsJSON string) (toolspermission.Action, bool) {
	if class := state.classFor(ctx); class != SubjectClassSub && state.FullAccessFor(state.sessionFromContext(ctx)) {
		return toolspermission.ActionAllow, true
	}
	return state.enforceClass(ctx, subject, name, meta, argsJSON)
}

func (state *PermissionGate) sessionFromContext(ctx context.Context) string {
	if state == nil {
		return ""
	}
	state.mu.RLock()
	resolver := state.SessionFromContext
	state.mu.RUnlock()
	if resolver == nil {
		return ""
	}
	return resolver(ctx)
}
