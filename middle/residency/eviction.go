package residency

// eviction.go 是**两层 LRU 超限收敛共用的判据**（`content_lru` 与 `resident_lru` 的循环体）。
//
// 为什么在这里（plan §2.4 的 R6：`content_lru.go` / `resident_lru.go` → `middle/residency/`，
// AGENTS §8.3「同一判据一处」）：「按使用序挑出可卸载候选 → 折算这一层还能留几个 → 卸载
// 最旧的一个 → 再挑」这段判断，正文层（可见正文窗口）与引擎层（引擎 bundle 驻留）**逐字同形**，
// 只依赖**读数**（使用序、该会话是否占用本层、是否受保护、额度）与**两个动作**（卸载一个
// 会话、报告一次失败）。落地在这里之后，宿主只剩"取数 + 执行"，两层不再各写一份循环。
//
// 两层的差异**全部在宿主的读数里**，不在判据里：
//
//   - 使用序来源与是否先收敛序：正文层先 `PruneOrder`（收养"有正文但不在序里"的会话），
//     引擎层不收敛（以 `unit.Resident() && engine.HasSession(...)` 现算占用）；
//   - 是否有"本次触发的会话"：正文层有（`Protect`，刚被使用且过渡锁可能被调用方持有），
//     引擎层没有（留空）；
//   - 额度来源：正文层 `limits.loaded_content_limit`、引擎层 `limits.resident_limit`。
//
// 口径（与迁移前逐字一致，2026-10-17 M5-R6-content-lru 接收者转移）：
//
//   - 使用序索引 0 = **最近使用**，末尾最旧（`Order` 的口径）；
//   - **受保护集既不计数也不参与挑选**，本次触发的会话（`Protect`）也不参与本轮卸载，
//     但计入受保护数——否则会多放掉别的会话；
//   - 「本已无正文」的会话既不进候选也不计数（空 ID 一律跳过）；
//   - 额度判据复用 `Budget`（`LegacyLimit > 0` 时 = 该层总上限 − 受保护数，否则 = 统一窗口）；
//   - 没有候选时**容忍超限**（受保护集会挡住收敛，不做任何事）；
//   - 卸载失败**立即停**（正文留在内存、数据不丢），由宿主记一次日志，下一轮 touch 再试；
//   - 每次挑选与执行之间状态可能已变（并发激活/卸载），挑选**不做**二次确认——
//     那是宿主在执行动作内部的事（见 `application/assembly/content_lru.go` 的 `evictLoadedContent`）。

// EvictionSnapshot 是**一次**超限收敛的读数（宿主在同一临界区内取齐：挑选与执行之间
// 状态会变，但同一次挑选必须看到自洽的一组读数）。
type EvictionSnapshot struct {
	// Order 是当前使用序（索引 0 = 最近使用）；宿主取数前会先收敛使用序。
	Order Order
	// Protect 是本次触发的会话：绝不参与本轮卸载，但计入受保护数（引擎层没有这个角色，留空）。
	Protect string
	// Occupied 是此刻**占用本层内存**的会话集合（正文层 = 持有可见正文窗口；
	// 引擎层 = 会话单元标记驻留且引擎真有该会话）。用它过滤 Order。
	Occupied map[string]bool
	// Protected 是此刻在受保护集里的会话集合（既不计数也不参与挑选）。
	Protected map[string]bool
	// LegacyLimit 是该层的**显式**配置上限（`<= 0` = 没有分层上限，用统一窗口）。
	LegacyLimit int
	// Window 是统一窗口（`limits.session_release_window`）。
	Window int
}

// EvictionDeps 是超限收敛的宿主面（值语义：每次调用装配一次）。
type EvictionDeps struct {
	// Snapshot 在同一临界区内取齐一次读数（宿主在此收敛使用序）。必须非 nil。
	Snapshot func() EvictionSnapshot
	// Evict 卸载一个会话在本层的内存占用（只卸内存副本：先 flush/落盘，再释放）。必须非 nil。
	Evict func(sessionID string) error
	// OnEvictError 记录一次卸载失败（保持超限容忍，下一轮再试）。
	OnEvictError func(sessionID string, err error)
}

// EvictUntilWithinLimit 反复卸载"最旧且可卸载"的会话，直到占用本层的会话数进入额度；
// 卸载失败即停，没有候选即返回（容忍超限）。
func EvictUntilWithinLimit(deps EvictionDeps) {
	if deps.Snapshot == nil || deps.Evict == nil {
		return
	}
	for {
		snapshot := deps.Snapshot()
		protected := 0
		// Order[0] = 最近使用 ⇒ 末尾最旧。
		evictable := make([]string, 0, len(snapshot.Order))
		for _, sessionID := range snapshot.Order {
			if sessionID == "" || !snapshot.Occupied[sessionID] {
				continue
			}
			if sessionID == snapshot.Protect || snapshot.Protected[sessionID] {
				protected++
				continue
			}
			evictable = append(evictable, sessionID)
		}
		budget := Budget(snapshot.LegacyLimit, protected, snapshot.Window)
		if len(evictable) <= budget {
			return
		}
		candidate := evictable[len(evictable)-1]
		if err := deps.Evict(candidate); err != nil {
			if deps.OnEvictError != nil {
				deps.OnEvictError(candidate, err)
			}
			return
		}
	}
}
