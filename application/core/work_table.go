package core

import (
	"context"
	applicationtable "github.com/RedHuang-0622/seelex/application/execution/table"
	"github.com/RedHuang-0622/seelex/application/execution/taskcontext"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	"strings"
)

// ── 工作表格（Work Table）投影 ──────────────────────────────
// task 就是 worktable 条目：单一 task 注册表（backend/tool/task/task.go，
// Actor + Mailbox，保护粒度=task）是权威状态源；plan/subagent 节点生命周期
// 被动同步进注册表（B5），todolist 融合为 kind=todo 的 task（B0），主动
// taskadd 直接入注册表（B1 幂等）。
//
// 事件（B2）：task 内部变更 → task.changed（逐任务增量，脏标记驱动）；
// worktable 结构 → worktable.changed（整表，CSP 汇聚 latest-wins）。
// retry（B3）：status=retry + RetryCount，前端展示 RETRY n。
//
// 落点（M5-R5b）：**投影判据**（行怎么排、状态怎么映射、证据怎么截断）已下沉
// `middle/view/worktable`；本文件只剩宿主侧的三件事——锁下取数、注册表同步、事件发布。

// refreshWorkTableLocked 在 service.ViewMu 持锁时重建工作表格投影。
//
// 两个输入都必须是**锁外采样**后按值传进来的：任务注册表快照与后台作业投影都要读
// 宿主，而后台作业投影会对每条记录做 stat + 读日志末窗（文件 I/O）。持进程级视图
// 写锁采样 = 把整块交互面押在一次慢活上（2026-09-29 锁面审计 §2.5，与 2026-09-29
// 压缩持 ViewMu 推帧的"后果②"同形）。
func (state *serviceState) refreshWorkTableLocked(tasks []dto.TaskRecord, asyncRuns []dto.AsyncRunRecord) {
	rows, batches := applicationtable.BuildRows(applicationtable.RowInputs{
		Plan:         state.Snapshot.Runtime.Plan,
		Tasks:        tasks,
		SubAgentTree: state.Snapshot.Runtime.SubAgentTree,
		AsyncRuns:    asyncRuns,
		Limits:       workTableBounds(),
	})
	state.Snapshot.Runtime.WorkTable = rows
	state.Snapshot.Runtime.WorkTableBatches = batches
}

// publishWorkTable 在锁外发布整表（CSP 汇聚发布器，latest-wins；items 必须
// 是同一临界区克隆的不可变快照，保证 revision 与内容一致）。
func (state *serviceState) publishWorkTable(revision uint64, requestID string, items []model.WorkItem, batches []model.WorkTableBatch) {
	if state.workTable == nil {
		return
	}
	state.workTable.Send(applicationtable.WorkTableUpdate{
		Revision: revision, RequestID: requestID, Items: items, Batches: batches,
	})
}

// publishTaskChanged 发布单 task 增量（task.changed；直发 hub，不汇聚——
// payload 小，逐任务保证不丢）。事件携带归属会话 sid（前端按会话过滤，
// 后台注册表变更不污染当前视图）。
func (service *Service) publishTaskChanged(record dto.TaskRecord, revision uint64, requestID, sessionID string) {
	if service.Events == nil {
		return
	}
	// 归属会话补齐（与整表路径同源）：增量只源自实时注册表（后台分区写不发
	// 增量，见 seelebridge TaskAddFor/TaskSetStatusFor），而实时注册表恒属于
	// 当前任务会话；注册表记录本身不带 SessionID，整表投影才给它标
	// currentTaskSessionID。不补齐时整表已带键的行会被这份无键增量整行替换
	// （前端 protocol.js 按 task_id 整行覆盖），前端"仅本会话"筛选随即丢掉
	// 正在运行的行（子代理任务每状态迁移都发增量，最容易撞上）。
	if record.SessionID == "" {
		record.SessionID = sessionID
	}
	// 与整表投影同源补齐（plan 行依赖取自 plan 邻接面）：否则 task.changed
	// 会把该行的依赖列擦成空。
	item := service.workItemForRecord(record, sessionID)
	service.publishSessionEvent(EventTaskChanged, revision, requestID, sessionID, model.TaskChangedEvent{TaskID: item.ID, Task: item})
}

