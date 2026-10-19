// stage.go — **叶子暂存**：工具记录「阶梯提交」里最低那一级台阶。
//
// 生态位（2026-10-19 方向 C）：工具读数的链路是多级的 ——
// `subagent/teammate（执行单元）-> main（会话累计）-> 存储（会话自己的文件）`。
// 执行单元（一个子代理节点 / 一个 teammate 角色会话）在做工期间**自己攒**，
// 只在"这一级收口"时一次性把增量交给上一级；会话累计只在真相源提交点落盘。
//
// 为什么需要这一级（现场病灶）：没有它，工具一完成就直接写会话累计 ——
// 子代理/员工的高频工具把主侧台账当成写缓存用，而主侧台账又要落盘，
// "工具记录写繁忙"就是这么来的（docs/2026-10-19-three-defect-fixes 方向 C）。
//
// 边界（写死在这一级，不要在别处再写一份）：
//   - **不碰会话锁、不碰视图、不碰磁盘**：只有本结构自己的一把锁；
//   - **不写第二份判据/词表**：成败判定与失败原因归并都由调用方（application/core）
//     算好再喂进来，累加本身复用 `Accumulator`；
//   - **同步交出**：`TakeForCommit` 返回时增量已经离开本暂存（返回即入账，
//     不靠 goroutine、不排队）。
package record

import (
	"strings"
	"sync"

	"github.com/RedHuang-0622/seelex/contract/dto"
)

// StageToolNameLimit 是单个暂存的**工具名条目**上限。
//
// 超上限后新工具名并入 `StageOverflowTool` 桶：**只丢"名字的细分"，计数一分不丢**
// （读数要的是成功率与失败原因分布，一个长尾工具名不值得把暂存做成无界内存）。
// 这条只写注释与测试，不扩展 DTO、不新增看板字段。
const StageToolNameLimit = 256

// StageOverflowTool 是条目超限后新工具名并入的桶名。
const StageOverflowTool = "other"

// Stage 是一个**执行单元**（子代理节点 / teammate 角色会话）的工具记录本地暂存。
//
// 零值不可用；用 NewStage 构造。并发：单把锁（工具热路径每个调用结束进一次）。
type Stage struct {
	mu        sync.Mutex
	unitID    string
	sessionID string
	acc       *Accumulator
	// distinct 是当前窗口里已记过的工具名个数（条目上限的计数面；交出一批后归零）。
	distinct int
	// seq 是**本单元**的提交序号：每次交出增量自增一次，跨调用的幂等靠它
	// （主侧按 (sessionID, unitID) 记 lastSeq，`seq <= lastSeq` 直接忽略）。
	seq uint64
}

// NewStage 构造一个执行单元的暂存（unitID/sessionID 只做身份记录与诊断，
// 归属由调用方从执行上下文取，**不从视图读**）。
func NewStage(unitID, sessionID string) *Stage {
	return &Stage{
		unitID:    strings.TrimSpace(unitID),
		sessionID: strings.TrimSpace(sessionID),
		acc:       NewAccumulator(),
	}
}

// UnitID 返回本暂存的执行单元键（"subagent:<nodeID>" / "role:<roleSessionID>"）。
func (stage *Stage) UnitID() string {
	if stage == nil {
		return ""
	}
	return stage.unitID
}

// SessionID 返回本暂存归属的主会话（执行上下文给定；空 = 没有归属）。
func (stage *Stage) SessionID() string {
	if stage == nil {
		return ""
	}
	return stage.sessionID
}

// Record 纯本地累加一次调用（成功也记；判定与原因归并由调用方算好）。
func (stage *Stage) Record(tool string, ok bool, reason string) {
	if stage == nil {
		return
	}
	tool = strings.TrimSpace(tool)
	if tool == "" {
		return
	}
	stage.mu.Lock()
	defer stage.mu.Unlock()
	if _, seen := stage.acc.Stat(tool); !seen && stage.distinct >= StageToolNameLimit {
		tool = StageOverflowTool
	}
	if _, seen := stage.acc.Stat(tool); !seen {
		stage.distinct++
	}
	stage.acc.Record(tool, ok, reason)
}

// TakeForCommit **同步**交出本窗口的增量并清空暂存；返回 (seq, 增量读数)。
//
// seq 从 1 起自增（0 = 什么也没有，调用方据此跳过这次提交）。没有新读数时
// 不动 seq、返回空增量 —— 重复收口因此是幂等的（幂等判据仍在主侧那**一格**，
// 这里不复制一份）。
func (stage *Stage) TakeForCommit() (uint64, []dto.ToolRecordStat) {
	if stage == nil {
		return 0, nil
	}
	stage.mu.Lock()
	defer stage.mu.Unlock()
	snapshot, dirty := stage.acc.Snapshot()
	if !dirty {
		return 0, nil
	}
	stage.seq++
	stage.acc = NewAccumulator()
	stage.distinct = 0
	return stage.seq, snapshot.Tools
}
