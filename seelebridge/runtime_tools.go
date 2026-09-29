package seelebridge

import (
	"context"
	"fmt"
	"strings"

	frameworktools "github.com/RedHuang-0622/Seele/tools"
	toolspermission "github.com/RedHuang-0622/Seele/tools/permission"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/docker"
	seetelemetry "github.com/RedHuang-0622/seelex/seelebridge/internal/telemetry"
	"github.com/RedHuang-0622/seelex/seelebridge/task"
	seeltools "github.com/RedHuang-0622/seelex/seelebridge/tools"
)

// mcpRegistryAdapter 把 Runtime 工具注册表适配为 mcp.RegistryPort
// （懒解析：注册表在 NewRuntime 中稍后才装配，调用时判空）。
type mcpRegistryAdapter struct{ runtime *Runtime }

func (a mcpRegistryAdapter) Unregister(name string) error {
	if a.runtime == nil || a.runtime.registry == nil || a.runtime.registry.Registry == nil {
		return nil
	}
	return a.runtime.registry.Registry.Unregister(name)
}

func (a mcpRegistryAdapter) Register(provider frameworktools.ToolProvider) error {
	if a.runtime == nil || a.runtime.registry == nil || a.runtime.registry.Registry == nil {
		return nil
	}
	return a.runtime.registry.Registry.Register(provider)
}

// SetPermissionConfig 安装权限门控：Mode + Groups + Subjects + Rules + ApprovalHandler。
// 门控作为 tools.Registry middleware 在每次工具调度前生效。
func (r *Runtime) SetPermissionConfig(cfg toolspermission.PermissionConfig, handler toolspermission.ApprovalHandler) {
	if r.permission != nil {
		r.permission.Set(cfg, handler)
	}
}

// SetRoleSessionPolicyResolver 注入"角色会话（员工）→ ToolsPolicy"的读面：权限门据此
// 把员工角色会话里的工具调用判成 emp_ro / emp_rw 主体（缺位 → 执行选择页面提权）。
// nil = 关闭员工主体识别（全部按 root 判）。读面按 TTL 缓存，每次工具调用不额外读盘。
func (r *Runtime) SetRoleSessionPolicyResolver(resolver func(sessionID string) (string, bool)) {
	if r.permission != nil {
		r.permission.SetRoleSessionPolicyResolver(resolver)
	}
}

// SetRoleSessionOwnerResolver 注入"角色会话（员工/评审者）→ 宿主主会话"的读面：
// 员工/评审者越权提权的审批按宿主主会话归属，复用**现有审批面板**（视图单格 +
// 目录 awaiting_approval + 会话快照），而不是弹在一个用户看不见的角色会话号上。
//
// 不折算的后果是可复现的：继承全量工具的员工一旦撞上"规则要求问人"的工具
// （write_file / edit_file / bash / adm 组 / 共享外设），审批请求没有面板承载，
// 调用只能等到审批超时被拒——员工因此事实上拿不到 sudo 口令。
// nil = 审批按调用会话原样归属（旧行为）。
func (r *Runtime) SetRoleSessionOwnerResolver(resolver func(sessionID string) (string, bool)) {
	if r.permission != nil {
		r.permission.SetRoleSessionOwnerResolver(resolver)
	}
}

