// plan / task / goal 栈通道的条目结构与迁移语义（my_design §2.4/§3.1/§4）。
//
// 事实模型（后端无关，落盘动作见 stack_journal*.go）：
//   - 条目是 STACK_ITEM 行：JSON 后端落 session/{plan,task,goal}/
//     {active,history}.jsonl，SQL/Redis 后端落各自的表/键；head 只存水位
//     （§2.0 规则 1），条目内容不进 head；
//   - 批次语义（§0 条目 4）：批次内未完成 → 整批留 active；全部完成 → 整批
//     弹栈归档；goal 是单条目批次（LIFO，只有栈顶可弹）；
//   - history 锚（§4）：item_message_id = 条目进入 active 时的 message 坐标；
//     batch_message_from = 批次首条目坐标；batch_message_to = 整批弹栈坐标；
//   - 状态迁移由 EVENT（plan.*/task.*/goal.*）记录（§2.4 + 附录 A.1）；
//   - 写序 = 数据 → head（发布点）→ EVENT（I5）；head 未发布的投影按条目
//     revision 判定不可见，崩溃后不需要清理磁盘。
//
// 变更入口就是下面这组 stack* 函数：它们只构造 StackMutation 并交给
// stackCommit（actor 临界区），本身不碰任何后端。
package sessionstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// StackKind 是设计稿 §2.4 的三类栈（ER STACK.kind）。
type StackKind string

const (
	// StackKindPlan 是计划栈（批次弹栈）。
	StackKindPlan StackKind = "plan"
	// StackKindTask 是任务栈（批次弹栈）。
	StackKindTask StackKind = "task"
	// StackKindGoal 是 goal 治理栈（单条目批次，LIFO）。
	StackKindGoal StackKind = "goal"
	// StackKindSubagent 是第四栈：subagent 批次（S18，§2.4/§8.2）。条目 =
	// 一次派发的子代理现场，含结果状态；子代理子树不入主会话三栈 fork 拷贝。
	StackKindSubagent StackKind = "subagent"
)

// terminalStackStatus 是「批次内该项已完成」的状态集合：批次内全部条目落在这
// 一集合内才整批弹栈归档。needs_user_decision / paused / active 不算完成。
var terminalStackStatus = map[string]bool{
	"closed":    true,
	"completed": true,
	"failed":    true,
	"aborted":   true,
	"archived":  true,
	"done":      true,
}

// StackItemInput 是一次入栈请求的条目（payload 为该域自有结构，栈通道按原样
// 存取，不解释语义）。
type StackItemInput struct {
	ItemID  string
	Kind    StackKind
	Status  string
	Payload json.RawMessage
	// RoleName / RoleSessionID 是群聊角色归属（§8.3/R2）。goal 栈条目用它记录
	// techleader 子会话锚；其它栈未用到时留空。
	RoleName      string
	RoleSessionID string
	// EnteredAt 为零值时由通道按当前时间盖章。
	EnteredAt time.Time
}

// StackItemRecord 是 ER STACK_ITEM 的一行（含批次与 message 锚）。
type StackItemRecord struct {
	ItemID  string          `json:"item_id"`
	BatchID string          `json:"batch_id"`
	StackID string          `json:"stack_id"`
	Kind    StackKind       `json:"kind"`
	Seq     uint64          `json:"seq"`
	Status  string          `json:"status"`
	Payload json.RawMessage `json:"payload,omitempty"`
	// RoleName / RoleSessionID 是群聊角色归属（§8.3/R2）。
	RoleName      string `json:"role_name,omitempty"`
	RoleSessionID string `json:"role_session_id,omitempty"`
	// Revision 是发布该投影时的 stack head_seq；reader 只承认 revision ≤ head
	// 的行（head 未发布 = 未提交）。
	Revision uint64 `json:"revision"`
	// ItemMessageID/ItemMessageSeq = 条目进入 active 时的 message 坐标。
	ItemMessageID  string `json:"item_message_id,omitempty"`
	ItemMessageSeq uint64 `json:"item_message_seq,omitempty"`
	// BatchMessageFrom/BatchMessageTo = 批次首条目坐标 / 整批弹栈坐标。
	BatchMessageFrom string `json:"batch_message_from,omitempty"`
	BatchMessageTo   string `json:"batch_message_to,omitempty"`
	// BatchMessageToSeq 是弹栈坐标的 seq 形式（fork 按 message 点重建栈快照用）。
	BatchMessageToSeq uint64    `json:"batch_message_to_seq,omitempty"`
	EnteredAt         time.Time `json:"entered_at"`
	ClosedAt          time.Time `json:"closed_at,omitempty"`
}

