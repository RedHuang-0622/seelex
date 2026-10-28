package boot

import (
	"github.com/RedHuang-0622/seelex/application"
	"github.com/RedHuang-0622/seelex/application/assembly"
	assemble "github.com/RedHuang-0622/seelex/backend/engine/assemble"
	"github.com/RedHuang-0622/seelex/backend/engine/plugin"
	"github.com/RedHuang-0622/seelex/backend/engine/skill"
	"github.com/RedHuang-0622/seelex/backend/limit/overlay"
	toolreg "github.com/RedHuang-0622/seelex/backend/tool/registry"
	"github.com/RedHuang-0622/seelex/internal/host"
	"github.com/RedHuang-0622/seelex/internal/startup"
	"github.com/RedHuang-0622/seelex/store"
	storeboard "github.com/RedHuang-0622/seelex/store/board"
	"github.com/RedHuang-0622/seelex/store/workspace"
)

func InitSessionManager(router *store.Router, eng *host.EnginePort) *host.SessionManager {
	manager := host.NewSessionManager().WithRouter(router)
	manager.InjectSaveLoad(
		func(sessionID string) error { return router.Save(sessionID, eng.RawHistory()) },
		func(sessionID string) error {
			history, err := router.Load(sessionID)
			if err != nil {
				return err
			}
			return eng.ReplaceRawHistory(sessionID, history)
		},
	)
	return manager
}

func InitApplication(
	eng *host.EnginePort, runtime *assemble.Runtime, plugins *plugin.Manager,
	sessions *host.SessionManager, skills *skill.Registry,
	workspaces *workspace.Repo,
	events *application.EventHub, approval application.ApprovalBroker,
	boards *storeboard.SessionStore,
	// storePath 是数据根（`-store` 解析后的值）：运行期上限覆盖层与工具记录
	// 计数开关都落在它下面的 runtime-limits.json，所以它是**显式输入**而不是
	// 包级变量的隐含读取。
	storePath string,
) (*application.Service, error) {
	// 项目侧的连接配置 / 多环境 SSH 表读面注入给 Runtime：执行面的决策段按会话
	// 固定环境名解析本地/SSH 执行面；环境名由适配器注入，不来自模型工具参数。
	// Router 不认识项目配置，真正的实现就是这里这个 workspace 仓库。
	//
	// 顺序说明：Runtime 在 InitRuntime 里先建好（它要读账号档与 limits），环境表
	// 读面是**事后注入**的读面，所以这里是一个 setter 而不是构造参数——构造期注入
	// 会把 Runtime 的创建顺序绑死在 workspace 仓库上。
	runtime.SetWorkspaceEnvironmentPort(host.WorkspacePort{Repo: workspaces})
	// 会话固定环境：读装配层的写穿缓存，不逐次回放会话 SSH 状态的 JSONL——
	// 决策段跑在每一次工具调用上。
	runtime.SetSessionSSHEnvResolver(host.SessionSSHEnvResolver(sessions.Router()))

	sessionPort := host.SessionPort{Manager: sessions, Runtime: runtime, Boards: boards}
	// 会话展示元数据（置顶/别名/排序位）按项目存一份 blob：与 SessionRecord 的
	// 落盘路径完全隔离，不会被回合结束的记录重建覆盖。
	sessionPort.Meta = store.NewSessionMetaStore(sessions.Router())
	// 会话级操作（删除/读历史）按会话绑定项目解析，避免视图切换后活跃
	// 写作用域变化导致删错/读错项目（R3 键漂移 + 列表污染根因）。
	sessionPort.SetWorkspaceResolver(func(sessionID string) string {
		if workspace, ok := workspaces.SessionWorkspace(sessionID); ok {
			return workspace.ID
		}
		return ""
	})
	// 已知项目来源：绑定缺失时归属解析按"数据实际所在"定位，都没有 = 未关联
	// （默认项目），绝不按活跃写作用域猜（视图切换会改变它）。
	sessionPort.SetProjectSource(func() []string {
		items := workspaces.List()
		ids := make([]string, 0, len(items))
		for _, item := range items {
			ids = append(ids, item.ID)
		}
		return ids
	})
	service, err := assembly.New(application.Dependencies{
		Engine: eng, Runtime: host.RuntimePort{Runtime: runtime},
		Plugins: host.PluginPort{Manager: plugins}, Skills: host.SkillPort{Registry: skills},
		Sessions: sessionPort, Workspace: host.WorkspacePort{Repo: workspaces},
		Events: events, Approval: approval,
		// 员工提示词的一次有界优化（Agent Team 入职面板）：实现方是 Runtime
		// 主 completer；未配置账号/completer 时应用层返回可展示错误。
		RolePrompt: runtime,
		// 装配期分配员工权限（与用户权限同一张权责表）：装配团队时把在编员工落成
		// 各自的主体条目 emp_<角色名>，员工回合的工具判定与工具面都按它生效。
		EmployeePermissions: runtime,
		// 会话驻留窗口的落盘口：设置弹窗改的值写进数据根 runtime-limits.json，
		// 下次启动由 applyRuntimeLimitsOverlay 叠回 limits 之上。
		ReleaseWindowStore: overlay.StoreFile{Path: startup.RuntimeLimitsPath(storePath)},
		// 每账号上下文窗口的落盘口：同一个覆盖层文件的 account_windows 表
		// （设置 → 账号的挡位/自定义值）。
		AccountWindowStore: overlay.StoreFile{Path: startup.RuntimeLimitsPath(storePath)},
		// 手动添加账号（设置 → 账户 → 添加）：写账号档 + 热注册进运行时。路径取
		// startup.AccountsPath()——**与启动装载同一份**（二进制旁的 config/accounts.yaml
		// 优先，回退 CWD），否则会出现"界面加的那条不在启动读的那份里"。
		AccountAdmin: host.AccountAdmin{Path: startup.AccountsPath(), Runtime: runtime},
		// 工具记录计数开关的落盘口：同一个覆盖层文件的 tool_record_enabled 格。
		ToolRecordStore: overlay.StoreFile{Path: startup.RuntimeLimitsPath(storePath)},
		// 系统提示词的**环境段**：装配期解析一次"这台机器上 bash 工具实际落到哪个壳"，
		// 写进提示词（模型写命令前唯一的方言事实；2026-10-15 审计 D4）。解析结果进程内
		// 缓存，因此前缀逐轮稳定。
		EnvironmentSection: toolreg.ResolveShellFact().PromptSection(),
	}, ServicePorts(host.RuntimePort{Runtime: runtime}))
	if err != nil {
		return nil, err
	}
	// 权限向量接管：把 (会话, 角色) → 生效权限向量的读面接进执行门。
	//
	// 这是"判定的输入只有一条"的接线口：owner（application/policy 的 RoleApplication）
	// 自己实现"登记优先、缺失按四级链路解析"，执行门只消费结论并按 policy 的格位表
	// 判定。decided=false（没登记、也没有任何一层表态）留给执行门按 ctx 身份派生
	// ——绝不在执行门里再造一份解析。
	if runtime != nil {
		roleOwner := assembly.ComponentsFor(service).Policy.Role
		runtime.SetRolePermissionVectorResolver(func(sessionID, role string) ([]string, bool, error) {
			vector, err := roleOwner.RolePermissionVector(sessionID, role)
			if err != nil {
				return nil, false, err
			}
			// 空向量 = "没有来源定过"（显式"全不给"由 `!tag` 向量表达，不是空集），
			// 因此这里把它报成 decided=false，让执行门落回 ctx 身份派生。
			return vector, len(vector) > 0, nil
		})
	}
	return service, nil
}
