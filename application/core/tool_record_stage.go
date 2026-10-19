package core

// tool_record_stage.go — 工具记录的**阶梯提交**（叶子 → 主侧会话累计 → 存储）。
//
// 链路（2026-10-19 方向 C，用户口径原文）：`subagent/teammate -> mainagent -> 工具记录存储`。
// 三条纪律落在这里：
//
//  1. **归属键显式**：一笔读数必须带 `(ownerSessionID, unitID)`，owner 只从**执行上下文**
//     取（引擎注册节点时随 ctx 绑定的主会话 / teammate 事件自带的 `MainSessionID`）。
//     `Snapshot.Session.ID` 与 `currentViewSessionID()` **永不作兜底**——切会话即串写
//     就是这么来的（#8/#13）。取不到 ⇒ 丢弃 + 计数（`ToolRecordDroppedReadings`）。
//  2. **阶梯提交**：单元（子代理节点 / 角色会话）在做工期间只往**自己的**暂存里累加
//     （`store/record.Stage`，不碰 ViewMu、不碰磁盘）；单元收口时 `CloseUnitToolRecord`
//     一次性把它交给会话累计。
//  3. **唯一入口**：读数进会话累计**只有** `CommitUnitToolRecord` 一条路（同 seq 幂等、
//     `sessionID` 空则丢弃）。主代理自己的读数不经暂存（它本身就是主侧），仍走
//     `recordToolOutcome`——那条与这条共用同一份判据（`toolStatusFailed` / `toolRecordReason`）。
//
// 落盘仍在 `FlushToolRecord`（批次、会话粒度），门在 `toolRecordWritableSession`：
// 草稿一次都不写；落盘前先抽干该会话的全部叶子暂存（`drainStagesFor`）。

import (
	"strings"
	"sync"

	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/store/record"
)

// 单元键的两族前缀：口径只在这里一份（别处只经 subagentToolRecordUnitID /
// roleToolRecordUnitID / Close* 折算，不要自己拼字符串）。
const (
	subagentToolRecordUnitPrefix = "subagent:"
	roleToolRecordUnitPrefix     = "role:"
)

// 读数被丢弃的两种原因（`ToolRecordDroppedReadings` 的键）。
const (
	// toolRecordDropNoOwner = 执行上下文没有给出归属主会话（视图会话不许顶替）。
	toolRecordDropNoOwner = "no_owner_session"
	// toolRecordDropNoUnit = 既没有节点 ID 也没有事件 ID（认不出这是哪个执行单元）。
	toolRecordDropNoUnit = "no_unit_identity"
)

// subagentToolRecordUnitID 折算子代理执行单元的键：有 NodeID 用节点，
// **没有 NodeID 时用事件 ID 兜底**（整条丢弃是旧病灶 #9，读数是真发生过的）。
func subagentToolRecordUnitID(nodeID, eventID string) string {
	if nodeID = strings.TrimSpace(nodeID); nodeID != "" {
		return subagentToolRecordUnitPrefix + nodeID
	}
	if eventID = strings.TrimSpace(eventID); eventID != "" {
		return subagentToolRecordUnitPrefix + eventID
	}
	return ""
}

// roleToolRecordUnitID 折算 teammate 执行单元的键：角色会话是它的身份；
// 缺角色会话时退到事件 ID（同样宁可粗也不能整条丢）。
func roleToolRecordUnitID(roleSessionID, eventID string) string {
	if roleSessionID = strings.TrimSpace(roleSessionID); roleSessionID != "" {
		return roleToolRecordUnitPrefix + roleSessionID
	}
	if eventID = strings.TrimSpace(eventID); eventID != "" {
		return roleToolRecordUnitPrefix + eventID
	}
	return ""
}

// toolRecordUnitKey 是暂存/幂等序号的键：`(owner 主会话, 执行单元)`。
//
// 为什么两维而不是只按单元：同一个节点 ID 在两个会话里可以同时存在（恢复/重放），
// 只按单元记账会让两边的读数撞进一格。
type toolRecordUnitKey struct {
	sessionID string
	unitID    string
}

// toolRecordStages 是叶子暂存的登记表 + 提交幂等序号 + 丢弃计数。
//
// 一把锁（只有工具热路径与收口/抽干会进）：
//   - `Record` 是**热路径**：只做本地累加（不动会话累计、不动视图、不碰磁盘）；
//   - `take` 是收口：交出增量并清空该单元；
//   - `lastSeq` 让"同一单元同一批交两次"只计一次（幂等判据只有这一格）。
type toolRecordStages struct {
	mu      sync.Mutex
	stages  map[toolRecordUnitKey]*record.Stage
	lastSeq map[toolRecordUnitKey]uint64
	dropped map[string]int
}

