package seelebridge

// runtime_teamwork_context_test.go — 钉住成员工作上下文读面（要求③）。
//
// 三条判据各有事实支撑：
//   - **默认不取正文**：include_body=false 时一次 Peek 都不该发生（读面不改事实）；
//   - **有界**：逐成员正文裁剪，缺省 4KB、硬上限 32KB，且不切碎 UTF-8 字符；
//   - **空闲/归属显式**：没派过活的成员 Idle=true，成员带得出它的归属阶段；
//   - 收口事实从计划侧搬（closed），使"收口造成的空"与"没派过活"分得开。

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/RedHuang-0622/Seele/jobs"

	seeletelemetry "github.com/RedHuang-0622/seelex/seelebridge/internal/telemetry"
	"github.com/RedHuang-0622/seelex/seelebridge/teamwork"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// fakeMemberJobs 是收窄端口的最小假体：它**只**有读法（读面的类型级保证）。
type fakeMemberJobs struct {
	snapshots []jobs.Record
	bodies    map[jobs.Handle]string

	peeks int
}

func (f *fakeMemberJobs) Snapshot(jobs.Scope) []jobs.Record { return f.snapshots }

func (f *fakeMemberJobs) Observe(handle jobs.Handle) (jobs.Record, bool) {
	for _, record := range f.snapshots {
		if record.Handle == handle {
			return record, true
		}
	}
	return jobs.Record{}, false
}

func (f *fakeMemberJobs) Peek(_ context.Context, handle jobs.Handle, _ jobs.FetchBudget) (string, jobs.Record, error) {
	f.peeks++
	record, _ := f.Observe(handle)
	return f.bodies[handle], record, nil
}

func runningWorker(handle jobs.Handle, seq int, subject string, bytes int64) jobs.Record {
	return jobs.Record{
		Handle: handle, Seq: seq, Kind: teamwork.KindWorker, State: jobs.StateRunning,
		Scope: jobs.Scope{Session: "s-team", Subject: subject},
		Bytes: bytes, Lines: 12,
	}
}

func TestTeamworkContextNoBodyByDefault(t *testing.T) {
	plan := boardPlanFixture() // arch（design 阶段）+ impl_ui（impl 阶段）
	port := &fakeMemberJobs{
		snapshots: []jobs.Record{runningWorker("a7", 7, teamworkSubjectForTest("arch"), 1234)},
		bodies:    map[jobs.Handle]string{"a7": "documents"},
	}
	view := buildTeamworkContextView(context.Background(), plan, port.snapshots, port, nil, teamworkContextOptions{})

	if port.peeks != 0 {
		t.Fatalf("缺省（include_body=false）不得读正文：Peek 被调了 %d 次", port.peeks)
	}
	if len(view.Members) != 2 {
		t.Fatalf("缺省应给出全部在编成员：%+v", view.Members)
	}
	arch := view.Members[0]
	if arch.Role != "arch" || arch.Milestone != "m-design" {
		t.Fatalf("成员的归属里程碑必须来自它出现的工作项（arch → m-design）：%+v", arch)
	}
	if arch.Handle != "a7" || arch.State != string(jobs.StateRunning) || !arch.Running {
		t.Fatalf("在跑成员必须带得出它的作业行：%+v", arch)
	}
	if arch.Idle {
		t.Fatalf("在跑成员不得标空闲：%+v", arch)
	}
	if arch.Bytes != 1234 {
		t.Fatalf("作业行投影不一致：%+v", arch)
	}
	if arch.Body != "" || arch.BodyBytes != 0 {
		t.Fatalf("默认不取正文时 body 必须为空：%+v", arch)
	}
	idle := view.Members[1]
	if idle.Role != "impl_ui" || !idle.Idle || idle.Handle != "" {
		t.Fatalf("没派过活的成员应显式标空闲（不是靠 handle 为空反推）：%+v", idle)
	}
	if idle.Milestone != "m-impl" {
		t.Fatalf("impl_ui 的归属里程碑应为 m-impl：%+v", idle)
	}
}

