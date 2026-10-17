package dto

// search_result.go —— 会话历史检索的**结果形状**（跨层契约）。
//
// 形状放契约层的原因：`contract.SearchHistory` 是门面/前端直接消费的端口，
// 返回类型若指向引擎上下文包，会把 `seelectx → store/Seele` 整条依赖拖进
// 契约层（P3）。引擎包保留同名别名与检索实现，形状只在这里定义一次。

// SearchRecord 是检索到的单条聊天记录（命中帧范围内事件的真实内容）。
type SearchRecord struct {
	Role      string `json:"role"`                 // user | assistant | tool
	Content   string `json:"content,omitempty"`    // 截断后的正文
	ToolName  string `json:"tool_name,omitempty"`  // assistant 工具调用名 / tool 结果名
	ResultRef string `json:"result_ref,omitempty"` // 工具结果句柄（read_tool_result 可读回）
	Truncated bool   `json:"truncated,omitempty"`  // 该条被 token 预算截断
}

// SearchHit 是一次检索命中的压缩帧：段标识 + 单元范围 + 摘要 + 真实聊天记录。
// Score 是词法相关性分数（仅用于排序展示，不作为事实）。
type SearchHit struct {
	SegmentID string         `json:"segment_id"`
	From      int            `json:"from"`
	To        int            `json:"to"`
	Summary   string         `json:"summary"`
	Score     float64        `json:"score"`
	Units     int            `json:"units"`     // 实际读回的完整协议单元数
	Records   []SearchRecord `json:"records"`   // 帧范围内的真实聊天记录
	Truncated bool           `json:"truncated"` // 预算耗尽：该帧记录被截断 / 后续命中被丢弃
}

// SearchResult 是一次检索的完整结果（工具与 GUI 的权威返回）。
type SearchResult struct {
	Query         string      `json:"query"`
	Hits          []SearchHit `json:"hits"`
	IndexedFrames int         `json:"indexed_frames"` // 压缩栈帧总数（语义索引规模）
	TotalUnits    int         `json:"total_units"`    // 事件流完整协议单元总数
	Budget        int         `json:"budget"`         // 实际生效的 token 预算
	Truncated     bool        `json:"truncated"`      // 预算耗尽：部分命中未返回
	Note          string      `json:"note,omitempty"` // 边界提示（未压缩兜底等）
}
