package teamwork

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/RedHuang-0622/Seele/jobs"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// outputs_test.go — S5「作业正文活到 close」的两条路：
//
//  ① 输出**归属**：派发时把产品自有的落点同时交给 jobs.Spec.OutputPath 与作业载荷。
//     框架侧因此不建写句柄、只按偏移读，销项 / 驱逐 / Close 都不删它——正文活到产品
//     自己决定的那一刻。
//  ② 清理**时机**：逐人退场（team_retire）不清理正文；整队收口（team_close）清一次，
//     且发生在"逐成员退场"之后（先停作业再清文件，顺序不能换）。
//
// 这两条都是"没有别人会替你做"的接线：缺 ① 则框架自建文件、阶段收尾一 fetch 就带走正文；
// 缺 ② 则目录随收口次数无界累积，或反过来在退场那一刻就把正文清掉。

// fakeJobOutputs 是 JobOutputs 的替身：路径可预测（`jobs/<role>-<n>.log`），并把
// 分配 / 清理记进同一条调用流水（顺序断言因此和 worktree/session 共表）。
type fakeJobOutputs struct {
	log   *callLog
	paths []string
}

func (o *fakeJobOutputs) JobOutputPath(_ context.Context, role string) (string, error) {
	path := "jobs/" + role + "-" + strconv.Itoa(len(o.paths)+1) + ".log"
	o.paths = append(o.paths, path)
	o.log.record("output:alloc:" + path)
	return path, nil
}

func (o *fakeJobOutputs) ClearJobOutputs(context.Context) error {
	o.log.record("output:clear")
	return nil
}

// WriteJobOutput / LatestJobOutputPath 在本用例里不被执行体触达（fakeRunner 不写文件），
// 但接口是完整的：不做"只实现一半"的替身。
func (o *fakeJobOutputs) WriteJobOutput(string, string) error { return nil }

func (o *fakeJobOutputs) LatestJobOutputPath(context.Context, string) (string, bool, error) {
	return "", false, nil
}

// newOutputFixture 装配一个 Coordinator（其余端口与 teamwork_test 同款）。
//
// withOutputs 决定是否装配产品输出面——两种形态都要能跑（装配 = 正文归产品，
// 未装配 = 交回框架自建），所以它是一条开关而不是两个 fixture。
// 替身与断言共用**同一条** callLog（各建一条日志就是"断言看不见调用"的经典坑）。
func newOutputFixture(t *testing.T, withOutputs bool) (*Coordinator, jobs.Manager, *memoryPlanStore, *fakeRunner, *callLog, *fakeJobOutputs) {
	t.Helper()
	store := &memoryPlanStore{}
	runner := &fakeRunner{}
	log := &callLog{}
	var outputs *fakeJobOutputs
	var port JobOutputs
	if withOutputs {
		outputs = &fakeJobOutputs{log: log}
		port = outputs
	}
	manager, err := jobs.New(
		jobs.WithExecutor(WorkerExecutor(runner, 4)),
		jobs.WithLimits(jobs.Limits{InFlight: 8}),
	)
	if err != nil {
		t.Fatalf("jobs.New: %v", err)
	}
	t.Cleanup(func() { _ = manager.Close(context.Background()) })
	coordinator, err := New(Options{
		Key:          sessionstore.Key{ProjectID: "p", SessionID: "s"},
		Store:        store,
		Jobs:         manager,
		Workers:      runner,
		Worktrees:    fakeWorktrees{log: log},
		Sessions:     fakeSessions{log: log},
		Boards:       fakeBoards{log: log},
		JobOutputs:   port,
		MaxTeammates: 6,
	})
	if err != nil {
		t.Fatalf("teamwork.New: %v", err)
	}
	return coordinator, manager, store, runner, log, outputs
}

func requestsOf(runner *fakeRunner) []WorkerRequest {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	return append([]WorkerRequest(nil), runner.requests...)
}

