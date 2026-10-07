package core

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/RedHuang-0622/seelex/application/contract"
	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/application/core/session_runtime"
	"github.com/RedHuang-0622/seelex/application/event"
	"github.com/RedHuang-0622/seelex/application/prompt"
	"github.com/RedHuang-0622/seelex/session"
)

// transitionView 返回视图过渡锁（G5）：影响视图指针/当前视图会话生命周期
// 的命令共用视图 key（同会话串行、不同会话并行目前只适用于不触碰视图的
// 生命周期段；视图命令保持单例过渡，见 transitionForKey 注释）。
func (service *Service) transitionView() sync.Locker {
	return service.transitionForKey("")
}

// perSessionExecution 报告宿主是否具备逐会话执行能力（生产 seelebridge
// Runtime.PerSessionExecution=true；测试桩缺省 false 走旧语义）。
func (service *Service) perSessionExecution() bool {
	if provider, ok := service.Deps.Runtime.(interface{ PerSessionExecution() bool }); ok {
		return provider.PerSessionExecution()
	}
	return false
}

// transitionForSession 返回目标会话生命周期的过渡锁：逐会话宿主按会话 key
// 串行（不同会话并行），其余宿主回退视图 key。
func (service *Service) transitionForSession(sessionID string) sync.Locker {
	if !service.perSessionExecution() {
		return service.transitionView()
	}
	return service.transitionForKey(sessionID)
}

// newGeneratedSessionID 生成显式会话 ID（逐会话宿主 fork/切项目新建用；
// 原子序号防跨会话碰撞，不依赖引擎 StartSession 活跃别名）。
func (service *Service) newGeneratedSessionID(prefix string) string {
	seq := service.sessionIDSeq.Add(1)
	return fmt.Sprintf("%s_%d_%d", prefix, time.Now().UnixNano(), seq)
}

// setWorkspaceWriteScope 设置 legacy Router 写作用域。逐会话工具根能力
// （per-session project root）实现前，不得因 PerSessionExecution 跳过：
// 工具/工作树仍读进程级 projectScope，跳过会导致工作区绑定不生效。
func (service *Service) setWorkspaceWriteScope(workspaceID string) {
	service.Deps.Sessions.SetWorkspace(workspaceID)
}

// bindGlobalProjectRoot 设置进程级项目根。同上：per-session root 未实现前
// 必须绑定全局根，否则工作区会话的路径类工具失效。
func (service *Service) bindGlobalProjectRoot(rootPath string) error {
	return service.Deps.Runtime.BindProjectRoot(rootPath)
}

// unbindGlobalProjectRoot 清空进程级项目根。
func (service *Service) unbindGlobalProjectRoot() {
	service.Deps.Runtime.UnbindProjectRoot()
}

