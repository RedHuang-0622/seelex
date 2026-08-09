package seelexctx

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// ── 运行时上限（limits）────────────────────────────────────────
// seele.yaml 的 limits 段：集中治理需要跨模块一致、影响资源消耗或用户可见
// 行为的运行时上限。局部 UI/格式常量仍由所属模块维护，不宣称消除所有常量。
// 缺失字段 → 零值 → 消费方套用 DefaultLimits 的默认值；无需改代码即可调参。

// Limits 是运行时行为上限集合（零值 = 未配置，走默认）。
type Limits struct {
	// 时延类（秒；0 = 无限制/未配置）
	ToolCallTimeoutSec     int `yaml:"tool_call_timeout"`     // 工具调用超时（0 = 无限制）
	ApprovalTimeoutSec     int `yaml:"approval_timeout"`      // 审批等待
	PlanDecisionTimeoutSec int `yaml:"plan_decision_timeout"` // preflight 决策回合
	HeartbeatIntervalSec   int `yaml:"heartbeat_interval"`    // workplan 心跳间隔
	ReplanWindowSec        int `yaml:"replan_window"`         // replan 频率窗口
	TavilyTimeoutSec       int `yaml:"tavily_timeout"`        // tavily HTTP 超时
	// 预算/上限类
	MaxConcurrentReplans   int `yaml:"max_concurrent_replans"`       // replan 并发上限
	MaxReplansPerWindow    int `yaml:"max_replans_per_window"`       // 窗口内 replan 次数
	MaxReplanProviderReqs  int `yaml:"max_replan_provider_requests"` // 窗口内 provider 请求预算
	MaxReplansPerPlanChain int `yaml:"max_replans_per_plan_chain"`   // 单计划链 replan 次数
	HistoryWindow          int `yaml:"history_window"`               // 会话可见历史条数
	PlanNodeEvents         int `yaml:"plan_node_events"`             // 节点详情时间线上限
	PlanNodeMaxLoops       int `yaml:"plan_node_max_loops"`          // 子代理节点循环上限
	EvidenceChars          int `yaml:"evidence_chars"`               // 证据/输出截断
	ReplanEvidenceBytes    int `yaml:"replan_evidence_bytes"`        // replan 证据字节上限
	InputLoopLimit         int `yaml:"input_loop_limit"`             // 输入循环上限
	ReferencePageSize      int `yaml:"reference_page_size"`          // 引用工具默认分页
	MaxReferencePageSize   int `yaml:"max_reference_page_size"`      // 引用工具分页上限
	GrepMaxResults         int `yaml:"grep_max_results"`             // grep 默认结果数
	SessionNameRunes       int `yaml:"session_name_runes"`           // 会话名截断
	PreflightRetry         int `yaml:"preflight_retry"`              // preflight 重试次数
	OutputReserveTokens    int `yaml:"output_reserve_tokens"`        // provider 输出预留 token
	ToolTokenOverhead      int `yaml:"tool_token_overhead"`          // 工具 token 估算开销
	ContextMaxUnits        int `yaml:"context_max_units"`            // 上下文压缩扫描单元上限
	MessageShardSize       int `yaml:"message_shard_size"`           // 会话存储分片条数
	SummaryChars           int `yaml:"summary_chars"`                // 摘要截断字符数
	TodoMaxItems           int `yaml:"todo_max_items"`               // todolist 清单项上限
	WorkTableRows          int `yaml:"work_table_rows"`              // 工作表格（work table）最大行数
	WalkTimeoutSec         int `yaml:"walk_timeout"`                 // glob/grep 目录遍历超时（秒）
	MaxToolResultChars     int `yaml:"max_tool_result_chars"`        // 工具结果最大字符数（0 → 默认；超大结果归档为 result_ref）
	// docker 守护进程自动恢复（2026-08-07）：bash 命令因 Docker Desktop
	// 未运行失败时，自动启动守护进程并重跑一次命令（真实环境有 docker CLI
	// 但 daemon 未启动是常见状态，沙箱应帮模型把环境"修好"而不是报错）。
	DisableDockerAutoStart bool `yaml:"disable_docker_auto_start"` // true = 关闭自动拉起（默认开启）
	DockerStartTimeoutSec  int  `yaml:"docker_start_timeout"`      // 启动等待上限（秒；0 → 60）
	// fork 子代理长任务宽松预算（2026-08-08）：fork_subagents 是同步编排
	// 工具，总时长 = 全部子代理工作量之和，不能被通用工具超时（30 分钟）
	// 掐死；fork 用独立大预算。子代理节点循环数复用 effort 调节值
	// （PlanPolicy.MaxNodeLoops），不做独立常量。
	ForkTimeoutSec int `yaml:"fork_timeout"` // fork 工具总超时（秒；0 → 7200 = 2 小时）
}

