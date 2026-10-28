package teamwork

// context_view_test.go 是 team_context 读面投影的**域级**用例（P21-22：随实现从
// `backend/engine/assemble/runtime_teamwork_context.go` 搬进本域）。宿主级用例（handler 接线、
// 读面作用域解析）留在装配层，走同身份别名覆盖同一份判据。

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/RedHuang-0622/Seele/jobs"
	storeteamwork "github.com/RedHuang-0622/seelex/store/teamwork"
)

// stubMemberJobs 是读面端口替身：只实现"读"的三种方法（端口本来就只要这三种）。
type stubMemberJobs struct {
	records []jobs.Record
	bodies  map[jobs.Handle]string
	peeks   int
}

func (s *stubMemberJobs) Snapshot(jobs.Scope) []jobs.Record { return s.records }

func (s *stubMemberJobs) Observe(handle jobs.Handle) (jobs.Record, bool) {
	for _, record := range s.records {
		if record.Handle == handle {
			return record, true
		}
	}
	return jobs.Record{}, false
}

func (s *stubMemberJobs) Peek(_ context.Context, handle jobs.Handle, _ jobs.FetchBudget) (string, jobs.Record, error) {
	s.peeks++
	return s.bodies[handle], jobs.Record{Handle: handle}, nil
}

func contextPlanFixture() storeteamwork.Plan {
	return storeteamwork.Plan{
		TeamID: "team-1", Version: 2,
		Members: []storeteamwork.Member{
			{Role: "arch", RoleSessionID: "role-arch", ToolsPolicy: "readonly"},
			{Role: "impl_ui", RoleSessionID: "role-ui", ToolsPolicy: "readwrite"},
		},
		Milestones: []storeteamwork.Milestone{{
			ID: "m1",
			Items: []storeteamwork.WorkItem{
				{ID: "wi-1", Role: "arch", Name: "设计"},
				{ID: "wi-2", Role: "impl_ui", Name: "实现"},
			},
		}},
	}
}

// TestContextViewTakesBodyOnlyWhenAsked：默认不取正文（看一眼成员状态不该把内容变成"已读"）——
// 只有 include_body=true 才走端口的那一次非消费读法。
func TestContextViewTakesBodyOnlyWhenAsked(t *testing.T) {
	plan := contextPlanFixture()
	record := jobs.Record{Handle: "bteam-1", Seq: 7, State: jobs.StateRunning, Scope: jobs.Scope{Subject: SubjectForRole("arch")}}
	port := &stubMemberJobs{records: []jobs.Record{record}, bodies: map[jobs.Handle]string{"bteam-1": "正文"}}

	view := BuildContextView(context.Background(), plan, port.records, port, nil, ContextOptions{})
	if port.peeks != 0 {
		t.Fatalf("默认不该读正文：peeks=%d", port.peeks)
	}
	arch := view.Members[0]
	if !arch.Running || arch.Idle || arch.Handle != "bteam-1" || arch.Body != "" {
		t.Fatalf("默认读法 = %+v", arch)
	}

	withBody := BuildContextView(context.Background(), plan, port.records, port, nil, ContextOptions{IncludeBody: true})
	if port.peeks != 1 {
		t.Fatalf("取正文时刚好读一次：peeks=%d", port.peeks)
	}
	if withBody.Members[0].Body != "正文" || withBody.Members[0].BodyBytes != len("正文") {
		t.Fatalf("正文与字节数：%+v", withBody.Members[0])
	}
}

// TestContextBodyBudgetClamp：预算夹取——缺省 4KB、超过硬上限按 32KB。
func TestContextBodyBudgetClamp(t *testing.T) {
	if got := ContextBodyBudget(0); got != ContextDefaultBodyBytes {
		t.Fatalf("缺省 = %d, want %d", got, ContextDefaultBodyBytes)
	}
	if got := ContextBodyBudget(-1); got != ContextDefaultBodyBytes {
		t.Fatalf("非正值同样按缺省 = %d", got)
	}
	if got := ContextBodyBudget(1024); got != 1024 {
		t.Fatalf("中间值原样 = %d", got)
	}
	if got := ContextBodyBudget(1 << 20); got != ContextMaxBodyBytes {
		t.Fatalf("超过硬上限按 %d，得到 %d", ContextMaxBodyBytes, got)
	}
}

