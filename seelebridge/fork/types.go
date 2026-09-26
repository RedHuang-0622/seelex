package fork

import (
	"context"
	"encoding/json"
)

// SubagentsContractDescription 是 fork_subagents 工具的契约描述（追加在
// 工具 Description 后，指导模型使用）。
const SubagentsContractDescription = `
- Fork N isolated subagents in parallel (worktree-isolated) and return their structured outputs.
- max_concurrency: optional cap on parallel subagents (default: policy limit).
- Returns a summary JSON with each subagent's output.
`

// Input 是 fork_subagents 的参数契约。
type Input struct {
	Subagents      []SubagentSpec `json:"subagents"`
	MaxConcurrency int            `json:"max_concurrency,omitempty"`
	// TimeoutSec 是本批 fork 的总超时覆盖（秒）：长任务可不填（走
	// limits.fork_timeout，默认 2h）；简单审查/只读任务按需给 1200（20 分钟）
	// 等更紧的上限，避免排队或异常时挂太久。
	TimeoutSec int `json:"timeout_sec,omitempty"`
	// Async 打开**作业化派发**（打点 L-5）：这一批子代理登记成 Kind=subagent 的作业，
	// 调用立刻返回受理回执（每个子代理一个句柄），结果经 job_manage(op=fetch) 取回。
	//
	// 默认 false = 保持既有语义（阻塞到全部子代理与 summary 节点跑完，直接返回结果）。
	// 打开后才可能"在子代理跑动中观察/干预"——阻塞调用期间模型根本没有下一次调用。
	Async bool `json:"async,omitempty"`
}

// SubagentSpec 是单个子代理的派工规格。
type SubagentSpec struct {
	ID   string `json:"id"`
	Goal string `json:"goal"`
}

// SubagentJobSpec 是一条子代理作业的登记输入。
type SubagentJobSpec struct {
	ID        string
	Goal      string
	SessionID string
}

// SubagentJobs 是子代理作业的登记面（Kind=subagent），由 Runtime 注入。
//
// nil = 作业面不可用（能力关闭 / 未装配）：此时 async 模式必须**拒绝**，不得静默
// 退化成阻塞调用——那会让模型以为自己在用作业面。
//
// 三件事就够（执行体在 plan 编排里，不在本包）：
//
//	Add      登记一条作业并挂上取消口（job_manage(op=kill) / 会话销毁会调它）
//	Note     追加已产出的正文（节点摘要 / 中途发现）——kill 之后这些字节仍可取回
//	Complete 合成终态（运行体系被动 done；与 job_manage(op=done) 是两个入口）
type SubagentJobs interface {
	Add(spec SubagentJobSpec, cancel context.CancelFunc) (string, error)
	Note(handle, text string)
	Complete(handle, state string)
}

// PlanCanonical 生成 fork DAG 的规范 JSON（审计/展示；非模型输入）。
func PlanCanonical(input Input) string {
	encoded, _ := json.Marshal(input)
	return string(encoded)
}