// DefaultLimits 返回全部默认值（与重构前的硬编码常量一一对应，行为不变）。
func DefaultLimits() Limits {
	return Limits{
		ToolCallTimeoutSec:     int((30 * time.Minute) / time.Second), // 旧默认 120s 已提高
		ApprovalTimeoutSec:     600,                                   // 等待用户审批
		PlanDecisionTimeoutSec: 10,
		HeartbeatIntervalSec:   15,
		ReplanWindowSec:        60,
		TavilyTimeoutSec:       15,
		MaxConcurrentReplans:   2,
		MaxReplansPerWindow:    6,
		MaxReplanProviderReqs:  6,
		MaxReplansPerPlanChain: 2,
		HistoryWindow:          200,
		PlanNodeEvents:         30,
		PlanNodeMaxLoops:       15,
		EvidenceChars:          800,
		ReplanEvidenceBytes:    12 * 1024,
		InputLoopLimit:         9999,
		ReferencePageSize:      4000,
		MaxReferencePageSize:   12000,
		GrepMaxResults:         20,
		SessionNameRunes:       16,
		PreflightRetry:         2,
		OutputReserveTokens:    512,
		ToolTokenOverhead:      64,
		ContextMaxUnits:        4,
		MessageShardSize:       100,
		SummaryChars:           800,
		TodoMaxItems:           20,
		WorkTableRows:          200,
		WalkTimeoutSec:         30,
		// fork 汇总窗口按子代理数 ×n 放大：4×2000 字结论 ≈ 24KB，默认
		// 60000 字节（约 2 万汉字）给足余量——窗口是容灾上限不是截断线。
		MaxToolResultChars:     60000,
		DockerStartTimeoutSec:  60,
		ForkTimeoutSec:         7200,
	}
}

// DefaultToolResultLimit 返回工具结果字符预算的 seelex 生效默认值。
// 所有消费方（processor / controller / application.core）以此为兜底，
// 与 seelex.yaml limits 段 max_tool_result_chars 的覆盖合并后保持一致；
// 未配置 → 本默认（60000）。框架 ctx_manager 默认（约 4000，见 seele.go
// re-export）不再作为兜底，仅保留 re-export 语义。
func DefaultToolResultLimit() int { return DefaultLimits().MaxToolResultChars }

// WithDefaults 把零值字段替换为默认值，返回完整配置。
// ToolCallTimeoutSec 特殊：0 是显式"无限制"语义（limits 段存在时），不补默认。
func (l Limits) WithDefaults() Limits {
	def := DefaultLimits()
	if l.ApprovalTimeoutSec == 0 {
		l.ApprovalTimeoutSec = def.ApprovalTimeoutSec
	}
	if l.PlanDecisionTimeoutSec == 0 {
		l.PlanDecisionTimeoutSec = def.PlanDecisionTimeoutSec
	}
	if l.HeartbeatIntervalSec == 0 {
		l.HeartbeatIntervalSec = def.HeartbeatIntervalSec
	}
	if l.ReplanWindowSec == 0 {
		l.ReplanWindowSec = def.ReplanWindowSec
	}
	if l.TavilyTimeoutSec == 0 {
		l.TavilyTimeoutSec = def.TavilyTimeoutSec
	}
	if l.MaxConcurrentReplans == 0 {
		l.MaxConcurrentReplans = def.MaxConcurrentReplans
	}
	if l.MaxReplansPerWindow == 0 {
		l.MaxReplansPerWindow = def.MaxReplansPerWindow
	}
	if l.MaxReplanProviderReqs == 0 {
		l.MaxReplanProviderReqs = def.MaxReplanProviderReqs
	}
	if l.MaxReplansPerPlanChain == 0 {
		l.MaxReplansPerPlanChain = def.MaxReplansPerPlanChain
	}
	if l.HistoryWindow == 0 {
		l.HistoryWindow = def.HistoryWindow
	}
	if l.PlanNodeEvents == 0 {
		l.PlanNodeEvents = def.PlanNodeEvents
	}
	if l.PlanNodeMaxLoops == 0 {
		l.PlanNodeMaxLoops = def.PlanNodeMaxLoops
	}
	if l.EvidenceChars == 0 {
		l.EvidenceChars = def.EvidenceChars
	}
	if l.ReplanEvidenceBytes == 0 {
		l.ReplanEvidenceBytes = def.ReplanEvidenceBytes
	}
	if l.InputLoopLimit == 0 {
		l.InputLoopLimit = def.InputLoopLimit
	}
	if l.ReferencePageSize == 0 {
		l.ReferencePageSize = def.ReferencePageSize
	}
	if l.MaxReferencePageSize == 0 {
		l.MaxReferencePageSize = def.MaxReferencePageSize
	}
	if l.GrepMaxResults == 0 {
		l.GrepMaxResults = def.GrepMaxResults
	}
	if l.SessionNameRunes == 0 {
		l.SessionNameRunes = def.SessionNameRunes
	}
	if l.PreflightRetry == 0 {
		l.PreflightRetry = def.PreflightRetry
	}
	if l.OutputReserveTokens == 0 {
		l.OutputReserveTokens = def.OutputReserveTokens
	}
	if l.ToolTokenOverhead == 0 {
		l.ToolTokenOverhead = def.ToolTokenOverhead
	}
	if l.ContextMaxUnits == 0 {
		l.ContextMaxUnits = def.ContextMaxUnits
	}
	if l.MessageShardSize == 0 {
		l.MessageShardSize = def.MessageShardSize
	}
	if l.SummaryChars == 0 {
		l.SummaryChars = def.SummaryChars
	}
	if l.TodoMaxItems == 0 {
		l.TodoMaxItems = def.TodoMaxItems
	}
	if l.WorkTableRows == 0 {
		l.WorkTableRows = def.WorkTableRows
	}
	if l.WalkTimeoutSec == 0 {
		l.WalkTimeoutSec = def.WalkTimeoutSec
	}
	if l.MaxToolResultChars == 0 {
		l.MaxToolResultChars = def.MaxToolResultChars
	}
	if l.DockerStartTimeoutSec == 0 {
		l.DockerStartTimeoutSec = def.DockerStartTimeoutSec
	}
	if l.ForkTimeoutSec == 0 {
		l.ForkTimeoutSec = def.ForkTimeoutSec
	}
	return l
}

