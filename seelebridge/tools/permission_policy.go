package tools

import (
	"context"
	"strings"
	"time"

	frameworktools "github.com/RedHuang-0622/Seele/tools"
	toolspermission "github.com/RedHuang-0622/Seele/tools/permission"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
)

// permission_policy.go — 权责模型（主体 × 路由组 × 位 × 动作）的单一落点。
//
// 长期口径见 docs/2026-09-15-agent-permission-routing-groups/README.md §2；本文件是
// 它在 seelex 侧的可执行形态：
//
//	主体（谁在调用）   root（主代理 / entry 节点 / goalplan 节点，uid 0）
//	                   sub（子代理：plan kind:agent 节点 / fork_subagents 派生）
//	                   emp_ro / emp_rw（员工：由 dto.RoleSpec.ToolsPolicy 决定）
//	路由组（哪一族）   ro / rw / rw_session / rw_desktop / ctl / adm（按名字 glob 路由）
//	位（rwx）          组给 mode，主体给 grant；位齐 → 组默认动作，位缺 → 不可路由
//	动作              组默认 allow/ask；rules 做最细粒度覆盖（LMRW 最后匹配胜出）
//
// 三条产品决定（不是可选配置）：
//
//  1. **子代理没有人类在环**：任何"需要问人"的调用（组默认 ask / 规则 ask / 位缺）
//     一律**直接拒绝**，绝不落到执行选择页面上去挂起等待。子代理的授权范围由位
//     表达（位齐 = 授权范围内），作用域另有 worktree / ProjectScope 收窄。
//  2. **员工有宿主人类**：位齐 → 该放行放行、该问人问人；位缺（违权）→ 走执行
//     选择页面**提权**（人类的选择页 = sudo 口令）。显式 deny 规则仍然硬，不因
//     提权页面而变成可放开。
//  3. **主代理完全落回框架**：组默认 + rules 的判定与接线前逐条一致，权责模型
//     对 root 只增加"位必须齐"这一道（root 全位，等价于不增加）。
//
// 主体与框架 `permission.Engine` 是同一个 id：middleware 把主体写进 ctx
// （WithEngine），checker 用它查授权表，本文件的 Enforcer 用同一身份做类策略。

// SubjectClass 是"谁在调用"的分类口径（Linux user 维度）。
type SubjectClass string

const (
	// SubjectClassRoot 是主代理 / Plan entry 节点 / goalplan 节点：持全部位（uid 0 / sudo）。
	SubjectClassRoot SubjectClass = SubjectClass(toolspermission.SubjectRoot)
	// SubjectClassSub 是子代理：ro + rw(project)，ctl=0、adm=0、rw 不含 desktop/session。
	SubjectClassSub SubjectClass = "sub"
	// SubjectClassEmployeeRO 是 ToolsPolicy=readonly 的员工：只有读位。
	SubjectClassEmployeeRO SubjectClass = "emp_ro"
	// SubjectClassEmployeeRW 是 ToolsPolicy=readwrite 的员工：读位 + 项目写位。
	SubjectClassEmployeeRW SubjectClass = "emp_rw"
)

// 路由组名（位与默认动作见 DefaultPermissionGroupList）。
const (
	GroupRO        = "ro"         // 读簇：不改任何共享状态
	GroupRW        = "rw"         // 写簇：项目文件 + 自有工作台
	GroupRWSession = "rw_session" // 写簇：本会话可变 transcript（子代理无位）
	GroupRWDesktop = "rw_desktop" // 写簇：共享外设（一块桌面，子代理无位）
	GroupCTL       = "ctl"        // 叫停 loop 簇：结束 / 挂起 / 派生 / 装载执行结构
	GroupADM       = "adm"        // 属主簇：改变能力面本身（仅 root）
)

// 位的 resource 限定口径（rw 必须落在哪片资源上）。
const (
	ResourceProject = "project" // 会话绑定的项目根（已被 ProjectScope 收窄）
	ResourceSession = "session" // 本会话的可变工作台
	ResourceDesktop = "desktop" // 共享外设：所有并行子代理共用一块桌面
)

// 位值（r=4 / w=2 / x=1）。
const (
	bitRead    uint8 = 4
	bitWrite   uint8 = 2
	bitExecute uint8 = 1
	bitAll     uint8 = bitRead | bitWrite | bitExecute
)

