package policy

// policy.go —— 权限**判据**（主体 × 路由组 × 位 × 动作）的单一落点。
//
// 2026-10-16 从 seelebridge/tools/permission_policy.go 的前半段归位（M2-C4，行为零变化）：
// 这里只留**纯判据与词表**；带状态的执行门（PermissionGate 及其 classFor /
// decideFor / enforce* / visibleFor / ToolFaceFor*）与注册表仍留在 backend/tool/registry。
//
// 长期口径见 docs/2026-09-15-agent-permission-routing-groups/README.md §2；下面是
// 它在 seelex 侧的可执行形态：
//
//	主体（谁在调用）   root（主代理 / entry 节点 / goalplan 节点，uid 0）
//	                   sub（子代理：plan kind:agent 节点 / fork_subagents 派生）
//	                   emp_ro / emp_rw（员工：由 dto.RoleSpec.ToolsPolicy 决定）
//	路由组（哪一族）   ro / rw / rw_session / rw_desktop / ctl / adm / bash（按名字 glob 路由）
//	位（rwx）          组给 mode，主体给 grant；位齐 → 组默认动作，位缺 → 不可路由
//	动作              组默认 allow/ask；rules 做最细粒度覆盖（LMRW 最后匹配胜出）
//
// 三条产品决定（不是可选配置）：
//
//  1. **子代理没有人类在环**：任何"需要问人"的调用（组默认 ask / 规则 ask / 位缺）
//     一律**直接拒绝**，绝不落到执行选择页面上去挂起等待。子代理的授权范围由位
//     表达（位齐 = 授权范围内），作用域另有 worktree / ProjectScope 收窄。
//  2. **员工有宿主人类，且赋权即免审批**：位齐（在 leader 下发的权限范围内）→
//     **直接执行**，不再落回 rules 的"问人"；位缺 / 未分封（权外）→ 走执行选择
//     页面**提权**（人类的选择页 = sudo 口令，单次放行）。显式 deny 规则仍然硬，
//     不因提权页面而变成可放开。
//  3. **主代理完全落回框架**：组默认 + rules 的判定与接线前逐条一致，权责模型
//     对 root 只增加"位必须齐"这一道（root 全位，等价于不增加）。
//
// 主体与框架 `permission.Engine` 是同一个 id：middleware 把主体写进 ctx
// （WithEngine），checker 用它查授权表，backend/tool/registry 的 Enforcer 用同一身份做类策略。

import (
	"strings"

	toolspermission "github.com/RedHuang-0622/Seele/tools/permission"
	"github.com/RedHuang-0622/seelex/contract/dto"
)

// permission_policy.go — 权责模型（主体 × 路由组 × 位 × 动作）的单一落点。
//
// 长期口径见 docs/2026-09-15-agent-permission-routing-groups/README.md §2；本文件是
// 它在 seelex 侧的可执行形态：
//
//	主体（谁在调用）   root（主代理 / entry 节点 / goalplan 节点，uid 0）
//	                   sub（子代理：plan kind:agent 节点 / fork_subagents 派生）
//	                   emp_ro / emp_rw（员工：由 dto.RoleSpec.ToolsPolicy 决定）
//	路由组（哪一族）   ro / rw / rw_session / rw_desktop / ctl / adm / bash（按名字 glob 路由）
//	位（rwx）          组给 mode，主体给 grant；位齐 → 组默认动作，位缺 → 不可路由
//	动作              组默认 allow/ask；rules 做最细粒度覆盖（LMRW 最后匹配胜出）
//
// 三条产品决定（不是可选配置）：
//
//  1. **子代理没有人类在环**：任何"需要问人"的调用（组默认 ask / 规则 ask / 位缺）
//     一律**直接拒绝**，绝不落到执行选择页面上去挂起等待。子代理的授权范围由位
//     表达（位齐 = 授权范围内），作用域另有 worktree / ProjectScope 收窄。
//  2. **员工有宿主人类，且赋权即免审批**：位齐（在 leader 下发的权限范围内）→
//     **直接执行**，不再落回 rules 的"问人"；位缺 / 未分封（权外）→ 走执行选择
//     页面**提权**（人类的选择页 = sudo 口令，单次放行）。显式 deny 规则仍然硬，
//     不因提权页面而变成可放开。
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
// 字面量的**唯一事实在 contract/dto**（dto.PermissionGroup*）：组名
// 同时是"前端权限装配面板"的格子名与"写入侧校验"的枚举，两边必须是同一份；
// 这里只做别名，方便本包内部引用。
const (
	GroupRO        = dto.PermissionGroupRO        // 读簇：不改任何共享状态
	GroupRW        = dto.PermissionGroupRW        // 写簇：项目文件 + 自有工作台
	GroupRWSession = dto.PermissionGroupRWSession // 写簇：本会话可变 transcript（子代理无位）
	GroupRWDesktop = dto.PermissionGroupRWDesktop // 写簇：共享外设（一块桌面，子代理无位）
	GroupCTL       = dto.PermissionGroupCTL       // 叫停 loop 簇：结束 / 挂起 / 派生 / 装载执行结构
	GroupADM       = dto.PermissionGroupADM       // 属主簇：改变能力面本身（仅 root）
	GroupBash      = dto.PermissionGroupBash      // 命令执行：bash / bash_bg / bg_manage
	// GroupSSH 是**唯一的远端能力**：允许把调用打到会话固定环境对应的远端执行面。
	// 远端根内/外由该能力下的路径白名单细分，不再拆成第二枚权限。
	GroupSSH = dto.PermissionGroupSSH
)