// AssignEmployeePermissions 实现 contract.EmployeePermissionPort：在**装配期**把
// 在编员工落成各自的主体条目 emp_<角色名>（员工权限与用户权限同一张权责表）。
//
// 口径：
//   - readonly / readwrite → 按档位派生默认位（与 emp_ro / emp_rw 同口径）；
//   - **显式权限格子**（RoleSpec.PermissionGroups 非空）→ 逐格分配，优先于档位；
//     未列出的组 = 0 位（"这一族能力明确不开"，不是"继承默认"）；
//   - 空（inherit）/ full / 未识别且**没有**显式格子 → **不写条目**：这类员工按
//     宿主默认判（继承就是继承，不该被一条自造的员工条目改写成"另一套语义"）。
//
// 返回错误只在"角色名缺失 / 权限格子非法 / 既无可用档位又无显式格子"时发生。
// 装配方必须显式处理：静默继续的结果是"看起来分配了、其实按宿主默认判"，这是
// 权限面上最坏的一种沉默。
func (r *Runtime) AssignEmployeePermissions(roles []dto.RoleSpec) error {
	if r == nil || r.permission == nil {
		return nil
	}
	permissions := make([]seeltools.EmployeePermission, 0, len(roles))
	for _, role := range roles {
		policy := strings.ToLower(strings.TrimSpace(role.ToolsPolicy))
		groups, err := dto.NormalizePermissionGroups(role.PermissionGroups)
		if err != nil {
			return fmt.Errorf("员工 %q 的权限格子非法: %w", role.RoleName, err)
		}
		if len(groups) == 0 && seeltools.EmployeeGroupsForPolicy(policy) == nil {
			continue
		}
		permissions = append(permissions, seeltools.EmployeePermission{
			RoleName: role.RoleName,
			Policy:   policy,
			Groups:   groups,
		})
	}
	if len(permissions) == 0 {
		return nil
	}
	return r.permission.SetEmployeePermissions(permissions)
}

// EmployeePermissions 读回装配期分配过的员工权限（巡检/诊断面）。
func (r *Runtime) EmployeePermissions() []seeltools.EmployeePermission {
	if r == nil || r.permission == nil {
		return nil
	}
	return r.permission.EmployeePermissions()
}

// bashDiagnosticMiddleware marks entry to and exit from the framework tool
// registry. Together with scopedBash's process stages, it distinguishes a
// stalled handler from a stall in the registry/framework after the handler
// has already returned. It is no-op unless a diagnostic observer is installed.
func (r *Runtime) bashDiagnosticMiddleware() frameworktools.Middleware {
	return func(name string, next frameworktools.ToolHandler) frameworktools.ToolHandler {
		if name != "bash" {
			return next
		}
		return frameworktools.HandlerFunc(func(ctx context.Context, argsJSON string) (string, error) {
			r.observeBash(BashDiagnosticEvent{Stage: "bash.registry.dispatch.start"})
			result, err := next.Execute(ctx, argsJSON)
			if err != nil {
				r.observeBash(BashDiagnosticEvent{Stage: "bash.registry.dispatch.error", Err: err})
				return result, err
			}
			r.observeBash(BashDiagnosticEvent{Stage: "bash.registry.dispatch.done"})
			return result, nil
		})
	}
}

// ensureDockerForRuntime 是 tools 域的接线面：按 limits 配置执行自动恢复
// （disable_docker_auto_start 关闭时返回 nil 表示"不处理"）。
func (r *Runtime) ensureDockerForRuntime(ctx context.Context) error {
	if r == nil {
		return nil
	}
	return docker.EnsureForRuntime(ctx, r.limits.DisableDockerAutoStart, r.limits.DockerStartTimeoutSec, r.dockerProbe)
}

// observeBash 投递 scoped bash 诊断事件（工具调用不可被诊断改变；观察者
// 意外 panic 也不影响工具调用）。
func (r *Runtime) observeBash(event BashDiagnosticEvent) {
	if r == nil {
		return
	}
	r.bashObserverMu.RLock()
	observer := r.bashObserver
	r.bashObserverMu.RUnlock()
	if observer == nil {
		return
	}
	defer func() { _ = recover() }()
	observer(event)
}

// registerProjectScopedTools overrides the Seele builtin filesystem tools
// （委托 tools.Router；RegisterBuiltins 内调用）。
func (r *Runtime) registerProjectScopedTools() {
	router := seeltools.NewRouter(r.scopedToolsDeps())
	router.Register()
	r.scopedTools = router
	// 后台命令的输出目录是进程级资源：登记进逆序关停链，否则每个进程都在临时目录
	// 里留一份无人回收的日志（规格 §8.3 指标 5 实测）。
	r.lifecycle = append(r.lifecycle, router.CloseAsync)
}

