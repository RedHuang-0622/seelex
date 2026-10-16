package core

import (
	"fmt"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/store/record"
	"strings"
	"sync"
)

// tool_record.go — **工具记录**（成功/失败计数 + 失败原因）的应用侧落点。
//
// 生态位：工具完成的判定分散在三条族边界上（主代理/后台会话的 `tool.completed`、
// 子代理的 `subagent.tool.completed`、teammate 的 `teammate.tool.completed`），
// 但"这次算不算失败"与"失败原因是哪一类"**只有一格词表**（`dto.ToolEventStatus` +
// `dto.ToolReason*`）。于是本文件负责三件事：
//
//  1. `toolStatusFailed` / `toolRecordReason` 是这两格的**唯一判据**（三条路径都调它）；
//  2. 读数**按会话**在内存里累加（`toolRecordAccumulators`），开关（设置里那一格）在这里判；
//  3. **批量落盘**：`FlushToolRecord` 在真相源提交时刻（草稿物化进真相源）把这一会话的
//     整份累计写进**会话自己的**存储——不做"每次工具调用写一次盘"（高频写会让磁盘一直忙，
//     2026-10-15 用户口径）。
//
// 口径（用户指定）：开关打开时**成功与失败都记**（没有分母的失败计数会把优化带偏）；
// 只有**结果帧**才记（running/未知词不是一次调用的结束）；失败还要记原因（饼图的数据）。

// toolRecordAccumulators 是**每会话**一份的内存累计（键 = 会话 ID）。
//
// 为什么按会话分：读数是会话作用域的（跟会话目录走），看板也只显示当前视图会话的读数；
// 进程级一份会让"切会话后看到别人的读数"。
type toolRecordAccumulators struct {
	mu     sync.Mutex
	byID   map[string]*record.Accumulator
	loaded map[string]bool
}

func newToolRecordAccumulators() *toolRecordAccumulators {
	return &toolRecordAccumulators{byID: map[string]*record.Accumulator{}, loaded: map[string]bool{}}
}

// accumulator 取（必要时建）某会话的累计。
func (table *toolRecordAccumulators) accumulator(sessionID string) *record.Accumulator {
	sessionID = strings.TrimSpace(sessionID)
	if table == nil || sessionID == "" {
		return nil
	}
	table.mu.Lock()
	defer table.mu.Unlock()
	if existing, ok := table.byID[sessionID]; ok {
		return existing
	}
	created := record.NewAccumulator()
	table.byID[sessionID] = created
	return created
}

// markLoaded 报告"这个会话还是第一次从存储读回"（每个会话只读一次，免得每次记录/看板都读盘）。
func (table *toolRecordAccumulators) markLoaded(sessionID string) bool {
	if table == nil {
		return false
	}
	table.mu.Lock()
	defer table.mu.Unlock()
	if table.loaded[sessionID] {
		return false
	}
	table.loaded[sessionID] = true
	return true
}

// SetToolRecordEnabled 开关计数（设置里那一个开关的落点）。
//
// 次序与 SetSessionReleaseWindow 同款：**先落盘、再改内存**——写盘失败 ⇒ 内存不动 ⇒
// 返回值与"重启后的值"一致，前端回滚到最后一份权威值是诚实的。宿主没装配落盘口
// （Deps.ToolRecordStore == nil）⇒ 只本次运行有效，不算失败。
//
// 关掉**不删**已记的读数：那是已经发生的事实；关只影响"接下来还记不记"。
func (service *Service) SetToolRecordEnabled(enabled bool) (bool, error) {
	if service == nil || service.components.toolRecord == nil {
		return false, nil
	}
	if service.Deps.ToolRecordStore != nil {
		if err := service.Deps.ToolRecordStore.SaveToolRecordEnabled(enabled); err != nil {
			return service.ToolRecordEnabled(), err
		}
	}
	service.components.toolRecord.setEnabled(enabled)
	return service.ToolRecordEnabled(), nil
}

// ApplyToolRecordEnabled 只改内存（**装配期**用：启动时把配置/覆盖层里的值装进来）。
//
// 与 SetToolRecordEnabled 的分工：那条是"用户在界面上拨开关"，必须先落盘再改内存；
// 这条是"启动装配"，值本来就从盘上来，再写一遍盘只会让"用户改过没有"这件事在
// 覆盖层文件里失真（每次启动都写成显式值），也白写一次磁盘。
func (service *Service) ApplyToolRecordEnabled(enabled bool) {
	if service == nil || service.components.toolRecord == nil {
		return
	}
	service.components.toolRecord.setEnabled(enabled)
}

// ToolRecordEnabled 返回当前开关状态（设置面板与看板都读它）。
func (service *Service) ToolRecordEnabled() bool {
	if service == nil || service.components.toolRecord == nil {
		return false
	}
	return service.components.toolRecord.enabledValue()
}

