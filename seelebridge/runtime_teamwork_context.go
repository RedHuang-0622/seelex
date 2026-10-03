package seelebridge

// runtime_teamwork_context.go — 成员工作上下文**读面**（`team_context`）。
//
// 契约锚点：team-board-gui-tui-contract.md 的读面一节 + 用户要求③（"点开成员看工作
// 上下文（含空闲）"）。看板给的是团队形状，这里给的是**一个人此刻在干什么**。
//
// 一条铁律：**读面不改事实**。
//   - 作业表只用 Snapshot/Observe（内存读，不动游标）；
//   - 正文只用 jobs.Manager.Peek——读增量、**不推进游标、不销项**（jobs I-4 的对偶：
//     Fetch 是"我读过了、拿走了"，Peek 是"我看看，别动"）；
//   - 默认不取正文（include_body=false）：看一眼成员状态不该把内容变成"已读"。
//
// 有界：正文逐成员裁剪，默认 4KB、上限 32KB（与看板审计投影 32 条同一类口径——
// 读面不能把会话快照变成无界载荷）。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/RedHuang-0622/Seele/jobs"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	seeletelemetry "github.com/RedHuang-0622/seelex/seelebridge/internal/telemetry"
	"github.com/RedHuang-0622/seelex/seelebridge/teamwork"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

const (
	// teamworkContextDefaultBodyBytes 是 include_body=true 但不给 max_bytes 时，**每个成员**
	// 的正文预算。
	teamworkContextDefaultBodyBytes = 4 << 10
	// teamworkContextMaxBodyBytes 是逐成员正文预算的硬上限：调用方要多少给多少就等于
	// 把"看一眼成员"变成一次无界读取。
	teamworkContextMaxBodyBytes = 32 << 10
)

// teamworkMemberJobs 是本读面用到的**最小**作业面：Peek 是"非消费读法"的锚点。
//
// 收窄成小接口而不是直接用 jobs.Manager：读面只需要这三种读法，用接口把"它绝不会
// Fetch/Done/Kill"这件事写进类型里（想消费也拿不到那个方法）。
type teamworkMemberJobs interface {
	Snapshot(scope jobs.Scope) []jobs.Record
	Observe(handle jobs.Handle) (jobs.Record, bool)
	Peek(ctx context.Context, handle jobs.Handle, budget jobs.FetchBudget) (string, jobs.Record, error)
}

// teamworkMemberOutputs 是本读面用到的**最小**输出面：只按角色名定位最近一份产品自有
// 正文（回读那条残边用，见 teamworkAttachEvictedBody）。
//
// 同样收窄成小接口：读面只读，拿不到"分配 / 写入 / 清理"的方法。nil（未装配产品面）=
// 没有回读——框架自建文件按句柄命名、驱逐即删，本就没有"按角色名回读"这回事。
type teamworkMemberOutputs interface {
	LatestJobOutputPath(ctx context.Context, role string) (string, bool, error)
}

