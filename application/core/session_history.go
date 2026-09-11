package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/RedHuang-0622/seelex/application/core/chat"
	"github.com/RedHuang-0622/seelex/application/core/context_runtime"
	"github.com/RedHuang-0622/seelex/application/core/session_runtime"
	"github.com/RedHuang-0622/seelex/application/core/task_context"
	"github.com/RedHuang-0622/seelex/application/core/view_state"
	"github.com/RedHuang-0622/seelex/session"
)

// persistedPlanRestorer 是 Runtime 的可选能力：resume 时按 plan 参数恢复
// 可执行 Plan（不可用时保留可见投影）。
type persistedPlanRestorer interface {
	RestorePlan(context.Context, string) error
}

// resumeSession 是会话切换的应用边界：目标已驻留（含运行中）热加载；目标
// 未驻留且无会话运行中时同步冷加载；目标未驻留且有会话运行中时改为异步
// 冷加载——先给目标会话空壳 + 权威 restoring 状态，后台完成装载后再发布
// 内容基线，消除同步磁盘 I/O 在视图过渡 key 上的串行等待（“恢复中”不再
// 等于前端 RPC 长期不返回）。
func (service *Service) resumeSession(sessionID string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return errors.New("session ID is required")
	}

	transition := service.transitionForSession(sessionID)
	transition.Lock()
	defer transition.Unlock()

	service.ViewMu.RLock()
	hot := service.sessionLoaded(sessionID)
	running := service.anyChatRunningLocked()
	restoring := service.isRestoringLocked(sessionID)
	previous := service.Core.Snapshot.Session.ID
	service.ViewMu.RUnlock()

	if restoring {
		// 同目标已有后台冷加载在途：空壳已激活，重复请求幂等（不重复装载、
		// 不推进 epoch——自身装载不能被自己打断）。
		return nil
	}
	if hot {
		// 阶段 2：目标会话已驻留（含运行中）→ 热加载，只换视图指针 +
		// 投影会话 scope，不重建历史、不触碰 X/M/R（不变量 Ⅱ）。推进 epoch，
		// 让在途后台冷加载不再抢占视图。
		service.bumpViewEpoch()
		return service.hotAttachSession(sessionID)
	}
	if !running {
		// 空闲：保持同步冷加载（无运行中会话共享全局根/写作用域，串行装载
		// 更简单；activateEpoch=0 表示无条件激活）。
		service.bumpViewEpoch()
		if err := service.resumeSessionCold(sessionID, 0); err != nil {
			// 同步冷加载失败也保持“ResumeSession 返回错误 ⇒ 视图停留在切换
			// 前会话”的不变量（与后台冷加载失败路径 handleColdRestoreFailure
			// 同一语义）：resumeSessionCold 的迟到失败（如 context 挂接）发生
			// 在视图激活之后，不回滚会让前端误以为还在 previous 而把后续输入
			// 路由进一个用户看不到的会话（切换失败后“输入发不出去/发错会话”）。
			service.rollbackSyncResumeFailure(sessionID, previous)
			return err
		}
		return nil
	}
	// 运行中 + 目标未驻留：异步冷加载。先激活 restoring 空壳并立即返回，
	// 由后台 goroutine 完成装载；期间其它切换仍可快速进行（冷加载不占视图
	// 过渡 key），迟到完成由 epoch 判定不再抢占。
	epoch, err := service.beginAsyncRestore(sessionID)
	if err != nil {
		return err
	}
	go service.resumeSessionColdInBackground(sessionID, previous, epoch)
	return nil
}

