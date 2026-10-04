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
// 未驻留时冷加载。冷加载一律先激活目标会话的 restoring 空壳（视图立即指向
// 目标、会话树显示“恢复中”），装载——磁盘三读 / wire 装配 / 引擎恢复 / 队列
// 回填——在**视图过渡 key 之外**完成：键只保护「判定视图意图」与「按意图序号
// 发布基线」两段，一次冷加载不再把别的会话的切换与提交排在自己的磁盘读之后
// （“一个会话冷加载，另一个会话就没反应”）。
//
// 空闲（无会话运行中）走同一条链路，只是前台调用**同步**等装载收口，保持
// 「ResumeSession 返回 ⇒ 目标已装载（失败则视图回退到切换前会话）」的契约；
// 运行中则立即返回、装载在后台完成（前端 RPC 不再长期不返回，“恢复中”不等于
// 卡住）。
func (service *Service) resumeSession(sessionID string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return errors.New("session ID is required")
	}

	transition := service.transitionForSession(sessionID)
	transition.Lock()

	service.ViewMu.RLock()
	hot := service.sessionLoaded(sessionID)
	running := service.anyChatRunningLocked()
	restoring := service.isRestoringLocked(sessionID)
	previous := service.Core.Snapshot.Session.ID
	service.ViewMu.RUnlock()

	if restoring {
		// 同目标已有装载在途：空壳已激活，重复请求幂等（不重复装载、不推进
		// epoch——自身装载不能被自己打断）。
		transition.Unlock()
		return nil
	}
	if hot {
		// 阶段 2：目标会话已驻留（含运行中）→ 热加载，只换视图指针 +
		// 投影会话 scope，不重建历史、不触碰 X/M/R（不变量 Ⅱ）。推进 epoch，
		// 让在途冷加载不再抢占视图。
		service.bumpViewEpoch()
		transition.Unlock()
		return service.hotAttachSession(sessionID)
	}
	// 目标未驻留：冷加载。先激活 restoring 空壳并立刻**让出**视图过渡 key，
	// 装载不再占着它跑完整段磁盘 I/O（原来整段装载都在键内，别的会话的
	// resume/submit 只能排队，用户视角是「另一个会话断掉」）。
	epoch, err := service.beginAsyncRestore(sessionID)
	transition.Unlock()
	if err != nil {
		return err
	}
	if running {
		// 运行中：装载交后台，本次请求立即返回。
		go service.resumeSessionColdInBackground(sessionID, previous, epoch)
		return nil
	}
	// 空闲：同步等装载收口，调用方返回时目标已装载（或被更新的切换取代、
	// 只完成自身会话状态）。
	if err := service.resumeSessionCold(sessionID, epoch); err != nil {
		service.handleColdRestoreFailure(sessionID, previous, epoch, err)
		return err
	}
	return nil
}

