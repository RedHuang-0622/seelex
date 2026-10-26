package core

import (
	"errors"
	"fmt"
	"strings"

	"github.com/RedHuang-0622/seelex/application/session/runtime"
	"github.com/RedHuang-0622/seelex/application/session/switchpolicy"
	"github.com/RedHuang-0622/seelex/contract/protocol"
)

// ForkSession 从父会话的指定切断点创建独立子会话，并切换到子会话继续。
// 一期决策契约：深拷贝（含 tool-results 物理复制）+ 血缘 meta（子会话侧
// forked_from 事实源）；切断点锚定 EventSeq + 段落边界；运行中拒绝 fork；
// 跨项目 fork 一期禁止（子会话恒落在父会话所在项目）。
//
// 口径声明：这是 **ForkSession（会话分叉）**——产出独立项目级会话，继承规则
// 与可达集合裁剪见 `session_runtime/fork.go` 头部（那条路只服务本入口）。
// **ForkSubagent（`fork_subagents` 子代理派发）不是同一条路**：它经 workplan
// DAG 建子代理现场，存储侧落 `subagent_<hash>/` 子树、不建独立 blob 目录、
// 大工具输出引用主会话 `big_tool_result`（见 `store/fork_store.go` 头部、
// my_design §8.1/§8.2），派发链不经过 `PrepareFork`，因此没有这条按 ref 裁剪。
// forkSessionLocked 在持有会话切换锁时执行 fork 落盘：解析切断点 → 构建
// 截断后的子会话快照 → 写入子会话键 → 绑定项目并切换写作用域。
// 只检查父会话自身是否运行中（并行会话的其它会话执行不影响 fork）。
func (service *Service) forkSessionLocked(parentID string, request model.ForkRequest) (string, error) {
	service.ViewMu.RLock()
	closed := service.closed
	draining := service.draining
	running := false
	if unit := service.sessions.Unit(parentID); unit != nil {
		running = unit.ChatState().Running
	}
	service.ViewMu.RUnlock()
	gate := switching.ForkGate{
		Closed: closed, Draining: draining, Running: running,
		ErrClosed:   errors.New("application is shut down"),
		ErrDraining: ErrApplicationDraining,
		ErrRunning:  ErrChatRunning,
	}
	if err := gate.Reject(); err != nil {
		return "", err
	}
	forkPort, ok := service.Deps.Sessions.(session_runtime.SessionForkPort)
	if !ok {
		return "", errors.New("session fork: durable storage is unavailable")
	}
	location := service.components.sessions.LocateSession(parentID)
	var childID string
	if service.perSessionExecution() {
		childID = service.newGeneratedSessionID("fork")
		if activator, ok := service.Deps.Engine.(interface{ ActivateSession(string) error }); ok {
			if err := activator.ActivateSession(childID); err != nil {
				return "", fmt.Errorf("activate forked session %q: %w", childID, err)
			}
		}
	} else {
		childID = strings.TrimSpace(service.Deps.Engine.StartSession())
		if childID == "" {
			return "", errors.New("engine returned an empty session ID")
		}
	}
	forkContext, err := service.components.sessions.PrepareFork(location, childID, parentID, request)
	if err != nil {
		return "", err
	}
	// 会话粒度深拷贝（thin-wrapper §5）：子会话 record 前缀 + 上下文栈
	// 引用与父不相交（T2.7/B4）；改子不影响父。
	forkContext.Record = switching.DeepCopyRecord(forkContext.Record, session_runtime.CloneSessionPlanStack)
	forkContext.Events = append([]model.TranscriptEvent(nil), forkContext.Events...)
	forkContext.ToolResults = append([]model.StoredToolResult(nil), forkContext.ToolResults...)
	// 子会话内存 transcript 的序号基线对齐到继承事件的最大 seq：磁盘快照
	// 带父会话的序号区间，而子会话内存可能没有任何基线（子引擎冷启动为空、
	// 无可导入的引擎历史），此时子会话自己的事件会从 seq 1 起算并与继承
	// 区间重叠——落盘合并按 seq 覆盖，子会话自己的消息会被旧行顶掉
	// （2026-09-11 TC-A2-01 回归：fork 后 C 的第一轮聊天丢失）。
	service.components.tasks.SeedTranscriptSeqFor(childID, switching.MaxTranscriptEventSeq(forkContext.Events))
	// ProviderHistory 是可重建缓存：子会话冷恢复由事件流/record 重建，
	// 不继承父的 provider 缓存（避免消息↔事件坐标映射的不确定性）。
	// ToolResults 为父通道全量物理复制（含 compressed:<segment_id> 原文）。
	// A3 单写者：fork 首次落盘子会话是第 8 个写点——它不走
	// PersistCurrentSession（子会话此时还没有可读 record，无需 record 重建），
	// 但仍必须与其它写点共用同一把会话落盘单写者：否则 fork 快照写会与该子会话
	// 首轮落盘交错（后写的 record/事件覆盖先写的）。
	if err := service.components.sessions.RunSessionWrite(childID, func() error {
		return forkPort.SaveSessionSnapshotWorkspace(location.WorkspaceID, childID, nil, forkContext.Record, forkContext.Events, forkContext.ToolResults)
	}); err != nil {
		return "", fmt.Errorf("write forked session snapshot: %w", err)
	}
	if err := forkPort.SaveContextStateWorkspace(location.WorkspaceID, childID, forkContext.Context); err != nil {
		return "", fmt.Errorf("write forked session context: %w", err)
	}
	// 注册子会话：绑定项目 + 切换写作用域（resumeSession 依赖 binding 定位
	// 目标会话，不能等目录刷新）。
	if service.Deps.Workspace != nil && location.Workspace != nil {
		service.Deps.Workspace.BindSession(childID, location.WorkspaceID)
	}
	service.setWorkspaceWriteScope(location.WorkspaceID)
	// F-4：fork 子引擎只是“占位登记”（逐会话宿主显式建 bundle，legacy
	// 宿主经 StartSession 分配 ID），登记完成后一律卸载，让后续 resume
	// 走 cold_load 从 fork 快照装载正文。legacy 宿主若不卸载，resume 会
	// 把空引擎判为已加载 → 热挂载空视图（子会话 record 有正文但
	// SnapshotOf 恒 total=0）；不支持 UnloadSession 的引擎跳过，行为与
	// 修复前一致（其 resume 本就无热挂载语义）。
	if unloader, ok := service.Deps.Engine.(interface{ UnloadSession(string) error }); ok {
		_ = unloader.UnloadSession(childID)
	}
	service.components.sessions.RequestCatalogRefresh()
	return childID, nil
}