// rollbackSyncResumeFailure 在同步冷加载失败后恢复视图一致性（调用方持视图
// 过渡锁；仅空闲分支可达，无运行中会话）。目标是保证：ResumeSession 返回
// 错误时视图仍停留在切换前会话，前端后续输入路由到用户看到的那一个会话。
//
//   - 视图已被目标激活（迟到失败）：目标驻留则热挂载回退，否则重置到草稿空壳
//     （与 handleColdRestoreFailure 一致）；
//   - 视图尚未切到目标（早失败）：若 previous 驻留则对其热挂载一次，把可能已
//     被目标 workspace 占用的全局项目根/写作用域切回 previous（无运行中会话，
//     安全）。
func (service *Service) rollbackSyncResumeFailure(sessionID, previousID string) {
	service.ViewMu.RLock()
	stillOnTarget := service.Core.Snapshot.Session.ID == sessionID
	service.ViewMu.RUnlock()
	if stillOnTarget {
		if previousID != "" && service.sessionLoaded(previousID) {
			if err := service.hotAttachSession(previousID); err != nil {
				runChatDebug("rollback sync resume to %q after %q failure: %v", previousID, sessionID, err)
				service.resetViewToDraftAfterRestoreFailure()
			}
			return
		}
		service.resetViewToDraftAfterRestoreFailure()
		return
	}
	// 早失败：视图本来就在 previous（或其草稿）。previous 驻留时热挂载一次，
	// 修正可能被目标 workspace 占用的全局根；未驻留（草稿）则无需动作。
	if previousID != "" && service.sessionLoaded(previousID) {
		_ = service.hotAttachSession(previousID)
	}
}

// bumpViewEpoch 推进视图切换序号（任何新的视图激活都推进；后台冷加载完成
// 时只有仍为最新 epoch 才允许发布基线）。
func (service *Service) bumpViewEpoch() {
	service.ViewMu.Lock()
	service.nextViewEpochLocked()
	service.ViewMu.Unlock()
}

// beginAsyncRestore 激活目标会话的 restoring 空壳：视图指针立即切到目标，
// 会话树/当前快照呈现 SessionStatusRestoring，内容由后台 resumeSessionCold
// 完成后发布。调用方已持有视图过渡锁；内部自取 Core.ViewMu。
func (service *Service) beginAsyncRestore(sessionID string) (uint64, error) {
	service.ViewMu.Lock()
	if service.closed {
		service.ViewMu.Unlock()
		return 0, errors.New("application is shut down")
	}
	epoch := service.nextViewEpochLocked()
	service.setRestoringLocked(sessionID)
	name := service.components.sessions.SessionTitleFor(sessionID).Value
	unit := service.sessionUnitLocked(sessionID)
	unit.SetChatState(ChatState{}, nil)
	unit.SetCancel(nil)
	unit.SetRequests(nil)
	// 空壳：清空该会话单元的可见区（新建单元默认空，此处幂等），避免把
	// 上一视图会话 A 的内容留在 B 的投影里。
	service.components.view.SessionViewMutateLocked(sessionID, func(view *session.View) {
		view.Conversation = nil
		view.Chat = ChatState{}
		view.ReadFiles = nil
		view.TotalMessages = 0
		view.HistoryOffset = 0
		view.HasMoreHistory = false
		view.ConversationWindow = 0
	})
	service.Core.Snapshot.Session = SessionState{ID: sessionID, Name: name, Status: SessionStatusRestoring}
	service.sessions.SetActive(sessionID)
	service.Core.Snapshot.Conversation = nil
	service.Core.Snapshot.Task = nil
	service.Core.Snapshot.Runtime.Plan = nil
	service.Core.Snapshot.Runtime.TodoItems = nil
	service.Core.Snapshot.Runtime.SubAgentTree = nil
	service.Core.Snapshot.Runtime.WorkTable = nil
	service.Core.Snapshot.Runtime.WorkTableBatches = nil
	service.Core.Snapshot.Interaction = nil
	service.setSessionChatLockedFor(sessionID, unit.ChatState())
	service.mirrorActiveViewLocked()
	revision := service.bumpLocked()
	service.ViewMu.Unlock()
	service.publishSessionEvent(EventSnapshotChanged, revision, "", sessionID, nil)
	service.components.sessions.RequestCatalogRefresh()
	return epoch, nil
}

// resumeSessionColdInBackground 后台执行冷加载：完成/失败后按 epoch 判定
// 是否仍可发布视图基线；被更新的切换取代时只完成目标会话自身装载，不抢占
// 视图。
func (service *Service) resumeSessionColdInBackground(sessionID, previousID string, epoch uint64) {
	if err := service.resumeSessionCold(sessionID, epoch); err != nil {
		service.handleColdRestoreFailure(sessionID, previousID, epoch, err)
	}
}

