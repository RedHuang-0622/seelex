package core

// 内容 LRU：resident_lru.go 管「引擎 bundle 是否驻留」，本文件管**第二层**
// 内存——已加载会话的可见正文（View.Conversation 的窗口）。会话数一多，每个
// 窗口（limits.history_window 条消息 + 工具输出）都会常驻内存；内容上限走
// seelexctx.Limits.loaded_content_limit（默认 12）。候选必须排除：当前视图
// 会话、running/queued/awaiting_approval（含待批与排队输入）、restoring、
// 本已无正文的会话。
//
// 驱逐动作**只卸载正文**：清空 View.Conversation 并置 View.ContentUnloaded
// （「内容未加载」标志），会话事实/元数据/标题/统计/窗口标志（TotalMessages/
// HistoryOffset/HasMoreHistory/ConversationWindow/Chat/ReadFiles）全部保留；
// 卸载前先 flush（内存正文里可能还有未落盘的收尾消息），磁盘是唯一事实源，
// 不丢数据。驱逐后请求目录刷新——左侧列表条目与标题来自存储层枚举的会话头
// （零正文读），条目不会消失，刷新也不会把正文拉回内存。
//
// 回读（reloadSessionContent）复用既有分页读回路径：被驱逐后再激活
// （ActivateSession/ResumeSession 的 hot_attach 分支）或分页
// （LoadMoreHistory/LoadLatestHistory）时按保留的窗口标志从磁盘整窗回读。
//
// 锁与死锁说明（刻意与 resident_lru.go 不同）：本文件在卸载候选时**不取**
// 目标会话的过渡锁。touchContent 的调用方（resumeSession/hotAttachSession/
// resumeSessionCold）自己就持有目标会话的过渡 key，若收敛再去阻塞申请另一个
// 会话的 key，两个并发激活（A 持 X 等 Y、B 持 Y 等 X）会构成 ABBA 死锁。
// 卸载只动内存 + 先写盘，状态判定与改动一律在 Core.ViewMu 临界区内二次确认
// （挑选与实际卸载之间状态可能已变，二次确认即跳过），因此不依赖过渡锁。

import (
	"fmt"
	"log"

	"github.com/RedHuang-0622/seelex/seelexctx"
	"github.com/RedHuang-0622/seelex/session"
)

// loadedContentLimit 把配置值收敛为生效上限（<=0 = 未配置 → 默认值；与
// resident_lru.go 同一兜底语义）。
func loadedContentLimit(configured int) int {
	if configured <= 0 {
		return seelexctx.DefaultLimits().LoadedContentLimit
	}
	return configured
}

// touchContent 记录一次会话可见正文使用（冷加载完成/热挂载/分页读回/回读），
// 并触发超限卸载。调用方不得持有 Core.ViewMu（本方法自取锁）。
func (service *Service) touchContent(sessionID string) {
	if service == nil || sessionID == "" {
		return
	}
	service.ViewMu.Lock()
	service.markContentRecentLocked(sessionID)
	service.ViewMu.Unlock()
	service.reconcileLoadedContentLimit(sessionID)
}

// markContentRecentLocked 把会话移到内容 LRU 使用序最前（索引 0 = 最近使用；
// 调用方持有 Core.ViewMu）。
func (service *Service) markContentRecentLocked(sessionID string) {
	if sessionID == "" {
		return
	}
	next := make([]string, 0, len(service.loadedContentOrder)+1)
	next = append(next, sessionID)
	for _, existing := range service.loadedContentOrder {
		if existing != sessionID {
			next = append(next, existing)
		}
	}
	service.loadedContentOrder = next
}

// forgetContentLocked 把会话移出内容 LRU 使用序（正文已卸载；调用方持有
// Core.ViewMu）。正文下次装载/回读时经 touchContent 重新入序（最近使用端）。
func (service *Service) forgetContentLocked(sessionID string) {
	filtered := service.loadedContentOrder[:0]
	for _, existing := range service.loadedContentOrder {
		if existing != sessionID {
			filtered = append(filtered, existing)
		}
	}
	service.loadedContentOrder = filtered
}

