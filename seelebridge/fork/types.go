package fork

import (
	"context"
	"encoding/json"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// SubagentsContractDescription 是 fork_subagents 工具的契约描述（追加在
// 工具 Description 后，指导模型使用）。
const SubagentsContractDescription = `
- The call returns an acceptance receipt with one job handle per subagent and NEVER waits
  for results: this is background dispatch, the same job face as bash_bg / read_batch.
- Fetch each subagent's output with job_manage(op=fetch, handle); look at progress with
  op=observe (handle or none = list this session's jobs); terminate early with op=kill
  (already produced output is kept); retire the row with op=done once it is terminal.
  One kill cancels the WHOLE batch (they share a single plan run).
- max_concurrency: optional cap on parallel subagents (default: policy limit).
`

// Input 是 fork_subagents 的参数契约。
//
// 这一批子代理**只走作业化派发**（打点 L-5）：调用立刻返回每个子代理的句柄，产出经
// `job_manage(op=fetch, handle)` 取回。没有"阻塞到全部跑完"的分支——串行派发会把
// "观察/提前终止子代理"变成不可能（阻塞期间模型没有下一次调用，不是缺工具，是缺时机），
// 而这条能力正是它与 bash_bg / read_batch 同属一个作业面的原因。
type Input struct {
	Subagents      []SubagentSpec `json:"subagents"`
	MaxConcurrency int            `json:"max_concurrency,omitempty"`
	// TimeoutSec 是本批 fork 的总超时覆盖（秒）：长任务可不填（走
	// limits.fork_timeout，默认 2h）；简单审查/只读任务按需给 1200（20 分钟）
	// 等更紧的上限，避免排队或异常时挂太久。
	TimeoutSec int `json:"timeout_sec,omitempty"`
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
	Complete(handle string, state dto.AsyncState)
}

// PlanCanonical 生成 fork DAG 的规范 JSON（审计/展示；非模型输入）。
func PlanCanonical(input Input) string {
	encoded, _ := json.Marshal(input)
	return string(encoded)
}