// handleColdRestoreFailure 后台冷加载失败的降级：用户若仍停留在失败的恢复
// 会话上，尽力回退到切换前的会话（运行中源会话通常驻留，hot_attach 不触碰
// 其引擎锁）；已被更新的切换接管时静默结束。
func (service *Service) handleColdRestoreFailure(sessionID, previousID string, epoch uint64, cause error) {
	service.ViewMu.Lock()
	service.clearRestoringLocked(sessionID)
	stillActive := service.Core.Snapshot.Session.ID == sessionID
	latest := service.viewEpoch == epoch
	service.ViewMu.Unlock()
	if !stillActive || !latest {
		return
	}
	if previousID != "" {
		if err := service.hotAttachSession(previousID); err == nil {
			// 视图已异步回退到切换前会话：Bridge 的订阅可能仍钉在失败的
			// 目标上，用进程级事件通知它对齐（见 publishViewSessionChanged）。
			service.publishViewSessionChanged()
			return
		}
	}
	service.resetViewToDraftAfterRestoreFailure()
	service.publishViewSessionChanged()
}

// resetViewToDraftAfterRestoreFailure 在“无前一会话可回退”时把视图重置到
// 新建草稿空壳（不做任何持久化，避免用空壳覆盖目标会话记录）。
func (service *Service) resetViewToDraftAfterRestoreFailure() {
	service.ViewMu.Lock()
	slot := service.draft
	if slot == nil {
		slot = &draftSlot{ID: service.newDraftSessionIDLocked(), CreatedAt: time.Now()}
		service.draft = slot
	}
	slot.UpdatedAt = time.Now()
	draftID := slot.ID
	service.nextViewEpochLocked()
	service.Core.Snapshot.Session = SessionState{ID: draftID, Name: draftSessionName, Draft: true, Status: SessionStatusDraft}
	service.sessions.SetActive(draftID)
	service.Core.Snapshot.Conversation = nil
	service.Core.Snapshot.Task = nil
	service.Core.Snapshot.Runtime.Plan = nil
	service.Core.Snapshot.Interaction = nil
	runtime := service.sessionUnitLocked(draftID)
	runtime.SetChatState(ChatState{}, nil)
	runtime.SetCancel(nil)
	runtime.SetRequests(nil)
	service.Core.Snapshot.Chat = runtime.ChatState()
	service.components.sessions.SetSessionTitleLocked(draftID, SessionTitle{})
	service.components.tasks.ResetForNewSessionLocked()
	revision := service.bumpLocked()
	service.ViewMu.Unlock()
	service.publishRuntimeProjections()
	service.publishSessionEvent(EventSnapshotChanged, revision, "", draftID, nil)
}