func TestTeamworkContextBounded(t *testing.T) {
	plan := boardPlanFixture()
	long := strings.Repeat("x", 10000)
	port := &fakeMemberJobs{bodies: map[jobs.Handle]string{"a7": long}}
	records := []jobs.Record{runningWorker("a7", 7, teamworkSubjectForTest("arch"), int64(len(long)))}

	// 缺省预算 = 4KB。
	view := buildTeamworkContextView(context.Background(), plan, records, port, nil,
		teamworkContextOptions{Roles: []string{"arch"}, IncludeBody: true})
	arch := view.Members[0]
	if !arch.BodyTruncated || arch.BodyBytes != teamworkContextDefaultBodyBytes {
		t.Fatalf("缺省预算应为 4KB 且标记截断：bytes=%d truncated=%v", arch.BodyBytes, arch.BodyTruncated)
	}

	// 超上限的请求被夹到硬上限（不是照单全收）：正文比上限更长才看得出这条。
	huge := strings.Repeat("y", 40<<10)
	hugePort := &fakeMemberJobs{bodies: map[jobs.Handle]string{"a7": huge}}
	records = []jobs.Record{runningWorker("a7", 7, teamworkSubjectForTest("arch"), int64(len(huge)))}
	view = buildTeamworkContextView(context.Background(), plan, records, hugePort, nil,
		teamworkContextOptions{Roles: []string{"arch"}, IncludeBody: true, MaxBytes: 1 << 20})
	arch = view.Members[0]
	if arch.BodyBytes != teamworkContextMaxBodyBytes || !arch.BodyTruncated {
		t.Fatalf("超上限请求应夹到 %d 字节：bytes=%d truncated=%v",
			teamworkContextMaxBodyBytes, arch.BodyBytes, arch.BodyTruncated)
	}

	// 预算大于正文 → 不截断、不补白。
	short := strings.Repeat("s", 10)
	shortPort := &fakeMemberJobs{bodies: map[jobs.Handle]string{"a7": short}}
	records = []jobs.Record{runningWorker("a7", 7, teamworkSubjectForTest("arch"), int64(len(short)))}
	view = buildTeamworkContextView(context.Background(), plan, records, shortPort, nil,
		teamworkContextOptions{Roles: []string{"arch"}, IncludeBody: true, MaxBytes: 16})
	arch = view.Members[0]
	if arch.BodyTruncated || arch.Body != short {
		t.Fatalf("预算大于正文时不该标截断：%+v", arch)
	}

	// 多字节正文：裁剪不得切碎字符（切半字符是损坏，不是截断）。
	plan = boardPlanFixture()
	port = &fakeMemberJobs{bodies: map[jobs.Handle]string{"a7": strings.Repeat("中文", 100)}}
	view = buildTeamworkContextView(context.Background(), plan, records, port, nil,
		teamworkContextOptions{Roles: []string{"arch"}, IncludeBody: true, MaxBytes: 7})
	arch = view.Members[0]
	if arch.BodyBytes > 7 {
		t.Fatalf("裁剪必须守住字节预算：%d > 7", arch.BodyBytes)
	}
	if !arch.BodyTruncated {
		t.Fatalf("按预算裁剪了就必须标截断：%+v", arch)
	}
	if strings.ContainsRune(arch.Body, '\uFFFD') || !utf8.ValidString(arch.Body) {
		t.Fatalf("裁剪切碎了 UTF-8 字符：%q", arch.Body)
	}
}

func TestTeamworkContextRolesFilterAndClosedFlag(t *testing.T) {
	plan := boardPlanFixture()
	plan.State = sessionstore.TeamworkState{State: sessionstore.TeamworkStateClosed}
	port := &fakeMemberJobs{}
	view := buildTeamworkContextView(context.Background(), plan, nil, port, nil,
		teamworkContextOptions{Roles: []string{" impl_ui ", ""}})

	if len(view.Members) != 1 || view.Members[0].Role != "impl_ui" {
		t.Fatalf("roles 过滤应只留点名的角色（空白项忽略）：%+v", view.Members)
	}
	if !view.Closed {
		t.Fatal("收口事实必须从计划侧搬出来（closed=true）——否则分不清'收口造成的空'与'没派过活'")
	}
}

