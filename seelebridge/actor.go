package seelebridge

import "github.com/RedHuang-0622/seelex/seelexctx/snapshot"

// ── Actor 模型消息边界 ────────────────────────────────────────
// seelex 的并发语义：每个会话是独立 actor（自己的锁 = 自己的状态），actor
// 之间不共享状态、不竞争锁，只交换消息（值对象）。
//
// 本接口是主会话 actor 与子代理 actor 之间的上下文消息通道：
//   - ParentEvidence（消息进）：主 actor 对外投影父上下文快照（值对象，
//     一次性、用完即弃），供子代理执行前装配；
//   - MergeBack（消息出）：子代理产出经合并后投递到主 actor 的 mailbox
//     （队列），主 actor 在自己处理消息前（锁外）消费注入。
//
// 死锁教训（2026-08-02 冒烟实测）：plan_run 作为主代理的工具调用在
// Session.ChatStream 内同步执行（主会话锁全程持有），任何跨 actor 直接访问
// 主会话状态（History/AppendHistory）都会形成循环等待（持锁者等待自己的
// 工作）。因此本通道的实现必须遵守"状态私有、消息进出"。
type ContextExchanger interface {
	// ParentEvidence 返回父上下文快照（nil = 无父上下文，跳过证据注入）。
	// 实现不得访问正在 ChatStream 中的主会话（会死锁）；应从无锁数据面
	// （application 镜像 / 遥测）构造新值对象。
	ParentEvidence() *snapshot.ContextSnapshot

	// MergeBack 接收子代理合并产出（merger.MergeBack → Format 文本）。
	// 实现只做无锁投递（追加 mailbox），注入由主 actor 在锁外完成。
	MergeBack(content string)
}

// SetContextExchanger 装配父子上下文消息通道（Actor 消息边界）。
// 传入 nil 关闭通道（父证据不注入、回传跳过——降级不报错）。
func (r *Runtime) SetContextExchanger(exchanger ContextExchanger) {
	if r == nil {
		return
	}
	r.exchangerMu.Lock()
	r.exchanger = exchanger
	r.exchangerMu.Unlock()
}

// contextExchanger 返回当前装配的交换器（nil = 未装配）。
func (r *Runtime) contextExchanger() ContextExchanger {
	if r == nil {
		return nil
	}
	r.exchangerMu.RLock()
	exchanger := r.exchanger
	r.exchangerMu.RUnlock()
	return exchanger
}