// bindSessionProjectRoot 把指定会话自己的项目根绑到工具面（按会话分格）。
// runChat 起点调用：多项目并行时，后台会话的 read_file/write_file/bash 只解析
// 自己的项目根，而不是视图会话的根（工作区污染回归）。
//
// 未绑定工作区的会话保持旧语义（工具面回退进程默认根）；绑定失败只记日志，
// 不阻塞回合——失败时该会话解析回退默认根，与修复前行为一致。
func (service *Service) bindSessionProjectRoot(sessionID string) {
	if service == nil || service.Deps.Runtime == nil || service.Deps.Workspace == nil {
		return
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	workspace, ok := service.Deps.Workspace.SessionWorkspace(sessionID)
	if !ok || strings.TrimSpace(workspace.RootPath) == "" {
		return
	}
	if err := service.Deps.Runtime.BindProjectRootFor(sessionID, workspace.RootPath); err != nil {
		log.Printf("[workspace] 会话 %s 的项目根绑定失败（工具面回退默认根）：%v", sessionID, err)
	}
}

// writeSessionSystemPrompt 把当前 prompt 栈写入**目标会话**的引擎：会话路由
// 宿主走 SetSystemPromptFor，绝不触碰全局活跃别名——运行中会话的引擎锁可能
// 被 ChatStream 全程持有，写全局别名会排队到那个会话跑完（用户视角的应用冻结）。
//
// 新会话的引擎实例是**新建**的，必须显式写一次：ApplyActiveTaskSystemPromptFor
// 的"文本没变就不写"缓存会跳过写入，空 prompt 就此留在新引擎上。切换/冷恢复
// 路径与新建路径共用这一份判据（此前三处各写一遍）。
func (service *Service) writeSessionSystemPrompt(sessionID string) {
	if service == nil || service.Deps.Engine == nil || service.promptStack == nil {
		return
	}
	promptText := service.promptStack.Render()
	if routed, ok := service.Deps.Engine.(interface{ SetSystemPromptFor(string, string) }); ok {
		routed.SetSystemPromptFor(sessionID, promptText)
		return
	}
	service.Deps.Engine.SetSystemPrompt(promptText)
}

// openSessionEngine 给一个**新会话**装上它自己那一格：建引擎 bundle、挂接
// 自己的 context store、写自己的 system prompt，并在给了工作区时记录会话级
// 项目绑定（workspace.Repo 绑定 + framework 显式键）。返回实际生效的会话 ID
// （多会话宿主 = 传入的早分配 SID；legacy 单会话引擎按引擎自分配的 ID）。
//
// 它刻意不碰**进程级**执行面（全局工程根 / Router 写作用域）：那条面属于
// "当前视图会话"，后台新建的会话必须留给 runChat 起点的 bindSessionProjectRoot
// 按会话分格处理——否则新建一个后台会话就会把别的运行中会话的项目根改掉。
//
// 草稿物化（首次提交）与定时任务新建会话共用这一份；两处的差别只在"谁来定
// 那个 ID"与"要不要顺带换视图指针"，不在这几行装配本身。
func (service *Service) openSessionEngine(sessionID string, workspace *WorkspaceInfo) (string, error) {
	newID := strings.TrimSpace(sessionID)
	if activator, ok := service.Deps.Engine.(interface{ ActivateSession(string) error }); ok {
		// 会话路由宿主：按早分配 SID 显式创建引擎 bundle（草稿阶段
		// HasSession=false，此刻才建）。
		if err := activator.ActivateSession(newID); err != nil {
			return "", fmt.Errorf("create engine session %q: %w", newID, err)
		}
	} else {
		newID = strings.TrimSpace(service.Deps.Engine.StartSession())
		if newID == "" {
			return "", errors.New("engine returned an empty session ID")
		}
	}
	// framework DurableHistory 按会话 workspace 显式键落盘（R3 键漂移收敛）。
	if workspace != nil {
		service.Deps.Runtime.SetSessionWorkspace(newID, workspace.ID)
	} else {
		service.Deps.Runtime.SetSessionWorkspace(newID, "")
	}
	// 新会话绑定**它自己的** context store（与 resume 同一条挂接路径）。这里不能
	// 只解绑：解绑状态下这个会话的整段第一生命周期都推不了压缩帧、也没有任何栈块
	// （见 attachSessionContextFor 的注释与 2026-10-04 现场）。早分配 SID 就是本会话
	// 的最终键，全新键上的 Load 只落到空记录——既不继承上一个会话的四栈，也不多写。
	if err := service.attachSessionContextFor(session_runtime.WorkspaceID(workspace), newID); err != nil {
		return "", err
	}
	service.writeSessionSystemPrompt(newID)
	if workspace != nil && service.Deps.Workspace != nil {
		service.Deps.Workspace.BindSession(newID, workspace.ID)
	}
	return newID, nil
}

// transitionForKey 返回指定 key 的会话过渡锁（G5 per-session keyed）：会
// 话路由引擎（SessionChatEngine）下，key=会话 ID 表示该会话生命周期命令
// 串行、不同会话可并行；非路由单会话引擎一律归一到视图 key——该宿主没有
// 跨会话并行面，任何 per-session 拆分都会重新引入进程级引擎串写。
func (service *Service) transitionForKey(key string) sync.Locker {
	if _, routed := service.Deps.Engine.(contract.SessionChatEngine); !routed {
		return service.components.sessions.TransitionLock("")
	}
	return service.components.sessions.TransitionLock(key)
}

// sessionUnitLocked 返回指定会话的会话单元（聊天运行态已收进 SessionUnit，
// 9.5 起 core 直接经会话单元接入；按需创建）。
// 调用方必须持有 Core.ViewMu；域内锁序 Core.ViewMu → Domain.mu → Unit.mu，不反向。
func (service *Service) sessionUnitLocked(sessionID string) *session.SessionUnit {
	unit := service.sessions.Unit(sessionID)
	if unit == nil {
		if sessionID == "" {
			unit = session.NewDraftUnit()
		} else {
			unit, _ = session.NewSessionUnit(sessionID)
		}
		service.sessions.Register(unit)
	}
	return unit
}

// currentViewSessionID 返回当前视图会话 ID（读锁内快照；供解锁后发布
// 视图级事件时携带 sid——早分配 SID 后订阅键恒等于视图 ID，空 sid 不再
// 是"跟随视图"的隐式通配）。
func (service *Service) currentViewSessionID() string {
	service.ViewMu.RLock()
	defer service.ViewMu.RUnlock()
	return service.Core.Snapshot.Session.ID
}

// effortForSession 返回指定会话生效的 effort 级别（G4：Unit 内选择优先；
// 未选择回退进程级默认 effortManager.Current）。不持有 Core.ViewMu——Unit
// 自带锁，进程默认由 EffortManager 自身锁保护。
func (service *Service) effortForSession(sessionID string) string {
	if unit := service.sessions.Unit(sessionID); unit != nil {
		if level := unit.EffortLevel(); level != "" {
			return level
		}
	}
	return service.effortManager.Current()
}

// syncPlanPolicyFor 按会话 effort 向引擎写入该会话的 plan 策略槽（G1-C：
// plan_load/plan_run 在引擎内按执行 ctx 的会话读取自己的槽；chat 起点同步，
// 保证每个会话（含后台）都携带自己的额度，而不是继承进程默认槽）。
func (service *Service) syncPlanPolicyFor(sessionID string) {
	if service == nil || service.Deps.Runtime == nil {
		return
	}
	service.Deps.Runtime.SetPlanPolicyFor(sessionID, prompt.PlanningPolicy(service.effortForSession(sessionID)))
}

// syncSessionReasoningEffort 把档位对应的 wire 思考强度下发给**跟随会话**的账号
// （`reasoning_effort: session`），返回被改动的账号数。
//
// 与 syncPlanPolicyFor 的分工：plan 策略槽按会话写进引擎（每个会话一份），而思考
// 强度落在**账号客户端**上、账号池是进程级共享的（一个账号一个 ChatClient），所以
// 这里不按会话分格——只在档位变化时下发一次，多个会话各自的档位在下发口上按"后切
// 者生效"落到同一批账号（账号池本来就是进程级共享，不是本次引入的语义）。
//
// 已知边界：只在档位**变化**时下发。进程刚起来时 session 账号保持空值（= 不下发，
// provider 用自己的默认），第一次切档才与用户所选档位对齐——补这个口需要第二个调用
// 点（装配期播种），而这条链刻意只保留一个调用点。
//
// 映射不在这里：档位 → provider 词表只有一份实现（application/prompt 的
// effortProfiles，经 prompt.ReasoningEffortFor 读），这里只负责"把值送到账号池"。
// 端口是窄可选的（contract.ReasoningEffortPort）：宿主没实现就整步退化为 no-op，
// 不影响 loop/预算那半条链。
func (service *Service) syncSessionReasoningEffort(level string) int {
	if service == nil || service.Deps.Runtime == nil {
		return 0
	}
	sink, ok := service.Deps.Runtime.(contract.ReasoningEffortPort)
	if !ok {
		return 0
	}
	return sink.SetSessionReasoningEffort(prompt.ReasoningEffortFor(level))
}

// permissionTierForSession 返回指定会话生效的权限档位（G4：Unit 内选择优先；
// 未选择回退进程默认档位）。不持有 Core.ViewMu——Unit 自带锁，进程默认由
// service.permissionTierDefault 提供。
func (service *Service) permissionTierForSession(sessionID string) string {
	if unit := service.sessions.Unit(sessionID); unit != nil {
		if tier, ok := unit.PermissionTier(); ok && tier != "" {
			return tier
		}
	}
	if service == nil {
		return dto.PermissionTierManual
	}
	if service.permissionTierDefault != "" {
		return service.permissionTierDefault
	}
	return dto.PermissionTierManual
}

// fullAccessForSession 是档位的**兼容派生读面**：full 档 ⇔ 旧的全权开启。
func (service *Service) fullAccessForSession(sessionID string) bool {
	return dto.PermissionTierIsFullAccess(service.permissionTierForSession(sessionID))
}

// syncFullAccessFor 按会话档位同步执行门（G4：chat 起点调用，保证每个
// 会话都按自己的选择运行——后台/新会话不继承其它会话的遗留档位）。
//
// 门是**按会话解析**的（seelebridge PermissionGate.sessionTier）：本调用只写
// 目标会话那一格，不会覆盖别的会话已生效的档位——历史缺陷正是「进程级单布尔
// + 起点同步」，B 会话起跑会把 A 会话的全权关掉，用户看到「点了全权仍弹审批/
// 仍被拒」。
func (service *Service) syncFullAccessFor(sessionID string) {
	if service == nil || service.Deps.Runtime == nil {
		return
	}
	_ = service.Deps.Runtime.SetPermissionTierFor(sessionID, service.permissionTierForSession(sessionID))
}

// anyChatRunningLocked 报告是否存在任意会话的运行中聊天。M1 单飞执行
// 闸门依赖它：切换会话/新建会话必须等所有会话空闲；同会话二次提交仍走
// 会话内队列。
func (service *Service) anyChatRunningLocked() bool {
	for _, sid := range service.sessions.UnitIDs() {
		unit := service.sessions.Unit(sid)
		if unit != nil && unit.ChatState().Running {
			return true
		}
	}
	return false
}

// mirrorActiveChatLocked 把当前活跃会话的聊天运行态写入会话 view（阶段 1：
// Snapshot.Chat 由 view 统一镜像；切换会话后调用保证前端读到活跃会话状态）。
func (service *Service) mirrorActiveChatLocked() {
	sessionID := service.Core.Snapshot.Session.ID
	if unit := service.sessions.Unit(sessionID); unit != nil {
		service.setSessionChatLockedFor(sessionID, unit.ChatState())
	} else {
		service.setSessionChatLockedFor(sessionID, ChatState{})
	}
}

// queuedChatRequests 把会话域排队输入（不透明载荷）还原为执行内核的
// chatRequest 列表（排序一致；无法还原的载荷跳过）。
func queuedChatRequests(requests []session.QueuedRequest) []chatRequest {
	out := make([]chatRequest, 0, len(requests))
	for _, request := range requests {
		if payload, ok := request.Payload.(chatRequest); ok {
			out = append(out, payload)
		}
	}
	return out
}

// activeQueuedChatRequestsLocked 返回当前会话域的排队输入（还原为执行内核
// 的 chatRequest；调用方持有 Core.ViewMu）。会话域是队列唯一所有者。
func (service *Service) activeQueuedChatRequestsLocked() []chatRequest {
	unit := service.sessions.Unit(service.Core.Snapshot.Session.ID)
	if unit == nil {
		return nil
	}
	return queuedChatRequests(unit.PendingRequests())
}

// publishSessionEvent 发布事件；装配的 EventHub 支持会话路由时携带
// sessionID，否则退化为普通 Publish。
func (service *Service) publishSessionEvent(kind event.EventKind, revision uint64, requestID, sessionID string, payload any) event.Event {
	if hub, ok := service.Events.(event.SessionAwareHub); ok {
		return hub.PublishSession(kind, revision, requestID, sessionID, payload)
	}
	return service.Events.Publish(kind, revision, requestID, payload)
}

// publishViewSessionChanged 通告权威视图会话已被应用内部切换（进程级事件、
// 空 sid，投递给所有订阅者）。只用于**后台异步**改回视图会话的场景（运行中
// 冷恢复失败：hot attach 回切换前会话，或退化为新建草稿）——此时 GUI Bridge
// 的事件订阅键仍钉在失败目标会话上，其会话级订阅已永远沉默，渲染层停在
// restoring 空壳（输入区禁用、消息发不出去）。同步切换（BeginNewSession /
// ResumeSession / Fork / Activate）由 Bridge 调用方自己重订阅，不需要本事件。
func (service *Service) publishViewSessionChanged() {
	service.ViewMu.RLock()
	revision := service.Core.Snapshot.Revision
	service.ViewMu.RUnlock()
	service.publishSessionEvent(EventViewSessionChanged, revision, "", "", nil)
}

// publishChatStateFor 下发指定会话的权威聊天运行态（chat.changed）。运行/排队
// 是后端口径，客户端不得从"收到增量事件"反推自己是否在跑。
//
// 载荷刻意不带 revision（=0）：本事件是运行态的整体替换，而同一批转换往往先
// 发 snapshot.changed（触发客户端重拉并把 revision floor 抬到当前值），带
// revision 的 chat.changed 会被协议层判为"已由权威快照表示"而丢弃。
// 只读会话单元自有状态，因此必须在释放 Core.ViewMu 之后调用。
func (service *Service) publishChatStateFor(sessionID string) {
	unit := service.sessions.Unit(sessionID)
	if unit == nil {
		return
	}
	chat := unit.ChatState()
	service.publishSessionEvent(EventChatChanged, 0, chat.RequestID, sessionID, chat)
}

// bindProjectRootIfSafe 在安全条件下重绑全局项目根（P3/G5 收口）：
//   - 任意会话运行中 → 一律不重绑。全局 projectScope 是进程级执行面（工具/
//     工作树仍读全局根），在途会话的任何一次路径工具调用都依赖它；即使目标
//     是当前视图会话（异步冷恢复把视图先切到目标、原会话仍在后台跑），重绑
//     也会让后台运行中会话的后续工具解析到新项目根（跨会话串写）。
//   - 无任何会话运行中 → 可安全重绑（当前视图会话的工具需要正确根）。
//
// 返回是否已绑定；未绑定时调用方必须跳过全局 SetWorkspace（Router 写作用域
// 同样全局，不能为后台会话切换）。运行中跳过的重绑在进程回到空闲后由
// rebindViewWorkspaceWhenIdle 在 runChat 收尾统一对齐到当前视图会话。
func (service *Service) bindProjectRootIfSafe(_ string, rootPath string) bool {
	service.ViewMu.RLock()
	anyRunning := service.anyChatRunningLocked()
	service.ViewMu.RUnlock()
	if anyRunning {
		return false
	}
	if service.Deps.Runtime == nil {
		return false
	}
	if err := service.Deps.Runtime.BindProjectRoot(rootPath); err != nil {
		return false
	}
	return true
}

// rebindViewWorkspaceWhenIdle 在进程变为完全空闲后，把全局项目根/Router 写
// 作用域对齐到当前视图会话的工作区：运行期间为保护在途会话跳过的重绑在这里
// 补齐，避免"切到的会话工具仍指向上一个运行会话的项目根"（后台收尾即触发）。
// 当前视图会话无工作区时清掉运行期可能残留的全局根。调用方不得持有 Core.ViewMu。
func (service *Service) rebindViewWorkspaceWhenIdle() {
	if service == nil || service.Deps.Workspace == nil {
		return
	}
	service.ViewMu.RLock()
	sessionID := service.Core.Snapshot.Session.ID
	service.ViewMu.RUnlock()
	if workspace, ok := service.Deps.Workspace.SessionWorkspace(sessionID); ok {
		if service.bindProjectRootIfSafe(sessionID, workspace.RootPath) {
			service.setWorkspaceWriteScope(workspace.ID)
		}
		return
	}
	service.ViewMu.RLock()
	idle := !service.anyChatRunningLocked()
	service.ViewMu.RUnlock()
	if !idle {
		return
	}
	service.unbindGlobalProjectRoot()
	service.setWorkspaceWriteScope("")
}

// SubmitToSession 是会话级提交 API（M2：多会话并行执行）。目标会话即活跃
// 会话时等价 Submit（同会话运行中排队）；目标会话为其它会话且已加载（引擎
// 已实例化）时，在该会话上下文中后台并行启动（不切换活跃会话）；未加载的
// 会话先切换恢复再提交（兼容 M1 语义）。
func (service *Service) SubmitToSession(ctx context.Context, sessionID, text string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return errors.New("session ID is required")
	}
	// 路由只认显式 sessionID，绝不重读「当前会话」：SubmitToSession 与
	// 切换（ResumeSession/hot_attach）之间存在 TOCTOU 窗口，若先读 current
	// 再委托 Submit（内部再读一次 current），切换落在两次读之间会把 A 的
	// 输入路由进 B 的队列（压力测试 TestStressConcurrentSessionsDoNotPollute
	// 抓到：queued-2 进入 sess-4 视图）。
	// 草稿门：显式目标可能就是视图里那份尚未物化的草稿。草稿没有可冷回读的历史，
	// 必须先物化（建引擎 bundle、绑项目、清 composer），否则下面"目标未加载"的
	// 判定会把它当成冷会话去 ActivateSession→冷加载那条 draft record。
	if err := service.materializeDraftForSubmit(sessionID, text); err != nil {
		return err
	}
	// sessionLoaded 的活跃会话回退分支会读 Snapshot.Session.ID（引擎端口不支持
	// 会话路由时），本方法是锁外公开入口，因此在这里持 Core.ViewMu 读锁取值。
	service.ViewMu.RLock()
	loaded := service.sessionLoaded(sessionID)
	service.ViewMu.RUnlock()
	if !loaded || service.sessionContentUnloaded(sessionID) {
		// 目标会话未加载，或可见正文已被内容 LRU 卸载：切换恢复（含正文冷
		// 回读）后再提交——否则新回合的可见消息会落进一个没有窗口的会话视图。
		// ActivateSession 持 TransitionLock，完成后目标即当前会话，后续显式
		// 路由不依赖 current。
		if err := service.ActivateSession(sessionID); err != nil {
			return err
		}
	}
	// 上面这次激活可能走**异步冷加载**（有其它会话运行中）：RPC 已把视图切到
	// restoring 空壳就返回、装载在后台。此时不能立即开回合（restoring 期间不得
	// 开新回合），也不能阻塞等待（本 API 契约是非阻塞后台启动，见
	// TestParallelSessionsExecuteConcurrently）——submitConversationFor 收到
	// restoring 时会自动把提交挂到装载完成点（deferSubmitUntilRestored）。
	return service.submitConversationFor(ctx, sessionID, text)
}

