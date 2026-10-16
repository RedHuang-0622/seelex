package bootstrap

import (
	"github.com/RedHuang-0622/seelex/application"
	seelebridge "github.com/RedHuang-0622/seelex/backend/engine/assemble"
	"github.com/RedHuang-0622/seelex/backend/limit/overlay"
	toolreg "github.com/RedHuang-0622/seelex/backend/tool/registry"
	"github.com/RedHuang-0622/seelex/plugin"
	"github.com/RedHuang-0622/seelex/session"
	"github.com/RedHuang-0622/seelex/skill"
	"github.com/RedHuang-0622/seelex/store"
	"github.com/RedHuang-0622/seelex/store/workspace"
	"github.com/RedHuang-0622/seelex/wiring"
	"github.com/RedHuang-0622/seelex/wiring/adapters"
	"github.com/RedHuang-0622/seelex/wiring/config"
)

func InitSessionManager(router *store.Router, eng *adapters.EnginePort) *session.Manager {
	manager := session.NewManager().WithRouter(router)
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
	eng *adapters.EnginePort, runtime *seelebridge.Runtime, plugins *plugin.Manager,
	sessions *session.Manager, skills *skill.Registry,
	workspaces *workspace.Repo,
	events *application.EventHub, approval *application.ApprovalBroker,
	// storePath 是数据根（`-store` 解析后的值）：运行期上限覆盖层与工具记录
	// 计数开关都落在它下面的 runtime-limits.json，所以它是**显式输入**而不是
	// 包级变量的隐含读取。
	storePath string,
) (*application.Service, error) {
	sessionPort := adapters.SessionPort{Manager: sessions, Runtime: runtime}
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
	return wiring.New(application.Dependencies{
		Engine: eng, Runtime: adapters.RuntimePort{Runtime: runtime},
		Plugins: adapters.PluginPort{Manager: plugins}, Skills: adapters.SkillPort{Registry: skills},
		Sessions: sessionPort, Workspace: adapters.WorkspacePort{Repo: workspaces},
		Events: events, Approval: approval,
		// 员工提示词的一次有界优化（Agent Team 入职面板）：实现方是 Runtime
		// 主 completer；未配置账号/completer 时应用层返回可展示错误。
		RolePrompt: runtime,
		// 装配期分配员工权限（与用户权限同一张权责表）：装配团队时把在编员工落成
		// 各自的主体条目 emp_<角色名>，员工回合的工具判定与工具面都按它生效。
		EmployeePermissions: runtime,
		// 会话驻留窗口的落盘口：设置弹窗改的值写进数据根 runtime-limits.json，
		// 下次启动由 applyRuntimeLimitsOverlay 叠回 limits 之上。
		ReleaseWindowStore: overlay.StoreFile{Path: config.RuntimeLimitsPath(storePath)},
		// 每账号上下文窗口的落盘口：同一个覆盖层文件的 account_windows 表
		// （设置 → 账号的挡位/自定义值）。
		AccountWindowStore: overlay.StoreFile{Path: config.RuntimeLimitsPath(storePath)},
		// 工具记录计数开关的落盘口：同一个覆盖层文件的 tool_record_enabled 格。
		ToolRecordStore: overlay.StoreFile{Path: config.RuntimeLimitsPath(storePath)},
		// 系统提示词的**环境段**：装配期解析一次"这台机器上 bash 工具实际落到哪个壳"，
		// 写进提示词（模型写命令前唯一的方言事实；2026-10-15 审计 D4）。解析结果进程内
		// 缓存，因此前缀逐轮稳定。
		EnvironmentSection: toolreg.ResolveShellFact().PromptSection(),
	})
}