// DefaultPermissionGroupList 是默认路由组表：按工具名 glob 路由，组带对象 mode 与默认动作。
//
// 路由是 LMRW（最后匹配胜出，与框架 routeLocked 同语义），因此组之间不重叠；
// 语义判据见设计稿 §2.2：ro 不碰共享状态、rw 写项目/工作台、ctl 改循环控制流、
// adm 改能力面本身。
func DefaultPermissionGroupList() []toolspermission.PermissionGroup {
	return []toolspermission.PermissionGroup{
		{
			Name:    GroupRO,
			Mode:    bitRead,
			Default: toolspermission.ActionAllow,
			Match: []string{
				"read_file", "read_plan", "read_tool_result", "read_compressed_turn",
				"search_history", "grep_search", "glob",
				"get_time", "web_search",
				"todo_status", "todolist_status", "plan_status", "goal_status",
				"plugins_list", "skills_list", "mcp_list",
				"plan_validate", "plan_export",
				"computer_screenshot", "computer_windows", "computer_wait",
			},
		},
		{
			Name:     GroupRW,
			Mode:     bitRead | bitWrite,
			Resource: ResourceProject,
			Default:  toolspermission.ActionAllow,
			Match: []string{
				"write_file", "edit_file", "bash",
				"todo_init", "todo_add", "todo_done",
				"todolist_init", "todolist_add", "todolist_done",
				"task_add", "taskadd",
				"project_refresh", "plugin_create", "skill_create",
			},
		},
		{
			Name:     GroupRWSession,
			Mode:     bitRead | bitWrite,
			Resource: ResourceSession,
			Default:  toolspermission.ActionAllow,
			Match:    []string{"compact_context"},
		},
		{
			Name:     GroupRWDesktop,
			Mode:     bitRead | bitWrite,
			Resource: ResourceDesktop,
			Default:  toolspermission.ActionAsk,
			Match: []string{
				"computer_click", "computer_move", "computer_drag", "computer_scroll",
				"computer_type", "computer_keys", "computer_focus",
			},
		},
		{
			Name:    GroupCTL,
			Mode:    bitExecute,
			Default: toolspermission.ActionAllow,
			Match: []string{
				"task_complete", "task_failed", "task_needs_user_decision", "task_check_node",
				"ask_approve", "fork_subagents",
				"plan_load", "plan_clear", "plan_run",
				// goal 栈是主代理的治理状态：policy.go 对子代理整族不可见，
				// 因此跟着 ctl 一起断位（比设计稿 §3.2 的 rw 更贴合现有口径）。
				"goal_begin", "goal_update", "goal_propose_finish",
			},
		},
		{
			Name:    GroupADM,
			Mode:    bitAll,
			Default: toolspermission.ActionAsk,
			Match: []string{
				"switch_plugin", "switch_mode", "skill_activate",
				"plugins_reload", "mcp_create", "mcp_load",
			},
		},
	}
}

// DefaultPermissionSubjects 是默认授权表：主体 → 各组位与 resource 限定。
//
// root 全位（含 desktop / session）；sub 拿满项目写位但 ctl/adm 断位、无共享外设位；
// 员工按 ToolsPolicy 两档（只读 / 读+项目写），ctl/adm 与共享外设同样断位。
func DefaultPermissionSubjects() map[toolspermission.Subject]toolspermission.SubjectGrant {
	root := toolspermission.SubjectGrant{Bits: map[string]toolspermission.GrantBit{
		GroupRO:        {Bits: bitRead},
		GroupRW:        {Bits: bitRead | bitWrite, Resources: []string{ResourceProject}},
		GroupRWSession: {Bits: bitRead | bitWrite, Resources: []string{ResourceSession}},
		GroupRWDesktop: {Bits: bitRead | bitWrite, Resources: []string{ResourceDesktop}},
		GroupCTL:       {Bits: bitExecute},
		GroupADM:       {Bits: bitAll},
	}}
	sub := toolspermission.SubjectGrant{Bits: map[string]toolspermission.GrantBit{
		GroupRO:  {Bits: bitRead},
		GroupRW:  {Bits: bitRead | bitWrite, Resources: []string{ResourceProject}},
		GroupCTL: {Bits: 0},
		GroupADM: {Bits: 0},
	}}
	employeeRO := toolspermission.SubjectGrant{Bits: map[string]toolspermission.GrantBit{
		GroupRO:  {Bits: bitRead},
		GroupRW:  {Bits: 0},
		GroupCTL: {Bits: 0},
		GroupADM: {Bits: 0},
	}}
	employeeRW := toolspermission.SubjectGrant{Bits: map[string]toolspermission.GrantBit{
		GroupRO:  {Bits: bitRead},
		GroupRW:  {Bits: bitRead | bitWrite, Resources: []string{ResourceProject}},
		GroupCTL: {Bits: 0},
		GroupADM: {Bits: 0},
	}}
	return map[toolspermission.Subject]toolspermission.SubjectGrant{
		SubjectForClass(SubjectClassRoot):       root,
		SubjectForClass(SubjectClassSub):        sub,
		SubjectForClass(SubjectClassEmployeeRO): employeeRO,
		SubjectForClass(SubjectClassEmployeeRW): employeeRW,
	}
}