// awaitRestore 等目标会话的后台冷加载结束（restoring 清除）后返回。多会话
// 并发装载时信号按“集合有变化”广播，收到后重读集合复判；ctx 取消即放弃。
func (service *Service) awaitRestore(ctx context.Context, sessionID string) error {
	for {
		service.ViewMu.Lock()
		restoring := service.isRestoringLocked(sessionID)
		signal := service.restoreSignalLocked()
		service.ViewMu.Unlock()
		if !restoring {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-signal:
		}
	}
}

// deferSubmitUntilRestored 把一次对话提交挂到后台冷加载完成点：restoring 期间
// 不得开新回合，而提交入口又不得阻塞（交互/后台两条路径都要保持“立即受理”），
// 因此后台等装载完成（或失败清除 restoring）后再提交。失败只记日志——装载失败
// 时目标会话本身不可用，与既有“后台提交失败”口径一致。
func (service *Service) deferSubmitUntilRestored(ctx context.Context, sessionID, text string) {
	go func() {
		if err := service.awaitRestore(ctx, sessionID); err != nil {
			return
		}
		if err := service.submitConversationFor(ctx, sessionID, text); err != nil {
			log.Printf("[submit] deferred submit to %q after restore: %v", sessionID, err)
		}
	}()
}

// sessionLoaded 报告目标会话引擎是否已实例化（后台提交前置检查）。
func (service *Service) sessionLoaded(sessionID string) bool {
	if routed, ok := service.Deps.Engine.(contract.SessionChatEngine); ok {
		return routed.HasSession(sessionID)
	}
	return sessionID == service.Core.Snapshot.Session.ID
}

