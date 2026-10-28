package assemble

// runtime_teamwork_context.go — 成员工作上下文读面（`team_context`）的**宿主面**。
//
// 落点沿革（P21-22「域方法随类型走」）：**投影与判据**（成员行组装、最新作业行选取、
// 按需取正文、行不在册时按角色名回读残边、正文预算与 UTF-8 安全裁剪）搬进团队域
// [`backend/multiagent/teamwork`](../../multiagent/teamwork/README.md) 的 `context_view.go`
// （plan §2.7 把 `runtime_teamwork*.go` 列为该域目标）；本文件只剩三件事：
//
//   - `teamworkReadScope`：读面要的三格读数（后端 / 作业表 / 作用域键）——**不建 Coordinator**，
//     因为读面只是读；
//   - `teamworkMemberOutputsFor`：产品自有输出面（nil = 框架自建文件，没有"按角色名回读"）；
//   - `teamworkContextHandler`：参数解析 + 一次 `backend.Store.ReadPlan` + 组装 + 编码回执。

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/RedHuang-0622/Seele/jobs"

	"github.com/RedHuang-0622/seelex/backend/multiagent/teamwork"
	"github.com/RedHuang-0622/seelex/contract/dto"
	seeletelemetry "github.com/RedHuang-0622/seelex/internal/telemetry"
)

// 同值常量别名（事实源在域包，既有调用点与用例零改动）。
const (
	teamworkContextDefaultBodyBytes = teamwork.ContextDefaultBodyBytes
	teamworkContextMaxBodyBytes     = teamwork.ContextMaxBodyBytes
)

// 同身份别名：投影与判据只有一份实现（域包 `context_view.go`）。
type (
	// teamworkMemberJobs 是域包 `MemberJobsPort` 的同身份别名。
	teamworkMemberJobs = teamwork.MemberJobsPort
	// teamworkMemberOutputs 是域包 `MemberOutputsPort` 的同身份别名。
	teamworkMemberOutputs = teamwork.MemberOutputsPort
	// teamworkContextOptions 是域包 `ContextOptions` 的同身份别名。
	teamworkContextOptions = teamwork.ContextOptions
)

var (
	buildTeamworkContextView = teamwork.BuildContextView
	readOutputHead           = teamwork.ReadOutputHead
)

// teamworkContextHandler 读回成员工作上下文（只读，不改任何事实）。
func (r *Runtime) teamworkContextHandler(ctx context.Context, argsJSON string) (string, error) {
	var raw struct {
		Roles       []string `json:"roles"`
		IncludeBody bool     `json:"include_body"`
		MaxBytes    int      `json:"max_bytes"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &raw); err != nil {
		return "", fmt.Errorf("team_context: 参数解析失败: %w", err)
	}
	backend, manager, key, err := r.teamworkReadScope(ctx)
	if err != nil {
		return "", err
	}
	plan, err := backend.Store.ReadPlan(ctx, key)
	if err != nil {
		return "", fmt.Errorf("team_context: %w", err)
	}
	records := manager.Snapshot(jobs.Scope{Session: key.SessionID})
	view := buildTeamworkContextView(ctx, plan, records, manager, teamworkMemberOutputsFor(backend), teamworkContextOptions{
		Roles:       raw.Roles,
		IncludeBody: raw.IncludeBody,
		MaxBytes:    raw.MaxBytes,
	})
	encoded, err := json.Marshal(view)
	if err != nil {
		return "", fmt.Errorf("team_context: 编码回执: %w", err)
	}
	return string(encoded), nil
}

// teamworkReadScope 解析"读面"需要的三件：后端、作业表、作用域键。
//
// 与 coordinatorFor 分开：读面**不建 Coordinator**——建它就要装配执行体，而读面只是读。
func (r *Runtime) teamworkReadScope(ctx context.Context) (*TeamworkBackend, jobs.Manager, dto.SessionKey, error) {
	// 作用域解析只有一份判据（runtime_teamwork_scope.go）；读面额外要求作业表在册。
	backend, manager, key, err := r.resolveTeamworkScope(seeletelemetry.SessionIDFromContext(ctx))
	if err != nil {
		return nil, nil, dto.SessionKey{}, err
	}
	if manager == nil {
		return nil, nil, dto.SessionKey{}, errTeamworkNotAssembled
	}
	return backend, manager, key, nil
}

// teamworkMemberOutputsFor 取读面用的最小输出面：只有装配了产品自有输出面才谈得上回读
// （未装配 = 框架自建文件，驱逐即删、按句柄命名，没有"按角色名回读"这回事）。
func teamworkMemberOutputsFor(backend *TeamworkBackend) teamworkMemberOutputs {
	if backend == nil || backend.JobOutputs == nil {
		return nil
	}
	return backend.JobOutputs
}
