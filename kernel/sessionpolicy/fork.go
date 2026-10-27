package switching

// fork.go — `ForkSession` / `ForkSessionLatest`（会话分叉）的**门控与纯助手**。
//
// 落点沿革：2026-10-17 随 **M5-R6-fork** 接收者转移从 `application/assembly/session_fork.go`
// 归位（plan §2.4 的 `session_fork.go` → `middle/switch/`）。
//
// 口径声明（迁移前注释逐字保留其含义）：这是 **ForkSession（会话分叉）**——产出独立项目级会话；
// **ForkSubagent（`fork_subagents` 子代理派发）不是同一条路**（它经 workplan DAG 建子代理现场，
// 存储侧落 `subagent_<hash>/` 子树，派发链不经过 `PrepareFork`）。
//
// 本片只搬"门控判据 + 两个纯助手"；`forkSessionLocked` 的**落盘编排**仍留在宿主——它的每一步
// 都拿着 `session_runtime` 的后端类型（`ForkContext` / `SessionForkPort` / `Location`），中间层无法命名；
// 那片随 R6 的 sessionusecase（`session/` 归位）一起收（已登记）。

import (
	"errors"

	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
)

// ErrForkGateUnassembled 是门控端口的**装配缺陷**信号（该轴拒绝放行但宿主没带错误值）。
var ErrForkGateUnassembled = errors.New("fork gate error is not assembled")

// ForkGate 是 fork 门控的读数与宿主错误值（宿主在视图锁内取数后装配成值）。
type ForkGate struct {
	// Closed 是应用已关闭；Draining 是正在排空；Running 是**父会话**自身在跑
	// （并行会话里别的会话执行不影响 fork）。
	Closed   bool
	Draining bool
	Running  bool
	// ErrClosed / ErrDraining / ErrRunning 是对应轴上原样返回的宿主错误值。
	ErrClosed   error
	ErrDraining error
	ErrRunning  error
}

// Reject 按迁移前的**原次序**判决门控（关闭 → 排空 → 父会话在跑），返回 nil 即放行。
// 次序是判据的一部分：同时"已关闭"与"在跑"时调用方拿到的必须是关闭那条错误。
func (gate ForkGate) Reject() error {
	switch {
	case gate.Closed:
		return forkGateError(gate.ErrClosed)
	case gate.Draining:
		return forkGateError(gate.ErrDraining)
	case gate.Running:
		return forkGateError(gate.ErrRunning)
	}
	return nil
}

func forkGateError(err error) error {
	if err == nil {
		return ErrForkGateUnassembled
	}
	return err
}

// MaxTranscriptEventSeq 返回事件流中的最大 Seq（空流返回 0）。子会话内存 transcript 的序号基线
// 要对齐到"继承事件的最大 seq"，否则子会话自己的事件会从 seq 1 起算并与继承区间重叠，
// 落盘合并按 seq 覆盖时会把子会话自己的消息顶掉（2026-09-11 TC-A2-01 回归）。
func MaxTranscriptEventSeq(events []model.TranscriptEvent) uint64 {
	var maxSeq uint64
	for _, event := range events {
		if event.Seq > maxSeq {
			maxSeq = event.Seq
		}
	}
	return maxSeq
}

// DeepCopyRecord 深拷贝 fork 子会话 record：Conversation 消息（含 Tool 引用）、Tasks、Checkpoints、
// ToolResults、ReadFiles、PlanStack 全部新建切片/对象，父子引用不相交。
//
// `clonePlanStack` 由宿主注入：PlanStack 的克隆实现（`model.CloneRuntimeState` 组合）今天在
// 后端 `session` 域，中间层不能 import（plan §1.1），所以这里只规定"必须深拷贝"这条判据。
func DeepCopyRecord(record model.SessionRecord, clonePlanStack func([]model.SessionPlanFrame) []model.SessionPlanFrame) model.SessionRecord {
	copied := record
	copied.Conversation.Messages = make([]model.Message, len(record.Conversation.Messages))
	for index, message := range record.Conversation.Messages {
		cloned := message
		if message.Tool != nil {
			tool := *message.Tool
			cloned.Tool = &tool
		}
		copied.Conversation.Messages[index] = cloned
	}
	copied.Tasks = append([]dto.TaskRecord(nil), record.Tasks...)
	copied.Checkpoints = append([]model.TaskCheckpoint(nil), record.Checkpoints...)
	copied.ToolResults = append([]model.ToolResultRef(nil), record.ToolResults...)
	if clonePlanStack != nil {
		copied.PlanStack = clonePlanStack(record.PlanStack)
	}
	copied.Execution.ReadFiles = append([]model.ReadFileRef(nil), record.Execution.ReadFiles...)
	return copied
}
