package teamwork

// store_test.go — M8 落盘 owner 的行为门禁：物理路径、整份替换、只追加、
// 崩溃残尾、并发追加与冷读。
//
// 这些断言直接跑在 `Store` 上（不经过根 store 的兼容转发）：M8 之后它是这两条
// 追加流水与计划 head 的**唯一**实现，根包只剩单层转发。

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/RedHuang-0622/seelex/contract/dto"
	storelayout "github.com/RedHuang-0622/seelex/store/internal/layout"
)

func teamworkStoreFixture(t *testing.T) (*Store, *storelayout.Layout, dto.SessionKey) {
	t.Helper()
	layout := storelayout.New(t.TempDir())
	scope := dto.SessionKey{ProjectID: "project-teamwork", SessionID: "session-teamwork"}
	return New(layout), layout, scope
}

// coldStore 用**同一个数据根**新建 layout + Store：冷读证明读面不依赖内存态。
// SessionRoot 固定是 `<dataRoot>/project-<hash>/session-<hash>`，上溯两级即数据根。
func coldStore(t *testing.T, layout *storelayout.Layout, scope dto.SessionKey) *Store {
	t.Helper()
	dataRoot := filepath.Dir(filepath.Dir(layout.SessionRoot(layoutScope(scope))))
	return New(storelayout.New(dataRoot))
}

func validPlan(teamID string) Plan {
	return Plan{
		TeamID:  teamID,
		Version: 1,
		Members: []Member{{Role: "exec", RoleSessionID: teamID + "-exec"}},
		Milestones: []Milestone{{
			ID: "m1", Name: "实现", Status: MilestonePending,
			Items: []WorkItem{{ID: "i1", Milestone: "m1", Role: "exec", Name: "写代码"}},
		}},
	}
}

// TestStorePhysicalPathsAreUnchanged 钉住"零数据迁移"：计划 head 仍是
// metadata/teamwork.json，两条流水与 jobs 目录仍在 <sessionRoot>/teamwork 下。
func TestStorePhysicalPathsAreUnchanged(t *testing.T) {
	store, layout, scope := teamworkStoreFixture(t)
	sessionRoot := layout.SessionRoot(layoutScope(scope))

	if got, want := layout.ModulePath(layoutScope(scope), ModuleID), filepath.Join(sessionRoot, "metadata", "teamwork.json"); got != want {
		t.Fatalf("计划 head 路径变了：got %q want %q", got, want)
	}
	if ModuleID != "teamwork" {
		t.Fatalf("模块身份变了：%q（改它就是改 head 文件名）", ModuleID)
	}
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"dir", store.Dir(scope), filepath.Join(sessionRoot, "teamwork")},
		{"events", store.EventPath(scope), filepath.Join(sessionRoot, "teamwork", "events.jsonl")},
		{"bindings", store.BindingPath(scope), filepath.Join(sessionRoot, "teamwork", "worktrees.jsonl")},
		{"jobs", store.JobsDir(scope), filepath.Join(sessionRoot, "teamwork", "jobs")},
	}
	for _, item := range cases {
		if item.got != item.want {
			t.Errorf("%s 路径变了：got %q want %q", item.name, item.got, item.want)
		}
	}
}

// TestWritePlanReplacesWholePayload：计划是 head 整份替换——第二次写入必须让第一份
// 的内容从盘上消失（不是追加、不是合并）。
func TestWritePlanReplacesWholePayload(t *testing.T) {
	store, layout, scope := teamworkStoreFixture(t)

	if err := store.WritePlan(scope, validPlan("team-a"), 6); err != nil {
		t.Fatalf("第一次写计划：%v", err)
	}
	raw, err := os.ReadFile(layout.ModulePath(layoutScope(scope), ModuleID))
	if err != nil {
		t.Fatalf("读计划 head：%v", err)
	}
	if !strings.Contains(string(raw), "team-a") {
		t.Fatalf("第一次写入没落盘：%s", raw)
	}

	second := validPlan("team-b")
	second.Members = append(second.Members, Member{Role: "review", RoleSessionID: "team-b-review"})
	if err := store.WritePlan(scope, second, 6); err != nil {
		t.Fatalf("第二次写计划：%v", err)
	}
	raw, err = os.ReadFile(layout.ModulePath(layoutScope(scope), ModuleID))
	if err != nil {
		t.Fatalf("读计划 head：%v", err)
	}
	if strings.Contains(string(raw), "team-a") {
		t.Fatalf("整份替换后旧内容仍在盘上：%s", raw)
	}
	loaded, err := store.ReadPlan(scope)
	if err != nil {
		t.Fatalf("读回计划：%v", err)
	}
	if loaded.TeamID != "team-b" || len(loaded.Members) != 2 {
		t.Fatalf("读回的计划不是最后写入的那一份：%+v", loaded)
	}
}