// Durations 返回常用的时间转换（秒字段 → time.Duration）。
func (l Limits) Durations() (toolCall, approval, planDecision, heartbeat, replanWindow, tavily time.Duration) {
	return time.Duration(l.ToolCallTimeoutSec) * time.Second,
		time.Duration(l.ApprovalTimeoutSec) * time.Second,
		time.Duration(l.PlanDecisionTimeoutSec) * time.Second,
		time.Duration(l.HeartbeatIntervalSec) * time.Second,
		time.Duration(l.ReplanWindowSec) * time.Second,
		time.Duration(l.TavilyTimeoutSec) * time.Second
}

// LoadLimits 读取 seele.yaml 的 limits 配置段：
//   - 文件不存在或 limits 段缺失 → DefaultLimits()（完整默认值）；
//   - limits 段存在 → 解析字段（未写字段 0 → 调用方 WithDefaults 补默认；
//     tool_call_timeout: 0 是显式"无限制"，保留）；
//   - 解析失败或负值显式报错。
func LoadLimits(path string) (Limits, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultLimits(), nil
		}
		return Limits{}, fmt.Errorf("limits: read config: %w", err)
	}
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return Limits{}, fmt.Errorf("limits: parse config: %w", err)
	}
	root := documentContent(&document)
	limitsNode, ok := root["limits"]
	if !ok {
		return DefaultLimits(), nil
	}
	var check Limits
	if err := limitsNode.Decode(&check); err != nil {
		return Limits{}, fmt.Errorf("limits: parse config: %w", err)
	}
	if check.ToolCallTimeoutSec < 0 || check.ApprovalTimeoutSec < 0 || check.PlanDecisionTimeoutSec < 0 ||
		check.HeartbeatIntervalSec < 0 || check.ReplanWindowSec < 0 || check.TavilyTimeoutSec < 0 ||
		check.MaxConcurrentReplans < 0 || check.MaxReplansPerWindow < 0 || check.MaxReplanProviderReqs < 0 || check.MaxReplansPerPlanChain < 0 ||
		check.HistoryWindow < 0 || check.PlanNodeEvents < 0 || check.PlanNodeMaxLoops < 0 ||
		check.EvidenceChars < 0 || check.ReplanEvidenceBytes < 0 || check.InputLoopLimit < 0 ||
		check.ReferencePageSize < 0 || check.MaxReferencePageSize < 0 || check.GrepMaxResults < 0 ||
		check.SessionNameRunes < 0 || check.PreflightRetry < 0 || check.OutputReserveTokens < 0 ||
		check.ToolTokenOverhead < 0 || check.ContextMaxUnits < 0 || check.MessageShardSize < 0 || check.SummaryChars < 0 || check.TodoMaxItems < 0 || check.WorkTableRows < 0 || check.WalkTimeoutSec < 0 ||
		check.MaxToolResultChars < 0 || check.DockerStartTimeoutSec < 0 ||
		check.ForkTimeoutSec < 0 {
		return Limits{}, fmt.Errorf("limits: values must not be negative")
	}
	return check, nil
}

// documentContent 返回 YAML 文档根映射（忽略 null/空文档）。
func documentContent(document *yaml.Node) map[string]*yaml.Node {
	if document == nil || len(document.Content) == 0 {
		return nil
	}
	root := document.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil
	}
	content := make(map[string]*yaml.Node, len(root.Content)/2)
	for index := 0; index+1 < len(root.Content); index += 2 {
		content[root.Content[index].Value] = root.Content[index+1]
	}
	return content
}