// publishTaskDeltas 拉取注册表快照，锁内重建 worktable，发布
// worktable.changed（结构安全网）；task.changed 由 CSP 消费者直发。
func (service *Service) publishTaskDeltas() {
	tasks := service.Deps.Runtime.TaskSnapshot()
	asyncRuns := service.asyncRunsForTable()
	service.ViewMu.Lock()
	service.refreshWorkTableLocked(tasks, asyncRuns)
	revision := service.bumpLocked()
	requestID := service.Core.Snapshot.Chat.RequestID
	items := CloneWorkItems(service.Core.Snapshot.Runtime.WorkTable)
	batches := CloneWorkTableBatches(service.Core.Snapshot.Runtime.WorkTableBatches)
	service.ViewMu.Unlock()
	service.publishWorkTable(revision, requestID, items, batches)
}

// syncTasksFromSources 同步当前活跃会话的 plan/子代理树到其自身 task scope。
func (service *Service) syncTasksFromSources() {
	service.ViewMu.RLock()
	sid := service.Core.Snapshot.Session.ID
	service.ViewMu.RUnlock()
	service.syncTasksFromSourcesFor(sid)
}

// syncTasksFromSourcesFor 把指定会话的 plan 节点与子代理树生命周期投影进该
// 会话自身的 task scope（R6/P2 收口：写自有域，后台会话不再污染当前注册表）。
//
// 子代理行按**归属会话**收窄（2026-09-29）：子代理树是进程级的一张（多会话的
// fork 共处一树，见 backend/multiagent/subagent/subagent_tree.go），而工作表格行是会话
// 粒度的——前端按 `row.session_id` 做「仅本会话」与「实发」筛选，并把空归属
// 兜底成"本会话"。整棵树下投影的后果是：行落在谁的表格里取决于**谁触发了这次
// 同步**（视图切走后为后台会话同步，就把视图会话的子代理写进后台会话的分区，
// 还把后台自己的行贴上视图会话号），于是每个会话都能看到别的会话的子代理。因此
// 只投影 MainSessionID == 本会话的节点；未标注归属（旧记录/无 ctx 会话的注册
// 路径）不猜——宁可少一行，也不把别人的行摊给每个会话。
func (service *Service) syncTasksFromSourcesFor(sessionID string) {
	service.ViewMu.RLock()
	viewSessionID := service.Core.Snapshot.Session.ID
	// 空入参 = 当前视图会话（与 scope 的空值语义同源）；归属判据不接受空键。
	ownerSessionID := sessionID
	if ownerSessionID == "" {
		ownerSessionID = viewSessionID
	}
	plan := service.sessionActivePlanLocked(sessionID)
	tree := applicationtable.CloneSubAgentTreeForSync(service.Core.Snapshot.Runtime.SubAgentTree)
	// 活跃会话的 task scope 恒为实时注册表（""）；后台会话写自身分区。
	scope := sessionID
	if sessionID == "" || sessionID == viewSessionID {
		scope = ""
	}
	service.ViewMu.RUnlock()

	if plan != nil {
		var walk func(nodes []model.PlanNode, parentID string)
		walk = func(nodes []model.PlanNode, parentID string) {
			for _, node := range nodes {
				service.syncPlanNodeTask(scope, node, parentID)
				walk(node.Children, "plan:"+node.ID)
			}
		}
		walk(plan.Nodes, "")
	}
	var walkTree func(items []dto.SubAgentTreeNode, parentID string)
	walkTree = func(items []dto.SubAgentTreeNode, parentID string) {
		for _, node := range items {
			if node.ID == "" || node.ID == "main" {
				walkTree(node.Children, parentID)
				continue
			}
			if strings.TrimSpace(node.MainSessionID) != ownerSessionID {
				continue
			}
			service.syncSubagentTask(scope, node, parentID)
			walkTree(node.Children, node.ID)
		}
	}
	walkTree(tree, "")
}

// sessionActivePlanLocked 返回指定会话当前 plan 投影：活跃会话读 Snapshot 镜像，
// 后台会话读会话自身的 plan 栈（task_context 每会话持有）。
func (service *Service) sessionActivePlanLocked(sessionID string) *model.PlanState {
	if sessionID == service.Core.Snapshot.Session.ID {
		return applicationtable.ClonePlanForSync(service.Core.Snapshot.Runtime.Plan)
	}
	return task_context.ActivePlanFromStack(
		service.components.tasks.PlanStackFor(sessionID),
		service.components.tasks.ActivePlanIDFor(sessionID),
	)
}