func newToolRecordStages() *toolRecordStages {
	return &toolRecordStages{
		stages:  map[toolRecordUnitKey]*record.Stage{},
		lastSeq: map[toolRecordUnitKey]uint64{},
		dropped: map[string]int{},
	}
}

// record 把一笔读数记进单元暂存（调用方已完成归属/单元键的判空与取整）。
func (table *toolRecordStages) record(key toolRecordUnitKey, tool string, ok bool, reason string) {
	if table == nil || key.sessionID == "" || key.unitID == "" {
		return
	}
	table.mu.Lock()
	stage := table.stages[key]
	if stage == nil {
		stage = record.NewStage(key.unitID, key.sessionID)
		table.stages[key] = stage
	}
	table.mu.Unlock()
	// 累加在登记表锁外（暂存自己有一把锁）：热路径不把登记表锁拖长。
	stage.Record(tool, ok, reason)
}

// take 交出某单元的增量（没有暂存 = 空）。
func (table *toolRecordStages) take(key toolRecordUnitKey) (uint64, []dto.ToolRecordStat) {
	if table == nil {
		return 0, nil
	}
	table.mu.Lock()
	stage := table.stages[key]
	table.mu.Unlock()
	if stage == nil {
		return 0, nil
	}
	// 交出在登记表锁外（暂存自己有一把锁）：空暂存留着无害，交出去的那一批才是事实。
	return stage.TakeForCommit()
}

// acceptSeq 报告该 seq 是否可以入账（`seq <= lastSeq` = 重复提交）。
func (table *toolRecordStages) acceptSeq(key toolRecordUnitKey, seq uint64) bool {
	if table == nil {
		return false
	}
	table.mu.Lock()
	defer table.mu.Unlock()
	if seq != 0 {
		if seq <= table.lastSeq[key] {
			return false
		}
		table.lastSeq[key] = seq
	}
	return true
}

// drainSession 交出某会话名下**全部**单元的增量（落盘前抽干：叶子没来得及收口也不丢）。
func (table *toolRecordStages) drainSession(sessionID string) []toolRecordCommit {
	if table == nil || sessionID == "" {
		return nil
	}
	table.mu.Lock()
	keys := make([]toolRecordUnitKey, 0, len(table.stages))
	for key := range table.stages {
		if key.sessionID == sessionID {
			keys = append(keys, key)
		}
	}
	table.mu.Unlock()
	commits := make([]toolRecordCommit, 0, len(keys))
	for _, key := range keys {
		seq, stats := table.take(key)
		if len(stats) == 0 {
			continue
		}
		commits = append(commits, toolRecordCommit{key: key, seq: seq, stats: stats})
	}
	return commits
}

// releaseSession 回收某会话的全部暂存与提交序号（会话释放/删除）。
//
// 为什么要丢序号：删掉的会话 ID 可能被复用（早分配 SID 的规则允许同形 ID），
// 继承旧 lastSeq 会让新会话的第一批读数被误判成"重复提交"而整批丢掉。
func (table *toolRecordStages) releaseSession(sessionID string) {
	if table == nil || sessionID == "" {
		return
	}
	table.mu.Lock()
	defer table.mu.Unlock()
	for key := range table.stages {
		if key.sessionID == sessionID {
			delete(table.stages, key)
		}
	}
	for key := range table.lastSeq {
		if key.sessionID == sessionID {
			delete(table.lastSeq, key)
		}
	}
}

// noteDropped 记一次"读数被丢弃"（诊断面：丢弃必须是可见的，不能静默）。
func (table *toolRecordStages) noteDropped(reason string) {
	if table == nil {
		return
	}
	table.mu.Lock()
	table.dropped[reason]++
	table.mu.Unlock()
}

// droppedCounts 返回丢弃计数快照。
func (table *toolRecordStages) droppedCounts() map[string]int {
	if table == nil {
		return nil
	}
	table.mu.Lock()
	defer table.mu.Unlock()
	counts := make(map[string]int, len(table.dropped))
	for reason, count := range table.dropped {
		counts[reason] = count
	}
	return counts
}

// unitCount 返回当前登记的暂存个数（测试/诊断读面）。
func (table *toolRecordStages) unitCount() int {
	if table == nil {
		return 0
	}
	table.mu.Lock()
	defer table.mu.Unlock()
	return len(table.stages)
}

// toolRecordCommit 是一次待入账的增量（抽干/收口共用同一形状）。
type toolRecordCommit struct {
	key   toolRecordUnitKey
	seq   uint64
	stats []dto.ToolRecordStat
}