// pruneContentOrderLocked 收敛使用序并返回当前持有正文的会话数（调用方持有
// Core.ViewMu）：丢弃已不再持有正文的会话（卸载/单元移除后），收养「有正文
// 但不在序里」的会话（视作最旧，兼容本机制上线前已装载的会话与直接构造视图
// 的宿主），因此返回值就是内存中已加载正文的会话数。
func (service *Service) pruneContentOrderLocked() int {
	kept := make([]string, 0, len(service.loadedContentOrder))
	tracked := make(map[string]struct{}, len(service.loadedContentOrder))
	for _, sessionID := range service.loadedContentOrder {
		if _, duplicate := tracked[sessionID]; duplicate {
			continue
		}
		if !service.sessionContentLoadedLocked(sessionID) {
			continue
		}
		tracked[sessionID] = struct{}{}
		kept = append(kept, sessionID)
	}
	for _, sessionID := range service.sessions.UnitIDs() {
		if _, ok := tracked[sessionID]; ok {
			continue
		}
		if !service.sessionContentLoadedLocked(sessionID) {
			continue
		}
		tracked[sessionID] = struct{}{}
		kept = append(kept, sessionID)
	}
	service.loadedContentOrder = kept
	return len(kept)
}

// reconcileLoadedContentLimit 超限时按 LRU 卸载空闲会话的已加载正文（无候选
// 则容忍超限：会话全被视图/运行/恢复占用时不驱逐；卸载失败保持原状，下轮
// touch 再试）。protect 是本次触发的会话（正在装载/激活，其过渡锁可能已被
// 调用方持有，且正文刚被使用），绝不参与本轮卸载。
func (service *Service) reconcileLoadedContentLimit(protect string) {
	if service == nil {
		return
	}
	limit := loadedContentLimit(Limits().LoadedContentLimit)
	for {
		service.ViewMu.Lock()
		loaded := service.pruneContentOrderLocked()
		if loaded <= limit {
			service.ViewMu.Unlock()
			return
		}
		candidate := service.pickContentEvictableCandidateLocked(protect)
		service.ViewMu.Unlock()
		if candidate == "" {
			// 超限但全部忙/视图/刚被使用：容忍（相关会话转为空闲或被切走后，
			// 下一次 touch 再收敛）。
			return
		}
		if err := service.evictLoadedContent(candidate); err != nil {
			log.Printf("[content-lru] evict %q: %v", candidate, err)
			return
		}
	}
}

// pickContentEvictableCandidateLocked 从使用序最旧端挑一个可卸载正文的会话
// （调用方持有 Core.ViewMu）：非当前视图、非本次触发的会话、非 restoring、
// 非 busy（running/queued/awaiting_approval/待批都不可卸载），且确实持有
// 已加载正文。
func (service *Service) pickContentEvictableCandidateLocked(protect string) string {
	current := service.Core.Snapshot.Session.ID
	for index := len(service.loadedContentOrder) - 1; index >= 0; index-- {
		sessionID := service.loadedContentOrder[index]
		if sessionID == "" || sessionID == current || sessionID == protect {
			continue
		}
		if service.isRestoringLocked(sessionID) {
			continue
		}
		unit := service.sessions.Unit(sessionID)
		if unit == nil || service.sessionBusy(unit) {
			continue
		}
		if !service.sessionContentLoadedLocked(sessionID) {
			continue
		}
		return sessionID
	}
	return ""
}