// DefaultPermissionRules 是默认的最细粒度覆盖层（args 级）：把"必须问人"和
// "必须拒绝"的工具钉死，其余交给路由组的默认动作。
//
// 顺序即语义：框架按 LMRW（最后匹配胜出）求值，所以 danger deny 放最后。
// 命令行工具（bash）的 patterns 匹配的是**命令本身**（见 policyArgsFor）。
func DefaultPermissionRules() []toolspermission.PermissionRule {
	return []toolspermission.PermissionRule{
		// 写盘 / 扩能力的工具：生效前需要人类过目。
		{ToolName: "write_file", Action: toolspermission.ActionAsk},
		{ToolName: "edit_file", Action: toolspermission.ActionAsk},
		{ToolName: "plugin_create", Action: toolspermission.ActionAsk},
		{ToolName: "skill_create", Action: toolspermission.ActionAsk},

		// bash 能力白名单：安全命令直接执行，越界命令问人，危险命令拒绝。
		{ToolName: "bash", Action: toolspermission.ActionAsk},
		{
			ToolName: "bash",
			Patterns: []string{"git *", "ls *", "cat *", "echo *", "head *", "tail *",
				"pwd", "which *", "whoami", "date", "df *", "du *", "printenv",
				"go test *", "go build *", "go vet *", "gofmt *"},
			Action: toolspermission.ActionAllow,
		},
		{
			ToolName: "bash",
			Patterns: []string{"npm install *", "npm ci*", "make *", "docker *", "rm *", "mv *", "chmod *", "chown *"},
			Action:   toolspermission.ActionAsk,
		},
		{
			ToolName: "bash",
			Patterns: []string{"rm -rf /*", "rm -fr /*", ":(){ :|:& };:", "dd if=* of=*", "mkfs*", "shutdown*", "reboot*"},
			Action:   toolspermission.ActionDeny,
		},
	}
}

// DefaultPermissionConfig 是 manual 模式的默认权责配置：分组 + 主体 + 缺位口径
// （缺位 = ask；子代理侧由 Enforcer 落成"直接拒绝"，员工侧落成"执行选择页面"）
// + 最细粒度规则。调用方（main.go）在其上叠加 seele.yaml 的 permission 段覆盖。
func DefaultPermissionConfig() toolspermission.PermissionConfig {
	return toolspermission.PermissionConfig{
		Mode:       toolspermission.ModeManual,
		Groups:     DefaultPermissionGroupList(),
		Subjects:   DefaultPermissionSubjects(),
		MissingBit: toolspermission.ActionAsk,
		Rules:      DefaultPermissionRules(),
	}
}

// SubjectForClass 把主体类映射成框架授权主体 id（= 写入 ctx 的 engine id）。
func SubjectForClass(class SubjectClass) toolspermission.Subject {
	switch class {
	case SubjectClassSub:
		return toolspermission.Subject(SubjectClassSub)
	case SubjectClassEmployeeRO:
		return toolspermission.Subject(SubjectClassEmployeeRO)
	case SubjectClassEmployeeRW:
		return toolspermission.Subject(SubjectClassEmployeeRW)
	default:
		return toolspermission.Subject(toolspermission.SubjectRoot)
	}
}

// ClassForToolsPolicy 把员工的 ToolsPolicy 口径映射成主体类：
// readonly → emp_ro；readwrite → emp_rw；full / inherit(空) / 未识别 → root（继承宿主默认）。
func ClassForToolsPolicy(policy string) SubjectClass {
	switch strings.ToLower(strings.TrimSpace(policy)) {
	case "readonly":
		return SubjectClassEmployeeRO
	case "readwrite":
		return SubjectClassEmployeeRW
	default:
		return SubjectClassRoot
	}
}

// ClassForNodeScope 把工具调度 ctx 的节点作用域映射成主体类：带 NodeID 的
// RoleSubAgent 节点 = 子代理；其余（无作用域 / 主代理 / entry / goalplan）= root。
// 口径与 tools/policy.go 的可见性判定同源（它同样只按 RoleSubAgent 收窄）。
func ClassForNodeScope(scope model.NodeScope) SubjectClass {
	if scope.NodeID != "" && scope.Role == model.RoleSubAgent {
		return SubjectClassSub
	}
	return SubjectClassRoot
}