// StageUnitToolOutcome 把一笔读数记进**执行单元自己的暂存**（叶子本地累加）。
//
// 三个前置判据（顺序即语义）：
//  1. 开关关着 ⇒ 叶子也不攒（关就是关）；
//  2. `sessionID` 空 ⇒ 丢弃 + 计数，**不回落到视图会话**；
//  3. `unitID` 空 ⇒ 丢弃 + 计数（认不出执行单元，记不进任何一格）。
//
// 成败判据与原因词表不在这里复制：调用方把 `failed` / `errorText` 交进来，原因
// 归并仍只经 `toolRecordReason`（本函数）。
func (service *Service) StageUnitToolOutcome(sessionID, unitID, tool string, failed bool, errorText string) {
	if service == nil || service.toolRecords == nil || service.toolStages == nil {
		return
	}
	if !service.ToolRecordEnabled() {
		return
	}
	sessionID = strings.TrimSpace(sessionID)
	unitID = strings.TrimSpace(unitID)
	if sessionID == "" {
		service.toolStages.noteDropped(toolRecordDropNoOwner)
		return
	}
	if unitID == "" {
		service.toolStages.noteDropped(toolRecordDropNoUnit)
		return
	}
	if strings.TrimSpace(tool) == "" {
		return
	}
	reason := ""
	if failed {
		reason = toolRecordReason(errorText)
	}
	service.toolStages.record(toolRecordUnitKey{sessionID: sessionID, unitID: unitID}, tool, !failed, reason)
}

// CommitUnitToolRecord 是**叶子 → 主侧**的唯一入口：同步、幂等、只碰台账锁。
//
//   - `sessionID` 空 ⇒ 丢弃（不回落视图会话，与 jobs 事件同口径）；
//   - `(sessionID, unitID)` 记 `lastSeq`，`seq <= lastSeq` ⇒ 幂等忽略；
//   - 合并进该会话的 `toolRecords.accumulator(sessionID)`（相加语义，见 record.Merge）；
//   - 开关关着 ⇒ 直接返回（叶子本就不该攒，这里是第二道门）。
func (service *Service) CommitUnitToolRecord(sessionID, unitID string, seq uint64, stats []dto.ToolRecordStat) {
	if service == nil || service.toolRecords == nil || service.toolStages == nil {
		return
	}
	sessionID = strings.TrimSpace(sessionID)
	unitID = strings.TrimSpace(unitID)
	if sessionID == "" {
		service.toolStages.noteDropped(toolRecordDropNoOwner)
		return
	}
	if unitID == "" {
		service.toolStages.noteDropped(toolRecordDropNoUnit)
		return
	}
	if len(stats) == 0 {
		return
	}
	if !service.ToolRecordEnabled() {
		return
	}
	if !service.toolStages.acceptSeq(toolRecordUnitKey{sessionID: sessionID, unitID: unitID}, seq) {
		return // 同一批交了两次：只计一次
	}
	accumulator := service.toolRecords.accumulator(sessionID)
	if accumulator == nil {
		return
	}
	accumulator.Merge(stats)
}

// CloseUnitToolRecord 是一次**单元收口**：把它的暂存一次性同步提交给主会话累计并清空。
//
// 返回即已入账（不丢 goroutine）：调用方（节点终态 / 角色回合收口）拿到返回值时，
// 这笔读数已经在会话累计里了。没有新读数 = 空操作，因此重复收口是幂等的。
func (service *Service) CloseUnitToolRecord(sessionID, unitID string) {
	if service == nil || service.toolStages == nil {
		return
	}
	sessionID = strings.TrimSpace(sessionID)
	unitID = strings.TrimSpace(unitID)
	if sessionID == "" || unitID == "" {
		return
	}
	seq, stats := service.toolStages.take(toolRecordUnitKey{sessionID: sessionID, unitID: unitID})
	if len(stats) == 0 {
		return
	}
	service.CommitUnitToolRecord(sessionID, unitID, seq, stats)
}

// CloseSubagentToolRecordUnit 子代理节点的收口（单元键折算只有一份：`subagent:<nodeID>`）。
func (service *Service) CloseSubagentToolRecordUnit(ownerSessionID, nodeID string) {
	service.CloseUnitToolRecord(ownerSessionID, subagentToolRecordUnitID(nodeID, ""))
}

// CloseRoleToolRecordUnit 角色会话的收口（单元键折算只有一份：`role:<roleSessionID>`）。
func (service *Service) CloseRoleToolRecordUnit(mainSessionID, roleSessionID string) {
	service.CloseUnitToolRecord(mainSessionID, roleToolRecordUnitID(roleSessionID, ""))
}

