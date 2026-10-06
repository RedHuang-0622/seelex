package workunit

import (
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/Seele/jobs"

	"github.com/RedHuang-0622/seelex/sessionstore"
)

// progress_test.go — 读面（progress.go）的形状证据：打点编解码只有一份、折算不编造事实、
// 按 nodeID 的读法把"List → 查找 → 折算"收成一份。
//
// 这些用例守的是**形状**（读面本身不碰生产装配）：给一个假账本、喂记录、断言读数。
// 步骤②的**生产接线**（宿主记录读面 / 子代理恢复定位 / teammate 会话级读回转调本读面）
// 由 `seelebridge` 包里的用例守（workunit_assembly_test.go、runtime_subagent_resume_test.go、
// workunit_team_test.go）。

// 作业面按能力切开之后，`jobs.Manager` 必须**逐格**满足（合成面见 contract.go 的断言）。
// 每一格单独可判，接手的人因此知道"哪一格漂了"，而不是只看见一条合成断言红。
var (
	_ JobSubmitter  = (jobs.Manager)(nil)
	_ JobReader     = (jobs.Manager)(nil)
	_ JobController = (jobs.Manager)(nil)
	_ JobSignals    = (jobs.Manager)(nil)
)

// fakeLedger 是会话账本替身：只装"一条记录清单"，够读面用（写侧不在这轮的面上）。
type fakeLedger struct {
	records []sessionstore.NodeSessionRecord
	err     error
}

func (f *fakeLedger) Save(string, string, sessionstore.NodeSessionRecord) error { return nil }
func (f *fakeLedger) Delete(string, string, string) error                       { return nil }
func (f *fakeLedger) List(string, string) ([]sessionstore.NodeSessionRecord, error) {
	return f.records, f.err
}

var _ SessionLedger = (*fakeLedger)(nil)

// TestStageCodecRoundTripsAndBounds：打点载荷只有一份编解码，且预览有界。
func TestStageCodecRoundTripsAndBounds(t *testing.T) {
	long := strings.Repeat("很长的预览", 200)
	payload := EncodeStages([]Stage{
		{Stage: " running ", Preview: long},
		{Stage: "", Preview: "空阶段丢弃"},
		{Stage: "done"},
	})
	if len(payload) == 0 {
		t.Fatal("非空打点表必须编出载荷")
	}
	stages := DecodeStages(payload)
	if len(stages) != 2 {
		t.Fatalf("解出 %d 条打点，想要 2 条（空阶段被丢弃）：%+v", len(stages), stages)
	}
	if stages[0].Stage != "running" {
		t.Fatalf("阶段名应去空白：%q", stages[0].Stage)
	}
	if runes := len([]rune(stages[0].Preview)); runes > StagePreviewLimit+1 {
		t.Fatalf("预览 = %d rune，超过上限 %d（+1 省略号）", runes, StagePreviewLimit)
	}
	if !strings.HasSuffix(stages[0].Preview, "…") {
		t.Fatalf("被裁的预览应以省略号结尾：%q", stages[0].Preview)
	}
	if stages[1].Stage != "done" || stages[1].Preview != "" {
		t.Fatalf("无预览那条应原样解回：%+v", stages[1])
	}

	// 空表 = 零值载荷（记录里"没有打点"这一格是零值，不是空数组）。
	if got := EncodeStages(nil); got != nil {
		t.Fatalf("空打点表必须编成 nil，实际 %q", got)
	}
	// 解不动 / 空载荷 → nil（读面宁可说"没有这一格事实"，也不把半个载荷猜成打点）。
	for _, payload := range [][]byte{nil, []byte(""), []byte("null"), []byte("{"), []byte("[]")} {
		if got := DecodeStages(payload); got != nil {
			t.Fatalf("DecodeStages(%q) = %+v，想要 nil", payload, got)
		}
	}
}

// TestProgressOfIsAReadOnlyProjection：折算逐一对应记录里已有的格子，不加事实。
func TestProgressOfIsAReadOnlyProjection(t *testing.T) {
	updated := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	record := sessionstore.NodeSessionRecord{
		NodeID:     "exec-wi-1",
		SessionID:  "sess-1-t-exec-wi-1",
		Goal:       "把读面定形",
		Status:     StatusRunning.String(),
		Summary:    "改到一半",
		Error:      "",
		StagesJSON: EncodeStages([]Stage{{Stage: "round_output", Preview: "已改完"}}),
		Worktree:   sessionstore.NodeWorktreeRecord{Path: `C:\wt\exec-wi-1`, Branch: "seelex/exec-wi-1"},
		UpdatedAt:  updated,
	}
	progress := ProgressOf(KindTeammate, record)
	if progress.Kind != KindTeammate || progress.NodeID != "exec-wi-1" || progress.SessionID != "sess-1-t-exec-wi-1" {
		t.Fatalf("身份读数不对：%+v", progress)
	}
	if !progress.InFlight {
		t.Fatal("Status=running ⇒ InFlight 必须为真（词表只有一份：InFlight）")
	}
	if progress.Status != StatusRunning.String() || progress.Goal != "把读面定形" || progress.Summary != "改到一半" {
		t.Fatalf("记录里的原词必须原样交回：%+v", progress)
	}
	if len(progress.Stages) != 1 || progress.Stages[0].Stage != "round_output" {
		t.Fatalf("打点必须解出来：%+v", progress.Stages)
	}
	if progress.Worktree.Branch != "seelex/exec-wi-1" || !progress.UpdatedAt.Equal(updated) {
		t.Fatalf("现场与时间戳必须原样交回：%+v", progress)
	}

	// 终态：InFlight 立刻为假；记录里没有的格子（时间戳 / 错误）留零值 = 没有这一格事实。
	record.Status = "done"
	record.Error = "boom"
	settled := ProgressOf(KindSubagent, record)
	if settled.InFlight {
		t.Fatal("Status=done ⇒ InFlight 必须为假")
	}
	if settled.Error != "boom" || settled.Kind != KindSubagent {
		t.Fatalf("终态读数不对：%+v", settled)
	}
	empty := ProgressOf(KindSubagent, sessionstore.NodeSessionRecord{})
	if empty.InFlight || empty.NodeID != "" || !empty.UpdatedAt.IsZero() {
		t.Fatalf("空记录折成零值读数（不编造）：%+v", empty)
	}
}