// stackWatermark 是单个 kind 的水位（head 只装水位，不装条目）。
type stackWatermark struct {
	HeadSeq      uint64 `json:"head_seq"`
	ActiveCount  int    `json:"active_count"`
	HistoryCount uint64 `json:"history_count"`
	// HistoryBytes 是归档数据文件在 head 发布时刻的字节长度（JSON 后端用它
	// 回收 head 未发布的归档尾行，使写路径完全不必解析归档文件）。
	HistoryBytes uint64 `json:"history_bytes,omitempty"`
	// LastCommitID 是该 kind 最近一次已发布提交的逻辑操作凭据（§2.0 规则 4、
	// D10/D13/S17：逐操作唯一、确定性推导，供发布失败后的重放判重）。
	LastCommitID string `json:"last_commit_id,omitempty"`
}

// stackModuleHead 是 metadata/stack.json payload：只有水位，不含条目内容。
type stackModuleHead struct {
	SessionID string                       `json:"session_id"`
	HeadSeq   uint64                       `json:"head_seq"`
	Kinds     map[StackKind]stackWatermark `json:"kinds,omitempty"`
}

func validStackKind(kind StackKind) bool {
	return kind == StackKindPlan || kind == StackKindTask ||
		kind == StackKindGoal || kind == StackKindSubagent
}

// stackKindIndex 把 kind 映射到固定下标（内存读投影数组用）。
func stackKindIndex(kind StackKind) int {
	switch kind {
	case StackKindTask:
		return 1
	case StackKindGoal:
		return 2
	case StackKindSubagent:
		return 3
	default:
		return 0
	}
}

// StackMutation 描述一次栈变更的结果（供上层与 EVENT 使用）。
type StackMutation struct {
	Revision  uint64
	Pushed    []StackItemRecord
	Updated   []StackItemRecord
	Archived  []StackItemRecord
	BatchID   string
	BatchDone bool
}

// stackState 是一次提交内可见的栈投影（actor 私有状态：只在提交临界区内被
// 触碰，读者拿到的是发布后的不可变副本）。
type stackState struct {
	revision uint64
	nextSeq  uint64
	active   []StackItemRecord
	archived []StackItemRecord
	// anchorID/anchorSeq 是提交时的已发布 message 坐标（§4 锚）。
	anchorID  string
	anchorSeq uint64
}

func (state *stackState) batchItems(batchID string) []StackItemRecord {
	var out []StackItemRecord
	for _, row := range state.active {
		if row.BatchID == batchID {
			out = append(out, row)
		}
	}
	return out
}

func (state *stackState) batchDone(batchID string) bool {
	batch := state.batchItems(batchID)
	if len(batch) == 0 {
		return false
	}
	for _, row := range batch {
		if !terminalStackStatus[row.Status] {
			return false
		}
	}
	return true
}

func (state *stackState) remove(itemID string) {
	out := state.active[:0]
	for _, row := range state.active {
		if row.ItemID != itemID {
			out = append(out, row)
		}
	}
	state.active = out
}

// closeBatch 把整批从 active 移出并盖上批次弹栈坐标。
func (state *stackState) closeBatch(batchID, status string) []StackItemRecord {
	var batch []StackItemRecord
	out := state.active[:0]
	for _, row := range state.active {
		if row.BatchID != batchID {
			out = append(out, row)
			continue
		}
		if status != "" {
			row.Status = status
		}
		batch = append(batch, row)
	}
	state.active = out
	return state.archive(batch)
}

