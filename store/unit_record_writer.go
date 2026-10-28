package store

import "github.com/RedHuang-0622/seelex/store/workunit"

// unit_record_writer.go — 会话记录**写侧的责任链**（用户口径，2026-10-06）。
//
// M9 之后实现归 `store/workunit`（`Link` / `Writer` 的唯一定义在那儿）；本文件只剩同身份
// 别名与构造函数——根包不再持有第二份链实现，也不在这里做落盘或校验。
//
// 为什么是责任链（完整说明见 `store/workunit/store.go` 的「写侧责任链」一节）：两条链写的是
// 同一张记录，teammate 的记录形状是 subagent 记录的纯增幅，于是 teammate 那一层 =
// 子代理链**再加一环**，而不是第二条写路径。

// NodeSessionRecordLink 是写链上的一环：拿到记录、**增补**自己这一层知道的事实、交下一环。
type NodeSessionRecordLink = workunit.Link

// NodeSessionRecordWriter 是会话记录写入的唯一出口：逐环增补 → 末端一次落盘。
//
// 零值不可用：`Save` 在链或末端缺失时**显式报错**（记录是恢复用的证据，静默丢它就是
// "重启失忆"被当成正常）。
type NodeSessionRecordWriter = workunit.Writer

// NewNodeSessionRecordWriter 以既有存储为末端起一条链。
func NewNodeSessionRecordWriter(recorder workunit.Recorder) *NodeSessionRecordWriter {
	return workunit.NewWriter(recorder)
}