// ActivateSession 切换当前展示/执行会话。M1 没有每会话驻留快照，切换即
// 恢复（resumeSession 从持久化重建）；运行中切换被拒绝，保证共享
// Snapshot 不串写。多页签并行驻留 = M2/M3。
func (service *Service) ActivateSession(sessionID string) error {
	return service.resumeSession(sessionID)
}

// SnapshotOf 返回指定会话的权威**会话快照**（G3 分型：SessionSnapshot，
// 传输完备且只含本会话事实——不带进程级目录/工作区/能力清单；进程制品见
// ProcessSnapshot）。活跃会话与其它已驻留（LIVE）会话走同一条组装路径：
// 会话身份/可见对话/聊天运行态/会话运行原件（本会话槽）/task/工作表格/
// 子代理树/本会话 revision（INV-G5，与进程 revision 分离）。
func (service *Service) SnapshotOf(sessionID string) (SessionSnapshot, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return SessionSnapshot{}, errors.New("session ID is required")
	}
	// C1 冷读面：未驻留（unit 不存在或 Resident=false）的会话走持久化基线，
	// 不再 clone 视图 Runtime 冒充其它会话（M4 撤销）。
	service.ViewMu.RLock()
	unit := service.sessions.Unit(sessionID)
	resident := service.sessionResidentLocked(unit, sessionID)
	viewSession := service.Core.Snapshot.Session.ID
	service.ViewMu.RUnlock()
	if !resident {
		return service.snapshotOfCold(sessionID)
	}
	if sessionID != viewSession && service.sessionContentUnloaded(sessionID) {
		// 可见正文已被内容 LRU 卸载的驻留会话：快照走持久化基线（只读 record，
		// 不把正文拉回内存，也不解除「内容未加载」状态）；磁盘不可读时才退回
		// 内存快照（宁可少一份基线，也不因内存治理报「快照不可用」）。
		if snapshot, err := service.snapshotOfCold(sessionID); err == nil {
			return snapshot, nil
		}
	}
	return service.snapshotOfResident(sessionID)
}