// FlushToolRecord 把一个会话的读数**批量**写进会话自己的存储（真相源提交时刻调一次）。
//
// 调用点：**一回合一次**（`runChat` 尾部的由忙转闲点）+ `materializeDraftSession`
// （草稿物化进真相源那一刻）+ Shutdown 兜底——粒度是"回合提交"，不是"每次工具调用"。
// 没有新东西（dirty=false）或没装配落盘口 ⇒ 空操作：没记过的会话不产生文件。
//
// 落盘失败**不回抛**（会话提交不该因为读数写不进去而失败）：错误留在槽位上，
// 读面（看板）拿它显示告警。
func (service *Service) FlushToolRecord(sessionID string) {
	store := service.toolRecordSnapshotStore()
	if store == nil || service.toolRecords == nil {
		return
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	accumulator := service.toolRecords.accumulator(sessionID)
	if accumulator == nil {
		return
	}
	snapshot, dirty := accumulator.Snapshot()
	if !dirty {
		return
	}
	if err := store.SaveToolRecordSnapshot(sessionID, snapshot); err != nil {
		service.components.toolRecord.noteFlushError(err)
		return
	}
	service.components.toolRecord.noteFlushError(nil)
}

// loadToolRecord 把该会话最后一次落盘的读数读回内存（每个会话只读一次）。
func (service *Service) loadToolRecord(sessionID string) {
	store := service.toolRecordSnapshotStore()
	if store == nil || service.toolRecords == nil {
		return
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" || !service.toolRecords.markLoaded(sessionID) {
		return
	}
	snapshot, ok, err := store.LoadToolRecordSnapshot(sessionID)
	if err != nil || !ok {
		return // 读不回来 = 当空读数（看板显示"还没记过"），不阻塞任何事
	}
	if accumulator := service.toolRecords.accumulator(sessionID); accumulator != nil {
		accumulator.Load(snapshot)
	}
}

// SetToolRecordSnapshotStore 注入（或收回：传 nil）读数落盘口。
//
// 为什么不是 Deps 字段而是装配后注入：它依赖存储后端与工作区解析（会话 → 项目作用域），
// 两者在装配顺序上都晚于 `wiring.New`（与 SetProjectBoardSink 同一处境）。
func (service *Service) SetToolRecordSnapshotStore(store contract.ToolRecordSnapshotStore) {
	if service == nil || service.components.toolRecord == nil {
		return
	}
	service.components.toolRecord.setSnapshotStore(store)
}

func (service *Service) toolRecordSnapshotStore() contract.ToolRecordSnapshotStore {
	if service == nil || service.components.toolRecord == nil {
		return nil
	}
	return service.components.toolRecord.snapshotStoreValue()
}

// flushAllToolRecords 把**所有**会话的读数批量落一次（停机兜底用）。
//
// 为什么停机也要落：批次落盘的天然缺口是"最后一批还没到提交点就退出了"——会话级累计
// 只活在内存里，不兜底就等于丢掉最后一段读数。这里逐个会话写整份快照（幂等：同一份
// 累计写两次结果一样）。
func (service *Service) flushAllToolRecords() {
	if service == nil || service.toolRecords == nil {
		return
	}
	service.toolRecords.mu.Lock()
	sessions := make([]string, 0, len(service.toolRecords.byID))
	for sessionID := range service.toolRecords.byID {
		sessions = append(sessions, sessionID)
	}
	service.toolRecords.mu.Unlock()
	for _, sessionID := range sessions {
		service.FlushToolRecord(sessionID)
	}
}

// ToolRecordBoard 返回**当前视图会话**的工具记录看板（TUI / GUI 同源读法）。
//
// 未注入读数表（测试/桩宿主）= 空看板（Enabled 仍如实报开关），不是错误：与其它可选
// 端口同口径——有就有、没有就是没装配，前端据此整块退场（不留空壳）。
// 读之前先懒加载一次该会话的落盘读数（重启后看板接着显示旧计数）。
func (service *Service) ToolRecordBoard() dto.ToolRecordBoard {
	board := dto.ToolRecordBoard{Enabled: service.ToolRecordEnabled()}
	if service == nil || service.toolRecords == nil {
		return board
	}
	sessionID := service.currentViewSessionID()
	service.loadToolRecord(sessionID)
	accumulator := service.toolRecords.accumulator(sessionID)
	if accumulator == nil {
		return board
	}
	total, success, failure := accumulator.Totals()
	board.Total, board.Success, board.Failure = total, success, failure
	board.Tools = accumulator.Stats()
	board.LedgerError = service.components.toolRecord.flushErrorMessage()
	return board
}

// RecordToolCommandOutcome 记一次**命令面**的结果（进程退出码）。
//
// 为什么需要它：命令工具（`bash`）的成功判据是**退出码**，不是"工具自己有没有报错"——
// 一条写错的命令会正常返回结果信封、退出码 1，工具层看它是成功。**只有一条命令的结果**
// 才算数，所以它与完成段那条路互斥（工具报错时没有退出码可报），不会重复计数。
//
// 退出码非零的原因归 `command-failed`（"这条命令失败了"是独立的一类修法：先看命令与 cwd，
// 而不是看权限/入参/通道）。上报口 = 工具层既有的 `bash.process.exited` 诊断事件
// （见 wiring/bootstrap 的 toolCommandOutcomeObserver：那条 stage 只由同步 bash 链发出）。
func (service *Service) RecordToolCommandOutcome(sessionID, tool string, exitCode int) {
	service.recordToolOutcome(sessionID, tool, exitCode != 0, fmt.Sprintf("exit code %d", exitCode))
}

// toolStatusFailed 是"这次工具调用算不算失败"的**唯一判据**（吃类型化状态那一格）。
func toolStatusFailed(status dto.ToolEventStatus) bool {
	return status == dto.ToolEventError
}

// toolRecordOutcomeForStatus 是**字符串状态**那两条链（teammate / 子代理）的唯一入口。
//
// 只有"结果帧"（success / error）才记：running 帧不是一次调用的结束，记它会把同一件事
// 记两笔（发起一笔 + 完成一笔），"成功率"就被自己的心跳稀释了；未知/零值词同样不记
// （认不得的词不该被当成成功——那是最容易造假的一种默认）。
func (service *Service) toolRecordOutcomeForStatus(sessionID, name, status, errorText string) {
	parsed, ok := dto.ParseToolEventStatus(strings.TrimSpace(status))
	if !ok || parsed == dto.ToolEventRunning || parsed == dto.ToolEventUnknown {
		return
	}
	service.recordToolOutcome(sessionID, name, toolStatusFailed(parsed), errorText)
}

// toolRecordReason 把失败文本归到**唯一一份原因词表**（dto.ToolReason*）。
//
// 为什么按文本归并（而不是让每个工具自带 reason code）：这是一次**有界**的取舍——
// 今天要的是"各原因占比"这个读数，不是一个精确的因果模型。词表按"修法是否同一件事"
// 切（权限/超时/业务拒绝/目标不存在/入参/冲突/通道/命令失败/其它），判不出来一律归 other：
// **宁可归到其它，也不要猜错原因**（猜错的原因分类会让优化走错方向，比没有分类更坏）。
//
// **已知局限**（规划）：自由文本分类是启发式。要把原因做成精确事实，得让每个工具在
// 报错处自带 reason code（那时本函数退化成"只读那个 code"的兜底）。
func toolRecordReason(text string) string {
	lowered := strings.ToLower(text)
	switch {
	case lowered == "":
		return dto.ToolReasonOther
	case containsAny(lowered, "permission", "denied", "unauthorized", "forbidden", "权限", "拒绝授权", "审批"):
		return dto.ToolReasonPermission
	case containsAny(lowered, "timeout", "timed out", "deadline exceeded", "超时", "超预算"):
		return dto.ToolReasonTimeout
	case containsAny(lowered, "拒绝", "refused", "rejected", "not allowed", "cannot", "不能", "不允许"):
		return dto.ToolReasonRefused
	case containsAny(lowered, "not found", "no such file", "cannot find", "does not exist", "unknown handle", "不存在", "找不到", "未知句柄"):
		return dto.ToolReasonNotFound
	case containsAny(lowered, "invalid", "required", "must have", "参数", "不合法", "缺少"):
		return dto.ToolReasonInvalidArgs
	case containsAny(lowered, "conflict", "already exists", "busy", "locked", "冲突", "已存在", "被占用"):
		return dto.ToolReasonConflict
	case containsAny(lowered, "exit code", "退出码"):
		return dto.ToolReasonCommandFailed
	case containsAny(lowered, "connection", "network", "dial", "eof", "account", "missing cli", "网络", "账号", "未装配"):
		return dto.ToolReasonTransport
	default:
		return dto.ToolReasonOther
	}
}

func containsAny(haystack string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(haystack, needle) {
			return true
		}
	}
	return false
}