// ReleaseSessionAsync 杀掉某会话名下所有在途后台命令（会话删除/归档时由 core 调用）。
//
// 为什么必须有人调它：作业是按会话登记的，会话没了就再没有任何一条
// job_manage 路径能拿到它——不终止就是无人认领的孤儿，而工作
// 打点表会一路跟着它显示 running。
//
// 返回被登记的句柄数（不是"确认杀死数"：杀不掉的仍由各自执行体收尾收敛）。
func (r *Runtime) ReleaseSessionAsync(sessionID string) int {
	if r == nil || r.scopedTools == nil {
		return 0
	}
	return r.scopedTools.CloseSessionAsync(sessionID)
}

// AsyncPendingFor 报告某会话还在跑的后台命令数。core 的"无进展预算"用它把
// "有在途执行被查询"算作进展——见 application/core/task_context/coordinator.go。
func (r *Runtime) AsyncPendingFor(sessionID string) int {
	if r == nil || r.scopedTools == nil {
		return 0
	}
	return r.scopedTools.AsyncPendingFor(sessionID)
}

// registerTaskTools 注册主动任务工具 taskadd（同上委托）。
func (r *Runtime) registerTaskTools() {
	task.NewTools(r.taskToolsDeps()).RegisterTaskTools()
}

// registerTodoTools 注册 todolist 工具族（委托 task.Tools；RegisterBuiltins 内调用）。
func (r *Runtime) registerTodoTools() {
	task.NewTools(r.taskToolsDeps()).RegisterTodoTools()
}

// scopedToolsDeps 把 Runtime 能力面注入 tools 域（Deps 全部为闭包）。
func (r *Runtime) scopedToolsDeps() seeltools.Deps {
	return seeltools.Deps{
		RegisterTool: r.RegisterTool,
		ProjectScope: r.projectScope,
		// 工具路径根按执行 ctx 的会话解析：后台/并行会话各用自己的项目根。
		SessionKey:             seetelemetry.SessionIDFromContext,
		FileSystem:             r.filesystem,
		GrepMaxResults:         r.limits.GrepMaxResults,
		WalkTimeoutSec:         r.limits.WalkTimeoutSec,
		ToolCallTimeout:        r.toolCallTimeout,
		ToolCallTimeoutSec:     r.limits.ToolCallTimeoutSec,
		DisableDockerAutoStart: r.limits.DisableDockerAutoStart,
		AsyncExecEnabled:       r.limits.AsyncExec.Enabled,
		AsyncBatchID:           r.CurrentTaskBatchFor,
		ObserveBash:            r.observeBash,
		EnsureDocker:           r.ensureDockerForRuntime,
		DockerDaemonDown:       docker.IsDaemonDown,
		DockerCLIPath:          docker.CLIPath,
		DockerHint:             docker.Hint,
	}
}