// sessionResidentLocked 判定目标会话引擎是否驻留（SnapshotOf 热/冷分界；
// 调用方持有 Core.ViewMu）。会话路由宿主看 Unit.Resident + 引擎
// HasSession（驱逐/归档后 false → 冷读）；legacy 单会话宿主没有 bundle
// 概念，其唯一引擎实例挂在视图会话上，视图会话即视为驻留（不触发冷读，
// 维持既有 active SnapshotOf 语义）。
func (service *Service) sessionResidentLocked(unit *session.SessionUnit, sessionID string) bool {
	if unit == nil {
		return false
	}
	if routed, ok := service.Deps.Engine.(interface{ HasSession(string) bool }); ok {
		if routed.HasSession(sessionID) {
			// 引擎 bundle 在内存 = 驻留（驱逐/归档先 UnloadSession，
			// HasSession 随即为 false → 冷读）。Unit.Resident 只用于 LRU
			// 诊断序，不作为快照热/冷的事实源。
			return true
		}
	}
	// 引擎 bundle 缺失：当前视图会话（含初始占位与未物化草稿）仍有内存快照
	// 语义，按热路径处理（空 record 的占位会话也不该误报"不可用"）；其它
	// 会话单元（驱逐后 Resident=false）显式 SnapshotOf 走持久化基线。
	return sessionID == service.Core.Snapshot.Session.ID
}