// TestTruncateContextBodyKeepsUTF8Intact：按字节裁但**不切碎**字符（切半字符是损坏，不是截断）。
func TestTruncateContextBodyKeepsUTF8Intact(t *testing.T) {
	body := "契约落成代码" // 每字 3 字节
	trimmed, truncated := TruncateContextBody(body, 7)
	if !truncated {
		t.Fatal("超出预算必须标截断")
	}
	if !utf8.ValidString(trimmed) {
		t.Fatalf("截断后必须仍是合法 UTF-8：%q", trimmed)
	}
	if len(trimmed) > 7 {
		t.Fatalf("截断后不得超过预算：%d", len(trimmed))
	}
	if short, flag := TruncateContextBody("短", 8); short != "短" || flag {
		t.Fatalf("预算内不该动：%q %v", short, flag)
	}
	if zero, flag := TruncateContextBody(body, 0); zero != body || flag {
		t.Fatalf("预算 <=0 = 不裁：%q %v", zero, flag)
	}
}

// TestLatestRecordForPrefersRunningThenSeq：最新作业行 = 优先在跑的，否则登记序号最大
// （按序号比而不是按句柄文本比）。
func TestLatestRecordForPrefersRunningThenSeq(t *testing.T) {
	subject := SubjectForRole("arch")
	records := []jobs.Record{
		{Handle: "b9", Seq: 9, State: jobs.StateDone, Scope: jobs.Scope{Subject: subject}},
		{Handle: "b3", Seq: 3, State: jobs.StateRunning, Scope: jobs.Scope{Subject: subject}},
		{Handle: "b-other", Seq: 99, State: jobs.StateRunning, Scope: jobs.Scope{Subject: SubjectForRole("pm")}},
	}
	chosen, found := latestRecordFor(records, subject)
	if !found || chosen.Handle != "b3" {
		t.Fatalf("在跑的优先：%+v", chosen)
	}
	allDone := []jobs.Record{
		{Handle: "b2", Seq: 2, State: jobs.StateDone, Scope: jobs.Scope{Subject: subject}},
		{Handle: "b7", Seq: 7, State: jobs.StateFailed, Scope: jobs.Scope{Subject: subject}},
	}
	if chosen, _ = latestRecordFor(allDone, subject); chosen.Handle != "b7" {
		t.Fatalf("都终结时取序号最大：%+v", chosen)
	}
	if _, found = latestRecordFor(records, SubjectForRole("ghost")); found {
		t.Fatal("没有这个主体 ⇒ 找不到")
	}
}

// TestContextViewIdleShapes：空闲的两种形态——没派过活（无残边）与行被逐出但产品自有文件里有正文
// （Evicted=true；只有 include_body 时才回读）。
func TestContextViewIdleShapes(t *testing.T) {
	plan := contextPlanFixture()
	light := BuildContextView(context.Background(), plan, nil, &stubMemberJobs{}, nil, ContextOptions{})
	for _, member := range light.Members {
		if !member.Idle || member.Running || member.Evicted || member.Body != "" {
			t.Fatalf("没有在册行 = 空闲且没有残边：%+v", member)
		}
	}

	// 残边回读走的是**产品自有文件**：这里用真文件钉住"读到正文并标 Evicted"。
	path := filepath.Join(t.TempDir(), "arch.txt")
	if err := os.WriteFile(path, []byte("旧正文"), 0o600); err != nil {
		t.Fatal(err)
	}
	outputs := &stubMemberOutputs{paths: map[string]string{"arch": path}}
	view := BuildContextView(context.Background(), plan, nil, &stubMemberJobs{}, outputs,
		ContextOptions{Roles: []string{" arch "}, IncludeBody: true})
	if len(view.Members) != 1 {
		t.Fatalf("角色过滤应只留匹配项（含首尾空白归一）：%+v", view.Members)
	}
	arch := view.Members[0]
	if !arch.Evicted || arch.Body != "旧正文" || arch.Milestone != "m1" {
		t.Fatalf("残边回读应当带 Evicted 标记与正文：%+v", arch)
	}
}

// TestContextViewClosureFlagAndRoles：闭板标记来自计划；roles 全空白 = 不过滤（全员）。
func TestContextViewClosureFlagAndRoles(t *testing.T) {
	plan := contextPlanFixture()
	plan.State.State = storeteamwork.StateClosed
	view := BuildContextView(context.Background(), plan, nil, &stubMemberJobs{}, nil, ContextOptions{Roles: []string{"  "}})
	if !view.Closed {
		t.Fatal("闭板标记必须从计划搬")
	}
	if len(view.Members) != 2 {
		t.Fatalf("全空白 roles = 不过滤：%d", len(view.Members))
	}
}

// stubMemberOutputs 是输出面替身（只实现按角色名定位那一个方法）。
type stubMemberOutputs struct {
	paths map[string]string
}

func (s *stubMemberOutputs) LatestJobOutputPath(_ context.Context, role string) (string, bool, error) {
	path, ok := s.paths[strings.TrimSpace(role)]
	return path, ok, nil
}
