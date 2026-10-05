package workunit

// progress.go — 工作单元的**进度读面**（只读）：把"一层共用的那张会话记录"折算成给人看的
// 进度读数，并把契约已钉住的 `SessionLedger`（Save/List）包成"按 nodeID 读一件事此刻的进度"。
//
// 为什么这一格属于 workunit（勘定见 docs/arch/workunit-progress-read-surface.md）：
//
//	① 数据形状早就统一——两条链都落同一张 `sessionstore.NodeSessionRecord`、都经同一个
//	   `workunit.SessionLedger` 读写；真正"看不懂"的原因是「记录 → 进度」这一步被**手写了三处**
//	   （subagent 写侧 `buildRecordLocked`、读侧 `restoreLocked`、teammate 侧
//	   `saveTeamUnitRecord`/`teamUnitRecords`）——同一件事的第二、第三份实现。
//	② `StagesJSON` 的载荷（`[{"stage":…,"preview":…}]`）两层本来就在编/解同一串，只是各自的
//	   Go 类型不同：这份编解码因此是**既成事实的形状**，不是新造的抽象。
//	③ `Progress` 的每个字段都逐一对应记录里**已有**的字段：不新增存储、不新增事件流、不新增
//	   归属（`Kind` 只是描述性标注）。
//
// **本轮只把读法定形**（本文件是"形状"），三份手写折算的**合并**归下一波步骤②——它跨
// `seelebridge/session/` 与 `seelebridge/workunit_team_records.go`，与两张作业表合一同一批做。
// 因此本文件今天**还没有生产调用者**（见交付报告「未决项」），这不要紧：形状先立住，
// 合并时只做"转调"而不改语义。
//
// 刻意**不**进这里（列出来才是"没有强塞"）：
//
//   - 写侧（`Begin/Finish/Reclaim`）——已在 contract.go；读面只管读；
//   - 归属 / 身份轴（team_id / work_item / role）——`Progress` 只带 `NodeID/SessionID`；
//     两个身份轴合并会让字段名说谎（先例：`dto.RoleToolActivity` 与 `SubagentToolEvent` 的裁决）；
//   - 实时事件流（stage/tool/assistant 增量）——载体与粒度两层不同，把事件流塞进契约包
//     就是"第二份进度真相"（contract.go 已把它列为刻意不装的东西）；
//   - 编排计划（milestones / work_items）与执行树——各层专有，是别的投影的事。

import (
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/RedHuang-0622/seelex/sessionstore"
)

// StagePreviewLimit 是一条打点的预览字符上限：记录会进恢复说明（system 注入）与详情面板，
// 必须有界。240 与 teammate 侧既有的 `teamUnitPreviewLimit` 同口径——合并折算时不会因为它
// 而改变任何一条现存记录的落盘内容。
const StagePreviewLimit = 240

// Stage 是一条打点（阶段 + 有界预览）：与 `NodeSessionRecord.StagesJSON` 的载荷同形状。
//
// 它同时是 subagent 的 `model.NodeStageLog` 与 teammate 的 `teamUnitStages` 的共同抽象面——
// 两边本来就在编/解同一串 `[{"stage":…,"preview":…}]`，只是各自的 Go 类型不同。
type Stage struct {
	Stage   string `json:"stage"`
	Preview string `json:"preview,omitempty"`
}

// EncodeStages 把打点表编成记录里的那串载荷：**唯一一份**（含预览的 rune 上限与空阶段丢弃）。
//
// 空表返回 nil（不是 `[]`）：记录里"没有打点"这一格是**零值**，而不是一条空数组——回灌与
// 恢复说明都按零值判"没到过任何阶段"。
func EncodeStages(stages []Stage) []byte {
	normalized := make([]Stage, 0, len(stages))
	for _, stage := range stages {
		name := strings.TrimSpace(stage.Stage)
		if name == "" {
			continue
		}
		normalized = append(normalized, Stage{Stage: name, Preview: clipStagePreview(stage.Preview)})
	}
	if len(normalized) == 0 {
		return nil
	}
	payload, err := json.Marshal(normalized)
	if err != nil {
		return nil
	}
	return payload
}