func TestTeamContextToolReportsIdleMembers(t *testing.T) {
	r := newTestRuntime(t)
	defer r.Shutdown()
	store := &memPlanStore{}
	if err := r.SetTeamworkBackend(teamworkTestBackend(store, "s-team")); err != nil {
		t.Fatalf("SetTeamworkBackend: %v", err)
	}
	if _, ok := r.registry.FindTool("team_context"); !ok {
		t.Fatal("注入 backend 后 team_context 应在工具面里（成员上下文读面）")
	}
	ctx := seeletelemetry.WithSessionID(context.Background(), "s-team")
	if _, err := r.teamPlanHandler(ctx, `{"team_id":"ctx","milestones":[{"id":"m-impl"}],"members":[{"role":"exec"}]}`); err != nil {
		t.Fatalf("team_plan: %v", err)
	}
	receipt, err := r.teamworkContextHandler(ctx, `{}`)
	if err != nil {
		t.Fatalf("team_context: %v", err)
	}
	if !strings.Contains(receipt, `"idle":true`) {
		t.Fatalf("没派过活的成员应显式 idle=true：%s", receipt)
	}
	if strings.Contains(receipt, `"body"`) {
		t.Fatalf("缺省不得带正文：%s", receipt)
	}
}

// fakeMemberOutputs 是回读残边用的最小输出面替身：按角色名给一个正文文件路径。
type fakeMemberOutputs struct {
	paths map[string]string
	reads int
}

func (f *fakeMemberOutputs) LatestJobOutputPath(_ context.Context, role string) (string, bool, error) {
	f.reads++
	path, ok := f.paths[role]
	return path, ok, nil
}

// TestTeamworkContextReadsEvictedBodyFromProductFile 钉住 prune 残边（devlog §4.2）：
// 作业行被框架 prune 逐出后按句柄读不到，但正文文件归产品、活到收口——读面按角色名回读，
// 而不是把"行不在册"冒充成"没写过正文"。Evicted 位让调用方分得清两种来源。
func TestTeamworkContextReadsEvictedBodyFromProductFile(t *testing.T) {
	plan := boardPlanFixture() // arch（design）+ impl_ui（impl）
	path := filepath.Join(t.TempDir(), "arch-2.log")
	if err := os.WriteFile(path, []byte("被逐出那一轮的正文"), 0o600); err != nil {
		t.Fatalf("造产品自有正文文件: %v", err)
	}
	outputs := &fakeMemberOutputs{paths: map[string]string{"arch": path}}

	// 作业表里**没有** arch 的行（= 已被逐出 / 收口回收）：正文仍从产品文件回读。
	view := buildTeamworkContextView(context.Background(), plan, nil, &fakeMemberJobs{}, outputs,
		teamworkContextOptions{Roles: []string{"arch"}, IncludeBody: true})
	arch := view.Members[0]
	if !arch.Idle || arch.Handle != "" {
		t.Fatalf("行不在册时该成员应显式空闲且无句柄：%+v", arch)
	}
	if !arch.Evicted {
		t.Fatalf("正文从产品文件回读时必须标 Evicted（否则分不清来源）：%+v", arch)
	}
	if arch.Body != "被逐出那一轮的正文" || arch.BodyBytes == 0 {
		t.Fatalf("正文应从产品自有文件回读：%+v", arch)
	}

	// 缺省不取正文 = 不碰文件系统（读面缺省姿势是"看一眼状态"）。
	light := &fakeMemberOutputs{paths: map[string]string{"arch": path}}
	view = buildTeamworkContextView(context.Background(), plan, nil, &fakeMemberJobs{}, light,
		teamworkContextOptions{Roles: []string{"arch"}})
	if light.reads != 0 {
		t.Fatalf("include_body=false 不该做目录/文件定位：reads=%d", light.reads)
	}
	if view.Members[0].Evicted || view.Members[0].Body != "" {
		t.Fatalf("不取正文时不得回读、不得标 Evicted：%+v", view.Members[0])
	}

	// 该角色确实没有正文文件（没派过活）：不标 Evicted、不标降级。
	view = buildTeamworkContextView(context.Background(), plan, nil, &fakeMemberJobs{},
		&fakeMemberOutputs{}, teamworkContextOptions{Roles: []string{"arch"}, IncludeBody: true})
	if arch = view.Members[0]; arch.Evicted || arch.Degraded || arch.Body != "" {
		t.Fatalf("没有正文文件时应保持纯空闲：%+v", arch)
	}
}

// teamworkSubjectForTest 直接借用生产口径（写死 "emp_" 就是第二处定义）。
func teamworkSubjectForTest(role string) string { return teamwork.SubjectForRole(role) }