func (service *Service) syncPlanNodeTask(sessionID string, node model.PlanNode, parentID string) {
	key := "plan:" + node.ID
	status := applicationtable.TaskStatusForNode(node.Status)
	identity := dto.ActorIdentity("main", service.Deps.Engine.SessionID())
	existing, found, err := service.Deps.Runtime.ResolveTaskByKeyFor(sessionID, key)
	if err != nil {
		return
	}
	if !found {
		spec := dto.TaskSpec{
			ID: key, Key: key, Phase: dto.TaskPhasePlan, Task: node.Label, Kind: "plan",
			SourceID: node.ID, Assignee: identity,
		}
		if parentID != "" {
			spec.Dependencies = []string{parentID}
		}
		_, _, _ = service.Deps.Runtime.TaskAddFor(sessionID, spec)
		return
	}
	if existing.Status != status {
		_, _ = service.Deps.Runtime.TaskSetStatusFor(sessionID, existing.ID, status, "node:"+node.Status.String())
	}
	// 被动认领：旧数据/恢复会话无 Assignee 时补主身份并上名单。
	if existing.Assignee == "" && identity != "" {
		_, _ = service.Deps.Runtime.TaskAttachParticipant(existing.ID, identity)
	}
}

func (service *Service) syncSubagentTask(sessionID string, node dto.SubAgentTreeNode, parentID string) {
	key := "subagent:" + node.ID
	status := applicationtable.TaskStatusForSubagent(node.Status)
	identity := dto.ActorIdentity("subagent", node.SessionID)
	existing, found, err := service.Deps.Runtime.ResolveTaskByKeyFor(sessionID, key)
	if err != nil {
		return
	}
	if !found {
		spec := dto.TaskSpec{
			ID: key, Key: key, Phase: dto.TaskPhaseSubagent, Task: node.Goal, Kind: "subagent",
			SourceID: node.ID,
		}
		if parentID != "" {
			spec.Dependencies = []string{"subagent:" + parentID}
		}
		created, _, _ := service.Deps.Runtime.TaskAddFor(sessionID, spec)
		_, _ = service.Deps.Runtime.TaskSetStatusFor(sessionID, created.ID, status, "subagent:"+node.Status.String())
		// 会话已注册 → 子代理认领（Assignee 变更为 subagent:<sessionID> 并上名单）。
		if identity != "" {
			_, _ = service.Deps.Runtime.TaskAttachParticipant(created.ID, identity)
		}
		return
	}
	if existing.Status != status {
		_, _ = service.Deps.Runtime.TaskSetStatusFor(sessionID, existing.ID, status, "subagent:"+node.Status.String())
	}
	// 被动认领：确保当前子代理身份在名单上并成为 Assignee。
	if identity != "" {
		_, _ = service.Deps.Runtime.TaskAttachParticipant(existing.ID, identity)
	}
}

// workItemForRecord 把注册表记录映射为工作表格行，并按 plan 邻接面补齐
// plan 行的依赖。
//
// task.changed 增量与 worktable.changed 整表必须同源：增量只带单条记录
// （没有 plan 上下文），若不补齐，前端按增量更新该行时会把依赖列擦成空。
func (service *Service) workItemForRecord(record dto.TaskRecord, sessionID string) model.WorkItem {
	service.ViewMu.RLock()
	plan := service.sessionActivePlanLocked(sessionID)
	service.ViewMu.RUnlock()
	return applicationtable.WorkItemForRecord(record, plan, workTableBounds())
}

// ── 工作打点表（上下文标记块）─────────────────────────────
// 动态任务状态不进 system prompt（避免前缀缓存失效），改由“打点表”承载：
// 请求尾部一段用标记语言锁住的只读块，随任务打点增量更新，任务终态后
// 删除（打点完即删），且不落历史 → 天然不参与上下文压缩。

const (
	workTableTraceMarkerOpen  = applicationtable.TraceMarkerOpen
	workTableTraceMarkerClose = applicationtable.TraceMarkerClose
)