// snapshotOfResident 组装驻留会话（引擎 bundle 在内存）的会话快照：走单元
// Runtime 槽/View/协调器每会话投影。调用方无需持有 Core.ViewMu（本方法自取）。
func (service *Service) snapshotOfResident(sessionID string) (SessionSnapshot, error) {
	// 宿主读面（审批表 / 任务注册表 / 后台作业投影）在 ViewMu **之外**采样：
	// AsyncRunsSnapshot() 会对每条后台作业做 stat + 读日志末窗（文件 I/O），
	// TaskSnapshot() 同样是宿主实现。持进程级视图锁跑它们 = 把整块交互面押在一次
	// 慢活上；更坏的情况是宿主实现回调进 Core.ViewMu（归档器当年就是"读
	// app.Snapshot()"）——RWMutex 写者优先下，持读锁再取读锁就是自己等自己，
	// 永久挂死（2026-09-29 锁面审计 §2.4/§2.5）。
	var approvals []Interaction
	if service.Approval != nil {
		approvals = service.Approval.PendingBySession(sessionID)
	}
	asyncRuns := service.asyncRunsForTable()
	taskRecords := service.Deps.Runtime.TaskSnapshot()

	service.ViewMu.RLock()
	defer service.ViewMu.RUnlock()
	unit := service.sessions.Unit(sessionID)
	if unit == nil {
		return SessionSnapshot{}, ErrSessionSnapshotUnavailable
	}
	view := service.components.view.SessionViewLocked(sessionID)
	name := service.components.sessions.SessionTitleFor(sessionID).Value
	runtime := unit.RuntimeState()
	revision := unit.SnapshotRevision()
	if revision == 0 {
		revision = service.Core.Snapshot.Revision
	}
	snapshot := SessionSnapshot{
		ProtocolVersion:    ProtocolVersion,
		Revision:           revision,
		Session:            SessionState{ID: sessionID, Name: name},
		Conversation:       append([]Message(nil), view.Conversation...),
		Chat:               unit.ChatState(),
		Runtime:            sessionRuntimeOf(runtime),
		Capabilities:       Capabilities{SessionResume: true, SessionSnapshot: true},
		Resident:           true,
		HistoryOffset:      view.HistoryOffset,
		TotalMessages:      view.TotalMessages,
		HasMoreHistory:     view.HasMoreHistory,
		ConversationWindow: view.ConversationWindow,
		ReadFiles:          append([]ReadFileRef(nil), view.ReadFiles...),
	}
	if len(approvals) > 0 {
		snapshot.Approvals = append([]Interaction(nil), approvals...)
	}
	if plan := service.planProjectionLocked(sessionID); plan != nil {
		snapshot.Runtime.Plan = clonePlanForSync(plan)
	}
	if task := service.components.tasks.VisibleTaskStateFor(sessionID); task != nil {
		taskCopy := *task
		taskCopy.ContextCompactions = append([]ContextCompaction(nil), task.ContextCompactions...)
		snapshot.Task = &taskCopy
	}
	// 工作表格是项目/全局台账（与视图投影同源）：会话快照里的表格也取全局
	// 读面，避免"切到哪个会话才看到哪些行"。会话级读面（落盘）仍走
	// TaskSnapshotFor。
	if len(taskRecords) > 0 || len(asyncRuns) > 0 {
		rows := buildWorkTable(snapshot.Runtime.Plan, taskRecords, nil, asyncRuns)
		snapshot.Runtime.WorkTable = rows
		snapshot.Runtime.WorkTableBatches = buildWorkTableBatches(rows)
	}
	return snapshot, nil
}