// bumpViewEpoch 推进视图切换序号（任何新的视图激活都推进；冷加载完成时只有
// 仍为最新 epoch 才允许发布基线）。
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
	// 团队看板同样要清：它是**上一个会话**的运行原件（本会话的会在装载完成时重采）。
	// 不清就有一段"B 是活跃会话、面板上却挂着 A 的团队看板"的过渡帧——数据没丢，
	// 但那一帧在说谎。
	service.Core.Snapshot.Runtime.TeamworkBoard = nil
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
	// 档位与其它会话字段同一口径重算：这条路径在 Core.ViewMu 下完成（不读盘），
	// 草稿槽位要么是就地新建（无持久设置可读），要么是保留中的槽位（其会话单元
	// 仍持有该会话的档位选择）——两种情况 syncViewPermissionTierLocked 给出的都是
	// 本会话的生效档位，而不是上一个会话留在快照里的那个。
	service.syncViewPermissionTierLocked(draftID)
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
// activateEpoch 是本次装载对应的视图意图序号（调用方在激活 restoring 空壳时
// 推进）：装载完成时若不是最新序号、或视图已不再指向目标，本次只完成目标会话
// 自身的状态装载（引擎驻留、可见投影、任务槽），不发布基线、不抢占当前视图。
func (service *Service) resumeSessionCold(sessionID string, activateEpoch uint64) error {
	// L2 草稿尾恢复（A4）必须在三读之前：可见正文的闸门是发布点，先恢复再读，
	// 恢复出来的行才能进本次加载的可见会话（见 session_pending_tail.go）。
	pendingTailNotice := service.recoverPendingMessageTailAtLoad(sessionID)
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
	// transcriptRenumbered 标记本次装载的 transcript 是**从可见会话重建**的
	// （durable 事件流缺失/过期）。重建会把事件序号重新编码（seq = 1..N），
	// 与压缩记录里的区间序号不再是同一套空间：任何拿序号定位的推导都必须停用
	// （见 task_context.RetainedFromForCompactions）。
	var transcriptRenumbered bool
	if hasRecord {
		// 读尾预算走 window 段的保留窗口规则（min(retain_tokens, ratio × 账号
		// 上下文窗口)）：与压缩侧同一份配置、同一实现，读尾不再是第二套硬编码
		// 数字（旧口径 60% 预算的 TargetAfterCompaction）。单元上限仍是存储层
		// 的分片选择边界（0 会被判为"不读"）。
		tailBudget := RetainedReadTailBudget(service.Deps.Runtime)
		latestUser := service.components.sessions.LatestUserContent(record.Conversation.Messages)
		if len(transcript) == 0 || (latestUser != "" && !service.components.sessions.TranscriptContainsUser(transcript, latestUser)) {
			// The durable Conversation is the source of truth. Rehydrate it into
			// the application transcript when the append-only tail is empty or
			// stale, otherwise the next prepareExecutionContext call would drop
			// the fallback history again.
			transcript = service.components.sessions.RecordConversationTranscript(record)
			transcriptRenumbered = true
		}
		engineHistory = task_context.TranscriptTailHistory(transcript, tailBudget, CurrentWindowConfig().MinRounds)
		// R2 运行期接线（v8 新链路）：直接装配 compact 摘要 + 尾窗 + 最近
		// K 条尝试；非 会话存储布局 ok=false 时保留旧装配结果。
		if wire, wireOK, wireErr := service.components.sessions.AssembleWireHistoryWorkspace(location, sessionID, tailBudget, 3); wireErr != nil {
			return fmt.Errorf("assemble wire history %q: %w", sessionID, wireErr)
		} else if wireOK && len(wire) > 0 {
			engineHistory = wire
		}
		recordHistory := service.components.sessions.RecordConversationResumeHistory(record, tailBudget, 4)
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
	// message.json 已发布项出队），并把"已发送未确认"的输入回填到本会话的
	// 内存队列（可见、可撤回；在下一次提交时随本会话一并提升发送）。
	if resent, lifecycleOK, lifecycleErr := service.components.sessions.QueueRecoverInputs(location, sessionID); lifecycleErr != nil {
		return fmt.Errorf("lifecycle recover %q: %w", sessionID, lifecycleErr)
	} else if lifecycleOK && len(resent) > 0 {
		service.reEnqueueRecoveredInputs(sessionID, resent)
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
			if service.Deps.Runtime != nil {
				// 该会话没有工作区：清掉按会话分格的工具根，避免解析到上一次绑定。
				service.Deps.Runtime.UnbindProjectRootFor(sessionID)
			}
		}
	}

	activePlan := task_context.ActivePlanFrame(record.PlanStack, record.ActivePlanID)
	var planRestoreErr error
	if hasRecord && activePlan != nil && activePlan.Arguments != "" {
		if restorer, ok := service.Deps.Runtime.(persistedPlanRestorer); ok {
			planRestoreErr = restorer.RestorePlan(context.Background(), activePlan.Arguments)
		}
	}
	workspaceProjection := service.collectWorkspaceProjection()
	// 需求变更（P1-1）：冷加载读回目标会话的权限档位（含磁盘读，在锁外完成；
	// 内存态落地在下面拿到会话单元之后）。
	storedTier := service.readStoredPermissionTier(sessionID)

	service.ViewMu.Lock()
	// 发布判据：装载完成时必须是「最新视图意图」且视图仍指向目标会话——
	// 目标会话在装载期间可能已经跑过自己的回合，也可能已被更新的切换取代，
	// 两种情形都只完成目标会话自身的状态装载（引擎驻留/可见投影/任务槽），
	// 不激活视图、不抢占、不发布。
	mayActivate := service.viewEpoch == activateEpoch && service.Core.Snapshot.Session.ID == sessionID
	// 装载在途标记：无论激活与否都先移除，避免装载完成后会话树/快照停留
	// 在“恢复中”。
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
	service.applyStoredPermissionTier(sessionID, storedTier)
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
		// 会话上下文事实（压缩栈 / 保留窗口起点）从 record.Execution.Task 还原：
		// 它们属于**会话**，不属于回合——进程重启不该把它们丢掉（2026-09-23 修的
		// 进程内孪生见 continuationTaskExecutionState 的注释）。不还原的两条后果：
		// 右栏「上下文压缩」整条为空；下一次落盘（sessionRecordLocked →
		// TaskStateFor）把 record 里的压缩历史写成空。
		var contextCompactions []ContextCompaction
		retainedFrom := 0
		if storedTask := record.Execution.Task; storedTask != nil {
			contextCompactions = append([]ContextCompaction(nil), storedTask.ContextCompactions...)
			// 保留窗口起点只在**存储事件流**上按压缩记录的区间推（口径见
			// task_context.RetainedFromForCompactions）：重建过的 transcript 序号
			// 空间不同，在那里定位会把窗口错误地推到会话中段（丢历史）。
			if !transcriptRenumbered {
				retainedFrom = task_context.RetainedFromForCompactions(transcript, storedTask.ContextCompactions)
			}
		}
		// 按**目标会话**路由装载：冷加载可能不激活视图（装载期间视图已被更新的
		// 切换取代），此时把目标会话的任务/plan 状态写进活跃槽既是串写，也会让目标
		// 会话自己的任务状态恒为空——它的下一次落盘就会把 record 里的压缩历史抹掉。
		service.components.tasks.RestoreSessionTaskLockedFor(sessionID, task_context.RestoredTaskState{
			PlanStack:           session_runtime.CloneSessionPlanStack(record.PlanStack),
			ActivePlanID:        record.ActivePlanID,
			Transcript:          transcript,
			TranscriptSeq:       transcriptSeq,
			Checkpoints:         record.Checkpoints,
			ToolResults:         record.ToolResults,
			Projection:          record.Projection,
			FallbackObjective:   service.components.sessions.LatestUserContent(record.Conversation.Messages),
			ContextCompactions:  contextCompactions,
			ContextRetainedFrom: retainedFrom,
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
		// 可见投影：冷恢复可能迟到完成，而目标会话在这期间可能已经跑过自己的
		// 回合——它的可见会话是更新的活事实，用恢复快照覆盖会把这一轮顶掉
		//（2026-09-11 TC-A2-01 第三层根因：fork 子会话首轮被迟到的基线整体
		// 覆盖）。装载因此只在目标可见会话仍为空时安装。
		if service.sessionViewEmptyLocked(sessionID) {
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
		// 旧格式会话（无 record）：已装载的可见条数只是「尾窗」，历史总数由
		// historyTotal 给出。不补这两项，HistoryOffset 恒为 0、
		// HasMoreHistory 恒为 false——长会话的「加载更早」永远点不出来
		//（早期历史读不到），是分页链路另一半红灯。
		if historyTotal > 0 {
			service.components.view.SessionViewMutateLocked(sessionID, func(current *session.View) {
				visible := view_state.DurableConversationCount(current.Conversation)
				if historyTotal > visible {
					current.TotalMessages = historyTotal
					current.HistoryOffset = historyTotal - visible
					current.HasMoreHistory = true
				}
			})
			service.mirrorActiveViewLocked()
		}
	}
	// 草稿尾恢复结论（A4）：用户可感知的一条 system 行（见 session_pending_tail.go）。
	// 放在两条分支之外：有 record 的新布局与只有 legacy history 的旧布局都要提示
	// "你上次中断时未提交的那几行被恢复了"。
	service.appendPendingTailNoticeLocked(sessionID, pendingTailNotice)
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
	if err := service.attachSessionContextFor(location.WorkspaceID, sessionID); err != nil {
		return err
	}
	if mayActivate {
		// 会话级 task 隔离（只在视图真的切到目标会话时才动）：整体替换**当前
		// 会话的实时注册表**（清空旧会话、恢复目标会话 task）、清空子代理树并
		// 重建目标会话的 fork 树与认领。三者都是**进程级**执行面（落盘/打点/
		// GUI 子树）；工作表格本体是全局台账（TaskSnapshot = 注册表 + 全部分区），
		// 不随本次切换丢行。不激活视图的装载（目标已被更新的切换取代）只完成
		// 自身会话状态，不得抢走运行中会话的执行面——原来无条件执行，B 在飞时
		// A 的后台冷加载会把注册表归属与子代理树改成 A。这三步写在投影发布之前，
		// 让紧随其后的 publishRuntimeProjections 带上目标会话的工作表格/子代理树。
		service.Deps.Runtime.SwitchSessionTasks(sessionID, record.Tasks)
		_ = service.Deps.Runtime.ClearSubagentTree()
		_ = service.Deps.Runtime.RestoreSubagentAnchors(sessionID)
		// 会话运行原件的**重建**（2026-10-04）：冷加载新建的会话单元 runtime 槽是空的
		// （UnloadSession 已经把上一份槽随单元一起拿走）。不在这里采一次，切回来/重启后
		// 打开会话的第一帧就缺 teamwork_board（前端整块退场），要等下一轮才回来。
		// 采集在 ViewMu 之外、发布快照之前——与热挂载路径同一口径。
		service.refreshRuntimeProjectionForSession(sessionID)
		service.publishSessionEvent(EventSnapshotChanged, revision, "", sessionID, nil)
		service.publishRuntimeProjections()
	}
	service.components.sessions.RequestCatalogRefresh()
	// G6 驻留 LRU：冷加载完成即记录使用序并收敛超限驻留（INV-G8）。
	service.touchResident(sessionID)
	// 内容 LRU：冷加载装载了可见正文，记录内容使用序并收敛超限正文。
	service.touchContent(sessionID)
	return nil
}

// attachSessionContextFor 把目标会话的 context 模块（system prompt + 四栈）挂到
// Runtime 上，供**下一轮 prompt 组装前**就绪。损坏的 context 显式失败，不静默降级成
// 内存栈。
//
// 为什么必须有这一条共用挂接：生产上有三条路径要让某个会话成为**当前会话**——
// 冷恢复/热挂载（resumeSession）、新建会话（materializeDraftSession，草稿物化）、
// 切项目时另起的独立会话（SwitchSessionWorkspace 的 startFreshSession）——它们此前
// 各自为政：只有 resume 挂接，另两条**只解绑不挂接**。
//
// 未绑定的代价（2026-10-04 现场）：Runtime 的会话上下文存储是会话级四栈与装配层推帧
// 的共同依赖。未绑定状态下 seelebridge 的 PushCompactionFrame /
// ReadbackCompactionSummary 直接报"会话上下文存储未绑定（压缩栈不可用）"——装配层
// 压缩推不了帧、拿不到可回读的 segment_id，帧正文只能写一句"没有模型生成的读后感"；
// stackBlocks（plan/task/skill/compact）与 relatedMemoryBlocks 也一律为空。而 resume
// 之后一切自愈，于是症状只在"新开的会话"里出现（现场两帧所属的会话键都是新建会话的
// 早分配 SID）。
//
// 全新会话键上挂接是安全的：store 的 Load 是惰性的，各通道 not-found 都按 S19 口径
// 静默初始化成空记录，既不继承上一个会话的四栈，也不多写任何东西——写只发生在
// Persist。挂接落点是**当前活跃 bundle**（会话槽化后按会话隔离），因此不会串台。
func (service *Service) attachSessionContextFor(workspaceID, sessionID string) error {
	store, ok := service.Deps.Sessions.(session_runtime.SessionContextPort)
	if !ok {
		return nil
	}
	if err := store.AttachSessionContext(workspaceID, sessionID); err != nil {
		return fmt.Errorf("attach session context %q: %w", sessionID, err)
	}
	return nil
}

// ResumeSession 是 GUI/TUI 会话选择的直接应用边界。它刻意绕过命令文本解析，
// 让一次点击有同步结果：恢复的快照或返回的错误。
func (service *Service) ResumeSession(sessionID string) error {
	return service.resumeSession(sessionID)
}

// LoadMoreHistory 把更早的一页历史前置到可见会话（GUI 顶部 sentinel 与
// 「加载更早」按钮的应用边界）。
//
// 分页契约（2026-09-11 修复）：
//   - 一页 = 一整窗（limits.history_window；入参只当上限建议）：半页会把
//     「窗口」和「页」两个尺寸混在一起——翻一次只多出半屏又丢掉半屏；
//   - 分页态写进**会话可见投影**（唯一事实源）后再镜像 Snapshot：只写
//     Snapshot 会在下一次镜像（新消息/工具事件/切换）被整体抹掉，offset
//     退回尾部，前端表现为「点了加载更早，内容回卷，再点还是同一页」；
//   - 窗口 = 从新 HistoryOffset 起的连续一段（上限 window）：窗口整体后退
//     一页，而不是把可见列表无限加长（WebView 渲染内存有硬上限）；
//   - 冷读面只到发布点：滑出窗口的本轮在飞行行要等这次落盘（回合收尾
//     PersistCurrentSession）才读得回来。窗口因此不贴尾，安装点保持
//     TotalMessages 如实前推，前端据「total − (offset+窗口条数)」提示
//     「下方还有新内容」，而不是把内容抹平；
//   - 冷读下标是**未过滤**空间（内部标记行也占位），可见下标与它之间隔着那些
//     行：一页读回来先按**行身份**找接缝（窗口首行在页里的位置），只把接缝
//     之前的那段可见行前置进窗口；接缝对不上就不假装连续（见 installVisibleHistory）。
func (service *Service) LoadMoreHistory(limit int) error {
	window := Limits().HistoryWindow
	if window <= 0 {
		window = 1
	}
	if limit <= 0 || limit > window {
		limit = window
	}

	service.ViewMu.RLock()
	offset := service.Core.Snapshot.HistoryOffset
	sessionID := service.Core.Snapshot.Session.ID
	workspaceID := currentWorkspaceIDLocked(service)
	service.ViewMu.RUnlock()
	if service.sessionContentUnloaded(sessionID) {
		// 内容 LRU 卸载后再分页：窗口当前不在内存，先把尾部窗口从磁盘回读
		// （offset 回到尾部），再按本页请求向更早推进；否则「加载更早」会从
		// 一个不存在的窗口出发（页错位）。
		if err := service.reloadSessionContent(sessionID); err != nil {
			return err
		}
		service.ViewMu.RLock()
		offset = service.Core.Snapshot.HistoryOffset
		service.ViewMu.RUnlock()
	}
	if offset <= 0 {
		return nil
	}

	loadOffset := offset - limit
	if loadOffset < 0 {
		loadOffset = 0
	}
	// 冷读面的下标是**未过滤**空间（内部标记行同样占位），可见下标不能直接当
	// 冷读下标用：这里多带一个窗口并按几何扩读，由接缝（窗口首行）对位确定这一
	// 页的右界（见 loadEarlierVisiblePage），再按行身份前置进窗口。
	seam := service.visibleWindowFirstRow(sessionID)
	page, err := service.loadEarlierVisiblePage(workspaceID, sessionID, offset, offset-loadOffset, window, seam)
	if err != nil {
		return err
	}
	return service.installVisibleHistory(sessionID, page, window, historyPagePrepend)
}

// LoadLatestHistory 把可见会话拉回最新一页（历史浏览后的「回到最新」）。
// 分页只移动窗口、不动数据：回到最新 = 重新读尾部窗口并贴尾；回看期间
// 错过的新消息由这次基线一并带回。
//
// 热读短路：内存窗口已经贴着有效尾（窗口右界 = 已可见总数）时，「回到最新」
// 没有任何可加载的东西——冷读面的右界是发布点，换上来的必然是更旧的一页，
// 把本轮尚未落盘的尾部整窗删掉。此时直接返回，内存即答案。
func (service *Service) LoadLatestHistory() error {
	window := Limits().HistoryWindow
	if window <= 0 {
		window = 1
	}
	service.ViewMu.RLock()
	sessionID := service.Core.Snapshot.Session.ID
	workspaceID := currentWorkspaceIDLocked(service)
	service.ViewMu.RUnlock()
	if service.sessionContentUnloaded(sessionID) {
		// 内容 LRU 卸载后「回到最新」= 从磁盘整窗回读（回读本身按尾部窗口安装
		// 并清除「内容未加载」标志，无需再读一次）。
		return service.reloadSessionContent(sessionID)
	}
	if service.visibleTailServedFromMemory(sessionID) {
		return nil
	}

	page, err := service.loadConversationTailPage(workspaceID, sessionID, window)
	if err != nil {
		return err
	}
	return service.installVisibleHistory(sessionID, page, window, historyPageReplace)
}

// visibleTailServedFromMemory 报告内存窗口是否已经把有效尾部整窗带到（非空且
// 贴尾）。贴尾 = 用户已经在最新处；窗口为空（草稿/刚被卸载）时按「未带到」
// 处理，必须走冷读。
func (service *Service) visibleTailServedFromMemory(sessionID string) bool {
	atTail := false
	service.components.view.SessionViewReadLocked(sessionID, func(view *session.View) {
		durable := view_state.DurableConversationCount(view.Conversation)
		atTail = durable > 0 && view.HistoryOffset+durable >= view.TotalMessages
	})
	return atTail
}

// loadConversationTailPage 读发布点处的尾部窗口：先探总数（未过滤空间，只用来
// 定位发布点），再从发布点向左读一整段。尾部整段被内部标记行占掉时（回合收尾刚
// 落的 checkpoint），一窗未过滤行里可能一条可见行都读不出来——「回到最新」会
// 拿到空页，所以这里按几何扩读向左补齐，直到可见行攒够一窗或读到序列开头。
//
// 探测与窗口读是两次独立加锁操作（与 session_runtime.LoadHistoryTailWindow 同一
// 观察项）：两读之间会话并发增长时窗口会短一两行，下一次加载自我纠正。
func (service *Service) loadConversationTailPage(workspaceID, sessionID string, window int) (conversationPage, error) {
	// limit=1 只为拿总数（区间读把 total 一并带回）。
	probe, err := service.loadConversationPage(workspaceID, sessionID, 0, 1)
	if err != nil {
		return conversationPage{}, err
	}
	diskTotal := probe.diskTotal
	if diskTotal <= 0 {
		return conversationPage{diskTotal: diskTotal}, nil
	}
	span := window
	if span <= 0 {
		span = 1
	}
	for {
		start := diskTotal - span
		if start < 0 {
			start = 0
		}
		page, err := service.loadConversationPage(workspaceID, sessionID, start, diskTotal-start)
		if err != nil {
			return conversationPage{}, err
		}
		if len(page.rows) >= window || start == 0 {
			return page, nil
		}
		span *= 2
	}
}

// loadEarlierVisiblePage 读可见窗口之前的一页历史（「加载更早」的冷读面）。
//
// 冷读面的下标是**未过滤**空间，可见下标不能直接当冷读下标用：按可见下标读回来
// 的一页可能整段落在窗口之前，与窗口之间留一道谁也不显示的洞。这里从「可见下标
// 减一页」起读、按几何向右扩读，直到页里出现窗口首行的对位（接缝找到）——此时
// 页尾正好落在窗口之前，页里的可见行就是紧挨着窗口的那一页。
//
// 扩到「已发布的行全读回来」仍没有对位（窗口首行是在飞行，磁盘上根本没有），或
// 窗口为空（没有对位锚）时，返回读到的整段，交给安装路径按「宁可窗口短一页，也
// 不在列表中间留一段谁都没有的下标区间」处理。
func (service *Service) loadEarlierVisiblePage(workspaceID, sessionID string, offset, limit, window int, seam Message) (conversationPage, error) {
	start := offset - limit
	if start < 0 {
		start = 0
	}
	span := limit + window
	if span <= 0 {
		span = 1
	}
	for {
		page, err := service.loadConversationPage(workspaceID, sessionID, start, span)
		if err != nil {
			return conversationPage{}, err
		}
		if containsSameRow(page.rows, seam) || (start == 0 && page.reachesPublishedTail()) {
			return page, nil
		}
		if !hasRowIdentity(seam) {
			// 没有对位锚（窗口为空）：扩读没有意义。
			return page, nil
		}
		span *= 2
	}
}

// conversationPage 是一段冷读结果，**两个坐标空间分开记**：
//
//   - 冷读面（sessionstore 由 message 行直接展开 conversation）把内部标记行
//     （上下文 checkpoint、运行期提示……）也当成普通会话行，行数与下标都是
//     **未过滤**空间的；
//   - 可见会话（view.Conversation / view.HistoryOffset / view.TotalMessages）
//     一律是**可见**空间：内部标记行不占位。
//
// 两个空间不能相减相加——「窗口永远差几格够不到尾」「回到最新看不到最新」那组
// 缺陷的根就是拿未过滤总数当可见总数。磁盘的未过滤总数因此只回答一个问题：
// 这一页读到发布点了吗。
type conversationPage struct {
	rows      []Message
	rawRows   int
	diskTotal int
	start     int
}

// reachesPublishedTail 报告这一页读到了发布点（未过滤空间的右界）。
func (page conversationPage) reachesPublishedTail() bool {
	return page.rawRows > 0 && page.start+page.rawRows >= page.diskTotal
}

// historyPageInstall 描述一页历史如何安装进可见窗口。
type historyPageInstall int

const (
	// historyPagePrepend 更早一页：前置到当前窗口头部（窗口整体后退）。
	historyPagePrepend historyPageInstall = iota
	// historyPageReplace 回到最新：整窗替换为尾部窗口。
	historyPageReplace
)

// currentWorkspaceIDLocked 返回当前视图会话的 workspace ID（调用方持有
// Core.ViewMu）。
func currentWorkspaceIDLocked(service *Service) string {
	if service.Core.Snapshot.CurrentWorkspace == nil {
		return ""
	}
	return service.Core.Snapshot.CurrentWorkspace.ID
}

// loadConversationPage 读回一段历史：record conversation 模块优先（长会话翻页不
// 反序列化整份 state），旧格式会话回退 provider 历史区间。返回的 rows 已过滤到
// 可见空间，rawRows / diskTotal 则是**未过滤**空间的行数（读回面把内部标记行也
// 当普通 conversation 行，见 conversationPage）。
func (service *Service) loadConversationPage(workspaceID, sessionID string, offset, limit int) (conversationPage, error) {
	page := conversationPage{start: offset}
	if limit <= 0 {
		return page, nil
	}
	if store, ok := service.Deps.Sessions.(session_runtime.SessionConversationRangePort); ok {
		messages, count, err := store.LoadConversationRangeWorkspace(workspaceID, sessionID, offset, limit)
		if err != nil {
			return conversationPage{}, fmt.Errorf("load conversation range: %w", err)
		}
		page.rows = service.components.sessions.RecordConversation(SessionRecord{Conversation: ConversationRecord{Messages: messages}})
		page.rawRows = len(messages)
		page.diskTotal = count
		return page, nil
	}
	history, count, err := service.components.sessions.LoadSessionHistoryRange(workspaceID, sessionID, offset, limit)
	if err != nil {
		return conversationPage{}, fmt.Errorf("load history range: %w", err)
	}
	page.rows = make([]Message, 0, len(history))
	for _, msg := range history {
		if !isVisibleHistoryMessage(msg) {
			continue
		}
		page.rows = append(page.rows, adaptEngineMessage(msg))
	}
	page.rawRows = len(history)
	page.diskTotal = count
	return page, nil
}

// installVisibleHistory 安装一页可见历史：写会话可见投影（事实源）→ 收敛
// 窗口 → 镜像 Snapshot → bump 并发布快照变更。分页态因此随会话走，后续任何
// 镜像（新消息/工具事件/切换）都不会把它抹掉。
//
// 坐标口径（2026-09-29 修复）：view.Conversation / view.HistoryOffset /
// view.TotalMessages 一律是**可见**空间；冷读面回的行数与下标是**未过滤**空间
// （内部标记行也占位）。两者不能相减相加，因此这里只认三条事实：
//   - 可见总数以会话可见投影为准（冷加载基线 + 追加路径共同维护），读数不倒退
//     ——它是前端「下方还有 N 条新内容」的唯一依据，倒退既谎报到底，又让一个
//     并不贴尾的窗口被 append 路径判成贴尾，下一条消息就插进列表中间（断层）。
//     磁盘的未过滤总数只回答「这一页读到发布点了吗」，不参与总数；
//   - 窗口位置由**行身份**（接缝）推出：前置页接在窗口首行之前，尾页把窗口里
//     发布点之后的热尾（本轮尚未落盘的行）接回页尾。冷读一页短于请求时按请求量
//     和估计起点算出来的位置会与内容各说各话（旧实现的「窗口起点与内容脱节」）；
//   - 只拼得上的才拼：接缝对不上（窗口首行不在磁盘上——整窗都是在飞行）时不
//     假装连续，宁可窗口原地不动，也不在列表中间留一段谁都没有的下标区间；
//     一页可见行都没读到时保持现有窗口。
func (service *Service) installVisibleHistory(sessionID string, page conversationPage, window int, mode historyPageInstall) error {
	if window <= 0 {
		window = 1
	}
	service.ViewMu.Lock()
	for index := range page.rows {
		if page.rows[index].ID == "" {
			page.rows[index].ID = fmt.Sprintf("message-%d", service.components.view.NextMessageSeqForLocked(sessionID))
		}
	}
	service.components.view.SessionViewMutateLocked(sessionID, func(view *session.View) {
		memoryRows := durableConversationRows(view.Conversation)
		memoryStart := view.HistoryOffset
		var rows []Message
		start := memoryStart
		switch mode {
		case historyPageReplace:
			rows, start = replaceVisibleTail(page.rows, memoryRows, memoryStart, view.TotalMessages, window)
		case historyPagePrepend:
			rows, start = prependVisibleHistory(page.rows, memoryRows, memoryStart, window)
		}
		if len(rows) == 0 {
			// 一页可见行都没读到（尾段整段是内部标记行、或会话还没有可见正文）：
			// 保持现有窗口与总数，只如实校正「还有更早历史」。
			view.HasMoreHistory = memoryStart > 0
			return
		}
		view.Conversation = rows
		view.HistoryOffset = start
		view.HasMoreHistory = start > 0
		view.ConversationWindow = window
		// 本路径安装了可见正文（分页/冷回读）：「内容未加载」标志随之清除。
		view.ContentUnloaded = false
		// 窗口不得越过可见总数：可见总数只增不减（镜像/追加路径同一口径）。
		if tail := start + view_state.DurableConversationCount(view.Conversation); tail > view.TotalMessages {
			view.TotalMessages = tail
		}
	})
	service.mirrorActiveViewLocked()
	revision := service.bumpLocked()
	service.ViewMu.Unlock()
	service.publishSessionEvent(EventSnapshotChanged, revision, "", sessionID, nil)
	// 内容 LRU：分页/回读都是一次正文使用（移动到使用序最前并收敛超限）。
	service.touchContent(sessionID)
	return nil
}

// replaceVisibleTail 组装「回到最新 / 冷回读」的尾部窗口：以冷读页为底，把内存
// 窗口里发布点之后的那段热尾（本轮尚未落盘的行）按**行身份**接回页尾；页尾接上
// 了热尾时窗口右界就是内存窗口的右界（内存窗口贴着有效尾），否则右界是可见总数
// （窗口右界 = 已发布的最新一条可见行）。
//
// 两个右界都在可见空间里，读回面多出来的内部标记行不会让窗口「永远差几格」。
// 可见总数落后于磁盘已发布的可见行（同一会话由单一视图台账维护，正常不会发生）
// 时这里给出的是下界：窗口里仍是真实的最新一行，只有起点偏保守。
func replaceVisibleTail(pageRows, memoryRows []Message, memoryStart, memoryTotal, window int) ([]Message, int) {
	rows := append([]Message(nil), pageRows...)
	end := memoryTotal
	if at := indexOfSameRow(memoryRows, lastRow(rows)); at >= 0 {
		rows = append(rows, memoryRows[at+1:]...)
		end = memoryStart + len(memoryRows)
	}
	rows = view_state.BoundConversationTail(rows, window)
	visible := len(durableConversationRows(rows))
	if visible == 0 {
		return nil, memoryStart
	}
	start := end - visible
	if start < 0 {
		start = 0
	}
	return rows, start
}

// prependVisibleHistory 组装「加载更早」的窗口：页里接缝（窗口首行）之前的可见行
// 前置到窗口头部，窗口整体后退一页（不改总数——总数是可见空间里所有已可见行）。
//
// 接缝对不上（窗口首行不在页里、或它前面没有可见行）时返回空：宁可窗口原地不动，
// 也不在列表中间留一段谁都没有的下标区间。窗口为空（没有对位锚）时没有「连续」
// 可言：这一页就是窗口之前的一段，按调用方给的可见下标推定位置。
func prependVisibleHistory(pageRows, memoryRows []Message, memoryStart, window int) ([]Message, int) {
	if len(pageRows) == 0 {
		return nil, memoryStart
	}
	if len(memoryRows) == 0 {
		rows := view_state.BoundConversationTail(append([]Message(nil), pageRows...), window)
		start := memoryStart - len(durableConversationRows(rows))
		if start < 0 {
			start = 0
		}
		return rows, start
	}
	seam := indexOfSameRow(pageRows, memoryRows[0])
	if seam <= 0 {
		return nil, memoryStart
	}
	earlier := append([]Message(nil), pageRows[:seam]...)
	if len(earlier) > window {
		earlier = earlier[len(earlier)-window:]
	}
	rows := view_state.BoundConversationHead(append(earlier, memoryRows...), window)
	start := memoryStart - len(earlier)
	if start < 0 {
		start = 0
	}
	return rows, start
}

// visibleWindowFirstRow 返回目标会话可见窗口的首条 durable 行——前置一页要接在它
// 前面，所以它是「更早一页」的接缝对位锚（窗口为空时返回零值行：没有对位锚）。
func (service *Service) visibleWindowFirstRow(sessionID string) Message {
	var first Message
	service.components.view.SessionViewReadLocked(sessionID, func(view *session.View) {
		rows := durableConversationRows(view.Conversation)
		if len(rows) > 0 {
			first = rows[0]
		}
	})
	return first
}

// hasRowIdentity 报告一行是否带得出身份（空行无法对位）。
func hasRowIdentity(message Message) bool {
	return message.ID != "" || message.Content != "" || message.ReasoningContent != "" || message.Tool != nil
}

// sameByContent 按「角色 + 正文 + 思考」对位（没有稳定消息 ID 的行只能这样认）；
// 空行（占位 assistant、只有工具调用的 tool 行）不参与内容对位——两条空占位长得
// 一模一样，认错就把接缝挪了位：工具行按调用 ID 对位，其余空行一律不对位。
func sameByContent(left, right Message) bool {
	if left.Role != right.Role {
		return false
	}
	if left.Content != "" || left.ReasoningContent != "" {
		return left.Content == right.Content && left.ReasoningContent == right.ReasoningContent
	}
	if left.Tool != nil && right.Tool != nil {
		return left.Tool.ID != "" && left.Tool.ID == right.Tool.ID
	}
	return false
}

// indexOfSameRow 返回 rows 里第一条与 want 同身份的行下标（-1 = 没找到）。先按
// 消息 ID 认（同一条消息在内存窗口与磁盘行里同 ID，最稳）；ID 认不出来再退回内容
// 对位——旧格式 provider 历史行没有稳定 ID（每次读回都由调用方重新派号），只有
// 「角色 + 正文 + 思考」可比。
func indexOfSameRow(rows []Message, want Message) int {
	if want.ID != "" {
		for index := range rows {
			if rows[index].ID == want.ID {
				return index
			}
		}
	}
	for index := range rows {
		if sameByContent(rows[index], want) {
			return index
		}
	}
	return -1
}

// containsSameRow 报告 rows 里有没有与 want 同身份的行。
func containsSameRow(rows []Message, want Message) bool {
	return indexOfSameRow(rows, want) >= 0
}

// lastRow 返回最后一行（空序列返回零值行：与任何行都不是同一条）。
func lastRow(rows []Message) Message {
	if len(rows) == 0 {
		return Message{}
	}
	return rows[len(rows)-1]
}

// durableConversationRows 取出可见窗口里参与历史游标的行（system 引导行不占
// durable 下标空间，与 view_state.DurableConversationCount 同一口径）。
func durableConversationRows(messages []Message) []Message {
	rows := make([]Message, 0, len(messages))
	for _, message := range messages {
		if message.Role == "system" {
			continue
		}
		rows = append(rows, message)
	}
	return rows
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