// resumeSessionCold 是 resumeSession 的冷加载主体：目标未驻留时重建引擎、
// 恢复 workspace 绑定与可见会话，然后发布一致快照。
//
// activateEpoch=0 表示同步装载（调用方已持视图过渡锁，无条件激活）；
// activateEpoch>0 表示后台装载：仅当装载完成时仍是最新切换目标才发布基线，
// 否则只完成目标会话自身状态装载（引擎驻留、可见投影、任务槽），不抢占
// 当前视图。
func (service *Service) resumeSessionCold(sessionID string, activateEpoch uint64) error {
	location := service.components.sessions.LocateSession(sessionID)
	// 会话恢复三读（record/history/transcript）相互独立，并行加载：
	// 大会话（数 MB）下全量解析总耗时从串行求和变为三路取最大值。
	// history 路径为尾部窗口读：先 (0,0) 取总数（只读 manifest，不解析 shard），
	// 再 (total-window, window) 只解析覆盖尾部窗口的 1-2 个 shard。
	var (
		record        SessionRecord
		hasRecord     bool
		history       []EngineMessage
		historyTotal  int
		historyErr    error
		transcript    []TranscriptEvent
		transcriptErr error
		recordErr     error
	)
	var loadGroup sync.WaitGroup
	loadGroup.Add(3)
	go func() {
		defer loadGroup.Done()
		record, hasRecord, recordErr = service.components.sessions.LoadSessionRecord(location, sessionID)
	}()
	go func() {
		defer loadGroup.Done()
		history, historyTotal, historyErr = service.components.sessions.LoadHistoryTailWindow(location)
	}()
	go func() {
		defer loadGroup.Done()
		transcript, transcriptErr = service.components.sessions.LoadSessionTranscript(location, sessionID)
		// 旧链路取代说明：rollout 重放恢复已退役——JSON 会话（含旧布局存量）
		// 统一走 wire 装配；rollout 日志仅保留审计/只读兼容接口，不再参与恢复。
	}()
	loadGroup.Wait()
	if recordErr != nil {
		return fmt.Errorf("load session record %q: %w", sessionID, recordErr)
	}
	if historyErr != nil && !hasRecord {
		return fmt.Errorf("load session %q: %w", sessionID, historyErr)
	}
	if historyErr != nil {
		// A v2 SessionRecord is authoritative for the visible transcript and
		// recovery checkpoint. Framework history is only a provider cache, so a
		// lost legacy cache must not make the saved session impossible to open.
		history = nil
	}
	if transcriptErr != nil && !hasRecord {
		return fmt.Errorf("load session transcript %q: %w", sessionID, transcriptErr)
	}
	engineHistory := history
	if hasRecord {
		budget := task_context.ContextBudgetFor(service.Deps.Runtime)
		latestUser := service.components.sessions.LatestUserContent(record.Conversation.Messages)
		if len(transcript) == 0 || (latestUser != "" && !service.components.sessions.TranscriptContainsUser(transcript, latestUser)) {
			// The durable Conversation is the source of truth. Rehydrate it into
			// the application transcript when the append-only tail is empty or
			// stale, otherwise the next prepareExecutionContext call would drop
			// the fallback history again.
			transcript = service.components.sessions.RecordConversationTranscript(record)
		}
		engineHistory = task_context.TranscriptTailHistory(transcript, budget.TargetAfterCompaction, 4)
		// R2 运行期接线（v8 新链路）：直接装配 compact 摘要 + 尾窗 + 最近
		// K 条尝试；非 会话存储布局 ok=false 时保留旧装配结果。
		if wire, wireOK, wireErr := service.components.sessions.AssembleWireHistoryWorkspace(location, sessionID, budget.TargetAfterCompaction, 3); wireErr != nil {
			return fmt.Errorf("assemble wire history %q: %w", sessionID, wireErr)
		} else if wireOK && len(wire) > 0 {
			engineHistory = wire
		}
		recordHistory := service.components.sessions.RecordConversationResumeHistory(record, budget.TargetAfterCompaction, 4)
		if len(engineHistory) == 0 || (latestUser != "" && !service.components.sessions.HistoryContainsUser(engineHistory, latestUser)) {
			engineHistory = recordHistory
		}
		if len(engineHistory) == 0 {
			engineHistory = session_runtime.RecordResumeHistory(record)
		}
	}
	// framework DurableHistory 按会话 workspace 显式键落盘（R3 键漂移收敛）：
	// 必须在引擎创建/恢复前登记绑定，后台会话 ChatStream 结束时不串写他域。
	service.Deps.Runtime.SetSessionWorkspace(sessionID, location.WorkspaceID)
	if enginePort, ok := service.Deps.Engine.(interface {
		ResumeSession(string, []EngineMessage) error
	}); ok {
		// M1：恢复目标会话自己的引擎实例（会话注册表内存缓存，切走不再销毁）。
		if err := enginePort.ResumeSession(sessionID, engineHistory); err != nil {
			return fmt.Errorf("resume engine session: %w", err)
		}
	} else if err := service.Deps.Engine.ReplaceHistory(sessionID, engineHistory); err != nil {
		return fmt.Errorf("replace engine history: %w", err)
	}
	// lifecycle 运行期接线：重启恢复队列（发送未确认项回 queued；
	// message.json 已发布项出队）。非 会话存储布局 ok=false 为正常空操作。
	if _, lifecycleOK, lifecycleErr := service.components.sessions.LifecycleRecover(location, sessionID); lifecycleErr != nil {
		return fmt.Errorf("lifecycle recover %q: %w", sessionID, lifecycleErr)
	} else {
		_ = lifecycleOK
	}
	// 会话级 system prompt：切换路径必须按目标会话路由，禁止触碰全局活跃
	// 引擎（运行中会话的 Session 锁可能被 ChatStream 全程持有，误触会阻塞
	// 到该会话 LLM 返回 —— 用户视角的死锁/长时间无响应）。
	if promptPort, ok := service.Deps.Engine.(interface {
		SetSystemPromptFor(string, string)
	}); ok {
		promptPort.SetSystemPromptFor(sessionID, service.promptStack.Render())
	} else {
		service.Deps.Engine.SetSystemPrompt(service.promptStack.Render())
	}

	total := historyTotal // 尾部窗口读返回的真实总数（无 record 的旧格式会话）
	if hasRecord {
		total = len(service.components.sessions.RecordConversation(record))
	}
	offset := total - Limits().HistoryWindow
	if offset < 0 {
		offset = 0
	}
	// 标题说明（审查 #5）：hasRecord 时标题取 record.Title（权威）；无 record
	// 的旧格式会话标题由 SessionTitleFromHistory 取窗口内首条 user 消息。
	visibleHistory := history
	currentWorkspace := location.Workspace
	if service.Deps.Workspace != nil {
		if currentWorkspace != nil {
			// 冷加载同样遵守「运行中不改根」：有后台会话运行中时跳过全局
			// 重绑（P3/G5），per-session workspace 记录照写。
			if service.bindProjectRootIfSafe(sessionID, currentWorkspace.RootPath) {
				service.setWorkspaceWriteScope(currentWorkspace.ID)
			}
			service.Deps.Workspace.BindSession(sessionID, currentWorkspace.ID)
		} else {
			if !service.anyChatRunningLocked() {
				service.unbindGlobalProjectRoot()
				service.setWorkspaceWriteScope("")
			}
			service.Deps.Workspace.UnbindSession(sessionID)
		}
	}

	activePlan := task_context.ActivePlanFrame(record.PlanStack, record.ActivePlanID)
	var planRestoreErr error
	if hasRecord && activePlan != nil && activePlan.Arguments != "" {
		if restorer, ok := service.Deps.Runtime.(persistedPlanRestorer); ok {
			planRestoreErr = restorer.RestorePlan(context.Background(), activePlan.Arguments)
		}
	}
	// 会话级 task 隔离：切换会话时整体替换注册表（清空旧会话、恢复目标
	// 会话 task）并清空子代理树，避免旧数据污染新会话工作台。
	service.Deps.Runtime.SwitchSessionTasks(sessionID, record.Tasks)
	_ = service.Deps.Runtime.ClearSubagentTree()
	// 恢复锚点：从主会话事件库/子会话记录重建目标会话的 fork 树与认领
	// （Assignee → subagent:<节点会话ID>；重启/切页后不再停留 main）。
	_ = service.Deps.Runtime.RestoreSubagentAnchors(sessionID)
	workspaceProjection := service.collectWorkspaceProjection()

	service.ViewMu.Lock()
	// 异步装载（activateEpoch>0）：仅在仍是最新切换目标且目标仍是当前视图
	// 会话时才发布基线；被更新的切换取代时，本次只完成目标会话自身的状态
	// 装载（引擎驻留/可见投影/任务槽），不激活视图、不抢占、不发布。
	mayActivate := activateEpoch == 0
	if activateEpoch != 0 {
		mayActivate = service.viewEpoch == activateEpoch && service.Core.Snapshot.Session.ID == sessionID
	}
	// restoring 标记只由后台装载设置：无论激活与否都先移除，避免装载完成
	// 后会话树/快照停留在“恢复中”。
	service.clearRestoringLocked(sessionID)
	name := session_runtime.SessionTitleFromHistory(history, displayUserInput)
	if hasRecord && record.Title.Value != "" {
		name = record.Title.Value
	}
	if mayActivate {
		service.Core.Snapshot.Session = SessionState{ID: sessionID, Name: name}
		service.sessions.SetActive(sessionID)
	}
	resumedRuntime := service.sessionUnitLocked(sessionID)
	resumedRuntime.SetCancel(nil)
	service.components.sessions.SetSessionTitleLocked(sessionID, SessionTitle{Value: name, Source: "legacy_history"})
	if hasRecord {
		service.components.sessions.SetSessionTitleLocked(sessionID, record.Title)
		transcriptSeq := uint64(0)
		if len(transcript) > 0 {
			transcriptSeq = transcript[len(transcript)-1].Seq
		}
		if record.Projection != nil && record.Projection.Checkpoint.CoversEventRange.End > transcriptSeq {
			transcriptSeq = record.Projection.Checkpoint.CoversEventRange.End
		}
		service.components.tasks.RestoreSessionTaskLocked(task_context.RestoredTaskState{
			PlanStack:         session_runtime.CloneSessionPlanStack(record.PlanStack),
			ActivePlanID:      record.ActivePlanID,
			Transcript:        transcript,
			TranscriptSeq:     transcriptSeq,
			Checkpoints:       record.Checkpoints,
			ToolResults:       record.ToolResults,
			Projection:        record.Projection,
			FallbackObjective: service.components.sessions.LatestUserContent(record.Conversation.Messages),
		})
	} else {
		service.components.tasks.ResetForNewSessionLocked()
	}
	// 阶段 1：冷加载重建写会话 view（Snapshot 是活跃会话的只读镜像）。
	view := &session.View{
		TotalMessages:      total,
		HistoryOffset:      offset,
		HasMoreHistory:     offset > 0,
		ConversationWindow: Limits().HistoryWindow,
	}
	if hasRecord {
		service.advanceMessageSeqLocked(sessionID, record.Conversation.Messages)
		view.Conversation = append(view.Conversation, Message{Role: "system", Content: "已恢复会话: " + sessionID, CreatedAt: time.Now()})
		view.Conversation = append(view.Conversation, service.components.sessions.RecordConversationTail(record, Limits().HistoryWindow)...)
		view.ReadFiles = append([]ReadFileRef(nil), record.Execution.ReadFiles...)
		// 可见投影：后台冷恢复（activateEpoch>0）可能迟到完成，而目标会话在这
		// 期间可能已经跑过自己的回合——它的可见会话是更新的活事实，用恢复快照
		// 覆盖会把这一轮顶掉（2026-09-11 TC-A2-01 第三层根因：fork 子会话首轮
		// 被迟到的基线整体覆盖）。后台装载因此只在目标可见会话仍为空时安装；
		// 同步装载（activateEpoch=0，调用方持视图过渡锁、无并发写）保持原语义。
		if activateEpoch == 0 || service.sessionViewEmptyLocked(sessionID) {
			service.components.view.SetSessionViewLocked(sessionID, view)
		}
		if mayActivate {
			if record.Execution.Task != nil {
				task := *record.Execution.Task
				task.ContextCompactions = append([]ContextCompaction(nil), record.Execution.Task.ContextCompactions...)
				service.Core.Snapshot.Task = &task
			} else {
				service.Core.Snapshot.Task = nil
			}
		}
		if planRestoreErr != nil {
			service.appendSessionMessageLocked(sessionID, "system", "The stored Plan is visible for review but could not be reloaded for execution with the current settings.", nil)
		}
	} else {
		service.appendHistoryLockedFor(sessionID, visibleHistory)
	}
	service.setSessionChatLockedFor(sessionID, resumedRuntime.ChatState())
	if mayActivate {
		service.mirrorPlanProjectionForSessionLocked(sessionID, func() *PlanState {
			return task_context.ActivePlanFromStack(record.PlanStack, record.ActivePlanID)
		})
		service.Core.Snapshot.Interaction = nil
	}
	systemPrompt := service.components.prompts.SystemPromptForActiveTaskLocked()
	if mayActivate && service.Deps.Workspace != nil {
		service.Core.Snapshot.CurrentWorkspace = currentWorkspace
		service.applyWorkspaceProjectionLocked(workspaceProjection)
	}
	revision := uint64(0)
	if mayActivate {
		revision = service.bumpLocked()
	}
	service.ViewMu.Unlock()
	if mayActivate {
		// 会话路由引擎的 prompt 已在装载段按目标会话经 SetSystemPromptFor
		// 写入（EnginePort 同步进程级缓存，新建引擎自动继承）；不再重复写
		// 全局活跃别名引擎——别名可能仍指向正在运行的被切走会话，全局写
		// 会被其 ChatStream 持有的 Session 锁阻塞（切换等收尾现象）。
		if _, ok := service.Deps.Engine.(interface{ SetSystemPromptFor(string, string) }); !ok {
			service.Deps.Engine.SetSystemPrompt(systemPrompt)
		}
	}
	// context 模块挂接：resume 恢复后加载会话四栈到 Runtime（下一轮 prompt
	// 组装前就绪）。损坏的 context 显式失败，不静默降级成内存栈。
	if store, ok := service.Deps.Sessions.(session_runtime.SessionContextPort); ok {
		if err := store.AttachSessionContext(location.WorkspaceID, sessionID); err != nil {
			return fmt.Errorf("attach session context %q: %w", sessionID, err)
		}
	}
	if mayActivate {
		service.publishSessionEvent(EventSnapshotChanged, revision, "", sessionID, nil)
		service.publishRuntimeProjections()
	}
	service.components.sessions.RequestCatalogRefresh()
	// G6 驻留 LRU：冷加载完成即记录使用序并收敛超限驻留（INV-G8）。
	service.touchResident(sessionID)
	return nil
}