// archive 给弹栈条目盖批次弹栈坐标并登记待归档（revision = 本次发布）。
func (state *stackState) archive(batch []StackItemRecord) []StackItemRecord {
	if len(batch) == 0 {
		return nil
	}
	to := batch[0].ItemMessageID
	toSeq := batch[0].ItemMessageSeq
	for _, row := range batch {
		if row.ItemMessageSeq >= toSeq {
			to, toSeq = row.ItemMessageID, row.ItemMessageSeq
		}
	}
	out := make([]StackItemRecord, 0, len(batch))
	for index := range batch {
		row := batch[index]
		row.Revision = state.revision
		row.StackID = string(row.Kind) + "|history"
		if row.BatchMessageTo == "" {
			row.BatchMessageTo = to
			row.BatchMessageToSeq = toSeq
		}
		if row.BatchMessageFrom == "" {
			row.BatchMessageFrom = row.ItemMessageID
		}
		out = append(out, row)
	}
	state.archived = append(state.archived, out...)
	return out
}

// pushItem 将一条 StackItemInput 落成 active 行（压栈语义的唯一实现点）。
func (state *stackState) pushItem(item StackItemInput, batchID, batchFrom string) (StackItemRecord, error) {
	if item.ItemID == "" {
		return StackItemRecord{}, errors.New("session storage: stack item requires item_id")
	}
	if containsStackItem(state.active, item.ItemID) {
		return StackItemRecord{}, fmt.Errorf("session storage: stack item %q already active", item.ItemID)
	}
	state.nextSeq++
	entered := item.EnteredAt
	if entered.IsZero() {
		entered = time.Now().UTC()
	}
	status := item.Status
	if status == "" {
		status = "active"
	}
	record := StackItemRecord{
		ItemID: item.ItemID, BatchID: batchID, StackID: string(item.Kind) + "|active",
		Kind: item.Kind, Seq: state.nextSeq, Status: status, Payload: item.Payload,
		RoleName: item.RoleName, RoleSessionID: item.RoleSessionID,
		Revision:         state.revision,
		ItemMessageID:    state.anchorID,
		ItemMessageSeq:   state.anchorSeq,
		BatchMessageFrom: batchFrom,
		EnteredAt:        entered,
	}
	state.active = append(state.active, record)
	return record, nil
}

func containsStackItem(rows []StackItemRecord, itemID string) bool {
	return slices.ContainsFunc(rows, func(row StackItemRecord) bool { return row.ItemID == itemID })
}

// maxSeqOf 返回一组条目行的最大 seq。
func maxSeqOf(rows []StackItemRecord) uint64 {
	var top uint64
	for _, row := range rows {
		if row.Seq > top {
			top = row.Seq
		}
	}
	return top
}

func maxU64(left, right uint64) uint64 {
	if left > right {
		return left
	}
	return right
}

// stackMutationCommitID 把一次栈变更映射为确定性、逐操作唯一的 commit_id
// （§2.0 规则 4 / D13 / S17）。凭据由操作身份推出：kind + 迁移类型 + 条目
// （batch/item/status），不含存储层现造号，也不含坐标/时间等重放时会漂移的
// 字段——同一逻辑操作的重放得到同一个凭据，不同操作（哪怕同一批同一项）
// 因状态不同而不同。
func stackMutationCommitID(kind StackKind, mutation StackMutation) string {
	var parts []string
	for _, row := range mutation.Pushed {
		parts = append(parts, "p|"+row.BatchID+"|"+row.ItemID+"|"+row.Status)
	}
	for _, row := range mutation.Updated {
		parts = append(parts, "u|"+row.BatchID+"|"+row.ItemID+"|"+row.Status)
	}
	for _, row := range mutation.Archived {
		parts = append(parts, "a|"+row.BatchID+"|"+row.ItemID+"|"+row.Status)
	}
	slices.Sort(parts)
	identity := string(kind) + "|" + strings.Join(parts, "&")
	return "stack-" + string(kind) + "-" + hash(identity)
}