// RoutePermissionGroup 按名字 glob 路由到路由组（LMRW：最后匹配胜出）。
// 与框架 checker.routeLocked 同语义，供 Enforcer 在判定前回答"这个工具分封过吗"。
func RoutePermissionGroup(groups []toolspermission.PermissionGroup, toolName string) (toolspermission.PermissionGroup, bool) {
	var matched toolspermission.PermissionGroup
	found := false
	for _, group := range groups {
		for _, pattern := range group.Match {
			if matchToolGlob(pattern, toolName) {
				matched, found = group, true
				break
			}
		}
	}
	return matched, found
}

// matchToolGlob 与框架 permission.matchGlob 同语义：只有 `*` 通配，整串匹配。
func matchToolGlob(pattern, name string) bool {
	if pattern == "" || name == "" {
		return pattern == name
	}
	if pattern == "*" {
		return true
	}
	if !strings.Contains(pattern, "*") {
		return pattern == name
	}
	pi, ni := 0, 0
	star, mark := -1, 0
	for ni < len(name) {
		switch {
		case pi < len(pattern) && pattern[pi] == name[ni]:
			pi++
			ni++
		case pi < len(pattern) && pattern[pi] == '*':
			star, mark = pi, ni
			pi++
		case star != -1:
			pi = star + 1
			mark++
			ni = mark
		default:
			return false
		}
	}
	for pi < len(pattern) && pattern[pi] == '*' {
		pi++
	}
	return pi == len(pattern)
}

// rolePolicyCacheTTL 是"角色会话 → 员工权限口径"解析结果的缓存时长。解析器由
// main.go 注入（读角色注册表），这里只负责把每次工具调用的成本压到一次 map 命中；
// 员工改权限最迟一个 TTL 生效。
const rolePolicyCacheTTL = 5 * time.Second

type rolePolicyCacheEntry struct {
	policy string
	found  bool
	at     time.Time
}

// subjectClassCtxKey 把本次调用的主体类随 ctx 传给 Enforcer：Middleware 入口解析
// 一次（节点作用域 + 角色会话登记），Enforce 从同一 ctx 取回，绝不放共享可变字
// 段上——并行会话/子代理同时调工具时共享字段必被互相污染。
type subjectClassCtxKey struct{}

func withSubjectClass(ctx context.Context, class SubjectClass) context.Context {
	return context.WithValue(ctx, subjectClassCtxKey{}, class)
}

func subjectClassFromContext(ctx context.Context) (SubjectClass, bool) {
	if ctx == nil {
		return "", false
	}
	class, ok := ctx.Value(subjectClassCtxKey{}).(SubjectClass)
	return class, ok
}

// classFor 解析本次调用的主体类：节点作用域（子代理）优先，其次是角色会话登记
// （员工 ToolsPolicy），都不是 = root（主代理 / entry / goalplan / 未登记会话）。
func (state *PermissionGate) classFor(ctx context.Context) SubjectClass {
	if class, ok := subjectClassFromContext(ctx); ok {
		return class
	}
	scope := model.NodeScopeFromContextOrEmpty(ctx)
	if class := ClassForNodeScope(scope); class == SubjectClassSub {
		return class
	}
	return state.roleSessionClass(state.sessionFromContext(ctx))
}

// roleSessionClass 查"这个会话是不是员工角色会话"，是则按 ToolsPolicy 给类。
// 未注入解析器 / 非角色会话 / 查不到 = root。
func (state *PermissionGate) roleSessionClass(sessionID string) SubjectClass {
	if state == nil || strings.TrimSpace(sessionID) == "" {
		return SubjectClassRoot
	}
	state.mu.RLock()
	resolver := state.RoleSessionPolicy
	state.mu.RUnlock()
	if resolver == nil {
		return SubjectClassRoot
	}
	now := time.Now()
	state.mu.Lock()
	if cached, ok := state.rolePolicyCache[sessionID]; ok && now.Sub(cached.at) < rolePolicyCacheTTL {
		state.mu.Unlock()
		if !cached.found {
			return SubjectClassRoot
		}
		return ClassForToolsPolicy(cached.policy)
	}
	state.mu.Unlock()

	policy, found := resolver(sessionID)

	state.mu.Lock()
	if state.rolePolicyCache == nil {
		state.rolePolicyCache = make(map[string]rolePolicyCacheEntry)
	}
	state.rolePolicyCache[sessionID] = rolePolicyCacheEntry{policy: policy, found: found, at: now}
	state.mu.Unlock()

	if !found {
		return SubjectClassRoot
	}
	return ClassForToolsPolicy(policy)
}