func TestDispatchAssignsProductOwnedOutput(t *testing.T) {
	t.Parallel()
	coordinator, manager, _, runner, _, outputs := newOutputFixture(t, true)
	ctx := context.Background()
	if err := coordinator.SetPlan(ctx, vmodelPlan()); err != nil {
		t.Fatalf("SetPlan: %v", err)
	}
	handle, stage, err := coordinator.Dispatch(ctx, "exec", "写实现")
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if stage != "impl" {
		t.Fatalf("阶段归属应为首个出现该角色的阶段：%q", stage)
	}
	record := waitTerminal(t, manager, handle)

	// ① 框架侧的读面指向**产品自有**文件（不是 `<outputDir>/<handle>.log`）。
	if len(outputs.paths) != 1 {
		t.Fatalf("一次派发应恰好分配一个落点：%v", outputs.paths)
	}
	if record.OutputRef != outputs.paths[0] {
		t.Fatalf("Record.OutputRef 应是分配给这次派发的产品自有路径：got %q want %q", record.OutputRef, outputs.paths[0])
	}
	// ② 载荷带同一个落点：执行体按给定的落点写（两处各算一次路径 = 两个文件）。
	requests := requestsOf(runner)
	if len(requests) != 1 {
		t.Fatalf("应恰好有一条 worker 载荷：%d", len(requests))
	}
	if requests[0].OutputPath != outputs.paths[0] {
		t.Fatalf("载荷里的 OutputPath 应与 Spec.OutputPath 同值：got %q want %q", requests[0].OutputPath, outputs.paths[0])
	}
}

func TestDispatchWithoutOutputsKeepsFrameworkOwnedFile(t *testing.T) {
	t.Parallel()
	coordinator, manager, _, runner, _, _ := newOutputFixture(t, false)
	ctx := context.Background()
	if err := coordinator.SetPlan(ctx, vmodelPlan()); err != nil {
		t.Fatalf("SetPlan: %v", err)
	}
	handle, _, err := coordinator.Dispatch(ctx, "exec", "写实现")
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	record := waitTerminal(t, manager, handle)
	// 未装配输出面 = 交回框架自建（路径由框架的 outputDir 决定），且载荷里不写落点——
	// 执行体因此走 sink.Note 那条路（有且只有一条路被走到）。
	if record.OutputRef == "" {
		t.Fatal("未装配输出面时框架应自建输出文件（OutputRef 不该为空）")
	}
	if requests := requestsOf(runner); len(requests) != 1 || requests[0].OutputPath != "" {
		t.Fatalf("未装配输出面时载荷不该带落点：%+v", requests)
	}
}

func TestRetireKeepsJobOutputsUntilClose(t *testing.T) {
	t.Parallel()
	coordinator, manager, _, _, log, _ := newOutputFixture(t, true)
	ctx := context.Background()
	if err := coordinator.SetPlan(ctx, vmodelPlan()); err != nil {
		t.Fatalf("SetPlan: %v", err)
	}
	handle, _, err := coordinator.Dispatch(ctx, "exec", "写实现")
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	waitTerminal(t, manager, handle)

	// 逐人退场：只释放工作区 + 清会话内容，**不**碰作业输出（正文活到收口）。
	if err := coordinator.Retire(ctx, "exec"); err != nil {
		t.Fatalf("Retire: %v", err)
	}
	for _, call := range log.snapshot() {
		if call == "output:clear" {
			t.Fatal("team_retire 不该清理作业输出（正文活到 team_close）")
		}
	}
	if _, ok := manager.Observe(handle); !ok {
		t.Fatal("team_retire 不该销项作业（回收统一收口到 team_close）")
	}

	// 整队收口：清一次，且发生在"逐成员退场"之后（先停作业再清文件）。
	if _, err := coordinator.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
	calls := log.snapshot()
	clearIndex := -1
	for index, call := range calls {
		if call == "output:clear" {
			clearIndex = index
		}
	}
	if clearIndex < 0 {
		t.Fatalf("team_close 必须清理作业输出：%v", calls)
	}
	for _, call := range calls[:clearIndex] {
		switch {
		case strings.HasPrefix(call, "worktree:"), strings.HasPrefix(call, "session:"), strings.HasPrefix(call, "output:alloc:"):
		default:
			t.Fatalf("清理必须发生在逐成员退场之后，之前的调用只应是退场两步与派发时的分配：%v", calls)
		}
	}
}