// toolErrorText 取错误的**原始**文本（没有错误 = 空串）。
//
// 为什么单独一个函数：工具记录的归因必须吃原始错误，不能吃给模型看的包装文案
// （见 tool_hooks.go 完成段的说明）。它同时是 nil 安全的（完成段只判 `status`，
// 成功路径上 toolErr 就是 nil）。
func toolErrorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// recordToolOutcome 在三族工具完成路径上按同一判据记一笔（成功与失败都记）。
//
// 它是**唯一**的调用口：三条路径各自知道"哪个会话、是谁、什么状态、错误原文"，本函数
// 负责"开关开了没""该不该记""原因归哪一格"与"记到哪份累计"。**只进内存**——落盘是批次
// （FlushToolRecord），不做逐次写盘。
func (service *Service) recordToolOutcome(sessionID, tool string, failed bool, errorText string) {
	if service == nil || strings.TrimSpace(tool) == "" || service.toolRecords == nil {
		return
	}
	if !service.ToolRecordEnabled() {
		return // 关就是关：不计数、不落盘
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return // 没有会话归属的调用不记（记不到任何会话的读数里）
	}
	accumulator := service.toolRecords.accumulator(sessionID)
	if accumulator == nil {
		return
	}
	reason := ""
	if failed {
		reason = toolRecordReason(errorText)
	}
	accumulator.Record(tool, !failed, reason)
}