// configSnapshot 取当前权限配置快照（Enforcer 用它做路由/分封判断）。
func (state *PermissionGate) configSnapshot() toolspermission.PermissionConfig {
	state.mu.RLock()
	defer state.mu.RUnlock()
	return state.cfg
}

// policyReady 报告分组与主体表是否已装配。未装配（旧的仅 rules 配置、或只测
// 规则的用例）时不启用主体类策略，完全落回框架的位/规则判定。
func (state *PermissionGate) policyReady(cfg toolspermission.PermissionConfig) bool {
	return len(cfg.Groups) > 0 && len(cfg.Subjects) > 0
}

// enforceClass 是主体类策略（框架 BitEnforcer 接口的实现面）：返回 ok=true 表示
// 本次判定由主体类决定，不再走框架的组/规则；ok=false 表示交回框架。
func (state *PermissionGate) enforceClass(ctx context.Context, subject toolspermission.Subject, name string, meta frameworktools.ToolMeta, argsJSON string) (toolspermission.Action, bool) {
	cfg := state.configSnapshot()
	if !state.policyReady(cfg) {
		return "", false
	}
	switch state.classFor(ctx) {
	case SubjectClassSub:
		return state.enforceSubAgent(cfg, subject, name, meta, argsJSON)
	case SubjectClassEmployeeRO, SubjectClassEmployeeRW:
		return state.enforceEmployee(cfg, subject, name, meta, argsJSON)
	default:
		return "", false
	}
}

// enforceSubAgent 是子代理的授权口径：
//
//  1. 显式 deny 规则（如 bash 的危险命令模式）永远硬 → EPERM；
//  2. 位缺（违权）→ 交回框架：checker 判"不可见"，中间件以 ErrToolNotVisible 直接
//     拒绝（= 不可路由，不弹选择页面）；
//  3. 未分封 → 交回框架：默认 ask，而子代理没有人类在环（Approval=nil）→ 拒绝；
//  4. 位齐 → 放行。子代理的授权范围由位表达（作用域另有 worktree / ProjectScope
//     收窄），"规则要求问人"的调用没有人可以问，位齐即授权。
func (state *PermissionGate) enforceSubAgent(cfg toolspermission.PermissionConfig, subject toolspermission.Subject, name string, meta frameworktools.ToolMeta, argsJSON string) (toolspermission.Action, bool) {
	if result, _ := state.decideFor(subject, name, meta, argsJSON); result == toolspermission.ResultDeny {
		return toolspermission.ActionDeny, true
	}
	if _, routed := RoutePermissionGroup(cfg.Groups, name); !routed {
		return "", false
	}
	if visible := state.visibleFor(subject, name, meta); !visible {
		return "", false
	}
	return toolspermission.ActionAllow, true
}

// enforceEmployee 是员工的授权口径：显式 deny 硬；位齐 → 交回框架（组默认 +
// rules：allow 放行、ask 走执行选择页面）；位缺 / 未分封 → 走执行选择页面提权。
func (state *PermissionGate) enforceEmployee(cfg toolspermission.PermissionConfig, subject toolspermission.Subject, name string, meta frameworktools.ToolMeta, argsJSON string) (toolspermission.Action, bool) {
	if result, _ := state.decideFor(subject, name, meta, argsJSON); result == toolspermission.ResultDeny {
		return toolspermission.ActionDeny, true
	}
	if _, routed := RoutePermissionGroup(cfg.Groups, name); !routed {
		return toolspermission.ActionAsk, true
	}
	if visible := state.visibleFor(subject, name, meta); !visible {
		return toolspermission.ActionAsk, true
	}
	return "", false
}

// decideFor / visibleFor 是框架 checker 的只读查询（checker 自身线程安全）。
// checker 未装配时按"放行且可见"处理，与框架 evaluate 的 nil-checker 分支一致。
func (state *PermissionGate) decideFor(subject toolspermission.Subject, name string, meta frameworktools.ToolMeta, argsJSON string) (toolspermission.CheckResult, bool) {
	state.mu.RLock()
	checker := state.checker
	state.mu.RUnlock()
	if checker == nil {
		return toolspermission.ResultAllow, true
	}
	return checker.DecideForMeta(subject, name, meta, argsJSON)
}

func (state *PermissionGate) visibleFor(subject toolspermission.Subject, name string, meta frameworktools.ToolMeta) bool {
	state.mu.RLock()
	checker := state.checker
	state.mu.RUnlock()
	if checker == nil {
		return true
	}
	return checker.VisibleForMeta(subject, name, meta)
}
