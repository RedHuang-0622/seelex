package sessionstore

import "errors"

// unit_record_writer.go — 会话记录**写侧的责任链**（用户口径，2026-10-06）。
//
// 写侧为什么是责任链：两条链**写的是同一张记录**——teammate 的记录形状是 subagent 记录的
// **纯增幅**（多一个身份块 `NodeUnitRecord`、现场四栏写全），所以 teammate 那一层 = 子代理链
// **再加一环**，而不是第二条写路径（"add but not modify"）。链上每一环只负责"我这一层知道的
// 事实"，且**只填空、不覆盖**别人已经写下的事实：teammate 那一环写下 `Kind=teammate` 之后，
// 子代理那一环看到身份已经有了就不再动它。
//
// 恢复侧反过来是**策略模式**（按记录快照里的身份分派）：要读的东西、要恢复的内容确实不同
// ——子代理恢复的是详情数据面 + 子代理树节点 + 现场登记，teammate 恢复的是团队现场
// （见 `seelebridge/runtime_unit_recovery.go`）。

// NodeSessionRecordLink 是写链上的一环：拿到记录、**增补**自己这一层知道的事实、交下一环。
type NodeSessionRecordLink func(NodeSessionRecord) NodeSessionRecord

// NodeSessionRecordWriter 是会话记录写入的唯一出口：逐环增补 → 末端一次落盘。
//
// 零值不可用：`Save` 在链或末端缺失时**显式报错**（记录是恢复用的证据，静默丢它就是
// "重启失忆"被当成正常）。
type NodeSessionRecordWriter struct {
	store *NodeSessionStore
	links []NodeSessionRecordLink
}

// NewNodeSessionRecordWriter 以既有存储为末端起一条链。
func NewNodeSessionRecordWriter(store *NodeSessionStore) *NodeSessionRecordWriter {
	return &NodeSessionRecordWriter{store: store}
}

// With 派生一条**在外面再包一环**的链（本链不动）：新的一环先跑，之后才走本链原有的环。
// 于是 teammate 那一层 = 子代理链外面再包一环（**add but not modify**：teammate 的记录就是
// subagent 的记录多一个身份块），不必另立第二条写路径。
//
// 顺序的意义：**越外层的环越先写自己知道的事实，内层的环只填空缺**（子代理那一环见
// `SubagentUnitRecordLink`：身份已经有了就不动它）。所以外层包进来的环可以放心先写。
func (w *NodeSessionRecordWriter) With(links ...NodeSessionRecordLink) *NodeSessionRecordWriter {
	if w == nil {
		return nil
	}
	derived := &NodeSessionRecordWriter{store: w.store}
	derived.links = append(derived.links, links...)
	derived.links = append(derived.links, w.links...)
	return derived
}

// Save 走完整条链再落盘（链上每一环只看得到自己这一层的事实）。
func (w *NodeSessionRecordWriter) Save(projectID, mainSessionID string, record NodeSessionRecord) error {
	if w == nil || w.store == nil {
		return errors.New("session storage: node session record writer is unavailable")
	}
	for _, link := range w.links {
		if link == nil {
			continue
		}
		record = link(record)
	}
	return w.store.Save(projectID, mainSessionID, record)
}

// Store 返回链的末端存储：**读面**（列举/读取/删除/项目作用域）仍走它，本类型只管
// "写怎么走链"。链没装末端时返回 nil（调用方按未装配处理）。
func (w *NodeSessionRecordWriter) Store() *NodeSessionStore {
	if w == nil {
		return nil
	}
	return w.store
}