// TestReadPlanMissingIsEmpty：还没声明过团队是正常状态，不是错误。
func TestReadPlanMissingIsEmpty(t *testing.T) {
	store, _, scope := teamworkStoreFixture(t)
	plan, err := store.ReadPlan(scope)
	if err != nil {
		t.Fatalf("缺失计划不是错误：%v", err)
	}
	if !PlanIsEmpty(plan) {
		t.Fatalf("缺失必须读成空计划：%+v", plan)
	}
}

// TestWritePlanValidatesBeforeWriting：校验失败时不得留下半个 head。
func TestWritePlanValidatesBeforeWriting(t *testing.T) {
	store, layout, scope := teamworkStoreFixture(t)
	broken := validPlan("team-a")
	broken.Version = 0
	if err := store.WritePlan(scope, broken, 6); err == nil {
		t.Fatal("version=0 的计划必须被拒绝")
	}
	if _, err := os.Stat(layout.ModulePath(layoutScope(scope), ModuleID)); !os.IsNotExist(err) {
		t.Fatalf("校验失败不得落盘：%v", err)
	}
}

// TestReadPlanColdStart：换一个 Store 实例（新 layout）仍能读回——读面不依赖内存态。
func TestReadPlanColdStart(t *testing.T) {
	store, layout, scope := teamworkStoreFixture(t)
	if err := store.WritePlan(scope, validPlan("team-cold"), 6); err != nil {
		t.Fatal(err)
	}
	cold := coldStore(t, layout, scope)
	loaded, err := cold.ReadPlan(scope)
	if err != nil {
		t.Fatalf("冷读计划：%v", err)
	}
	if loaded.TeamID != "team-cold" || len(loaded.Milestones) != 1 {
		t.Fatalf("冷读结果不对：%+v", loaded)
	}
}

// TestEventsAreAppendOnlyAndOrdered：审计只追加，文件前缀必须原样保留，读回顺序=发生顺序。
func TestEventsAreAppendOnlyAndOrdered(t *testing.T) {
	store, _, scope := teamworkStoreFixture(t)

	if err := store.AppendEvent(scope, Event{Kind: EventPlan, TeamID: "t"}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(store.EventPath(scope))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.AppendEvent(scope, Event{Kind: EventJoin, TeamID: "t", Role: "exec"}); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(store.EventPath(scope))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(after), string(before)) {
		t.Fatalf("追加改写了已有字节（不是 append-only）：\nbefore=%q\nafter=%q", before, after)
	}
	if len(after) <= len(before) {
		t.Fatal("追加后文件必须变长")
	}

	events, err := store.ReadEvents(scope)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Kind != EventPlan || events[1].Kind != EventJoin {
		t.Fatalf("读回顺序/内容不对：%+v", events)
	}
	if events[0].At.IsZero() || events[0].At.Location().String() != "UTC" {
		t.Fatalf("事件时间必须默认填 UTC：%v", events[0].At)
	}
}

// TestAppendEventRequiresKind：没有 kind 的审计行会让读面无法分类，必须显式拒绝。
func TestAppendEventRequiresKind(t *testing.T) {
	store, _, scope := teamworkStoreFixture(t)
	if err := store.AppendEvent(scope, Event{TeamID: "t"}); err == nil {
		t.Fatal("缺 kind 的事件必须被拒绝")
	}
	if _, err := os.Stat(store.EventPath(scope)); !os.IsNotExist(err) {
		t.Fatalf("被拒绝的写入不得创建文件：%v", err)
	}
}

// TestReadEventsMissingIsEmpty 与 TestReadBindingsMissingIsEmpty：缺失=空集，不是错误。
func TestReadEventsMissingIsEmpty(t *testing.T) {
	store, _, scope := teamworkStoreFixture(t)
	events, err := store.ReadEvents(scope)
	if err != nil || len(events) != 0 {
		t.Fatalf("缺失的审计流必须是空集：events=%v err=%v", events, err)
	}
}

func TestReadBindingsMissingIsEmpty(t *testing.T) {
	store, _, scope := teamworkStoreFixture(t)
	rows, err := store.ReadBindings(scope)
	if err != nil || len(rows) != 0 {
		t.Fatalf("缺失的绑定账本必须是空集：rows=%v err=%v", rows, err)
	}
}