// dedupeStackRowsByItemID 按条目键去重、高 revision 胜出（§2.0 规则 4 读侧、
// D10/S17：冷重载后重放留下的同 item_id 双行只保留最高 revision）。
func dedupeStackRowsByItemID(rows []StackItemRecord) []StackItemRecord {
	best := make(map[string]StackItemRecord, len(rows))
	order := make([]string, 0, len(rows))
	for _, row := range rows {
		if existing, ok := best[row.ItemID]; !ok || row.Revision > existing.Revision {
			if !ok {
				order = append(order, row.ItemID)
			}
			best[row.ItemID] = row
		}
	}
	out := make([]StackItemRecord, 0, len(best))
	for _, itemID := range order {
		out = append(out, best[itemID])
	}
	return out
}

// ---------- actor 消息：一次栈变更的闭包工厂 ----------

// stackPushMessage 压入一批条目（batchID 为空 = 本次入栈自成一批）。
func stackPushMessage(kind StackKind, batchID string, items []StackItemInput) func(*stackState) (StackMutation, error) {
	return func(state *stackState) (StackMutation, error) {
		if len(items) == 0 {
			return StackMutation{}, nil
		}
		if batchID == "" {
			batchID = "batch-" + randomID()
		}
		// batch_message_from = 批次首条目坐标（同批共享）；item_message_id 逐条
		// 用「进入 active 时」的当前坐标（§4）。
		batchFrom := state.anchorID
		for _, existing := range state.active {
			if existing.BatchID == batchID {
				batchFrom = existing.BatchMessageFrom
				break
			}
		}
		mutation := StackMutation{BatchID: batchID}
		for _, item := range items {
			if item.Kind == "" {
				item.Kind = kind
			}
			record, err := state.pushItem(item, batchID, batchFrom)
			if err != nil {
				return StackMutation{}, err
			}
			mutation.Pushed = append(mutation.Pushed, record)
		}
		return mutation, nil
	}
}

// stackSetStatusMessage 更新条目状态；所属批次全部完成时整批弹栈归档（§2.4）。
func stackSetStatusMessage(itemID, status string) func(*stackState) (StackMutation, error) {
	return func(state *stackState) (StackMutation, error) {
		index := slices.IndexFunc(state.active, func(row StackItemRecord) bool { return row.ItemID == itemID })
		if index < 0 {
			return StackMutation{}, fmt.Errorf("session storage: stack item %q is not on the stack", itemID)
		}
		target := state.active[index]
		target.Status = status
		if terminalStackStatus[status] {
			target.ClosedAt = time.Now().UTC()
		}
		state.active[index] = target
		mutation := StackMutation{Updated: []StackItemRecord{target}}
		if terminalStackStatus[status] && state.batchDone(target.BatchID) {
			mutation.BatchID = target.BatchID
			mutation.Archived = state.closeBatch(target.BatchID, status)
			mutation.BatchDone = true
		}
		return mutation, nil
	}
}

// stackPopTopMessage 弹出栈顶条目（goal LIFO：只有栈顶可收口）。
func stackPopTopMessage(itemID, status string) func(*stackState) (StackMutation, error) {
	return func(state *stackState) (StackMutation, error) {
		if len(state.active) == 0 {
			return StackMutation{}, errors.New("session storage: stack is empty")
		}
		top := state.active[len(state.active)-1]
		if itemID != "" && top.ItemID != itemID {
			return StackMutation{}, fmt.Errorf("session storage: %q is not the stack top (top=%q)", itemID, top.ItemID)
		}
		if status != "" {
			top.Status = status
		}
		if top.ClosedAt.IsZero() {
			top.ClosedAt = time.Now().UTC()
		}
		state.active = state.active[:len(state.active)-1]
		archived := state.archive([]StackItemRecord{top})
		return StackMutation{BatchID: top.BatchID, Archived: archived, BatchDone: true}, nil
	}
}