// sessionRuntimeOf 从全量 RuntimeState 投影提取会话专属运行原件（G3 字段
// 归属表：进程级字段 model/provider/plugin/accounts/skills/tools/scheduled
// 不进入 SessionSnapshot）。
func sessionRuntimeOf(runtime RuntimeState) SessionRuntime {
	return SessionRuntime{
		Effort:           runtime.Effort,
		FullAccess:       runtime.FullAccess,
		PermissionTier:   runtime.PermissionTier,
		Tokens:           runtime.Tokens,
		Replan:           runtime.Replan,
		Plan:             cloneRuntimeState(RuntimeState{Plan: runtime.Plan}).Plan,
		TodoItems:        append([]dto.TodoItem(nil), runtime.TodoItems...),
		SubAgentTree:     append([]dto.SubAgentTreeNode(nil), runtime.SubAgentTree...),
		GoalSkillActive:  runtime.GoalSkillActive,
		GoalGovernance:   cloneGoalGovernanceView(runtime.GoalGovernance),
		ActiveSkills:     append([]string(nil), runtime.ActiveSkills...),
		WorkTable:        CloneWorkItems(runtime.WorkTable),
		WorkTableBatches: CloneWorkTableBatches(runtime.WorkTableBatches),
	}
}

func cloneGoalGovernanceView(view *dto.GoalGovernanceView) *dto.GoalGovernanceView {
	if view == nil {
		return nil
	}
	copyView := *view
	// 收口账本是切片：克隆必须断开共享，否则一个视图的消费者能改到另一个视图的
	// 账本（会话快照按会话隔离的前提）。
	copyView.History = append([]dto.GoalHistoryView(nil), view.History...)
	return &copyView
}