// DecodeStages 解出记录里的打点表：**唯一一份**。
//
// 它不编造：空载荷 / `null` / 解不动（历史里落过别的形状）一律返回 nil——读面宁可说"这一格
// 没有事实"，也不把半个载荷猜成打点。
func DecodeStages(payload []byte) []Stage {
	if len(payload) == 0 {
		return nil
	}
	trimmed := strings.TrimSpace(string(payload))
	if trimmed == "" || trimmed == "null" {
		return nil
	}
	var stages []Stage
	if err := json.Unmarshal(payload, &stages); err != nil {
		return nil
	}
	if len(stages) == 0 {
		return nil
	}
	return stages
}

// clipStagePreview 把一段正文压成有界的一行（按 rune 裁，避免切断多字节字符）。
func clipStagePreview(text string) string {
	flat := strings.Join(strings.Fields(text), " ")
	if runes := []rune(flat); len(runes) > StagePreviewLimit {
		return string(runes[:StagePreviewLimit]) + "…"
	}
	return flat
}

// Progress 是一个工作单元"此刻跑到哪、结论是什么、还在不在跑"的读数——**两层交集**，也是
// 读面唯一的事实形状。
//
// 它只是 `NodeSessionRecord` 的**只读折算**：字段逐一对应记录里已有的格子，没有一个新事实源。
// 记录里没有的格子留零值 = "这件事没有这一格事实"（例如 teammate 刻意不写
// `StartedAt/EndedAt`——哪一刻开始、落定是**计划**里 `item.StartedAt/FinishedAt` 的事实），
// 不是"读数没算"。
type Progress struct {
	// Kind 是描述性标注（日志 / 审计 / 恢复说明前缀族）：**不是**分支判据。
	Kind Kind `json:"kind"`
	// NodeID 是这一份现场的身份：subagent = 节点 id；teammate = `<role>-<itemID>` / `<role>`。
	NodeID string `json:"node_id"`
	// SessionID 是这一件活自己的会话（记录落在哪个会话目录由账本作用域决定，不在这里）。
	SessionID string `json:"session_id"`
	Goal      string `json:"goal,omitempty"`
	// Status 是记录里的**原词**：终态由写方按自己的语义定名（teammate 记 done|failed），
	// 契约不替它写死。
	Status string `json:"status"`
	// InFlight = InFlight(Status)，是"还在不在跑"的**唯一**答案（词表只有一份）。
	InFlight  bool                            `json:"in_flight"`
	Summary   string                          `json:"summary,omitempty"`
	Error     string                          `json:"error,omitempty"`
	Stages    []Stage                         `json:"stages,omitempty"`
	Worktree  sessionstore.NodeWorktreeRecord `json:"worktree,omitempty"`
	UpdatedAt time.Time                       `json:"updated_at"`
}

// ProgressOf 把一条会话记录折成进度读数——**唯一一份折算**。
//
// 收编目标（步骤②）：subagent 写侧兜底与读侧还原（`seelebridge/session/subagent_sessions.go`
// 的 `buildRecordLocked` / `restoreLocked`）、teammate 折算（`workunit_team_records.go` 的
// `saveTeamUnitRecord` / `teamUnitRecords`）全部转调这里。本轮只把折算**形状**定下来：
// 它**不编造**记录里没有的事实，也不替任何一层写终态词表。
func ProgressOf(kind Kind, record sessionstore.NodeSessionRecord) Progress {
	return Progress{
		Kind:      kind,
		NodeID:    strings.TrimSpace(record.NodeID),
		SessionID: strings.TrimSpace(record.SessionID),
		Goal:      record.Goal,
		Status:    strings.TrimSpace(record.Status),
		InFlight:  InFlight(record.Status),
		Summary:   record.Summary,
		Error:     record.Error,
		Stages:    DecodeStages(record.StagesJSON),
		Worktree:  record.Worktree,
		UpdatedAt: record.UpdatedAt,
	}
}