// RemoteFaceGroupBits 返回"按面判定"两组的组位（判据单点）。
//
// 用函数而不是在调用点各写一遍字面量：位值必须与组表里的 Mode 一致，唯一的算法是
// "问这张表"，第二份写法迟早漂移。
func RemoteFaceGroupBits(groups []toolspermission.PermissionGroup, name string) (uint8, bool) {
	for _, group := range groups {
		if group.Name == name {
			return group.Mode, true
		}
	}
	return 0, false
}

// RemoteFaceToolsFromGroups 从路由组表里读出"真的有远端执行面"的工具名清单。
//
// **单一事实源仍然是组表**（GroupSSH 的 Match 那一栏）：registry 的
// RemoteFaceToolNames 就是调这里，因此"哪些工具能打到远端"只写得下一处。名单写错或
// 漏写会让"选了 ssh 但 handler 跑本地"重新变成可能，所以这份派生必须跟着组表走。
func RemoteFaceToolsFromGroups(groups []toolspermission.PermissionGroup) []string {
	for _, group := range groups {
		if group.Name != GroupSSH {
			continue
		}
		out := make([]string, 0, len(group.Match))
		for _, name := range group.Match {
			// 组表允许 glob（`*`），但"远端面清单"要的是具体工具名：通配形态在这里
			// 直接跳过，宁可漏（消费点自己补）也不要猜出一串不存在的工具。
			if strings.Contains(name, "*") || strings.TrimSpace(name) == "" {
				continue
			}
			out = append(out, name)
		}
		return out
	}
	return nil
}

// 位的 resource 限定口径（rw 必须落在哪片资源上）。
const (
	ResourceProject = "project" // 会话绑定的项目根（已被 ProjectScope 收窄）
	ResourceSession = "session" // 本会话的可变工作台
	ResourceDesktop = "desktop" // 共享外设：所有并行子代理共用一块桌面
)

// 位值（r=4 / w=2 / x=1）。字面量同样取自 dto（跨层词表），这里只是别名。
const (
	BitRead    uint8 = dto.PermissionBitRead
	BitWrite   uint8 = dto.PermissionBitWrite
	BitExecute uint8 = dto.PermissionBitExecute
	BitAll     uint8 = dto.PermissionBitAll
)

