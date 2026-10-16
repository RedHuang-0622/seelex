package residency

// order.go 是**"会话最近使用序"与释放额度的唯一实现**。
//
// 为什么在这里（AGENTS §8.3「同一判据一处」）：引擎驻留层（`resident_lru`）与正文层
// （`content_lru`）各自维护一条使用序，归档路径也要从序里摘会话——迁移前"移到序首（去重）"
// 与"按 ID 摘除"这两段算法在四处各写了一份。现在只有这一份，两个 LRU 与归档路径都用它。
//
// 口径（与迁移前逐字一致）：索引 0 = **最近使用**，末尾最旧；`Touch` 返回**新切片**
// （调用方重新赋值，不改旧切片的内容）；`Forget` 就地过滤（调用方重新赋值）。

// Order 是"最近使用序"。空 ID 一律视为无效（`Touch` 原样返回、`Forget` 无操作）——
// 迁移前正文层就是这么判的，引擎层的调用点也都先做过空值守卫。
type Order []string

// Touch 把 ID 移到序首（已在序里则先去重再前置）。
func (o Order) Touch(id string) Order {
	if id == "" {
		return o
	}
	next := make([]string, 0, len(o)+1)
	next = append(next, id)
	for _, existing := range o {
		if existing != id {
			next = append(next, existing)
		}
	}
	return next
}

// Forget 把 ID 移出序，其余相对顺序不变。
//
// **就地过滤（写回底层数组）**：调用方重新赋值即可继续用；但**不要**把这个切片交给
// 临界区之外的读者——`o[:0]` 会把后面的元素盖到前面（P20 现场：锁外遍历与这里的写撞成
// data race，见 `application/core/content_lru.go` 的快照复制）。跨临界区传递请先复制。
func (o Order) Forget(id string) Order {
	if id == "" || len(o) == 0 {
		return o
	}
	kept := o[:0]
	for _, existing := range o {
		if existing != id {
			kept = append(kept, existing)
		}
	}
	return kept
}

// PruneOrder 收敛使用序：去重、丢弃**已不再持有正文**的会话，并把"持有正文但不在序里"的会话
// 收养到序尾（视作最旧——兼容本机制上线前已装载的会话与直接构造视图的宿主）。
//
// loaded 报告某会话此刻是否持有正文（宿主读会话单元/视图；本包不碰状态）；
// unitIDs 是当前单元表里的会话，作为收养来源按给定顺序追加。返回值即收敛后的序
// （调用方据此更新自己的字段），因此它的长度就是"当前持有正文的会话数"。
func PruneOrder(order Order, loaded func(sessionID string) bool, unitIDs []string) Order {
	kept := make(Order, 0, len(order))
	tracked := make(map[string]struct{}, len(order))
	for _, sessionID := range order {
		if _, duplicate := tracked[sessionID]; duplicate {
			continue
		}
		if !loaded(sessionID) {
			continue
		}
		tracked[sessionID] = struct{}{}
		kept = append(kept, sessionID)
	}
	for _, sessionID := range unitIDs {
		if _, ok := tracked[sessionID]; ok {
			continue
		}
		if !loaded(sessionID) {
			continue
		}
		tracked[sessionID] = struct{}{}
		kept = append(kept, sessionID)
	}
	return kept
}

// Budget 计算"这一层还能留几个会话"的额度：
//   - `legacyLimit > 0`（显式配了分层上限）→ 该层总上限 − 受保护数，不足则 0；
//   - 否则 → 统一窗口 `window`（受保护集不参与计数，见释放面口径）。
func Budget(legacyLimit, protectedCount, window int) int {
	if legacyLimit > 0 {
		if budget := legacyLimit - protectedCount; budget > 0 {
			return budget
		}
		return 0
	}
	return window
}