// UnitReader 是"**按 nodeID 读一件事此刻的进度**"的读面：把契约已钉住的 `SessionLedger`
// （Save/List）包成一份按 NodeID 定位的读法。
//
// 为什么需要它：两条链现在都在手写"List → 线性查找 → 折算"（subagent 的 `ledgerRecord`、
// teammate 的 `teamUnitRecords`），读法已经是同一件事的第二、第三份；包成一份之后，前端要读
// 的"这件事跑到哪"只有一个后端入口。
//
// 它**不新增依赖、不新增存储**：只读 `SessionLedger`。
type UnitReader struct {
	ledger        SessionLedger
	projectID     string
	mainSessionID string
	kind          Kind
	owned         func(sessionstore.NodeSessionRecord) bool
}

// NewUnitReader 建一个**面向一层**的读面。
//
//	kind  = 这一层的读数标注（记录形状里没有 Kind 这一格，归属只有调用方知道）；
//	owned = 归属过滤：两层**共用同一张记录表**（同一个 `subagents/` 目录），因此"这条记录
//	        属不属于本次读"由调用方给出（teammate 侧今天就是按现场名单过滤的）。
//	        nil = 调用方保证这张账本只装这一层的记录。
func NewUnitReader(ledger SessionLedger, projectID, mainSessionID string, kind Kind, owned func(sessionstore.NodeSessionRecord) bool) *UnitReader {
	return &UnitReader{
		ledger:        ledger,
		projectID:     strings.TrimSpace(projectID),
		mainSessionID: strings.TrimSpace(mainSessionID),
		kind:          kind,
		owned:         owned,
	}
}

// Read 返回一件事的进度读数。
//
// 无存储 / 无记录 / 被归属过滤掉 → found=false（**正常读数，不是错误**）：未装配账本与"这件
// 事还没跑到过"对读面是同一件事——没有这一格事实。真读不动存储才返回 error。
func (r *UnitReader) Read(nodeID string) (Progress, bool, error) {
	records, err := r.records()
	if err != nil {
		return Progress{}, false, err
	}
	nodeID = strings.TrimSpace(nodeID)
	for _, record := range records {
		if strings.TrimSpace(record.NodeID) != nodeID {
			continue
		}
		return ProgressOf(r.kind, record), true, nil
	}
	return Progress{}, false, nil
}

// List 返回本账本里**归属于本读面**的全部单元进度读数（按 NodeID 稳定排序）。
//
// 跨层混排时由构造时的 `owned` 过滤；`Kind` 随每一份读数交回，调用方不必自己再猜一遍。
func (r *UnitReader) List() ([]Progress, error) {
	records, err := r.records()
	if err != nil {
		return nil, err
	}
	progress := make([]Progress, 0, len(records))
	for _, record := range records {
		progress = append(progress, ProgressOf(r.kind, record))
	}
	sort.SliceStable(progress, func(i, j int) bool { return progress[i].NodeID < progress[j].NodeID })
	return progress, nil
}

// records 读回本会话的单元记录（未装配账本 = 空读数；归属过滤在这里统一落地）。
func (r *UnitReader) records() ([]sessionstore.NodeSessionRecord, error) {
	if r == nil || r.ledger == nil {
		return nil, nil
	}
	records, err := r.ledger.List(r.projectID, r.mainSessionID)
	if err != nil {
		return nil, err
	}
	if r.owned == nil {
		return records, nil
	}
	kept := make([]sessionstore.NodeSessionRecord, 0, len(records))
	for _, record := range records {
		if r.owned(record) {
			kept = append(kept, record)
		}
	}
	return kept, nil
}