// workTableTraceBlock 返回当前活跃会话的打点表标记块（活跃会话即
// TaskSnapshotFor("") 的实时注册表语义；供旧调用面与进程级事件兼容，
// 多会话路径一律走 workTableTraceBlockFor）。
func (state *serviceState) workTableTraceBlock() string {
	return state.workTableTraceBlockFor("")
}

// workTableTraceBlockFor 返回指定会话的打点表标记块：只含该会话 scope 中
// 未终态任务（pending/running/doing/retry）**与在跑的后台命令**，按稳定顺序；
// 两者都没有时返回空串（块随任务完成/命令收尾自动消失）。
//
// 会话作用域（S1：打点表按会话取数）：打点表**不是**工作表格本体——工作
// 表格是全局台账（TaskSnapshot），而打点表注入在“正在组装下一次请求的会话”
// 的上下文尾部，必须只取该会话自己的 scope，否则会话会读到别的会话的
// 活动任务（历史污染/上下文串台）且看不到自己的打点：
//   - 活跃（视图）会话 = TaskSnapshotFor("")，即实时注册表（runtime.
//     SwitchSessionTasks 在 BeginNewSession/Resume/Activate 时与视图对齐，
//     live registry 恒属于当前视图会话）；
//   - 后台会话（视图已切走、它仍在并行跑） = TaskSnapshotFor(sessionID) 的
//     scope 分区。
//
// 后台执行不走那套分区：登记表里每条都自带归属会话键，按 effectiveSession 过滤
// 即可（"" 表示视图会话，用快照里的会话 ID 顶上）。
func (state *serviceState) workTableTraceBlockFor(sessionID string) string {
	if state == nil || state.Deps.Runtime == nil {
		return ""
	}
	state.ViewMu.RLock()
	viewID := state.Snapshot.Session.ID
	state.ViewMu.RUnlock()
	// 会话作用域取数（唯一实现见 project_board.go 的 taskRecordsForSession：视图/活跃
	// 会话 = 实时注册表 "" 分区，后台会话 = 自身 scope 分区）。
	records := state.taskRecordsForSession(sessionID, viewID)
	return applicationtable.BuildTraceBlock(applicationtable.TraceInputs{
		SessionID:     sessionID,
		ViewSessionID: viewID,
		Tasks:         records,
		AsyncRuns:     state.Deps.Runtime.AsyncRunsSnapshot(),
		TeamworkRuns:  teamworkJobCompletions(state.Deps.Runtime),
		Limits:        workTableBounds(),
	})
}

// refreshWorkTableSnapshot 是子代理树生命周期变更的被动投影入口（由 CSP
// 消费者 consumeSubagentLifecycle 驱动）：fork 注册/完成自动同步 task 注册表
// 并发布增量，不依赖模型主观意愿调用任何工具。
func (service *Service) refreshWorkTableSnapshot() {
	// 锁外取子代理树：Engine.SubAgentTree() 会对运行中子代理做 ExportSnapshot
	// （拿子代理会话锁），不能在持 service.ViewMu 时调用——避免与 runner 持会话
	// 锁 → 注册表 actor → 变更 channel 的路径成环死锁。
	tree := service.Deps.Engine.SubAgentTree()
	service.ViewMu.Lock()
	service.Core.Snapshot.Runtime.SubAgentTree = tree
	service.ViewMu.Unlock()
	// 子代理节点收口：进入终态的节点把它的**工具记录暂存**一次性交给主会话累计
	// （阶梯提交的收口点之一；归属取节点自己的 MainSessionID，与视图无关）。
	service.closeTerminalSubagentToolRecords(tree)
	service.refreshWorkTableFromSources()
}

// refreshWorkTableFromSources 是被动触发的统一入口：同步 plan/子代理树 →
// 注册表，再发布 worktable.changed + task.changed。
func (service *Service) refreshWorkTableFromSources() {
	service.syncTasksFromSources()
	service.publishTaskDeltas()
}

// ── CSP 生命周期消费者（取代同步回调 observer）────────────