// DefaultPermissionGroupList 是默认路由组表：按工具名 glob 路由，组带对象 mode 与默认动作。
//
// 路由是 LMRW（最后匹配胜出，与框架 routeLocked 同语义），因此工具名只落入最终
// 命中的组；语义判据见设计稿 §2.2：ro 不碰共享状态、rw 写项目/工作台、bash 执行
// 命令、ctl 改循环控制流、adm 改能力面本身。adm 是内部组，不进入公开权限标签。
func DefaultPermissionGroupList() []toolspermission.PermissionGroup {
	return []toolspermission.PermissionGroup{
		// ── SSH 远端能力（单组）────────────────────────────────────────────
		//
		// 为什么放在表头：路由是 LMRW（最后匹配胜出），这两组**不接管任何工具的名字
		// 路由**——同一支 read_file / bash 既能跑本地也能跑远端，名字里没有这个信息，
		// 名字路由再怎么排也分不出这两件事。真正的判据在"已定的执行面"上（决策段
		// 写进 ctx，路径门读它，见 registry 的 path_gate_face.go）：
		//
		//	远端面本身        → 要 ssh 位（x）
		//	远端面上走出远端根 → 仍用同一 ssh 位；路径白名单是它的配置细节
		//
		// 那 Match 这一栏在这里是什么？**声明远端执行面的工具清单**：组的 Match 是
		// "这一族能力会落在哪些工具上"的唯一写法，registry 的 RemoteFaceToolNames()
		// 直接从这里派生（见本文件的 RemoteFaceToolsFromGroups）。放在表头恰好让
		// ro / rw 在后面覆盖掉这些名字，从而"声明了但不改路由"——既保住本地行为
		// 逐字不变，也不新增第二张表。
		{
			Name:    GroupSSH,
			Mode:    BitExecute,
			Default: toolspermission.ActionAllow,
			Match: []string{
				// 与 registry.RemoteFaceToolNames() 是同一份数据（它从这一栏派生）。
				// 新增远端面工具时改这里一处，选择器的消费者闸门跟着变。
				"read_file", "write_file", "edit_file", "bash", "bash_bg", "bg_manage",
			},
		},
		{
			Name:    GroupRO,
			Mode:    BitRead,
			Default: toolspermission.ActionAllow,
			Match: []string{
				"read_file", "read_plan", "read_tool_result", "read_compressed_turn",
				"search_history", "grep_search", "glob",
				// read_batch 派发的是一批**只读**扇出作业（Kind=inline），不碰共享状态：
				// 派发面免打断，取回/终止/销项走 bg_manage（bash 簇）。
				"read_batch",
				"get_time", "web_search",
				"todo_status", "todolist_status", "goal_status",
				"plugins_list", "skills_list", "mcp_list",
				// show_files（2026-10-12，J-2）：agent 请求 GUI 打开待审阅文件。归 ro
				// 而非 rw_desktop/session——它不改任何共享状态：请求落在**本会话**的
				// 只读快照段里（人可忽略、可关掉，不是模态框/置顶窗），且工具返回值
				// 只带路径与计数（文件内容不进上下文，也不写任何文件）。定级结论：
				// 「agent 控制 agentGUI / 只读浏览」档，不高于会话读写档（见
				// docs/arch/agent-permission-subjects.md §2.2 与 §7.2 第 5 条）。
				"show_files",
				// project_board_read（M4）：按需读**派生投影**（会话任务行 / 里程碑）。
				// 归 ro：它只读、有界，不改任何共享状态，也不写任何文件；工具本身是否
				// 存在由装配门决定（limits.project_board.enabled）。
				"project_board_read",
				"computer_screenshot", "computer_windows", "computer_scroll_targets", "computer_wait",
			},
		},
		{
			Name:     GroupRW,
			Mode:     BitRead | BitWrite,
			Resource: ResourceProject,
			Default:  toolspermission.ActionAllow,
			Match: []string{
				"write_file", "edit_file",
				// jobs_manage 是 Seele jobs 的**通用管理工具**（observe/fetch/kill/done），
				// 也管理 teammate 等非 shell 作业，因此留在项目写簇；bash_bg / bg_manage
				// 只管理本会话 shell 执行体，归独立的 bash 簇。
				"jobs_manage",
				"todo_init", "todo_add", "todo_done",
				"todolist_init", "todolist_add", "todolist_done",
				"task_add", "taskadd",
				"project_refresh", "plugin_create", "skill_create",
			},
		},
		{
			Name:     GroupRWSession,
			Mode:     BitRead | BitWrite,
			Resource: ResourceSession,
			Default:  toolspermission.ActionAllow,
			Match:    []string{"compact_context"},
		},
		{
			Name:     GroupRWDesktop,
			Mode:     BitRead | BitWrite,
			Resource: ResourceDesktop,
			Default:  toolspermission.ActionAsk,
			Match: []string{
				"computer_click", "computer_move", "computer_drag", "computer_scroll",
				"computer_type", "computer_keys", "computer_focus",
			},
		},
		{
			Name:    GroupCTL,
			Mode:    BitExecute,
			Default: toolspermission.ActionAllow,
			Match: []string{
				"task_complete", "task_failed", "task_needs_user_decision", "task_check_node",
				"ask_approve", "fork_subagents",
				// goal 栈是主代理的治理状态：policy.go 对子代理整族不可见，
				// 因此跟着 ctl 一起断位（比设计稿 §3.2 的 rw 更贴合现有口径）。
				"goal_begin", "goal_update", "goal_done",
				// teamwork leader 编排面：改的是"团队顺序与作业"这条循环控制流，
				// 与 fork_subagents 同族——sub/员工断位（teammate 不该编排
				// 团队），主代理（root）默认 allow。
				"team_plan", "team_dispatch", "team_join", "team_milestone", "team_close", "team_context",
				// Work Item 口径（2026-10-03）：排活 / 调整 / 验收 / 判失败 / 中断恢复。
				// 与上面同族——它们改的都是"团队顺序与结论"，员工与子代理一律断位。
				"team_work", "team_item", "team_accept", "team_fail", "team_recover", "team_items",
			},
		},
		{
			Name:    GroupADM,
			Mode:    BitAll,
			Default: toolspermission.ActionAsk,
			Match: []string{
				"switch_plugin", "switch_mode", "skill_activate",
				"plugins_reload", "mcp_create", "mcp_load",
			},
		},
		{
			Name:    GroupBash,
			Mode:    BitExecute,
			Default: toolspermission.ActionAllow,
			Match:   []string{"bash", "bash_bg", "bg_manage"},
		},
	}
}