// evictLoadedContent 卸载一个空闲会话的已加载正文：先 flush（非活跃会话
// PersistCurrentSession——内存窗口里可能还有尚未落盘的收尾消息，先落盘才能
// 保证「只丢内存副本、不丢数据」），再清空 Conversation 并置
// View.ContentUnloaded。会话事实/元数据/标题/统计/窗口标志与磁盘数据全部
// 保留；重开或分页时经冷回读恢复同一窗口。卸载后请求目录刷新（列表条目与
// 标题来自存储层枚举，零正文读，条目不会消失）。
//
// 不取目标会话过渡锁的理由见文件头「锁与死锁说明」；因此挑选与卸载之间的
// 状态变化由 ViewMu 临界区内的二次确认兜住。
func (service *Service) evictLoadedContent(sessionID string) error {
	service.ViewMu.RLock()
	unit := service.sessions.Unit(sessionID)
	active := sessionID == service.Core.Snapshot.Session.ID
	busy := unit != nil && service.sessionBusy(unit)
	restoring := service.isRestoringLocked(sessionID)
	loaded := service.sessionContentLoadedLocked(sessionID)
	service.ViewMu.RUnlock()
	if unit == nil || active || busy || restoring || !loaded {
		// 状态在挑选与卸载之间已变：跳过，保持超限容忍。
		return nil
	}
	location := service.components.sessions.LocateSession(sessionID)
	if err := service.components.sessions.PersistCurrentSession(location, sessionID); err != nil {
		// flush 失败不卸载：正文留在内存，数据不丢；下轮 touch 再试。
		return fmt.Errorf("flush session %q before content eviction: %w", sessionID, err)
	}
	service.ViewMu.Lock()
	if sessionID == service.Core.Snapshot.Session.ID || !service.sessionContentLoadedLocked(sessionID) {
		service.ViewMu.Unlock()
		return nil
	}
	service.components.view.SessionViewMutateLocked(sessionID, func(view *session.View) {
		// 只卸载正文：窗口标志/统计/聊天运行态/文件引用保留，冷回读据此还原
		// 同一窗口位置（不重置 offset/total）。
		view.Conversation = nil
		view.ContentUnloaded = true
	})
	service.forgetContentLocked(sessionID)
	service.ViewMu.Unlock()
	service.components.sessions.RequestCatalogRefresh()
	return nil
}

// sessionContentLoadedLocked 报告目标会话当前是否持有已加载的可见正文
// （调用方持有 Core.ViewMu；单元缺失/已被卸载/窗口为空都算「无正文」——
// 「本已无内容」的会话不进候选，也不计入上限）。
func (service *Service) sessionContentLoadedLocked(sessionID string) bool {
	unit := service.sessions.Unit(sessionID)
	if unit == nil {
		return false
	}
	loaded := false
	unit.View.Read(func(view *session.View) {
		loaded = !view.ContentUnloaded && len(view.Conversation) > 0
	})
	return loaded
}

// sessionContentUnloaded 报告目标会话的可见正文是否已被内容 LRU 卸载（需要
// 冷回读才能给出窗口；会话单元缺失时按未卸载处理——无可回读的内存态）。
func (service *Service) sessionContentUnloaded(sessionID string) bool {
	unit := service.sessions.Unit(sessionID)
	if unit == nil {
		return false
	}
	unloaded := false
	unit.View.Read(func(view *session.View) { unloaded = view.ContentUnloaded })
	return unloaded
}

// ensureSessionContent 在目标会话正文已被卸载时从磁盘回读（热挂载不重建
// 历史，正文必须显式补回，否则用户切回来看到的是空会话）。
func (service *Service) ensureSessionContent(sessionID string) error {
	if !service.sessionContentUnloaded(sessionID) {
		return nil
	}
	return service.reloadSessionContent(sessionID)
}

// reloadSessionContent 冷回读被卸载的可见正文窗口：经与「回到最新」同一条尾部
// 窗口读面（先探已发布总数，再按总数定位窗口起点）读回磁盘尾部窗口并整窗安装
// （installVisibleHistory 会清掉「内容未加载」标志并按实际装入的行定起点）。
// 卸载时保留的窗口标志只用来判断「要不要回读」，不作为读取起点——磁盘为准。
func (service *Service) reloadSessionContent(sessionID string) error {
	window := Limits().HistoryWindow
	if window <= 0 {
		window = 1
	}
	workspaceID := service.components.sessions.LocateSession(sessionID).WorkspaceID
	page, err := service.loadConversationTailPage(workspaceID, sessionID, window)
	if err != nil {
		return fmt.Errorf("reload session content %q: %w", sessionID, err)
	}
	// 安装即一次正文使用（installVisibleHistory 负责 touch 内容 LRU）。
	return service.installVisibleHistory(sessionID, page, window, historyPageReplace)
}