// TestUnitReaderReadsByNodeIDAndFiltersOwnership：读法收成一份，归属过滤由调用方给出
// （两层共用同一张记录表，读面不替调用方猜）。
func TestUnitReaderReadsByNodeIDAndFiltersOwnership(t *testing.T) {
	ledger := &fakeLedger{records: []sessionstore.NodeSessionRecord{
		{NodeID: "exec-wi-1", SessionID: "s1", Status: StatusRunning.String()},
		{NodeID: "wu-2", SessionID: "s2", Status: "done"},
		{NodeID: "exec-wi-2", SessionID: "s3", Status: StatusQueued.String()},
	}}
	// 归属名单 = 现场名单（teammate 侧今天就是这么过滤的）。
	onScene := func(record sessionstore.NodeSessionRecord) bool {
		return strings.HasPrefix(record.NodeID, "exec-")
	}
	reader := NewUnitReader(ledger, "p-team", "sess-1", KindTeammate, onScene)

	progress, found, err := reader.Read("exec-wi-1")
	if err != nil || !found || progress.Kind != KindTeammate || !progress.InFlight {
		t.Fatalf("Read(exec-wi-1) = %+v found=%v err=%v", progress, found, err)
	}
	if _, found, err := reader.Read("wu-2"); err != nil || found {
		t.Fatalf("被归属过滤掉的记录不得被读出来：found=%v err=%v", found, err)
	}
	if _, found, err := reader.Read("nobody"); err != nil || found {
		t.Fatalf("没有这条记录 = found=false（正常读数，不是错误）：found=%v err=%v", found, err)
	}

	list, err := reader.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 2 || list[0].NodeID != "exec-wi-1" || list[1].NodeID != "exec-wi-2" {
		t.Fatalf("List 必须按 NodeID 稳定排序、且只回本层记录：%+v", list)
	}

	// 未装配账本 = 空读数（不是错误）——未装配与"还没跑到过"对读面是同一件事。
	empty := NewUnitReader(nil, "", "", KindSubagent, nil)
	if progress, found, err := empty.Read("wu-1"); err != nil || found || progress.NodeID != "" {
		t.Fatalf("未装配账本时 Read 必须给空读数：%+v found=%v err=%v", progress, found, err)
	}
	if list, err := empty.List(); err != nil || len(list) != 0 {
		t.Fatalf("未装配账本时 List 必须为空：%+v err=%v", list, err)
	}
}

// TestUnitReaderRecordSharesTheSameLookup：层专有格（History/ContextJSON/ResultJSON、
// 子代理打点里的 turn/at）不在 `Progress` 里——需要它们时读 `Record`，而**定位与归属过滤**
// 与 `Read` 走同一条实现（多一条读法就是多一份"按 nodeID 找记录"）。
func TestUnitReaderRecordSharesTheSameLookup(t *testing.T) {
	ledger := &fakeLedger{records: []sessionstore.NodeSessionRecord{
		{NodeID: "exec-wi-1", SessionID: "s1", Status: StatusRunning.String(), ResultJSON: []byte(`{"ok":true}`)},
		{NodeID: "wu-2", SessionID: "s2", Status: "done"},
	}}
	reader := NewUnitReader(ledger, "p", "sess", KindTeammate, func(record sessionstore.NodeSessionRecord) bool {
		return record.NodeID == "exec-wi-1"
	})
	record, found, err := reader.Record("exec-wi-1")
	if err != nil || !found || string(record.ResultJSON) != `{"ok":true}` {
		t.Fatalf("Record 必须交回原始记录：%+v found=%v err=%v", record, found, err)
	}
	if _, found, err := reader.Record("wu-2"); err != nil || found {
		t.Fatalf("归属过滤对 Record 同样生效：found=%v err=%v", found, err)
	}
	if _, found, err := reader.Record("nobody"); err != nil || found {
		t.Fatalf("没有这条记录 = found=false：found=%v err=%v", found, err)
	}
}

// TestUnitReaderCannotBranchOnKind：Kind 是描述性标注，读面不据它分支——同一份记录、同一份
// 读法，换一个 Kind 只换读数上的那一个字。
func TestUnitReaderCannotBranchOnKind(t *testing.T) {
	record := sessionstore.NodeSessionRecord{NodeID: "n-1", SessionID: "s-1", Status: StatusRunning.String()}
	sub := ProgressOf(KindSubagent, record)
	team := ProgressOf(KindTeammate, record)
	if sub.NodeID != team.NodeID || sub.SessionID != team.SessionID ||
		sub.InFlight != team.InFlight || sub.Status != team.Status {
		t.Fatalf("Kind 之外的一切读数必须一致：subagent=%+v teammate=%+v", sub, team)
	}
	if sub.Kind == team.Kind {
		t.Fatal("Kind 是这一层的读数标注，必须如实带出")
	}
}
