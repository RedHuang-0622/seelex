package dto

import "time"

// tool_record.go — **工具记录**（成功/失败计数 + 失败原因）的只读投影。
//
// 生态位：读数是**会话级**台账（`<sessionRoot>/tool-record.jsonl`，实现 =
// `application/execution/toolrecord` + `store/tool_record.go`），经 `Application.ToolRecordBoard()`
// 暴露**当前视图会话**的读数给 TUI / GUI。
//
// 它刻意**不进** `Snapshot`（V10 用户裁决，2026-10-15）：快照是会话作用域的可重放
// 状态，看板是按需拉取的派生读数；把计数塞进快照等于让每次工具调用都推进快照修订号。
// "这台机器哪个工具最该优化"的**整机**读数不在本形状里——真需要时另做显式汇总
// （读多份 JSONL），仍然不进快照。
//
// 口径（用户指定，2026-10-15）：
//   - 计数**可以关**（设置里开关）：关 = 不记、不落盘、看板空；
//   - 开之后**成功与失败都记**（否则算不出占比——只有分子没有分母的表会把优化带偏）；
//   - 失败还要记**原因**（饼图画的是各原因占比，不是按工具切）。
type ToolRecordBoard struct {
	// Enabled 是计数开关的当前值（false = 这一版没有在记）。
	Enabled bool `json:"enabled"`
	// Total / Success / Failure 是所有工具的合计（柱形图的总量口径）。
	Total   int `json:"total"`
	Success int `json:"success"`
	Failure int `json:"failure"`
	// Tools 按**总调用次数**降序（并列按首次出现序）：柱形图从左到右就是"用得最多"到"最少"。
	Tools []ToolRecordStat `json:"tools,omitempty"`
	// LedgerError 是台账**落盘**失败的可读说明（空 = 正常）。
	//
	// 为什么必须有这一格：内存计数与落盘是两条路（落盘失败不丢计数、也不阻塞回合），
	// 那就必须有人把"写不进去"说出来——否则看板显示的是"还没落盘的那段计数"，用户会以为
	// 它就是全部真相。
	LedgerError string `json:"ledger_error,omitempty"`
}

// ToolRecordStat 是一个工具的一格：成功 / 失败 / 各失败原因。
//
// 柱形图的一根柱子就是它（上半天 = Success，下半天 = Failure，高度按 Total 缩放）；
// 点开后画的是 Reasons（失败原因占比）。
type ToolRecordStat struct {
	Tool    string `json:"tool"`
	Success int    `json:"success"`
	Failure int    `json:"failure"`
	// Total 是 Success + Failure（读起来不用前端再加一次；排序也用这一格）。
	Total int `json:"total"`
	// LastAt 是最后一次**失败**时刻（"这个工具最近还在坏吗"）。
	LastAt  time.Time          `json:"last_at,omitzero"`
	Reasons []ToolRecordReason `json:"reasons,omitempty"`
}

// ToolRecordReason 是一个工具名下某一类失败原因的计数（饼图的一片）。
type ToolRecordReason struct {
	// Reason 取 ToolReason* 里的稳定标识；前端负责把它翻成人话标签。
	Reason string `json:"reason"`
	Count  int    `json:"count"`
}

// ToolRecordSnapshot 是**落盘**的那一份读数（一行一批：每一行都是"到目前为止的整份累计"）。
//
// 为什么是整份快照而不是增量行：落盘点是**批次**的（会话粒度 + 真相源提交时统一更新，
// 见 application/core 的 flushToolRecord），增量行只会让文件越来越长、读侧还要全量折叠；
// 整份快照"取最后一行"就是当前读数，行数 = 批次数。
//
// 不含开关与台账告警：那两格是运行期状态（`ToolRecordBoard` 才带），不是持久事实。
type ToolRecordSnapshot struct {
	At    time.Time        `json:"at"`
	Tools []ToolRecordStat `json:"tools,omitempty"`
}

// 失败原因词表（**稳定标识**：台账落盘、看板读回、前端标签三处都用它，不许各写一套字面量）。
//
// 它与"是谁报的错"无关，只回答"这一类的修法是不是同一件事"：
//   - permission  → 权限/审批拒绝：改权责档或补审批；
//   - timeout     → 超时/超预算：改预算或改走后台；
//   - refused     → 显式业务拒绝（团队闸门、范围闸门）：改调用姿势；
//   - not-found   → 目标不存在（文件/句柄/会话）：先确认目标；
//   - invalid-args→ 入参不合法：改 schema 或改提示词；
//   - conflict    → 并发/状态冲突（已被占用、已存在）：重读状态再动；
//   - transport   → 外部通道故障（网络/账号/CLI 缺失）：改环境或重试；
//   - other       → 判不出来（**保守默认**：宁可归到其它，也不要猜错原因）。
const (
	ToolReasonPermission  = "permission"
	ToolReasonTimeout     = "timeout"
	ToolReasonRefused     = "refused"
	ToolReasonNotFound    = "not-found"
	ToolReasonInvalidArgs = "invalid-args"
	ToolReasonConflict    = "conflict"
	ToolReasonTransport   = "transport"
	// command-failed → 命令本身退非零（工具没报错，是那条命令失败了）：先看命令与 cwd。
	ToolReasonCommandFailed = "command-failed"
	ToolReasonOther         = "other"
)

// ToolReasonVocabulary 是词表全集（顺序稳定）：前端标签、看板配色与用例都按它走，
// 避免"词表在三个地方各写一份"。
func ToolReasonVocabulary() []string {
	return []string{
		ToolReasonPermission, ToolReasonTimeout, ToolReasonRefused, ToolReasonNotFound,
		ToolReasonInvalidArgs, ToolReasonConflict, ToolReasonTransport, ToolReasonCommandFailed,
		ToolReasonOther,
	}
}
