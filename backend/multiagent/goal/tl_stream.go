package goal

import "context"

// tl_stream.go — b（ADVISOR）回合的**进行中**观察面。
//
// 生态位：b 回合的真实执行面在 seelebridge（角色会话 + ChatStream，本来就是流式
// 的），但旧实现把 onChunk 传成 nil——分片被丢掉，前端只能在回合结束后拿到终局
// 裁决。用户看到的症状是"渲染不及时"：评审在写什么、写到哪，界面上完全没有。
//
// 本文件用 **ctx 传递同一次调用内**的观察回调：
//   - 不新增共享状态（回调随 ctx 走，回合结束即失效）；
//   - 不改 TLEvaluator 契约（契约只承载裁决本身：TLDirective）；
//   - 不引入前端→后端的写入口（这只把后端产生的分片**单向**推出去）。
//
// 谁挂载：Supervisor.evaluateRound（每一轮 b 回合的唯一执行点；执行段不持 s.mu）。
// 谁消费：seelebridge 的角色回合（把它接到引擎的 onChunk）。

// TLDeltaSink 接收 b 回合的流式分片（每次调用是一段增量正文）。
type TLDeltaSink func(delta string)

type tlDeltaSinkKey struct{}

// WithTLDeltaSink 把观察回调挂到 ctx 上（回合开始处挂，defer 清理）。
func WithTLDeltaSink(ctx context.Context, sink TLDeltaSink) context.Context {
	if ctx == nil || sink == nil {
		return ctx
	}
	return context.WithValue(ctx, tlDeltaSinkKey{}, sink)
}

// TLDeltaSinkFrom 取出观察回调；未挂载时返回 nil（执行面按"不观察"处理，
// 不影响回合本身）。
func TLDeltaSinkFrom(ctx context.Context) TLDeltaSink {
	if ctx == nil {
		return nil
	}
	sink, _ := ctx.Value(tlDeltaSinkKey{}).(TLDeltaSink)
	return sink
}