// taskToolsDeps 把 Runtime 能力面注入 task 工具族（Deps 全部为闭包）。
func (r *Runtime) taskToolsDeps() task.Deps {
	return task.Deps{
		RegisterTool: r.RegisterTool,
		// 工具写的会话 = 调用它的那个会话（执行 ctx 的会话键），不是实时注册表。
		SessionFromContext: seetelemetry.SessionIDFromContext,
		TaskAddFor:         r.TaskAddFor,
		ReplaceTodoFor:     r.ReplaceTodoFor,
		AppendTodoFor:      r.AppendTodoFor,
		SetTodoStatusFor:   r.SetTodoStatusFor,
		TodoSnapshotFor:    r.TodoSnapshotFor,
		TodoMaxItems:       r.limits.TodoMaxItems,
	}
}
func (r *Runtime) AllTools() []Tool {
	return summarizeTools(r.registry.Registry.Tools())
}
func (r *Runtime) FullAccess() bool {
	return r.permission != nil && r.permission.FullAccess()
}
func (r *Runtime) RegisterBuiltins() {
	r.registerProjectScopedTools()
	r.registerForkTool()
	r.registerTodoTools()
	r.registerTaskTools()
	// computer use（截屏/窗口/输入注入）：支持桌面的平台默认注册，
	// SEELEX_COMPUTER_USE 可整体关闭；图片走会话媒体分区 + 随图队列，
	// 权限仍由 seele.yaml 的 permission.rules 逐次把关。
	r.registerComputerTools()
	r.scopedToolsReady = true
	// plan 工具（seelex-workplan provider）：plan_load/plan_clear/plan_validate/
	// plan_status/plan_export；plan_run 的执行内核在 seele-v2 slice 4 迁移后恢复。
	if r.planExecutor != nil {
		if err := r.registry.Registry.Register(r.planExecutor.Provider()); err != nil {
			return
		}
	}
}
func (r *Runtime) RegisterTool(
	name, description string,
	inputSchema map[string]interface{},
	handler func(context.Context, string) (string, error),
) {
	if r.scopedToolsReady && isProjectScopedTool(name) {
		return
	}
	r.registry.AddInline(name, description, inputSchema, handler)
}

// ToolMetas 返回**已装配内联工具面**每个工具的簇属声明（名字 → ToolMeta）。
//
// 这是打点 K-0 的读面：簇属在注册时由 tools.DeclaredToolMeta 填进注册表条目，
// 用例据此断言"全量工具都声明了 Groups"，诊断面据此回答"这个工具属于哪个簇"。
func (r *Runtime) ToolMetas() map[string]frameworktools.ToolMeta {
	if r == nil || r.registry == nil {
		return nil
	}
	return r.registry.InlineMetas()
}

// UndeclaredTools 返回**没有簇属声明**的工具名（K-0 判据：seelex 自己的静态工具面
// 必须为空）。非空意味着这个名字既没有权限策略、也没有并发分类——一张看得见的
// 清单，比一条"注册时忘了分封"的静默降级好。
func (r *Runtime) UndeclaredTools() []string {
	if r == nil || r.registry == nil {
		return nil
	}
	return r.registry.UndeclaredTools()
}

func (r *Runtime) SetFullAccess(on bool) {
	if r.permission != nil {
		r.permission.SetFullAccess(on)
	}
}

// SetFullAccessFor 按会话设置全权模式（G4 归属面）：执行门的全权短路按
// 工具调度 ctx 的会话解析，A 会话的决定不替 B 会话放行。
func (r *Runtime) SetFullAccessFor(sessionID string, on bool) {
	if r.permission != nil {
		r.permission.SetFullAccessFor(sessionID, on)
	}
}

// FullAccessFor 返回指定会话生效的全权模式（探针/诊断面；与 middleware
// 同源解析）。
func (r *Runtime) FullAccessFor(sessionID string) bool {
	return r.permission != nil && r.permission.FullAccessFor(sessionID)
}

// PermissionTier 返回进程级默认权限档位（装配/诊断面）。
func (r *Runtime) PermissionTier() string {
	if r.permission == nil {
		return dto.PermissionTierManual
	}
	return r.permission.PermissionTier()
}

// SetPermissionTierFor 按会话设置权限档位（G4 归属面）：执行门按工具调度 ctx
// 的会话解析档位，A 会话的切档不替 B 会话放行。未识别的档位 id 报错。
func (r *Runtime) SetPermissionTierFor(sessionID, tier string) error {
	if r.permission == nil {
		return nil
	}
	return r.permission.SetPermissionTierFor(sessionID, tier)
}
func (r *Runtime) VisibleTools(ctx context.Context) []Tool {
	return summarizeTools(r.agt.VisibleTools(ctx))
}
func isProjectScopedTool(name string) bool {
	switch name {
	case "read_file", "grep_search", "glob", "write_file", "edit_file", "bash":
		return true
	default:
		return false
	}
}