// ResumeSession 是 GUI/TUI 会话选择的直接应用边界。它刻意绕过命令文本解析，
// 让一次点击有同步结果：恢复的快照或返回的错误。
func (service *Service) ResumeSession(sessionID string) error {
	return service.resumeSession(sessionID)
}

// LoadMoreHistory 把更早的历史页前置到可见会话。
func (service *Service) LoadMoreHistory(limit int) error {
	if limit <= 0 {
		limit = Limits().HistoryWindow
	}

	service.ViewMu.RLock()
	offset := service.Core.Snapshot.HistoryOffset
	sessionID := service.Core.Snapshot.Session.ID
	service.ViewMu.RUnlock()
	if offset <= 0 {
		return nil
	}

	loadOffset := offset - limit
	if loadOffset < 0 {
		loadOffset = 0
	}
	loadLimit := offset - loadOffset

	workspaceID := ""
	service.ViewMu.RLock()
	if service.Core.Snapshot.CurrentWorkspace != nil {
		workspaceID = service.Core.Snapshot.CurrentWorkspace.ID
	}
	service.ViewMu.RUnlock()
	var adapted []Message
	total := 0
	if store, ok := service.Deps.Sessions.(session_runtime.SessionConversationRangePort); ok {
		messages, count, err := store.LoadConversationRangeWorkspace(workspaceID, sessionID, loadOffset, loadLimit)
		if err != nil {
			return fmt.Errorf("load conversation range: %w", err)
		}
		adapted = service.components.sessions.RecordConversation(SessionRecord{Conversation: ConversationRecord{Messages: messages}})
		total = count
	} else {
		history, count, err := service.components.sessions.LoadSessionHistoryRange(workspaceID, sessionID, loadOffset, loadLimit)
		if err != nil {
			return fmt.Errorf("load history range: %w", err)
		}
		total = count
		adapted = make([]Message, 0, len(history))
		for _, msg := range history {
			if !isVisibleHistoryMessage(msg) {
				continue
			}
			adapted = append(adapted, adaptEngineMessage(msg))
		}
	}

	service.ViewMu.Lock()
	for index := range adapted {
		if adapted[index].ID == "" {
			adapted[index].ID = fmt.Sprintf("message-%d", service.components.view.NextMessageSeqForLocked(sessionID))
		}
	}
	service.Core.Snapshot.Conversation = append(adapted, service.Core.Snapshot.Conversation...)
	service.Core.Snapshot.Conversation = view_state.BoundConversationHead(service.Core.Snapshot.Conversation, Limits().HistoryWindow)
	service.Core.Snapshot.HistoryOffset = loadOffset
	service.Core.Snapshot.TotalMessages = total
	service.Core.Snapshot.HasMoreHistory = loadOffset > 0
	service.Core.Snapshot.ConversationWindow = Limits().HistoryWindow
	revision := service.bumpLocked()
	service.ViewMu.Unlock()
	service.publishSessionEvent(EventSnapshotChanged, revision, "", sessionID, nil)
	return nil
}

func adaptEngineMessage(msg EngineMessage) Message {
	content := msg.Content
	if msg.Role == "user" {
		content = displayUserInput(content)
	} else if msg.Role == "assistant" || msg.Role == "tool" {
		content = chat.StripThoughtBlocks(content)
	}
	if context_runtime.IsProviderOnlyHistoryContent(content) {
		content = ""
	}
	message := Message{Role: msg.Role, Content: content, ReasoningContent: msg.ReasoningContent}
	for _, toolCall := range msg.ToolCalls {
		message.Tool = &ToolCall{
			ID: toolCall.ID, Name: toolCall.Name, Arguments: toolCall.Arguments, Status: "success",
		}
	}
	return message
}

func isVisibleHistoryMessage(message EngineMessage) bool {
	if message.Role == "system" {
		return false
	}
	if message.Role != "user" {
		return true
	}
	return displayUserInput(message.Content) != ""
}