// DefaultPermissionSubjects 是默认授权表：主体 → 各组位与 resource 限定。
//
// root 全位（含 desktop / session）；sub 拿满项目写位但 ctl/adm 断位、无共享外设位；
// 员工按 ToolsPolicy 两档（只读 / 读+项目写），ctl/adm 与共享外设同样断位。
//
// `ssh`（x）是唯一的远端能力位；默认给所有主体，向量里的 `!ssh` 会真正拦住。
func DefaultPermissionSubjects() map[toolspermission.Subject]toolspermission.SubjectGrant {
	root := toolspermission.SubjectGrant{Bits: map[string]toolspermission.GrantBit{
		GroupRO:        {Bits: BitRead},
		GroupRW:        {Bits: BitRead | BitWrite, Resources: []string{ResourceProject}},
		GroupRWSession: {Bits: BitRead | BitWrite, Resources: []string{ResourceSession}},
		GroupRWDesktop: {Bits: BitRead | BitWrite, Resources: []string{ResourceDesktop}},
		GroupCTL:       {Bits: BitExecute},
		GroupADM:       {Bits: BitAll},
		GroupSSH:       {Bits: BitExecute},
		GroupBash:      {Bits: BitExecute},
	}}
	sub := toolspermission.SubjectGrant{Bits: map[string]toolspermission.GrantBit{
		GroupRO:   {Bits: BitRead},
		GroupRW:   {Bits: BitRead | BitWrite, Resources: []string{ResourceProject}},
		GroupCTL:  {Bits: 0},
		GroupADM:  {Bits: 0},
		GroupSSH:  {Bits: BitExecute},
		GroupBash: {Bits: BitExecute},
	}}
	employeeRO := toolspermission.SubjectGrant{Bits: map[string]toolspermission.GrantBit{
		GroupRO:  {Bits: BitRead},
		GroupRW:  {Bits: 0},
		GroupCTL: {Bits: 0},
		GroupADM: {Bits: 0},
		GroupSSH: {Bits: BitExecute},
	}}
	employeeRW := toolspermission.SubjectGrant{Bits: map[string]toolspermission.GrantBit{
		GroupRO:   {Bits: BitRead},
		GroupRW:   {Bits: BitRead | BitWrite, Resources: []string{ResourceProject}},
		GroupCTL:  {Bits: 0},
		GroupADM:  {Bits: 0},
		GroupSSH:  {Bits: BitExecute},
		GroupBash: {Bits: BitExecute},
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
//
// 方言：本函数给的是 **POSIX 方言**那一份（历史默认；等价于
// `PermissionRulesForShell(ShellBash)`）。组合根按装配期解析出的**实际壳**选方言——
// 见 PermissionRulesForShell 与 shell_fact.go。
func DefaultPermissionRules() []toolspermission.PermissionRule {
	return PermissionRulesForShell(ShellBash)
}

// bashAllowPatterns 是"免打断的读命令"这一层的**唯一一份**表，按方言分槽。
//
// 为什么要按壳分派：这一层的模式匹配的是**命令文本**，它放行就意味着"这条命令不会
// 需要人看着"。而同一个字符串在不同壳里是两回事——`ls -la` 在 Git Bash 下是列目录，
// 在 PowerShell 下是**必然失败**（`-la` 不是 Get-ChildItem 的参数）。此前这张表写死
// POSIX 形态，而本机 shell 可能已经是 PowerShell，于是命令"**过闸即死**"：白名单放行、
// 执行失败、模型只拿到一条执行错误（审计 D4 实测 34.4% 的调用折在判定上）。
//
// 共享槽与方言槽分开：`git *` / `go test *` 这类两种壳下都成立的命令只写一份，
// 方言差异只出现在"读写文件/搜索/环境"那几条上。
var bashAllowPatterns = struct {
	Shared     []string
	POSIX      []string
	PowerShell []string
	Cmd        []string
}{
	Shared: []string{"git *", "echo *", "go test *", "go build *", "go vet *", "gofmt *"},
	POSIX: []string{"ls *", "cat *", "head *", "tail *",
		"pwd", "which *", "whoami", "date", "df *", "du *", "printenv"},
	PowerShell: []string{"Get-ChildItem*", "Get-Content *", "Get-Location", "Get-Date",
		"Get-Command *", "Get-Item *", "Select-String *", "Select-Object *", "Where-Object *",
		"Test-Path *", "Measure-Object *"},
	Cmd: []string{"dir *", "dir", "type *", "where *", "findstr *", "cd"},
}

// bashAllowListForShell 折出这一档壳的免打断模式（共享槽 + 方言槽）。
func bashAllowListForShell(kind ShellKind) []string {
	dialect := bashAllowPatterns.POSIX
	switch kind {
	case ShellPowerShell:
		dialect = bashAllowPatterns.PowerShell
	case ShellCmd:
		dialect = bashAllowPatterns.Cmd
	}
	patterns := make([]string, 0, len(bashAllowPatterns.Shared)+len(dialect))
	patterns = append(patterns, bashAllowPatterns.Shared...)
	return append(patterns, dialect...)
}

// PermissionRulesForShell 按**实际壳**给出同一张规则表。
//
// 除"免打断读命令"那一层之外，其余层（写类工具 ask、危险命令 deny、越界命令 ask）
// 与壳无关，只有一份——两处各写一遍必然漂移。
func PermissionRulesForShell(kind ShellKind) []toolspermission.PermissionRule {
	allow := bashAllowListForShell(kind)
	return []toolspermission.PermissionRule{
		// 写盘 / 扩能力的工具：生效前需要人类过目。
		{ToolName: "write_file", Action: toolspermission.ActionAsk},
		{ToolName: "edit_file", Action: toolspermission.ActionAsk},
		{ToolName: "plugin_create", Action: toolspermission.ActionAsk},
		{ToolName: "skill_create", Action: toolspermission.ActionAsk},

		// bash 能力白名单：安全命令直接执行，越界命令问人，危险命令拒绝。
		//
		// 口径同时覆盖 bash_bg（后台受管命令）——它跑的是同一类命令，只是活过这一轮。
		// 这张模式表就是"免打断的读命令"的**唯一落点**（bash_read 已删除）：模式匹配
		// 弱于服务端分类，因此这里刻意只放保守形态（无管道、无重定向、无替换），
		// 越界就退化成问人——宁可多弹一次审批，也不要"挂着只读名牌的写命令"。
		{ToolName: "bash", Action: toolspermission.ActionAsk},
		{ToolName: "bash_bg", Action: toolspermission.ActionAsk},
		{
			ToolName: "bash",
			Patterns: allow,
			Action:   toolspermission.ActionAllow,
		},
		{
			ToolName: "bash_bg",
			Patterns: allow,
			Action:   toolspermission.ActionAllow,
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
	return PermissionConfigForShell(ShellBash)
}

// PermissionConfigForShell 按**装配期解析出的实际壳**装配默认权责配置（组合根用它）。
//
// 与 DefaultPermissionConfig 的关系：同一份实现，只有"免打断读命令"那一层随方言变
// （见 PermissionRulesForShell）。默认入口保持 POSIX 口径，好让用例与其它宿主的行为
// 与宿主机器无关。
func PermissionConfigForShell(kind ShellKind) toolspermission.PermissionConfig {
	return toolspermission.PermissionConfig{
		Mode:       toolspermission.ModeManual,
		Groups:     DefaultPermissionGroupList(),
		Subjects:   DefaultPermissionSubjects(),
		MissingBit: toolspermission.ActionAsk,
		Rules:      PermissionRulesForShell(kind),
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

// ClassForSceneScope 把工具调度 ctx 的节点作用域映射成主体类：带 NodeID 的
// RoleSubAgent 节点 = 子代理；其余（无作用域 / 主代理 / entry / goalplan）= root。
// 口径与 tools/policy.go 的可见性判定同源（它同样只按 RoleSubAgent 收窄）。
func ClassForSceneScope(scope dto.SceneScope) SubjectClass {
	if scope.SceneID != "" && scope.Role == dto.RoleSubAgent {
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