// teamworkContextOptions 是 team_context 的入参（已归一）。
type teamworkContextOptions struct {
	Roles       []string
	IncludeBody bool
	MaxBytes    int
}

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
func (r *Runtime) teamworkReadScope(ctx context.Context) (*TeamworkBackend, jobs.Manager, sessionstore.Key, error) {
	if r == nil {
		return nil, nil, sessionstore.Key{}, errors.New("teamwork: runtime 为空")
	}
	r.teamworkMu.Lock()
	backend, manager := r.teamworkBackend, r.teamworkJobs
	r.teamworkMu.Unlock()
	if backend == nil || manager == nil {
		return nil, nil, sessionstore.Key{}, errors.New("teamwork: 读面未装配（缺 SetTeamworkBackend）")
	}
	sessionID := strings.TrimSpace(seeletelemetry.SessionIDFromContext(ctx))
	if sessionID == "" {
		return nil, nil, sessionstore.Key{}, errors.New("teamwork: 当前调用没有会话归属（team_* 工具必须在会话回合内调用）")
	}
	key, ok := backend.KeyFor(sessionID)
	if !ok {
		return nil, nil, sessionstore.Key{}, fmt.Errorf("teamwork: 无法解析会话 %q 的作用域键", sessionID)
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

// buildTeamworkContextView 把（计划 + 作业行 + 可选正文）组装成 dto.TeamworkContextView。
//
// 抽成自由函数：它是本读面的**唯一**组装路径（与看板的 buildTeamworkBoardView 同款），
// 于是"读到的是什么"只有一个答案，用例可以直接钉它。outputs 是回读残边用的输出面（nil =
// 没有回读），只在"行不在册"时才用得到。
func buildTeamworkContextView(ctx context.Context, plan sessionstore.TeamworkPlan, records []jobs.Record, port teamworkMemberJobs, outputs teamworkMemberOutputs, options teamworkContextOptions) dto.TeamworkContextView {
	view := dto.TeamworkContextView{
		TeamID:  plan.TeamID,
		Version: plan.Version,
		Closed:  plan.State.State == sessionstore.TeamworkStateClosed,
	}
	wanted := roleFilter(options.Roles)
	view.Members = make([]dto.TeamworkMemberContextView, 0, len(plan.Members))
	for _, member := range plan.Members {
		if wanted != nil {
			if _, ok := wanted[member.Role]; !ok {
				continue
			}
		}
		view.Members = append(view.Members, teamworkMemberContext(ctx, plan, member, records, port, outputs, options))
	}
	return view
}

// teamworkMemberContext 组装一个成员的工作上下文：先找它名下**最新**的作业行，再按需取正文。
func teamworkMemberContext(ctx context.Context, plan sessionstore.TeamworkPlan, member sessionstore.TeamworkMember, records []jobs.Record, port teamworkMemberJobs, outputs teamworkMemberOutputs, options teamworkContextOptions) dto.TeamworkMemberContextView {
	view := dto.TeamworkMemberContextView{
		Role:          member.Role,
		RoleSessionID: member.RoleSessionID,
		Worktree:      member.Worktree,
		ToolsPolicy:   member.ToolsPolicy,
		Milestone:     teamworkRoleMilestone(plan, member.Role),
	}
	subject := teamwork.SubjectForRole(member.Role)
	record, found := latestRecordFor(records, subject)
	if !found {
		// 计划里有这个人、作业表里没有它的行 = 它此刻空闲（没派过活，或收口已回收，或
		// 行被框架 prune 逐出）。残边（devlog §4.2）：最后一种情况下正文仍在产品自有
		// 文件里，读面按角色名回读它——"行不在册"不等于"没写过正文"。
		view.Idle = true
		teamworkAttachEvictedBody(ctx, outputs, member.Role, options, &view)
		return view
	}
	view.Handle = string(record.Handle)
	view.State = string(record.State)
	view.ExitCode = record.ExitCode
	view.Bytes = record.Bytes
	view.Lines = record.Lines
	view.Running = record.Running()
	view.Idle = !view.Running
	view.Degraded = record.Degraded
	view.Summary = record.Summary

	if !options.IncludeBody || view.Handle == "" {
		return view
	}
	body, _, err := port.Peek(ctx, record.Handle, jobs.FetchBudget{WaitMS: -1})
	if err != nil {
		// 正文读不出来不改判定，只把它标成降级：读面的失败不该让整条成员行消失。
		view.Degraded = true
		return view
	}
	trimmed, truncated := truncateContextBody(body, contextBodyBudget(options.MaxBytes))
	view.Body = trimmed
	view.BodyBytes = len(trimmed)
	view.BodyTruncated = truncated
	return view
}

// teamworkAttachEvictedBody 在"行不在册"时尝试从**产品自有文件**回读正文。
//
// 这条残边来自框架侧的一个事实：作业表是内存态，超过记录槽上限时 prune 逐出最老的终态行
// （框架没有 pin 概念，产品给的上限只是把概率压小，不能消掉）。逐出之后按句柄读不到任何
// 东西——但正文文件归产品、活到收口，于是读面按角色名回读最近一份。body 因此可能来自
// **文件**（Evicted=true）而不是按句柄的非消费读法，调用方据此分得清两种来源。
func teamworkAttachEvictedBody(ctx context.Context, outputs teamworkMemberOutputs, role string, options teamworkContextOptions, view *dto.TeamworkMemberContextView) {
	if outputs == nil || !options.IncludeBody {
		// 不取正文就不必碰文件系统：读面的缺省姿势是"看一眼成员状态"，不该顺手做一次目录扫描。
		return
	}
	path, ok, err := outputs.LatestJobOutputPath(ctx, role)
	if err != nil || !ok {
		// 定位不到 = 这个角色确实没有正文文件（从没派过活）。不是错误、也不改判定。
		return
	}
	view.Evicted = true
	budget := contextBodyBudget(options.MaxBytes)
	// 多读一个字节：读到预算之外才说明文件还有更多（截断标记据此成立，不是靠猜）。
	body, err := readOutputHead(path, budget+1)
	if err != nil {
		view.Degraded = true
		return
	}
	trimmed, truncated := truncateContextBody(body, budget)
	view.Body = trimmed
	view.BodyBytes = len(trimmed)
	view.BodyTruncated = truncated
}

// latestRecordFor 取某主体名下**最新**的那条作业行：优先在跑的，否则取句柄序号最大的
// （句柄形如 a12，序号单调递增 = 派发顺序）。
func latestRecordFor(records []jobs.Record, subject string) (jobs.Record, bool) {
	var chosen jobs.Record
	found := false
	for _, record := range records {
		if strings.TrimSpace(record.Scope.Subject) != subject {
			continue
		}
		if !found {
			chosen, found = record, true
			continue
		}
		if record.Running() && !chosen.Running() {
			chosen = record
			continue
		}
		if record.Running() == chosen.Running() && record.Seq > chosen.Seq {
			chosen = record
		}
	}
	return chosen, found
}

// teamworkRoleMilestone 反解角色**当前归属的里程碑**：这个角色在哪个里程碑的工作项里
// 出现过（按计划里的里程碑顺序取第一个）。
//
// 它是"这个人此刻在干什么"的定位坐标。阶段口径已退场：不再有"角色的归属阶段"这回事——
// 同一个角色可以在不同里程碑的多个工作项里出现，所以这个值回答的是"它最早出现在哪一步"，
// 而不是一条顺序事实（顺序事实是 milestones[].depends_on 与 work_items[].depends_on）。
func teamworkRoleMilestone(plan sessionstore.TeamworkPlan, role string) string {
	for _, milestone := range plan.Milestones {
		for _, item := range milestone.Items {
			if item.Role == role {
				return milestone.ID
			}
		}
	}
	return ""
}

// roleFilter 归一 roles 过滤：空 / 全空白 = 不过滤（全员）。
func roleFilter(roles []string) map[string]struct{} {
	set := map[string]struct{}{}
	for _, role := range roles {
		if trimmed := strings.TrimSpace(role); trimmed != "" {
			set[trimmed] = struct{}{}
		}
	}
	if len(set) == 0 {
		return nil
	}
	return set
}

// contextBodyBudget 把调用方给的预算夹到 [1, teamworkContextMaxBodyBytes]；
// 缺省（<=0）= teamworkContextDefaultBodyBytes。
func contextBodyBudget(requested int) int {
	if requested <= 0 {
		return teamworkContextDefaultBodyBytes
	}
	if requested > teamworkContextMaxBodyBytes {
		return teamworkContextMaxBodyBytes
	}
	return requested
}

// truncateContextBody 按**字节**预算裁剪，并保证不切碎一个 UTF-8 字符（正文是中文的
// 场合下，切半字符会让下游看到乱码——那不是"截断"，那是损坏）。
func truncateContextBody(body string, limit int) (string, bool) {
	if limit <= 0 || len(body) <= limit {
		return body, false
	}
	trimmed := body[:limit]
	for len(trimmed) > 0 && !utf8.ValidString(trimmed) {
		_, size := utf8.DecodeLastRuneInString(trimmed)
		trimmed = trimmed[:len(trimmed)-size]
	}
	return trimmed, true
}