// drainStagesFor 抽干某会话名下的全部叶子暂存（落盘前的兜底：叶子未收口也不丢）。
func (service *Service) drainStagesFor(sessionID string) {
	if service == nil || service.toolStages == nil {
		return
	}
	for _, commit := range service.toolStages.drainSession(sessionID) {
		service.CommitUnitToolRecord(commit.key.sessionID, commit.key.unitID, commit.seq, commit.stats)
	}
}

// releaseToolRecordSession 回收某会话的工具记录内存面（会话释放/删除时调用）。
//
// 回收三样：叶子暂存（含它们的 idempotency 序号）、该会话的累计、以及"读过盘"标记
// ——会话没了，这些读数的归属也就没了；留着只会让复用同一 ID 的新会话读到旧账。
func (service *Service) releaseToolRecordSession(sessionID string) {
	if service == nil {
		return
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	if service.toolStages != nil {
		service.toolStages.releaseSession(sessionID)
	}
	if service.toolRecords != nil {
		service.toolRecords.mu.Lock()
		delete(service.toolRecords.byID, sessionID)
		delete(service.toolRecords.loaded, sessionID)
		service.toolRecords.mu.Unlock()
	}
}

// noteDroppedToolRecordReading 记一次"读数被丢弃"（丢弃必须可见，不能静默）。
func (service *Service) noteDroppedToolRecordReading(reason string) {
	if service == nil || service.toolStages == nil {
		return
	}
	service.toolStages.noteDropped(reason)
}

// ToolRecordDroppedReadings 返回"读数被丢弃"的计数（诊断读面，不进快照）。
//
// 为什么单独一格读数而不是加快照字段：这是**健康度**读数（"有读数没落到任何会话"），
// 不是看板数据；给它开口子会把"丢弃"变成一件看得见但不算错的事。
func (service *Service) ToolRecordDroppedReadings() map[string]int {
	if service == nil || service.toolStages == nil {
		return nil
	}
	return service.toolStages.droppedCounts()
}

// toolRecordCountsStatus 报告这一帧状态是不是"一次调用的结束"（成功/失败两格）。
//
// 只有"结果帧"（success / error）才记：running 帧不是一次调用的结束，记它会把同一件事
// 记两笔（发起一笔 + 完成一笔），"成功率"就被自己的心跳稀释了；未知/零值词同样不记
// （认不得的词不该被当成成功——那是最容易造假的一种默认）。
//
// 这是**全链唯一**的"这一帧算不算一次调用"判据：子代理链（类型化状态）与 teammate 链
// （字符串状态，先经 dto.ParseToolEventStatus 解析）都落到它上面。
func toolRecordCountsStatus(status dto.ToolEventStatus) bool {
	return status == dto.ToolEventSuccess || status == dto.ToolEventError
}

// stageUnitToolOutcomeForStatus 是"字符串状态"那条链（teammate）进暂存的入口。
func (service *Service) stageUnitToolOutcomeForStatus(ownerSessionID, unitID, name, status, errorText string) {
	parsed, ok := dto.ParseToolEventStatus(strings.TrimSpace(status))
	if !ok || !toolRecordCountsStatus(parsed) {
		return
	}
	service.StageUnitToolOutcome(ownerSessionID, unitID, name, toolStatusFailed(parsed), errorText)
}

// stageSubagentToolOutcome 是子代理链进暂存的入口（事件自带身份：节点/事件 ID）。
func (service *Service) stageSubagentToolOutcome(ownerSessionID string, event dto.SubagentToolEvent) {
	if !toolRecordCountsStatus(event.Status) {
		return
	}
	unitID := subagentToolRecordUnitID(event.NodeID, event.ID)
	service.StageUnitToolOutcome(ownerSessionID, unitID, event.Name, toolStatusFailed(event.Status), event.Error)
}

// closeTerminalSubagentToolRecords 把**进入终态**的子代理节点的暂存收口（收口点之一）。
//
// 归属取的是树节点自己的 `MainSessionID`（注册时随执行 ctx 绑定的那一格），
// 不是视图会话——切会话不影响它是谁的读数。
func (service *Service) closeTerminalSubagentToolRecords(nodes []dto.SubAgentTreeNode) {
	for _, node := range nodes {
		if node.ID != "" && terminalSubagentStatus(node.Status) {
			service.CloseSubagentToolRecordUnit(node.MainSessionID, node.ID)
		}
		service.closeTerminalSubagentToolRecords(node.Children)
	}
}

// terminalSubagentStatus 报告子代理节点是否已经收口（终态三格：完成/失败/中断）。
func terminalSubagentStatus(status dto.SubAgentNodeStatus) bool {
	switch status {
	case dto.SubAgentDone, dto.SubAgentFailed, dto.SubAgentInterrupted:
		return true
	default:
		return false
	}
}