// startLifecycleConsumers wires the extracted WorkTable lifecycle consumers.
func (service *Service) startLifecycleConsumers() {
	if service.Deps.Runtime == nil {
		return
	}
	if service.lifecycleStop == nil {
		service.lifecycleStop = make(chan struct{})
	}
	runtime := service.Deps.Runtime
	applicationtable.NewLifecycle(applicationtable.LifecycleDeps{
		SubagentEvents:   runtime.SubagentTreeEvents(),
		TaskChanges:      runtime.TaskChangedChannel(),
		Stop:             service.lifecycleStop,
		RefreshSubagents: service.refreshWorkTableSnapshot,
		TaskContext: func() (uint64, string, string) {
			service.ViewMu.RLock()
			defer service.ViewMu.RUnlock()
			return service.Core.Snapshot.Revision, service.Core.Snapshot.Chat.RequestID, service.Core.Snapshot.Session.ID
		},
		PublishTaskChange: service.publishTaskChanged,
		SafeCall:          service.safeLifecycleCall,
		Async: applicationtable.AsyncConsumerDeps{
			AsyncEvents:    runtime.AsyncRunEvents(),
			TeamworkEvents: teamworkJobEvents(runtime),
			Refresh:        service.refreshWorkTableFromSources,
			TriggerAsync: func(triggered map[string]struct{}) int {
				if !Limits().AsyncExec.TriggerConversation {
					return 0
				}
				return service.triggerAsyncCompletions(triggered)
			},
			TriggerTeamwork: func(triggered map[string]struct{}) int {
				if !Limits().AsyncExec.TriggerConversation {
					return 0
				}
				return service.triggerTeamworkJobCompletions(triggered)
			},
		},
	}).Start()
}

func (service *Service) stopLifecycleConsumers() {
	if service.lifecycleStop != nil {
		service.lifecycleOnce.Do(func() { close(service.lifecycleStop) })
	}
}

// safeLifecycleCall 处理消费者中的 panic：数据竞争/逻辑故障不得静默吞掉
// 继续运行（可能持续污染其它会话）——先降级退出，再重新抛出 panic 让宿主
// 及时退出（fail-fast）。死锁类问题由带超时的探针用例负责超时即失败。
func (service *Service) safeLifecycleCall(call func()) {
	defer func() {
		if recovered := recover(); recovered != nil {
			service.handleLifecycleFault(recovered)
		}
	}()
	call()
}

// UpdateWorkItemStatus 是工作表格的人工状态更新入口（v1：todo 三态
// pending/doing/done）。plan / subagent 状态由执行器权威管理，手动更新
// 返回明确错误；成功路径发布 runtime.changed（todo 快照）与 task/worktable
// 增量。

// refreshRuntimeAfterTodoChange 在 todo 状态变更后重投影并发布三类增量：
// runtime.changed（既有面板/其他消费者）、worktable.changed 与 task.changed。
func (service *Service) refreshRuntimeAfterTodoChange() {
	projection := service.collectRuntimeProjection(context.Background())
	service.ViewMu.Lock()
	service.applyRuntimeProjectionLocked(projection)
	revision := service.bumpLocked()
	requestID := service.Core.Snapshot.Chat.RequestID
	sessionID := service.Core.Snapshot.Session.ID
	items := CloneWorkItems(service.Core.Snapshot.Runtime.WorkTable)
	batches := CloneWorkTableBatches(service.Core.Snapshot.Runtime.WorkTableBatches)
	service.ViewMu.Unlock()
	service.publishSessionEvent(EventRuntimeChanged, revision, requestID, sessionID, service.Snapshot().Runtime)
	service.publishWorkTable(revision, requestID, items, batches)
}

// asyncRunsForTable 读后台执行投影。三处建表入口（实时重投影、会话快照、冷读面）
// 都走它，保证同一张表不管从哪个读面看都有同一批后台行。
//
// 为什么留在宿主（M5-R5b）：这是**取数**（Runtime 端口 + 登记表锁），行怎么投影已在
// `middle/view/worktable`；登记表锁是叶子锁（tools 侧从不回调进 application），
// 所以持 ViewMu 时调用不会成环。
func (state *serviceState) asyncRunsForTable() []dto.AsyncRunRecord {
	if state == nil || state.Deps.Runtime == nil {
		return nil
	}
	return state.Deps.Runtime.AsyncRunsSnapshot()
}