// sessionEventFilter 构造会话级订阅谓词（口径见 SubscribeSession）。
func (service *Service) sessionEventFilter(sessionID string) func(event.Event) bool {
	if sessionID != "" {
		return func(event Event) bool {
			// G2/M2：进程类事件（resync.required / app.exit_requested）以空
			// sid 投递给全部订阅者；会话类事件必须 sid 精确匹配——空 sid
			// 不再作为显式会话订阅的通配（草稿占位由下方空订阅视图口径承接，
			// 待 G4 早分配 SID 后彻底移除）。
			if event.Kind == EventResyncRequired || event.Kind == EventExitRequested {
				return event.SessionID == ""
			}
			return event.SessionID == sessionID
		}
	}
	return func(event Event) bool {
		if event.Kind == EventResyncRequired || event.Kind == EventExitRequested {
			return event.SessionID == ""
		}
		// 草稿视图（空 sid）过渡口径：事件归属当前视图会话或仍为空占位。
		// G4 早分配 SID 后本分支只保留进程类判定。
		//
		// §2.12（未采用，设计级）：这里每次发布都经 actor 取 V，处于 hub 的
		// publishMu + subscriber.mu 内。曾试"在 session.Domain 里加 V 的无锁
		// 镜像"，被 T5.5 静态不变量（TestGlobalSharedFaceBounded：Domain 只允许
		// channel 字段，G 之外不得有共享可变状态）挡下；改动需先改架构决定。
		activeID := service.sessions.ActiveID()
		return event.SessionID == "" || event.SessionID == activeID
	}
}

// SubscribeSessionWithReplay 与 SubscribeSession 同一归属口径，但订阅附带
// replayWindow 条重放窗口：缓冲写满时事件不丢，落后的消费者可按 delivery_seq
// 增量补取（Subscription.ReplaySince）。装配的 hub 不支持窗口时退化为
// SubscribeSession 的旧溢出语义。
func (service *Service) SubscribeSessionWithReplay(sessionID string, buffer, replayWindow int) (Subscription, error) {
	hub, ok := service.Events.(interface {
		SubscribeWithReplay(func(event.Event) bool, int, int) Subscription
	})
	if !ok {
		return service.SubscribeSession(sessionID, buffer)
	}
	return hub.SubscribeWithReplay(service.sessionEventFilter(strings.TrimSpace(sessionID)), buffer, replayWindow), nil
}

// SubscribeSession 返回按会话过滤的事件订阅（只投递该会话或全局事件）。
//
// sessionID 为空表示「跟随当前视图会话」：草稿尚无真实 ID（首次提交才由引擎
// 生成），客户端无从预知，因此视图归属只能由本服务判定——session.Domain 是视
// 图指针的唯一持有者，草稿物化/切换的瞬间谓词即随之改变，不存在事件空洞。
// 显式 sessionID 为严格过滤（多页签回看、headless 观察其它会话）。
// 底层 Hub 不支持会话订阅时返回错误。
func (service *Service) SubscribeSession(sessionID string, buffer int) (Subscription, error) {
	hub, ok := service.Events.(interface {
		SubscribeFiltered(func(event.Event) bool, int) Subscription
	})
	if !ok {
		return Subscription{}, errors.New("event hub does not support session-scoped subscriptions")
	}
	return hub.SubscribeFiltered(service.sessionEventFilter(strings.TrimSpace(sessionID)), buffer), nil
}
