package core

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/RedHuang-0622/seelex/backend/engine/task"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/middle/view/worktable"
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
	rows := worktable.BuildWorkTable(
		state.Snapshot.Runtime.Plan,
		tasks,
		state.Snapshot.Runtime.SubAgentTree,
		asyncRuns,
		workTableBounds(),
	)
	state.Snapshot.Runtime.WorkTable = rows
	state.Snapshot.Runtime.WorkTableBatches = worktable.BuildWorkTableBatches(rows)
}

// publishWorkTable 在锁外发布整表（CSP 汇聚发布器，latest-wins；items 必须
// 是同一临界区克隆的不可变快照，保证 revision 与内容一致）。
func (state *serviceState) publishWorkTable(revision uint64, requestID string, items []WorkItem, batches []WorkTableBatch) {
	if state.workTablePublisher == nil {
		return
	}
	state.workTablePublisher.Send(worktable.WorkTableUpdate{
		Revision: revision, RequestID: requestID, Items: items, Batches: batches,
	})
}

// workTableEventPayload 组装 worktable.changed 的 payload：表格 + 批次头 +
// 子代理树增量。
//
// 树只在内容变化时随包下发（nil = 不带、空数组 = 已清空）：详情入口在前端
// 要先把行解析成节点（Plan DSL / 子代理树投影），而树此前只由整份快照与
// runtime.changed 携带；同一批次还有子代理在跑时，表格行已通过本事件到达、
// 树却还没到，详情点开会静默失败（2026-09-13 回归）。比较基准是"上一次
// 实际发布出去的树"，因此被 CSP 汇聚合并掉的中间更新不会漏发。
//
// 只由 workTablePublisher 的发布 goroutine 调用：lastPublishedSubagentTreeSig
// 因此无需额外加锁（发布器是单 goroutine）。
func (service *Service) workTableEventPayload(update worktable.WorkTableUpdate) WorkTableEvent {
	service.ViewMu.RLock()
	tree := worktable.CloneSubAgentTreeForSync(service.Core.Snapshot.Runtime.SubAgentTree)
	service.ViewMu.RUnlock()
	payload := WorkTableEvent{Items: update.Items, Batches: update.Batches}
	signature := worktable.SubagentTreePayloadSignature(tree)
	if signature == service.lastPublishedSubagentTreeSig {
		return payload
	}
	service.lastPublishedSubagentTreeSig = signature
	if tree == nil {
		// 显式空数组：前端据此清空既有树（nil/缺省 = 本次不带，保留）。
		tree = []dto.SubAgentTreeNode{}
	}
	payload.SubAgentTree = tree
	return payload
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
	service.publishSessionEvent(EventTaskChanged, revision, requestID, sessionID, TaskChangedEvent{TaskID: item.ID, Task: item})
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
	tree := worktable.CloneSubAgentTreeForSync(service.Core.Snapshot.Runtime.SubAgentTree)
	// 活跃会话的 task scope 恒为实时注册表（""）；后台会话写自身分区。
	scope := sessionID
	if sessionID == "" || sessionID == viewSessionID {
		scope = ""
	}
	service.ViewMu.RUnlock()

	if plan != nil {
		var walk func(nodes []PlanNode, parentID string)
		walk = func(nodes []PlanNode, parentID string) {
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
func (service *Service) sessionActivePlanLocked(sessionID string) *PlanState {
	if sessionID == service.Core.Snapshot.Session.ID {
		return worktable.ClonePlanForSync(service.Core.Snapshot.Runtime.Plan)
	}
	return task_context.ActivePlanFromStack(
		service.components.tasks.PlanStackFor(sessionID),
		service.components.tasks.ActivePlanIDFor(sessionID),
	)
}

func (service *Service) syncPlanNodeTask(sessionID string, node PlanNode, parentID string) {
	key := "plan:" + node.ID
	status := worktable.TaskStatusForNode(node.Status)
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
	status := worktable.TaskStatusForSubagent(node.Status)
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
func (service *Service) workItemForRecord(record dto.TaskRecord, sessionID string) WorkItem {
	item := worktable.TaskRecordToWorkItem(record, workTableBounds())
	if record.Kind != "plan" {
		return item
	}
	service.ViewMu.RLock()
	plan := service.sessionActivePlanLocked(sessionID)
	service.ViewMu.RUnlock()
	item.Dependencies = worktable.MergeWorkDependencies(
		item.Dependencies,
		worktable.PlanDependencies(plan, worktable.PlanNodeIDFor(record)),
	)
	return item
}

// ── 工作打点表（上下文标记块）─────────────────────────────
// 动态任务状态不进 system prompt（避免前缀缓存失效），改由“打点表”承载：
// 请求尾部一段用标记语言锁住的只读块，随任务打点增量更新，任务终态后
// 删除（打点完即删），且不落历史 → 天然不参与上下文压缩。

const (
	workTableTraceMarkerOpen  = "<!-- seelex:worktable:v1 -->"
	workTableTraceMarkerClose = "<!-- /seelex:worktable:v1 -->"
	workTableTraceMaxLines    = 30
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
	effectiveSession := sessionID
	if effectiveSession == "" || effectiveSession == viewID {
		effectiveSession = viewID
	}
	// 会话作用域取数（唯一实现见 project_board.go 的 taskRecordsForSession：视图/活跃
	// 会话 = 实时注册表 "" 分区，后台会话 = 自身 scope 分区）。
	records := state.taskRecordsForSession(sessionID, viewID)
	active := make([]dto.TaskRecord, 0, len(records))
	for _, record := range records {
		if record.Status != dto.TaskCompleted && record.Status != dto.TaskFailed {
			active = append(active, record)
		}
	}
	sort.SliceStable(active, func(left, right int) bool {
		return active[left].ID < active[right].ID
	})
	lines := make([]string, 0, len(active)+1)
	for _, record := range active {
		retry := ""
		if record.RetryCount > 0 {
			retry = fmt.Sprintf(" retry=%d", record.RetryCount)
		}
		lines = append(lines, fmt.Sprintf("- %s %s%s %s",
			record.ID, record.Status, retry, worktable.TruncateWorkEvidence(record.Task, 80)))
	}
	asyncLines := worktable.AsyncTraceLines(state.Deps.Runtime.AsyncRunsSnapshot(), effectiveSession, workTableBounds())
	lines = append(lines, asyncLines...)
	// teammate 作业（jobs.Manager 那张表）单独成行：句柄空间与取回工具都不同
	// （jobs_manage vs bg_manage），合并成同一种行模型就会拿错工具。
	teamworkLines := worktable.TeamworkTraceLines(teamworkJobCompletions(state.Deps.Runtime), effectiveSession, workTableBounds())
	lines = append(lines, teamworkLines...)
	if len(lines) == 0 {
		return ""
	}

	var builder strings.Builder
	builder.WriteString(workTableTraceMarkerOpen + "\n# 工作打点表（系统维护，只读；任务状态与打点以工作表格为准）\n")
	// 预算必须把**整块**算进去（开/闭标记 + 标题 + 读法说明），否则"块 ≤ 30 行"这条
	// 上限会被固定开销吃掉几行（打点 K-2 的判据是整块行数）。
	budget := workTableTraceMaxLines - 3 // 开标记 + 标题 + 闭标记
	hint := ""
	if len(asyncLines) > 0 {
		hint = "async:<句柄> 行是作业（bash_bg 后台命令 / read_batch 扇出读 / subagent）：" +
			"字节数=已产出输出量；在途行带标题，完成行带摘要（exit/行数/末行采样）。" +
			"句柄自带来源（工具作业 bt-3 / 子代理作业 bs-7）。" +
			"取结果用 bg_manage(op=fetch, handle)、只读看进展用 op=observe、终止用 op=kill、" +
			"销项用 op=done。"
		budget--
	}
	if len(teamworkLines) > 0 {
		// teammate 行的读法**不是** bg_manage：那是另一张表（jobs.Manager）。
		hint += "teamwork:<句柄> 行是 teammate 作业（leader 派出去的活）：" +
			"句柄形如 bteam-7（团队作业表；拿错工具会被显式拒绝，不会静默命中别的作业）。" +
			"结论用 team_context（非消费读）、产物正文用 jobs_manage(op=observe|fetch, handle)，" +
			"复核完用 team_accept / team_fail 收口（验收通过即销掉那条作业行，不必自己销项）。"
		if len(asyncLines) == 0 {
			budget--
		}
	}
	truncated := false
	if len(lines) > budget {
		// 提示行也占一格：截断要让模型看见"还有更多"，而不是静默变短。
		lines = lines[:budget-1]
		truncated = true
	}
	for _, line := range lines {
		builder.WriteString(line + "\n")
	}
	if truncated {
		builder.WriteString("- …（打点表已达上限，详情见工作表格）\n")
	}
	if hint != "" {
		builder.WriteString(hint + "\n")
	}
	builder.WriteString(workTableTraceMarkerClose)
	return builder.String()
}

// RefreshWorkTableSnapshot 是子代理树生命周期变更的被动投影入口（由 CSP
// 消费者 consumeSubagentLifecycle 驱动）：fork 注册/完成自动同步 task 注册表
// 并发布增量，不依赖模型主观意愿调用任何工具。
func (service *Service) RefreshWorkTableSnapshot() {
	// 锁外取子代理树：Engine.SubAgentTree() 会对运行中子代理做 ExportSnapshot
	// （拿子代理会话锁），不能在持 service.ViewMu 时调用——避免与 runner 持会话
	// 锁 → 注册表 actor → 变更 channel 的路径成环死锁。
	tree := service.Deps.Engine.SubAgentTree()
	service.ViewMu.Lock()
	service.Core.Snapshot.Runtime.SubAgentTree = tree
	service.ViewMu.Unlock()
	service.refreshWorkTableFromSources()
}

// refreshWorkTableFromSources 是被动触发的统一入口：同步 plan/子代理树 →
// 注册表，再发布 worktable.changed + task.changed。
func (service *Service) refreshWorkTableFromSources() {
	service.syncTasksFromSources()
	service.publishTaskDeltas()
}

// ── CSP 生命周期消费者（取代同步回调 observer）────────────

// startLifecycleConsumers 启动四个消费者 goroutine：子代理树信号 → 刷新
// 工作表格；plan 节点事件 → 投影；task 变更 → 直发 task.changed；后台执行表变化 →
// 重投影（这一路同时驱动"后台作业终态触发对话"，见 consumeAsyncRuns 的注释）。数据经
// channel（CSP）流转，runtime 侧不再同步回调进 application。
func (service *Service) startLifecycleConsumers() {
	if service.Deps.Runtime == nil {
		return
	}
	if service.lifecycleStop == nil {
		service.lifecycleStop = make(chan struct{})
	}
	go service.consumeSubagentLifecycle()
	go service.consumePlanNodeEvents()
	go service.consumeTaskChanges()
	go service.consumeAsyncRuns()
}

func (service *Service) stopLifecycleConsumers() {
	if service.lifecycleStop != nil {
		service.lifecycleOnce.Do(func() { close(service.lifecycleStop) })
	}
}

func (service *Service) consumeSubagentLifecycle() {
	events := service.Deps.Runtime.SubagentTreeEvents()
	if events == nil {
		return
	}
	for {
		select {
		case <-events:
			service.safeLifecycleCall(service.RefreshWorkTableSnapshot)
		case <-service.lifecycleStop:
			return
		}
	}
}

func (service *Service) consumePlanNodeEvents() {
	events := service.Deps.Runtime.PlanNodeEventChannel()
	if events == nil {
		return
	}
	for {
		select {
		case event, ok := <-events:
			if !ok {
				return
			}
			service.safeLifecycleCall(func() { service.HandlePlanNodeComplete(event) })
		case <-service.lifecycleStop:
			return
		}
	}
}

func (service *Service) consumeTaskChanges() {
	changes := service.Deps.Runtime.TaskChangedChannel()
	if changes == nil {
		return
	}
	for {
		select {
		case record, ok := <-changes:
			if !ok {
				return
			}
			service.ViewMu.RLock()
			revision := service.Core.Snapshot.Revision
			requestID := service.Core.Snapshot.Chat.RequestID
			sessionID := service.Core.Snapshot.Session.ID
			service.ViewMu.RUnlock()
			service.safeLifecycleCall(func() { service.publishTaskChanged(record, revision, requestID, sessionID) })
		case <-service.lifecycleStop:
			return
		}
	}
}

// consumeAsyncRuns 是第四个生命周期消费者：后台执行表一有可见变化（派发、终态、
// 驱逐、以及去抖后的"有新字节"）就重投影工作表格。
//
// 为什么走 refreshWorkTableFromSources 而不是自己发一种新事件：后台行只是整表的
// 一路输入，复用同一条发布路径才能保证"表格里看到的"与"上下文打点块里看到的"
// 永远同源、同一个 revision。信号是容量 1 的汇聚口，被合并掉的中间态无所谓——
// 每次重投影读的都是登记表当下全量。
//
// 同一个信号口还驱动**第二件事**：后台作业终态为**空闲**会话起一个回合
// （triggerAsyncCompletions，见 async_completion.go）。
//
// 为什么并进这一个消费者而不是再起一个 goroutine 抢同一个通道：信号口是容量 1 的
// **单接收者**通道——两个消费者同时等它时，一次发送只会交给先等待的那一个（FIFO），
// 另一个永远收不到。现场表现就是"作业完成了，可什么都没发生"（本消费者先起，于是
// 后起的那个一次也收不到）。信号是"有事发生"，一个消费者做完整套读侧动作才与契约一致。
func (service *Service) consumeAsyncRuns() {
	events := service.Deps.Runtime.AsyncRunEvents()
	// teammate 作业表（jobs.Manager）的信号口是**另一条**（见 contract.TeamworkJobCompletion）：
	// 两张表各有各的上游，这里并进同一个消费者等它们——一个消费者做完整套读侧动作才与
	// 契约一致（两个 goroutine 抢同一个容量 1 的通道时，后起的那个一次也收不到）。
	//
	// **装配顺序是契约**：两个信号口都必须在 `wiring.New` 之前就绪（组合根的顺序见
	// wiring/bootstrap：SetTeamworkBackend 在 InitApplication 之前；测试基座同序见
	// tool_full_chain_test.go 的 fullChainBackendInstaller）。消费者只在这里读一次通道，
	// 后装配的宿主读到的必然是 nil——nil 在 select 里永不触发，"做完自动返回"会**静默
	// 不存在**（不报错、不打日志）。这里不改成每次循环重读：一个消费者做完整套读侧动作
	// 才与契约一致，靠重读掩盖顺序问题会把"装配写错了"变成"时好时坏"。
	teamworkEvents := teamworkJobEvents(service.Deps.Runtime)
	if events == nil && teamworkEvents == nil {
		return
	}
	triggered := map[string]struct{}{}
	for {
		select {
		case <-events:
			service.safeLifecycleCall(service.refreshWorkTableFromSources)
			// 开关关闭时连扫描都不做（默认关，见 limits.async_exec.trigger_conversation）。
			if Limits().AsyncExec.TriggerConversation {
				service.safeLifecycleCall(func() { service.triggerAsyncCompletions(triggered) })
			}
		case <-teamworkEvents:
			// teammate 作业终态**不改工作表格的行**（那两张表分开），只走"做完自动返回"：
			// 空闲会话起一个回合，忙会话等它自己的回合边界（打点块里的 teammate 完成行）。
			if Limits().AsyncExec.TriggerConversation {
				service.safeLifecycleCall(func() { service.triggerTeamworkJobCompletions(triggered) })
			}
		case <-service.lifecycleStop:
			return
		}
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
func (service *Service) UpdateWorkItemStatus(id, status string) error {
	kind, index, err := parseWorkItemID(id)
	if err != nil {
		return err
	}
	switch kind {
	case "todo":
		if err := service.Deps.Runtime.SetTodoStatus(index, dto.TodoItemStatus(strings.ToLower(strings.TrimSpace(status)))); err != nil {
			return err
		}
		service.refreshRuntimeAfterTodoChange()
		return nil
	default:
		return fmt.Errorf("work_item: %s 任务状态由执行器管理，不支持手动更新（仅支持 todo: 前缀）", kind)
	}
}

func parseWorkItemID(id string) (kind string, index int, err error) {
	parts := strings.SplitN(strings.TrimSpace(id), ":", 2)
	if len(parts) != 2 || parts[0] == "" {
		return "", 0, fmt.Errorf("work_item: 无效 ID %q（应为 todo:<index> 等）", id)
	}
	kind = parts[0]
	if kind == "todo" {
		index, err = strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil || index < 0 {
			return "", 0, fmt.Errorf("work_item: 无效 todo 索引 %q", parts[1])
		}
	}
	return kind, index, nil
}

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