// stackCloseBatchMessage 显式把整批标记为完成并归档（调用方已知批次收口时用）。
func stackCloseBatchMessage(batchID, status string) func(*stackState) (StackMutation, error) {
	return func(state *stackState) (StackMutation, error) {
		if len(state.batchItems(batchID)) == 0 {
			return StackMutation{}, fmt.Errorf("session storage: batch %q is not on the stack", batchID)
		}
		return StackMutation{BatchID: batchID, Archived: state.closeBatch(batchID, status), BatchDone: true}, nil
	}
}

// stackReplaceMessage 用新的条目集合替换 active 栈，并推导出逐条迁移：
// 集合中缺失的条目按其末态弹栈归档、新条目压栈、状态变化者更新（goal 域
// Controller 的「保存当前栈投影」语义落到 §2.4 的逐条迁移 + EVENT 记录）。
func stackReplaceMessage(kind StackKind, items []StackItemInput, closedStatus string) func(*stackState) (StackMutation, error) {
	return func(state *stackState) (StackMutation, error) {
		var mutation StackMutation
		keep := make(map[string]bool, len(items))
		for _, item := range items {
			keep[item.ItemID] = true
		}
		// 1) 先归档已不在新投影中的条目（按条目收口）。
		for _, row := range slices.Clone(state.active) {
			if keep[row.ItemID] {
				continue
			}
			status := row.Status
			if !terminalStackStatus[status] {
				status = closedStatus
				if status == "" {
					status = "closed"
				}
			}
			row.Status = status
			row.ClosedAt = time.Now().UTC()
			state.remove(row.ItemID)
			mutation.Archived = append(mutation.Archived, state.archive([]StackItemRecord{row})...)
		}
		// 2) 再落新增/变更。
		for _, item := range items {
			if containsStackItem(state.active, item.ItemID) {
				index := slices.IndexFunc(state.active, func(row StackItemRecord) bool { return row.ItemID == item.ItemID })
				existing := state.active[index]
				changed := false
				if item.Status != "" && item.Status != existing.Status {
					existing.Status = item.Status
					changed = true
					if terminalStackStatus[item.Status] {
						existing.ClosedAt = time.Now().UTC()
					}
				}
				if len(item.Payload) > 0 && string(item.Payload) != string(existing.Payload) {
					existing.Payload = item.Payload
					changed = true
				}
				if changed {
					state.active[index] = existing
					mutation.Updated = append(mutation.Updated, existing)
				}
				continue
			}
			if item.Kind == "" {
				item.Kind = kind
			}
			record, err := state.pushItem(item, "batch-"+item.ItemID, state.anchorID)
			if err != nil {
				return StackMutation{}, err
			}
			mutation.Pushed = append(mutation.Pushed, record)
		}
		return mutation, nil
	}
}

// stackEventPayload 是栈迁移 EVENT 的 payload（附录 A.1：只记摘要，不复制正文）。
func stackEventPayload(row StackItemRecord) json.RawMessage {
	payload, _ := json.Marshal(map[string]any{
		"item_id": row.ItemID, "batch_id": row.BatchID, "status": row.Status,
		"item_message_id": row.ItemMessageID, "seq": row.Seq,
		"role_name": row.RoleName, "role_session_id": row.RoleSessionID,
	})
	return payload
}

// stackTransitionEvents 把一次迁移展开成 EVENT 行（plan.*/task.*/goal.*）。
func stackTransitionEvents(kind StackKind, mutation StackMutation) []structuralEvent {
	rows := make([]structuralEvent, 0, len(mutation.Pushed)+len(mutation.Updated)+len(mutation.Archived))
	appendRow := func(row StackItemRecord) {
		rows = append(rows, structuralEvent{
			Kind:            structuralEventKind(string(kind) + "." + row.Status),
			AnchorMessageID: row.ItemMessageID,
			AnchorSeq:       row.ItemMessageSeq,
			Payload:         stackEventPayload(row),
		})
	}
	for _, row := range mutation.Pushed {
		appendRow(row)
	}
	for _, row := range mutation.Updated {
		appendRow(row)
	}
	for _, row := range mutation.Archived {
		appendRow(row)
	}
	return rows
}