// TestCrashTailIsSkipped：崩溃只会留下半行尾声；读侧跳过它，但中间行损坏仍报错。
func TestCrashTailIsSkipped(t *testing.T) {
	store, _, scope := teamworkStoreFixture(t)
	if err := os.MkdirAll(store.Dir(scope), 0o700); err != nil {
		t.Fatal(err)
	}
	body := `{"kind":"plan","team_id":"t"}` + "\n" + `{"kind":"clo`
	if err := os.WriteFile(store.EventPath(scope), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	events, err := store.ReadEvents(scope)
	if err != nil {
		t.Fatalf("崩溃残尾必须被跳过而不是报错：%v", err)
	}
	if len(events) != 1 || events[0].Kind != EventPlan {
		t.Fatalf("残尾前的完整行必须读回：%+v", events)
	}

	broken := `{"kind":"plan"}` + "\n" + `{oops}` + "\n" + `{"kind":"close"}` + "\n"
	if err := os.WriteFile(store.EventPath(scope), []byte(broken), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReadEvents(scope); err == nil {
		t.Fatal("中间行损坏必须显式报错（不能当残尾静默丢弃）")
	}
}

// TestBindingsFoldLastRowAfterColdRead：绑定只追加，折叠取最后一行；Released 撤下。
func TestBindingsFoldLastRowAfterColdRead(t *testing.T) {
	store, layout, scope := teamworkStoreFixture(t)
	rows := []Binding{
		{WorkItem: "a", Role: "exec", SessionID: "s1"},
		{WorkItem: "b", Role: "review", SessionID: "s2"},
		{WorkItem: "a", Role: "exec", SessionID: "s1b"},
		{WorkItem: "b", Released: true, Reason: "done"},
	}
	for _, row := range rows {
		if err := store.AppendBinding(scope, row); err != nil {
			t.Fatal(err)
		}
	}

	cold := coldStore(t, layout, scope)
	stored, err := cold.ReadBindings(scope)
	if err != nil {
		t.Fatalf("冷读绑定：%v", err)
	}
	if len(stored) != 4 {
		t.Fatalf("追加行必须原样保留：%+v", stored)
	}
	if stored[0].At == 0 {
		t.Fatal("绑定时间必须默认填充")
	}
	current := Bindings(stored)
	if len(current) != 1 || current["a"].SessionID != "s1b" {
		t.Fatalf("折叠必须取最后一行且 Released 撤下：%+v", current)
	}
	if err := store.AppendBinding(scope, Binding{Role: "exec"}); err == nil {
		t.Fatal("缺 work_item 的绑定必须被拒绝")
	}
}

// TestConcurrentAppendsKeepWholeLines：两条流水共用模块锁，并发追加不得产生撕裂行。
func TestConcurrentAppendsKeepWholeLines(t *testing.T) {
	store, _, scope := teamworkStoreFixture(t)

	const writers, perWriter = 8, 25
	var wait sync.WaitGroup
	for index := 0; index < writers; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			for n := 0; n < perWriter; n++ {
				if err := store.AppendEvent(scope, Event{Kind: EventItem, Detail: "concurrent"}); err != nil {
					t.Errorf("并发追加事件：%v", err)
					return
				}
				if err := store.AppendBinding(scope, Binding{WorkItem: "w", Role: "exec", SessionID: "s"}); err != nil {
					t.Errorf("并发追加绑定：%v", err)
					return
				}
			}
		}(index)
	}
	wait.Wait()

	events, err := store.ReadEvents(scope)
	if err != nil {
		t.Fatalf("并发后审计流必须仍可解析：%v", err)
	}
	if len(events) != writers*perWriter {
		t.Fatalf("审计行数 = %d, want %d", len(events), writers*perWriter)
	}
	bindings, err := store.ReadBindings(scope)
	if err != nil {
		t.Fatalf("并发后绑定账本必须仍可解析：%v", err)
	}
	if len(bindings) != writers*perWriter {
		t.Fatalf("绑定行数 = %d, want %d", len(bindings), writers*perWriter)
	}
}

// TestJobOutputDirIsSessionScoped：作业输出目录在会话内，可重复取。
func TestJobOutputDirIsSessionScoped(t *testing.T) {
	store, _, scope := teamworkStoreFixture(t)
	dir, err := store.JobOutputDir(scope)
	if err != nil {
		t.Fatalf("JobOutputDir: %v", err)
	}
	if dir != store.JobsDir(scope) {
		t.Fatalf("目录必须等于 JobsDir：got %q want %q", dir, store.JobsDir(scope))
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		t.Fatalf("目录必须真的建出来：err=%v", err)
	}
}
