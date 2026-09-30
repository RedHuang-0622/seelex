package tools

import (
	"context"
	"fmt"
	"hash/fnv"
	"strings"
	"time"

	frameworktools "github.com/RedHuang-0622/Seele/tools"
	toolspermission "github.com/RedHuang-0622/Seele/tools/permission"
	"github.com/RedHuang-0622/seelex/application/contract/dto"
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
//
// 字面量的**唯一事实在 application/contract/dto**（dto.PermissionGroup*）：组名
// 同时是"前端权限装配面板"的格子名与"写入侧校验"的枚举，两边必须是同一份；
// 这里只做别名，方便本包内部引用。
const (
	GroupRO        = dto.PermissionGroupRO        // 读簇：不改任何共享状态
	GroupRW        = dto.PermissionGroupRW        // 写簇：项目文件 + 自有工作台
	GroupRWSession = dto.PermissionGroupRWSession // 写簇：本会话可变 transcript（子代理无位）
	GroupRWDesktop = dto.PermissionGroupRWDesktop // 写簇：共享外设（一块桌面，子代理无位）
	GroupCTL       = dto.PermissionGroupCTL       // 叫停 loop 簇：结束 / 挂起 / 派生 / 装载执行结构
	GroupADM       = dto.PermissionGroupADM       // 属主簇：改变能力面本身（仅 root）
)

// 位的 resource 限定口径（rw 必须落在哪片资源上）。
const (
	ResourceProject = "project" // 会话绑定的项目根（已被 ProjectScope 收窄）
	ResourceSession = "session" // 本会话的可变工作台
	ResourceDesktop = "desktop" // 共享外设：所有并行子代理共用一块桌面
)

// 位值（r=4 / w=2 / x=1）。字面量同样取自 dto（跨层词表），这里只是别名。
const (
	bitRead    uint8 = dto.PermissionBitRead
	bitWrite   uint8 = dto.PermissionBitWrite
	bitExecute uint8 = dto.PermissionBitExecute
	bitAll     uint8 = dto.PermissionBitAll
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
				// bash_read 是 bash 工具族里的**只读面**（设计文档 §A.4）：分类判定
				// 在服务端（security.ClassifyCommand），落在只读簇 ⇒ 默认 allow ⇒
				// 子代理/只读员工天然拿得到，且不弹审批。这正是工具级分裂的目的。
				"bash_read",
				// read_batch 派发的是一批**只读**扇出作业（Kind=inline），不碰共享状态：
				// 派发面免打断，取回/终止/销项走 job_manage（rw 簇）。
				"read_batch",
				"get_time", "web_search",
				"todo_status", "todolist_status", "plan_status", "goal_status",
				"plugins_list", "skills_list", "mcp_list",
				"plan_validate", "plan_export",
				"computer_screenshot", "computer_windows", "computer_scroll_targets", "computer_wait",
			},
		},
		{
			Name:     GroupRW,
			Mode:     bitRead | bitWrite,
			Resource: ResourceProject,
			Default:  toolspermission.ActionAllow,
			Match: []string{
				"write_file", "edit_file", "bash",
				// bash_bg / job_manage 与 bash 同组：它们管的正是 bash 起的执行体。
				// 放 CTL 会让 sub/员工断位，它们自己派发的后台作业就谁也取不回、杀不掉；
				// 真正的门是 handler 里的"句柄必须属于本会话"。
				"bash_bg", "job_manage",
				// jobs_manage 是 Seele jobs 的**通用管理工具**（observe/fetch/kill/done），
				// 与 bash_bg/job_manage 同组：它管的正是作业面的执行体（含 teammate 作业）。
				"jobs_manage",
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
				// teamwork leader 编排面：改的是"团队顺序与作业"这条循环控制流，
				// 与 fork_subagents/plan_* 同族——sub/员工断位（teammate 不该编排
				// 团队），主代理（root）默认 allow。
				"team_plan", "team_dispatch", "team_join", "team_milestone", "team_retire",
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
		//
		// 口径同时覆盖 bash_bg（后台受管命令）——它跑的是同一类命令，只是活过这一轮。
		// **不覆盖 bash_read**：那个名字的入参必须过服务端只读分类（K-4），
		// 命令模式规则在这里是第二道、也是更弱的一道（模型换个写法就能绕过模式匹配）。
		{ToolName: "bash", Action: toolspermission.ActionAsk},
		{ToolName: "bash_bg", Action: toolspermission.ActionAsk},
		{
			ToolName: "bash",
			Patterns: []string{"git *", "ls *", "cat *", "echo *", "head *", "tail *",
				"pwd", "which *", "whoami", "date", "df *", "du *", "printenv",
				"go test *", "go build *", "go vet *", "gofmt *"},
			Action: toolspermission.ActionAllow,
		},
		{
			ToolName: "bash_bg",
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
			ToolName: "bash_bg",
			Patterns: []string{"npm install *", "npm ci*", "make *", "docker *", "rm *", "mv *", "chmod *", "chown *"},
			Action:   toolspermission.ActionAsk,
		},
		{
			ToolName: "bash",
			Patterns: []string{"rm -rf /*", "rm -fr /*", ":(){ :|:& };:", "dd if=* of=*", "mkfs*", "shutdown*", "reboot*"},
			Action:   toolspermission.ActionDeny,
		},
		{
			ToolName: "bash_bg",
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

// sanitizeMeta 把"本次配置里不存在的簇"从工具自带的簇属里摘掉（打点 K-0 的
// 配套守卫，不是可选项）。
//
// 为什么必须摘：checker 的 DecideForMeta 在 meta.Groups 非空时走声明路径——
// 它按**组名**在当前配置的组表里查那个组；查不到时 required 停留在 meta.Bits，
// 而 seelex 的声明刻意把 Bits 留 0（"所需位"只有一个事实源：组 Mode）⇒ required
// 为 0 ⇒ 跳过位检查，**判定结果直接落成 allow**。于是"用户用 seelex.yaml 的
// permission.groups 换掉默认组表"这类配置会得到一次静默放权（bash 在组表里
// 找不到 rw 就不再要位、也不再问人）。
//
// 摘掉之后（Groups/Bits 归零、Kind 保留）判定落回**按名字路由**，也就是 K-0
// 之前的老路：组表里没有 → 未分封 → 默认 ask。语义边界因此是安全的：声明只在
// 它真的存在于本次配置时生效，声明与配置不一致时退回更保守的那条路。
//
// 保留 Kind 的理由：control 类的"仅 root 可路由"是**框架级**约束（
// permission/middleware.go），与组表是否存在无关；它收窄而不放权，摘掉反而更松。
func (state *PermissionGate) sanitizeMeta(meta frameworktools.ToolMeta) frameworktools.ToolMeta {
	if len(meta.Groups) == 0 {
		return meta
	}
	state.mu.RLock()
	known := state.groupNames
	kept := make([]string, 0, len(meta.Groups))
	for _, group := range meta.Groups {
		if known[group] {
			kept = append(kept, group)
		}
	}
	state.mu.RUnlock()
	if len(kept) == 0 {
		return frameworktools.ToolMeta{Kind: meta.Kind}
	}
	meta.Groups = kept
	return meta
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

// ToolFaceFor 回答"主体类的工具面上有没有这个工具"（可见性口径的静态版，供
// 工具列表过滤用；判定口径与框架 checker 的断位一致：按名字路由到组，主体持有
// 覆盖组 mode 的位即视为在面上）。
//
// 与 Enforcer 的关系：Enforcer 在**调用时**判定（位齐放行 / 位缺提权），本方法在
// **列工具时**判定。两者读同一份 PermissionConfig，因此"面上没有的工具有人调用"
// 只可能来自配置变更（TTL 内）或未分封的动态工具，仍由 Enforcer 兜住。
//
// 三条口径：
//   - 未装配权责模型（只有旧 rules 的配置 / 只测规则的用例）→ 一律在面上，
//     与 Enforcer 的 policyReady 同口径，不给旧配置新增限制；
//   - 未分封（不匹配任何路由组，如动态 MCP 工具）→ 员工面上不出现：员工侧位缺
//     落成"执行选择页面提权"，把未分封的工具摆进面上等于每次调用都顶一张页面
//     给宿主人类。要给员工开放，先在权限配置里分封（permission.groups）；
//   - 分封但位缺（如 emp_ro 的 rw/ctl/adm）→ 不在面上：工具面就是"授权范围内的
//     能力清单"，位缺的能力不摆出来（提权仍可通过选择页面发生）。
func (state *PermissionGate) ToolFaceFor(class SubjectClass, toolName string) bool {
	return state.ToolFaceForSubject(SubjectForClass(class), toolName)
}

// ToolFaceForSubject 是工具面的**主体版**判定：把主体（root / sub / emp_ro /
// emp_rw / emp_<角色>）在授权表里的位，与该工具所属路由组要求的位对齐。
//
// 与类版（ToolFaceFor）的关系：类版只认共享的三档主体，适合作"批量按类"的旧调用；
// 员工执行面要的是主体版——同一档位的两个员工可以有不同能力面（装配期各自的分配）。
func (state *PermissionGate) ToolFaceForSubject(subject toolspermission.Subject, toolName string) bool {
	if state == nil {
		return true
	}
	cfg := state.configSnapshot()
	if !state.policyReady(cfg) {
		return true
	}
	group, routed := RoutePermissionGroup(cfg.Groups, toolName)
	if !routed {
		return false
	}
	grant, ok := cfg.Subjects[subject]
	if !ok {
		return true
	}
	bits := grant.Bits[group.Name].Bits
	return bits&group.Mode == group.Mode
}

// ToolFaceForContext 是工具面过滤的入口：按 ctx 解析主体类（节点作用域 → 角色
// 会话登记 → root，与 Enforcer 的 classFor 同一份解析），只对员工
// （emp_ro / emp_rw）收窄工具面；root（主代理 / entry / goalplan）、sub（子代理）
// 与未登记会话一律返回 true——它们的可见性口径仍由 tools/policy.go 的既有规则
// 决定，本方法不新增限制。
//
// 员工侧的主体解析走与判定同一条路（resolveEmployeeSubject）：员工执行面在 ctx 里
// 带了角色名时，工具面按**这个员工自己的主体**算，而不是按共享的三档类——否则
// "给某个员工单独分配的能力面"在列工具时会被抹平（分配生效了，模型却看不到）。
func (state *PermissionGate) ToolFaceForContext(ctx context.Context, toolName string) bool {
	if state == nil {
		return true
	}
	class := state.classFor(ctx)
	switch class {
	case SubjectClassEmployeeRO, SubjectClassEmployeeRW:
		return state.ToolFaceForSubject(state.resolveEmployeeSubject(ctx, class), toolName)
	default:
		return true
	}
}

// EmployeeSubjectPrefix 是"员工主体"的命名前缀：**一个员工 = 一个主体**，
// 与 root / sub 平权地放在同一张授权表（主体 × 路由组 × 位）里。
//
// 这就是"员工的权限像用户权限一样分配"的落点：用户（root）的权限是一条主体条目，
// 员工的权限也是——区别只有名字（emp_<角色名>）与位（按需分配），而不是"用户有
// 一张表、员工只有三档枚举"。
const EmployeeSubjectPrefix = "emp_"

// 员工分组位的可读别名（与框架位值 r=4 / w=2 / x=1 一致）。
const (
	GroupBitRead    uint8 = bitRead
	GroupBitWrite   uint8 = bitWrite
	GroupBitExecute uint8 = bitExecute
	GroupBitAll     uint8 = bitAll
)

// EmployeeSubjectName 把角色名规范化成员工主体名：小写，非 [a-z0-9_-] 归一为 '_'。
//
// 归一化是必须的——主体名会写进配置文件（permission.subjects）当键，随便什么角色名
// 都塞进去会让配置面变成不可读的垃圾；空名不是员工。
//
// 但"归一"不能**丢身份**：纯非 ASCII 的角色名（中文角色名很常见）会被整串替换成
// '_'，那样 "评审官 1" 与 "工人 1" 会归一成同一个主体——两个员工共享一条授权，
// 权限互相泄漏。所以一旦有字符被替换（或有信息被丢掉），主体名追加角色原文的
// 短哈希：ASCII 角色名保持可读（emp_pm），非 ASCII 角色名保持可辨认且不撞车
// （emp_1_a1b2c3d4）。
func EmployeeSubjectName(roleName string) string {
	raw := strings.TrimSpace(roleName)
	if raw == "" {
		return ""
	}
	var builder strings.Builder
	dropped := false
	for _, symbol := range strings.ToLower(raw) {
		switch {
		case symbol >= 'a' && symbol <= 'z', symbol >= '0' && symbol <= '9', symbol == '_', symbol == '-':
			builder.WriteRune(symbol)
		default:
			builder.WriteRune('_')
			dropped = true
		}
	}
	slug := strings.Trim(builder.String(), "_")
	if slug == "" {
		return EmployeeSubjectPrefix + employeeSubjectHash(raw)
	}
	if !dropped {
		return EmployeeSubjectPrefix + slug
	}
	return EmployeeSubjectPrefix + slug + "_" + employeeSubjectHash(raw)
}

// employeeSubjectHash 是角色原文的稳定短哈希（8 位十六进制）：只在归一化丢信息时
// 用来区分"归一后同名"的不同角色。
func employeeSubjectHash(roleName string) string {
	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(roleName))
	return fmt.Sprintf("%08x", hasher.Sum32())
}

// EmployeeSubject 返回员工的主体 id（不是员工 → ok=false）。
func EmployeeSubject(roleName string) (toolspermission.Subject, bool) {
	name := EmployeeSubjectName(roleName)
	if name == "" {
		return "", false
	}
	return toolspermission.Subject(name), true
}

// EmployeePermission 是**装配期给一个员工分配的权限**：与用户权限同一张表
// （主体 × 路由组 × 位），区别只是主体名（emp_<角色名>）。
//
//   - Groups 非空 → 逐组给位（未列出的组 = 0 位，即该族能力不在面上）；
//   - Groups 为空 → 按 Policy 档位派生默认位（readonly → 读位；readwrite → 读位 +
//     项目写位），与 emp_ro / emp_rw 同口径。
//
// 空 / full / 未识别的 Policy 不给主体条目（"继承宿主默认"必须真的是继承宿主，
// 拿一条自造条目冒充继承是两回事）。
type EmployeePermission struct {
	RoleName string
	Policy   string
	Groups   map[string]uint8
}

// EmployeeGroupsForPolicy 把 ToolsPolicy 档位派生成逐组位（与 emp_ro / emp_rw
// 同口径）。未识别的口径返回 nil（= 不分配主体条目）。
func EmployeeGroupsForPolicy(policy string) map[string]uint8 {
	switch strings.ToLower(strings.TrimSpace(policy)) {
	case "readonly":
		return map[string]uint8{GroupRO: bitRead, GroupRW: 0, GroupCTL: 0, GroupADM: 0}
	case "readwrite":
		return map[string]uint8{GroupRO: bitRead, GroupRW: bitRead | bitWrite, GroupCTL: 0, GroupADM: 0}
	default:
		return nil
	}
}

// EmployeeGrantFor 把 ToolsPolicy 档位派生成框架授权条目（未识别口径 → root 位，
// 与 ClassForToolsPolicy 的兜底一致：给不出"员工档"时按宿主默认）。
func EmployeeGrantFor(policy string) toolspermission.SubjectGrant {
	groups := EmployeeGroupsForPolicy(policy)
	if groups == nil {
		subjects := DefaultPermissionSubjects()
		if root, ok := subjects[SubjectForClass(SubjectClassRoot)]; ok {
			return root
		}
		return toolspermission.SubjectGrant{}
	}
	return grantFromEmployeeGroups(groups)
}

// Grant 把一次员工权限分配落成框架授权条目。
func (permission EmployeePermission) Grant() (toolspermission.SubjectGrant, error) {
	if len(permission.Groups) > 0 {
		return grantFromEmployeeGroups(permission.Groups), nil
	}
	groups := EmployeeGroupsForPolicy(permission.Policy)
	if groups == nil {
		return toolspermission.SubjectGrant{}, fmt.Errorf("员工 %q 的权责口径 %q 不是可分配档位（只读/读写/显式分组位）",
			permission.RoleName, permission.Policy)
	}
	return grantFromEmployeeGroups(groups), nil
}

// grantFromEmployeeGroups 把逐组位落成授权条目，并按组补齐 resource 限定
// （与 DefaultPermissionSubjects 同口径：rw 限项目、rw_session 限本会话、
// rw_desktop 限共享外设；ro/ctl/adm 不限）。
func grantFromEmployeeGroups(groups map[string]uint8) toolspermission.SubjectGrant {
	bits := make(map[string]toolspermission.GrantBit, len(groups))
	for group, value := range groups {
		bits[group] = toolspermission.GrantBit{Bits: value, Resources: employeeResourcesFor(group)}
	}
	return toolspermission.SubjectGrant{Bits: bits}
}

func employeeResourcesFor(group string) []string {
	switch group {
	case GroupRW:
		return []string{ResourceProject}
	case GroupRWSession:
		return []string{ResourceSession}
	case GroupRWDesktop:
		return []string{ResourceDesktop}
	default:
		return nil
	}
}

// employeeSubjectCtxKey 携带"员工是谁"（角色名 + 权责档 + 逐格装配的权限）。放在 ctx
// 而不是共享字段上，是因为并行会话/并行角色回合同时调工具：共享字段必被互相污染。
type employeeSubjectCtxKey struct{}

// employeeIdentity 是一次调用里已确定的员工身份。
type employeeIdentity struct {
	RoleName string
	Policy   string
	// Groups 是**逐格装配**的权限（路由组 → 位）；空 = 按 Policy 档位派生。
	Groups map[string]uint8
}

// WithEmployeeSubjectClass 把"已经确定的员工主体类"放进 ctx：角色回合执行体
// （员工/ADVISOR 自己的工具回合）在起手就知道自己是谁（角色会话 + ToolsPolicy），
// 不该让下游再从会话号反查一遍归属。
//
// 它与 roleSessionClass（会话号 → 角色权责）给出同一个结论，但优先级更高：
// classFor 先读 ctx。于是即使角色会话归属索引冷启动没查到（例如角色回合比它的
// 主会话注册表先被读到），角色回合的工具调用照样按员工口径拦——"按构造授权"
// 永远比"按反查授权"可靠。
//
// toolsPolicy 用注册表里的员工口径（readonly / readwrite / full|空=继承宿主默认）；
// 无法识别的口径落回 root。写路径的枚举校验保证不会写入垃圾口径（见注册表校验）。
//
// 与 WithEmployeeSubject 的关系：本函数只表达**档位**（共享主体 emp_ro / emp_rw），
// 适合"只想知道按哪档判"的调用；要按员工自己的主体分配与判定（emp_<角色名>），用
// WithEmployeeSubject / WithEmployeeGrant（它们同时把档位写进主体类，本函数是子集）。
func WithEmployeeSubjectClass(ctx context.Context, toolsPolicy string) context.Context {
	return withSubjectClass(ctx, ClassForToolsPolicy(toolsPolicy))
}

// WithEmployeeSubject 把"员工是谁"放进 ctx：角色回合执行体（员工自己的工具回合）
// 起手就知道自己是谁。
//
// 它与 WithEmployeeSubjectClass 的差别正是"员工权限像用户权限一样分配"所需的那一环：
// 后者只带**权责档**（readonly/readwrite → 共享的 emp_ro / emp_rw 主体），前者多带
// **角色名**，于是判定与工具面都落到这个员工自己的主体 emp_<角色名> 上——装配期给
// 他分配了什么，运行时就是什么；两个同档位的员工也能有不同权限。
// 它同时把权责档写进主体类（classFor 先读 ctx），因此员工口径的兜底（位缺提权）
// 一并生效，不依赖会话号反查。
func WithEmployeeSubject(ctx context.Context, roleName, toolsPolicy string) context.Context {
	return WithEmployeeGrant(ctx, roleName, toolsPolicy, nil)
}

// WithEmployeeGrant 是 WithEmployeeSubject 的**逐格装配版**：除了角色名与档位，还带上
// 显式分配的权限格子（路由组 → 位，见 dto.NormalizePermissionGroups）。
//
// 为什么必须把格子也按构造带进来：主体类（emp_ro / emp_rw）只决定"按员工这条分支判"，
// 真正的能力面是主体条目 emp_<角色名> 的位。而"这个员工有哪些格子"只有装配期知道
// （注册表读面给出的 ToolsPolicy 只有档位）；不按构造带，装配好的格子就会在"角色会话
// 冷启动、反查没查到"的窗口里退化成档位默认——即"装配了但没生效"。
//
// 档位与格子的优先级与装配期一致：**格子非空则以格子为准**（档位只用于选分支）。
func WithEmployeeGrant(ctx context.Context, roleName, toolsPolicy string, groups map[string]uint8) context.Context {
	identity := employeeIdentity{
		RoleName: strings.TrimSpace(roleName),
		Policy:   toolsPolicy,
		Groups:   cloneEmployeeGroups(groups),
	}
	ctx = context.WithValue(ctx, employeeSubjectCtxKey{}, identity)
	return withSubjectClass(ctx, ClassForEmployeeGrant(toolsPolicy, groups))
}

// ClassForEmployeeGrant 把（档位 + 显式格子）映射成员工主体类。
//
// 类只用来选**分支**（员工分支 vs root 分支），不是能力面：能力面由 emp_<角色名>
// 条目的位决定。显式格子非空时，只要有任何非 ro 能力就按读写档分支（"这是有写/执行
// 能力的员工"），否则按只读档分支；档位为空/未识别也不会退化成 root——那会让
// "前端装配过权限的员工"按主代理判，是 fail-open。
func ClassForEmployeeGrant(toolsPolicy string, groups map[string]uint8) SubjectClass {
	if len(groups) == 0 {
		return ClassForToolsPolicy(toolsPolicy)
	}
	for group, bits := range groups {
		if group == GroupRO {
			continue
		}
		if bits != 0 {
			return SubjectClassEmployeeRW
		}
	}
	return SubjectClassEmployeeRO
}

// cloneEmployeeGroups 复制一份权限格子（ctx 里的身份不该与调用方的 map 共享可变状态）。
func cloneEmployeeGroups(groups map[string]uint8) map[string]uint8 {
	if len(groups) == 0 {
		return nil
	}
	cloned := make(map[string]uint8, len(groups))
	for group, bits := range groups {
		cloned[group] = bits
	}
	return cloned
}

// employeeFromContext 取 ctx 里的员工身份（没有 → 不是员工执行面）。
func employeeFromContext(ctx context.Context) (employeeIdentity, bool) {
	if ctx == nil {
		return employeeIdentity{}, false
	}
	identity, ok := ctx.Value(employeeSubjectCtxKey{}).(employeeIdentity)
	if !ok || identity.RoleName == "" {
		return employeeIdentity{}, false
	}
	return identity, true
}

// resolveEmployeeSubject 解析本次调用的**授权主体**：
//   - ctx 里带员工身份（员工执行面）→ 这个员工自己的主体 emp_<角色>，
//     缺条目时按他的（显式格子优先，其次档位）补一条默认（装配期显式分配的条目优先）；
//   - 其余 → 主体类对应的共享主体（root / sub / emp_ro / emp_rw）。
func (state *PermissionGate) resolveEmployeeSubject(ctx context.Context, class SubjectClass) toolspermission.Subject {
	identity, ok := employeeFromContext(ctx)
	if !ok {
		return SubjectForClass(class)
	}
	subject, ok := EmployeeSubject(identity.RoleName)
	if !ok {
		return SubjectForClass(class)
	}
	state.ensureEmployeeSubject(subject, identity)
	return subject
}

// ensureEmployeeSubject 保证员工主体在授权表里有条目：装配期显式分配优先，没有则按
// （显式格子 → 档位）派生默认位。条目缺位时主体会被框架当成"未授权主体"——那会把
// "没分配过"变成"没有任何能力"，而不是"按装配的口径判"。
func (state *PermissionGate) ensureEmployeeSubject(subject toolspermission.Subject, identity employeeIdentity) {
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.cfg.Subjects == nil {
		return
	}
	if _, exists := state.cfg.Subjects[subject]; exists {
		return
	}
	state.cfg.Subjects[subject] = grantForEmployeeIdentity(identity)
	state.rebuildLocked()
}

// grantForEmployeeIdentity 把一份员工身份落成授权条目：显式格子优先，否则按档位。
// 与装配期 Runtime.AssignEmployeePermissions 的派生口径同一套（EmployeeGrantFor /
// grantFromEmployeeGroups），因此"装配期分配"与"运行时补默认"给出同一个结论。
func grantForEmployeeIdentity(identity employeeIdentity) toolspermission.SubjectGrant {
	if len(identity.Groups) > 0 {
		return grantFromEmployeeGroups(identity.Groups)
	}
	return EmployeeGrantFor(identity.Policy)
}
