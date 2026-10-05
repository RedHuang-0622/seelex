package seelebridge

// workunit_team_test.go — teammate 单元在统一契约（seelebridge/workunit）上的接线用例。
//
// 四条钉住的事（都是行为，不是实现细节）：
//
//	① 中断后重启：记录说在跑 + 进程内无执行面 ⇒ 中断清单有条目，且**恢复说明**以
//	   workunit.RecoveryNotePrefix + " teammate" 开头、只进该角色**下一次**装配（读完即消）；
//	② 收口后记录被清：整队收口之后不再有"跑到哪"的残留（回灌读数是空的）；
//	③ 未提交产出的工作项不判死：进 review + 未合并标记（现场与产出都留），而收口闸门
//	   仍然拦住它（闸门不允许反向放松）；
//	④ AtTeamClose 的 Finish 不拆现场：策略只回答"什么时候回收"，拆现场是 Reclaim 的事。

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/Seele/jobs"
	seeletelemetry "github.com/RedHuang-0622/seelex/seelebridge/internal/telemetry"
	"github.com/RedHuang-0622/seelex/seelebridge/teamwork"
	"github.com/RedHuang-0622/seelex/seelebridge/worktree"
	"github.com/RedHuang-0622/seelex/seelebridge/workunit"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// teamTestKey 是本文件统一用的会话作用域（与 teamworkTestBackend 的 KeyFor 同键）。
var teamTestKey = sessionstore.Key{ProjectID: "p-team", SessionID: "sess-1"}

// ── ③ 收尾分类换源：与 node 同一份（workunit.ClassifyFinish），映射逐条钉 ──

// fakeTeamSpaces 是「一个 Work Item 一个 worktree」端口的替身：合并结果由用例给。
type fakeTeamSpaces struct {
	mergeErr error
	merged   []string
	released []string
}

func (f *fakeTeamSpaces) BindWorkspace(_ context.Context, binding teamwork.WorkspaceBinding) (teamwork.WorkspaceBinding, error) {
	return binding, nil
}

func (f *fakeTeamSpaces) MergeWorkspace(_ context.Context, binding teamwork.WorkspaceBinding) error {
	f.merged = append(f.merged, binding.WorkItem)
	return f.mergeErr
}

func (f *fakeTeamSpaces) ReleaseWorkspaceItem(_ context.Context, binding teamwork.WorkspaceBinding) error {
	f.released = append(f.released, binding.WorkItem)
	return nil
}

type fakeTeamReleaser struct{ released []string }

func (f *fakeTeamReleaser) ReleaseWorkspace(_ context.Context, role string) error {
	f.released = append(f.released, role)
	return nil
}

type fakeTeamSessions struct{ reset []string }

func (f *fakeTeamSessions) ResetSession(_ context.Context, roleSessionID string) error {
	f.reset = append(f.reset, roleSessionID)
	return nil
}

type fakeTeamQueue struct{ texts []string }

func (f *fakeTeamQueue) EnqueueTeammateMessage(_ context.Context, message teamwork.TeammateMessage) error {
	f.texts = append(f.texts, message.Text)
	return nil
}

// newSettleFixture 造一支"一件事正在跑、只等尾插"的团队（合并由 fake 决定）。
func newSettleFixture(t *testing.T, mergeErr error) (*teamwork.Coordinator, *memPlanStore, *fakeTeamSpaces, *fakeTeamQueue) {
	t.Helper()
	ctx := context.Background()
	store := &memPlanStore{}
	if err := store.WritePlan(ctx, teamTestKey, sessionstore.TeamworkPlan{
		TeamID: "t-settle", Version: 1,
		Members: []sessionstore.TeamworkMember{{Role: "exec", RoleSessionID: "sess-1-t-exec"}},
		Milestones: []sessionstore.TeamworkMilestone{{ID: "m1", Items: []sessionstore.TeamworkWorkItem{{
			ID: "wi-1", Milestone: "m1", Role: "exec", Name: "实现",
			Status:    sessionstore.TeamworkItemRunning,
			SessionID: "sess-1-t-exec-wi-1", Worktree: "seelex/exec-wi-1", Handle: "h-1",
		}}}},
	}, 0); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendBinding(ctx, teamTestKey, sessionstore.TeamworkBinding{
		WorkItem: "wi-1", Milestone: "m1", Role: "exec",
		SessionID: "sess-1-t-exec-wi-1", Worktree: "seelex/exec-wi-1",
	}); err != nil {
		t.Fatal(err)
	}
	manager, err := jobs.New()
	if err != nil {
		t.Fatalf("jobs.New: %v", err)
	}
	t.Cleanup(func() { _ = manager.Close(context.Background()) })
	spaces := &fakeTeamSpaces{mergeErr: mergeErr}
	queue := &fakeTeamQueue{}
	coordinator, err := teamwork.New(teamwork.Options{
		Key: teamTestKey, Store: store, Jobs: manager, Spaces: spaces, Teammates: queue,
		Worktrees: &fakeTeamReleaser{}, Sessions: &fakeTeamSessions{},
	})
	if err != nil {
		t.Fatalf("teamwork.New: %v", err)
	}
	return coordinator, store, spaces, queue
}

// TestTeamSettleClassifiesWithTheSharedClassifier 钉住收尾分类的**唯一来源**与逐条映射。
// 此前 teammate 侧只有一句"有错即 failed"：同一份"跑完了但没合进去"在 node 那条路上是
// 警告、在 teammate 这条路上是判死——leader 因此以为要重派，而产出就在现场里躺着。
func TestTeamSettleClassifiesWithTheSharedClassifier(t *testing.T) {
	uncommitted := fmt.Errorf("worktree %q: 现场有未提交改动: %w", "exec-wi-1", worktree.ErrUncommittedChanges)
	blocked := fmt.Errorf("worktree %q: 合并被挡: %w", "exec-wi-1", worktree.ErrMergeBlockedByMain)
	other := fmt.Errorf("worktree %q: git merge 撞了别的错", "exec-wi-1")

	cases := []struct {
		name         string
		mergeErr     error
		runErr       error
		want         workunit.OutcomeKind
		wantStatus   string
		wantUnmerged bool
	}{
		{"跑完且改动已合进去 → 落定待验收", nil, nil, workunit.OutcomeSettled, sessionstore.TeamworkItemReview, false},
		{"有未提交改动 → 不判死（review + 警告）", uncommitted, nil, workunit.OutcomeUncommitted, sessionstore.TeamworkItemReview, true},
		{"主工作区挡路 → 不判死（review + 警告）", blocked, nil, workunit.OutcomeMergeBlocked, sessionstore.TeamworkItemReview, true},
		{"合并撞别的错 → 判死", other, nil, workunit.OutcomeFailed, sessionstore.TeamworkItemFailed, false},
		{"这一轮本身失败 → 判死", nil, fmt.Errorf("provider 402"), workunit.OutcomeFailed, sessionstore.TeamworkItemFailed, false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			ctx := context.Background()
			coordinator, store, _, queue := newSettleFixture(t, testCase.mergeErr)
			outcome, err := coordinator.SettleWorkItemOutcome(ctx, teamwork.WorkerRequest{
				MainSessionID: "sess-1", WorkItemID: "wi-1",
			}, testCase.runErr)
			if err != nil {
				t.Fatalf("SettleWorkItemOutcome: %v", err)
			}
			if outcome.Kind != testCase.want {
				t.Fatalf("收尾分类必须与 node 同一份（workunit.ClassifyFinish）：得到 %s want %s（notice: %s）",
					outcome.Kind, testCase.want, outcome.Notice)
			}
			plan, err := store.ReadPlan(ctx, teamTestKey)
			if err != nil {
				t.Fatal(err)
			}
			item := plan.Milestones[0].Items[0]
			if got := item.StatusOrPending(); got != testCase.wantStatus {
				t.Fatalf("工作项状态 = %s，want %s（未提交 / 挡路都进 review：现场与产出都留，不许判死）",
					got, testCase.wantStatus)
			}
			if _, unmerged := plan.State.Unmerged["wi-1"]; unmerged != testCase.wantUnmerged {
				t.Fatalf("未合并标记 = %v，want %v（收口闸门据此判定「这份现场还能不能拆」）", unmerged, testCase.wantUnmerged)
			}
			if len(queue.texts) != 1 {
				t.Fatalf("尾插必须恰好一条（有界回执）：%v", queue.texts)
			}
		})
	}
}

// TestTeamCloseGateKeepsUnmergedScenes 钉住闸门**没有反向放松**：分类改成"不判死"之后，
// 一份还没合进去的产出仍然不许被整队收口静默拆掉；leader 处置完（验收入账）才放行。
func TestTeamCloseGateKeepsUnmergedScenes(t *testing.T) {
	ctx := context.Background()
	mergeErr := fmt.Errorf("worktree %q: %w", "exec-wi-1", worktree.ErrUncommittedChanges)
	coordinator, _, spaces, _ := newSettleFixture(t, mergeErr)
	if _, err := coordinator.SettleWorkItemOutcome(ctx, teamwork.WorkerRequest{
		MainSessionID: "sess-1", WorkItemID: "wi-1",
	}, nil); err != nil {
		t.Fatalf("SettleWorkItemOutcome: %v", err)
	}
	if _, err := coordinator.Close(ctx); err == nil || !strings.Contains(err.Error(), "wi-1") {
		t.Fatalf("收口闸门必须拦住「改动未合并」的现场（否则收敛成丢产出），得到 %v", err)
	}
	// 整份替换计划（leader 改成员/改里程碑时的常规动作）之后闸门**不松**：未合并标记
	// 是事实（那份现场里还留着没合进去的产出），按 item id 保留。
	replanned, err := coordinator.Plan(ctx)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	replanned.State.Unmerged = nil // 工具面 schema 里没有这一格：模拟 leader 回传的计划
	if err := coordinator.SetPlan(ctx, replanned); err != nil {
		t.Fatalf("SetPlan: %v", err)
	}
	if _, err := coordinator.Close(ctx); err == nil || !strings.Contains(err.Error(), "wi-1") {
		t.Fatalf("整份替换计划之后闸门也必须仍然拦住未合并的现场，得到 %v", err)
	}
	// leader 手工处置（补提交 / 变基 / 合并）之后验收入账 → 闸门放行。
	if err := coordinator.AcceptItem(ctx, "wi-1", "手工合并后验收"); err != nil {
		t.Fatalf("AcceptItem: %v", err)
	}
	if _, err := coordinator.Close(ctx); err != nil {
		t.Fatalf("验收之后收口应当放行: %v", err)
	}
	if len(spaces.merged) != 1 {
		t.Fatalf("尾插的合并必须恰好做过一次：%v", spaces.merged)
	}
}

// ── 宿主级：① 中断回灌 ② 收口清账 ④ 策略不拆现场 ──────────────────────

// teamHostFixture 是一个"跑过一轮"的宿主：真实 git 仓库 + 真实存储 + 两件工作项
// （两个角色各一件，用来钉"只拆自己这一份"）。
type teamHostFixture struct {
	runtime   *Runtime
	nodeStore *sessionstore.NodeSessionStore
	store     *memPlanStore
	router    *sessionstore.Router
}

// teamHostPlan 是本文件宿主用例的团队计划：exec 的 wi-1 在跑、test_case 的 wi-2 待验收。
func teamHostPlan() sessionstore.TeamworkPlan {
	return sessionstore.TeamworkPlan{
		TeamID: "t-host", Version: 1,
		Members: []sessionstore.TeamworkMember{
			{Role: "exec", RoleSessionID: "sess-1-t-exec", Worktree: "seelex/exec"},
			{Role: "test_case", RoleSessionID: "sess-1-t-test_case"},
		},
		Milestones: []sessionstore.TeamworkMilestone{{ID: "m1", Items: []sessionstore.TeamworkWorkItem{
			{
				ID: "wi-1", Milestone: "m1", Role: "exec", Name: "实现", Status: sessionstore.TeamworkItemRunning,
				SessionID: "sess-1-t-exec-wi-1", Worktree: "seelex/exec-wi-1", Handle: "h-1",
			},
			{
				ID: "wi-2", Milestone: "m1", Role: "test_case", Name: "复核", Status: sessionstore.TeamworkItemReview,
				SessionID: "sess-1-t-test_case-wi-2", Worktree: "seelex/test_case-wi-2",
			},
		}}},
	}
}

func newTeamHostFixture(t *testing.T, plan sessionstore.TeamworkPlan, bindings []sessionstore.TeamworkBinding) *teamHostFixture {
	t.Helper()
	ctx := context.Background()
	store := &memPlanStore{}
	if err := store.WritePlan(ctx, teamTestKey, plan, 0); err != nil {
		t.Fatal(err)
	}
	for _, binding := range bindings {
		if err := store.AppendBinding(ctx, teamTestKey, binding); err != nil {
			t.Fatal(err)
		}
	}
	router, err := sessionstore.NewRouter(filepath.Join(t.TempDir(), "session-storage.json"), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { router.Close() })
	runtime := newTestRuntime(t)
	t.Cleanup(func() { runtime.Shutdown() })
	runtime.AttachHistoryRouter(router)
	nodeStore := sessionstore.NewNodeSessionStore(router)
	runtime.AttachSubSessionStore(nodeStore)
	if err := runtime.BindProjectRoot(setupGitRepo(t)); err != nil {
		t.Fatalf("BindProjectRoot: %v", err)
	}
	if err := runtime.SetTeamworkBackend(teamworkTestBackend(store, "sess-1")); err != nil {
		t.Fatalf("SetTeamworkBackend: %v", err)
	}
	return &teamHostFixture{runtime: runtime, nodeStore: nodeStore, store: store, router: router}
}

func teamHostBindings() []sessionstore.TeamworkBinding {
	return []sessionstore.TeamworkBinding{
		{WorkItem: "wi-1", Milestone: "m1", Role: "exec", SessionID: "sess-1-t-exec-wi-1", Worktree: "seelex/exec-wi-1"},
		{WorkItem: "wi-2", Milestone: "m1", Role: "test_case", SessionID: "sess-1-t-test_case-wi-2", Worktree: "seelex/test_case-wi-2"},
	}
}

// saveTeamRecord 落一条 teammate 单元的会话记录（形状 = sessionstore.NodeSessionRecord）。
func saveTeamRecord(t *testing.T, store *sessionstore.NodeSessionStore, record sessionstore.NodeSessionRecord) {
	t.Helper()
	if err := store.Save("p-team", "sess-1", record); err != nil {
		t.Fatalf("Save(%s): %v", record.NodeID, err)
	}
}

// teamRecords 读回本会话的全部节点记录（含子代理的，调用方自己筛 NodeID）。
func teamRecords(t *testing.T, store *sessionstore.NodeSessionStore) []sessionstore.NodeSessionRecord {
	t.Helper()
	records, err := store.List("p-team", "sess-1")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	return records
}

// TestTeamUnitRecoverMarksInterruptedAndInjectsTheNoteOnce ①：
// 记录说在跑、而本进程没有它的执行面 ⇒ 中断清单有条目 + 恢复说明注入该角色下一次装配
// （前缀族与 subagent 同源，一次装配读完即消）。
func TestTeamUnitRecoverMarksInterruptedAndInjectsTheNoteOnce(t *testing.T) {
	ctx := context.Background()
	fixture := newTeamHostFixture(t, teamHostPlan(), teamHostBindings())
	// 旧进程的落盘（"跑到哪、现场在哪"）：三件在跑、一件已落定。
	saveTeamRecord(t, fixture.nodeStore, sessionstore.NodeSessionRecord{
		NodeID: "exec", SessionID: "sess-1-t-exec", Goal: "角色级派发的一轮",
		Status: teamUnitStatusRunning,
	})
	saveTeamRecord(t, fixture.nodeStore, sessionstore.NodeSessionRecord{
		NodeID: "exec-wi-1", SessionID: "sess-1-t-exec-wi-1", Goal: "把现场生命周期接进契约",
		Status: teamUnitStatusRunning, Summary: "已改完 worktree 认领",
		StagesJSON: []byte(`[{"stage":"running","preview":"改 worktree_manager"}]`),
		Worktree:   sessionstore.NodeWorktreeRecord{Path: filepath.Join(t.TempDir(), "wt"), Branch: "seelex/exec-wi-1"},
	})
	saveTeamRecord(t, fixture.nodeStore, sessionstore.NodeSessionRecord{
		NodeID: "exec-wi-2", SessionID: "sess-1-t-exec-wi-2", Goal: "还在本进程里跑着的一轮",
		Status: teamUnitStatusRunning,
	})
	saveTeamRecord(t, fixture.nodeStore, sessionstore.NodeSessionRecord{
		NodeID: "test_case-wi-2", SessionID: "sess-1-t-test_case-wi-2", Goal: "复核",
		Status: teamUnitStatusDone,
	})
	// 计划里补上 wi-2 的现场（记录/现场名单都从计划 + 账本读）。
	plan := teamHostPlan()
	plan.Milestones[0].Items = append(plan.Milestones[0].Items, sessionstore.TeamworkWorkItem{
		ID: "wi-2b", Milestone: "m1", Role: "exec", Name: "还在跑的另一件事",
		Status: sessionstore.TeamworkItemRunning, SessionID: "sess-1-t-exec-wi-2",
		Worktree: "seelex/exec-wi-2", Handle: "h-2",
	})
	if err := fixture.store.WritePlan(ctx, teamTestKey, plan, 0); err != nil {
		t.Fatal(err)
	}
	// "本进程还有执行面"的那一件：角色会话句柄在册（角色会话是进程内执行面）。
	state := fixture.runtime.roleTurnState()
	state.mu.Lock()
	if state.sessions == nil {
		state.sessions = map[string]*roleSessionHandle{}
	}
	state.sessions["sess-1-t-exec-wi-2"] = &roleSessionHandle{id: "sess-1-t-exec-wi-2", role: "exec"}
	state.mu.Unlock()

	recovery, err := fixture.runtime.RecoverTeamworkUnits(ctx, "sess-1")
	if err != nil {
		t.Fatalf("RecoverTeamworkUnits: %v", err)
	}
	got := map[string]bool{}
	for _, nodeID := range recovery.Resume.Interrupted {
		got[nodeID] = true
	}
	// ① 中断清单：记录说在跑 + 本进程已无它的执行面（角色级与 Work Item 级两种 nodeID 都在）。
	for _, want := range []string{"exec", "exec-wi-1"} {
		if !got[want] {
			t.Fatalf("记录说在跑、本进程没有执行面的单元 %q 必须记进中断清单：%v", want, recovery.Resume.Interrupted)
		}
	}
	// 还有执行面的（角色会话活着）与已落定的，都不许被说成中断。
	for _, unwanted := range []string{"exec-wi-2", "test_case-wi-2"} {
		if got[unwanted] {
			t.Fatalf("%q 不该被判中断（前者本进程还有执行面、后者已落定）：%v", unwanted, recovery.Resume.Interrupted)
		}
	}
	if recovery.Resume.Sessions != 4 {
		t.Fatalf("回灌读数应当数出这支团队的 4 条单元记录，得到 %d", recovery.Resume.Sessions)
	}

	// ② 恢复说明：前缀族 + kind（与既有的 subagent 说明同一种东西）。
	note := fixture.runtime.consumeTeamResumeNote("sess-1-t-exec-wi-1")
	if !strings.HasPrefix(note, workunit.RecoveryNotePrefix+" "+string(workunit.KindTeammate)) {
		t.Fatalf("恢复说明必须落在契约的前缀族里（%q + \" teammate\"）：%q", workunit.RecoveryNotePrefix, note)
	}
	if !strings.Contains(note, "把现场生命周期接进契约") {
		t.Fatalf("恢复说明要摆出记录里的事实（目标）：%q", note)
	}

	// ③ 注入到该角色**下一次装配**，且读完即消（同 SubagentResumeNote 的一次性语义）。
	if _, err := fixture.runtime.RecoverTeamworkUnits(ctx, "sess-1"); err != nil {
		t.Fatalf("RecoverTeamworkUnits（幂等重放）: %v", err)
	}
	request := teamwork.WorkerRequest{
		MainSessionID: "sess-1", Role: "exec", RoleSessionID: "sess-1-t-exec-wi-1",
		WorkItemID: "wi-1", Worktree: "seelex/exec-wi-1", Goal: "接着这件事做",
	}
	spec := fixture.runtime.workerRoleRoundSpec(request, "sess-1", 4)
	if !strings.Contains(spec.SystemPrompt, workunit.RecoveryNotePrefix+" "+string(workunit.KindTeammate)) {
		t.Fatalf("恢复说明必须注入该角色下一次装配（系统提示）：\n%s", spec.SystemPrompt)
	}
	next := fixture.runtime.workerRoleRoundSpec(request, "sess-1", 4)
	if strings.Contains(next.SystemPrompt, workunit.RecoveryNotePrefix) {
		t.Fatalf("恢复说明是**一次性**的（读完即消），下一次装配不该再带它：\n%s", next.SystemPrompt)
	}
}

// TestTeamUnitRecordsCarryTheSceneAndProgress 钉住**记录里存了什么**：契约的形状
// （sessionstore.NodeSessionRecord：Goal / Status / Summary / StagesJSON / Worktree）
// 与 NodeID 的契约命名（`<role>-<itemID>` / `<role>`）——重启回灌读的就是它。
func TestTeamUnitRecordsCarryTheSceneAndProgress(t *testing.T) {
	fixture := newTeamHostFixture(t, teamHostPlan(), teamHostBindings())
	scene := fixture.runtime.worktreeMgr.BeginNamed("exec-wi-1")
	if scene == nil {
		t.Fatal("前置：应当能建出真实现场")
	}
	request := teamwork.WorkerRequest{
		MainSessionID: "sess-1", Role: "exec", RoleSessionID: "sess-1-t-exec-wi-1",
		WorkItemID: "wi-1", Worktree: "seelex/exec-wi-1", Goal: "把现场生命周期接进契约",
	}
	key := teamUnitKeyFor(request)
	if key.NodeID != "exec-wi-1" {
		t.Fatalf("Work Item 单元的 nodeID = %q，want %q（契约命名 = 指派名去前缀）", key.NodeID, "exec-wi-1")
	}
	if roleKey := teamUnitKeyFor(teamwork.WorkerRequest{Role: "exec", RoleSessionID: "sess-1-t-exec", Worktree: "seelex/exec"}); roleKey.NodeID != "exec" {
		t.Fatalf("角色级单元的 nodeID = %q，want %q", roleKey.NodeID, "exec")
	}

	fixture.runtime.markTeamUnitRunning("sess-1", key)
	record := singleTeamRecord(t, fixture.nodeStore, "exec-wi-1")
	if record.SessionID != "sess-1-t-exec-wi-1" || record.MainSessionID != "sess-1" {
		t.Fatalf("记录必须能定位到自己的会话与归属：%+v", record)
	}
	if record.Goal != "把现场生命周期接进契约" || record.Status != teamUnitStatusRunning {
		t.Fatalf("记录要写出目标与状态（跑到哪）：%+v", record)
	}
	if record.Worktree.Path != scene.Path || record.Worktree.Branch != "seelex/exec-wi-1" {
		t.Fatalf("记录要写出**现场在哪**：got %+v want path=%q branch=%q", record.Worktree, scene.Path, "seelex/exec-wi-1")
	}
	if len(record.StagesJSON) == 0 {
		t.Fatal("记录要带一条打点（恢复说明据此说出中断前到哪一步）")
	}

	fixture.runtime.recordTeamUnitRoundOutput("sess-1", key, "已改完 worktree 认领")
	record = singleTeamRecord(t, fixture.nodeStore, "exec-wi-1")
	if !strings.Contains(record.Summary, "worktree 认领") {
		t.Fatalf("这一轮的产出预览要进 Summary（中断后「跑到哪」就是它）：%+v", record.Summary)
	}

	// 收尾分类 → 终态：没合并的归 done（这一轮跑完了），判死才归 failed。
	fixture.runtime.settleTeamUnitRecord("sess-1", key, workunit.Outcome{
		Kind: workunit.OutcomeUncommitted, Notice: "现场有未提交改动，本次未合并",
	})
	record = singleTeamRecord(t, fixture.nodeStore, "exec-wi-1")
	if record.Status != teamUnitStatusDone || !strings.Contains(record.Summary, "未提交") {
		t.Fatalf("未提交这一档应当记为 done + 说明（不判死）：%+v", record)
	}
	fixture.runtime.settleTeamUnitRecord("sess-1", key, workunit.Outcome{
		Kind: workunit.OutcomeFailed, Notice: "跑失败：provider 402",
	})
	record = singleTeamRecord(t, fixture.nodeStore, "exec-wi-1")
	if record.Status != teamUnitStatusFailed {
		t.Fatalf("判死那一档记为 failed：%+v", record)
	}

	fixture.runtime.clearTeamUnitRecord("sess-1", "sess-1-t-exec-wi-1")
	if len(teamRecords(t, fixture.nodeStore)) != 0 {
		t.Fatal("清掉之后不该再有这条记录")
	}
}

// TestWorkerRoundPersistsTheUnitRecord 端到端：派发（屏障 / 依赖 / 一 Work Item 一套现场都过
// 编排面）→ 角色回合跑一轮（假引擎，不碰网络）→ 尾插落定 → **会话记录被写成终态**。
//
// 它钉的是"落盘"真的接在**执行面**上，而不是只在 helper 层成立：跑之前记录说 running
// （重启即失忆的那一半，正是缺了它），尾插把分类写回之后记录说 done + 结论。
// 角色会话历史不落盘（那是另一件事），记录回答的是"这件事跑到哪、现场在哪"。
func TestWorkerRoundPersistsTheUnitRecord(t *testing.T) {
	ctx := seeletelemetry.WithSessionID(context.Background(), "sess-1")
	fixture := newTeamHostFixture(t, teamHostPlan(), teamHostBindings())
	// 这一件事还没派过（可派发）：清掉预置的 running / 句柄 / 现场。
	plan := teamHostPlan()
	plan.Milestones[0].Items[0].Status = ""
	plan.Milestones[0].Items[0].SessionID = ""
	plan.Milestones[0].Items[0].Worktree = ""
	plan.Milestones[0].Items[0].Handle = ""
	if err := fixture.store.WritePlan(ctx, teamTestKey, plan, 0); err != nil {
		t.Fatal(err)
	}
	engine := &fakeRoleEngine{id: "unused", output: "本轮的结论：现场已接进契约；下一步：跑用例"}
	fixture.runtime.SetRoleEngineFactory(func(sessionID string) (roleEngine, error) {
		engine.id = sessionID
		return engine, nil
	})
	if _, err := fixture.runtime.teamDispatchHandler(ctx, `{"item":"wi-1"}`); err != nil {
		t.Fatalf("team_dispatch: %v", err)
	}
	waitTeamUnitRecordStatus(t, fixture, "exec-wi-1", teamUnitStatusDone)
	record := singleTeamRecord(t, fixture.nodeStore, "exec-wi-1")
	if strings.TrimSpace(record.Summary) == "" {
		t.Fatalf("终态记录要留下这一轮怎么结束的说明：%+v", record)
	}
	if record.SessionID == "" {
		t.Fatalf("记录要写出它自己的角色会话号（回灌的钥匙）：%+v", record)
	}
	// 合并成功 = 现场已被回收（Finish 的 cleanup + Release）：记录那一格说的是"**此刻**
	// 现场在哪"，不是"曾经在哪"（曾经在哪是账本与审计的事实），因此这里必须为空——
	// 留一个已经不在的路径才是误导。合并不落地的那几档（未提交 / 挡路 / 判死）现场还在，
	// 记录就会带着它（见 TestTeamUnitRecordsCarryTheSceneAndProgress 的 running 一档）。
	if record.Worktree.Path != "" || record.Worktree.Branch != "" {
		t.Fatalf("合并成功之后现场已回收，终态记录不该再指着它：%+v", record.Worktree)
	}
	if _, ok := fixture.runtime.worktreeMgr.Info("exec-wi-1"); ok {
		t.Fatal("合并成功之后现场的注册表条目也该清掉（同一份现场不许留两处账）")
	}
	// 编排面的账也落定了：这一件事进待验收（尾插先合并、再插回执、再写态）。
	view, err := fixture.store.ReadPlan(ctx, teamTestKey)
	if err != nil {
		t.Fatal(err)
	}
	if got := view.Milestones[0].Items[0].StatusOrPending(); got != sessionstore.TeamworkItemReview {
		t.Fatalf("尾插之后这件事应当待验收，得到 %s", got)
	}
}

// waitTeamUnitRecordStatus 等到某个单元的记录落到目标状态（异步作业；超时即失败）。
func waitTeamUnitRecordStatus(t *testing.T, fixture *teamHostFixture, nodeID, want string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	var last sessionstore.NodeSessionRecord
	for time.Now().Before(deadline) {
		for _, record := range teamRecords(t, fixture.nodeStore) {
			if record.NodeID == nodeID {
				last = record
			}
		}
		if last.Status == want {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("等 %q 的单元记录变成 %q 超时：%+v", nodeID, want, last)
}

// singleTeamRecord 取某 nodeID 的单元记录（不存在即失败）。
func singleTeamRecord(t *testing.T, store *sessionstore.NodeSessionStore, nodeID string) sessionstore.NodeSessionRecord {
	t.Helper()
	for _, record := range teamRecords(t, store) {
		if record.NodeID == nodeID {
			return record
		}
	}
	t.Fatalf("盘上没有 %q 的单元记录", nodeID)
	return sessionstore.NodeSessionRecord{}
}

// TestTeamCloseClearsTheUnitRecords ②：整队收口之后，单元的会话记录被清——再回灌时
// 读数是空的（"收口后不再算残留"）。
func TestTeamCloseClearsTheUnitRecords(t *testing.T) {
	ctx := seeletelemetry.WithSessionID(context.Background(), "sess-1")
	fixture := newTeamHostFixture(t, teamHostPlan(), teamHostBindings())
	// 两件事都跑到待验收（收口闸门放行的那一档），各留一条记录。
	plan := teamHostPlan()
	plan.Milestones[0].Items[0].Status = sessionstore.TeamworkItemReview
	if err := fixture.store.WritePlan(ctx, teamTestKey, plan, 0); err != nil {
		t.Fatal(err)
	}
	saveTeamRecord(t, fixture.nodeStore, sessionstore.NodeSessionRecord{
		NodeID: "exec-wi-1", SessionID: "sess-1-t-exec-wi-1", Goal: "实现", Status: teamUnitStatusDone,
	})
	saveTeamRecord(t, fixture.nodeStore, sessionstore.NodeSessionRecord{
		NodeID: "test_case-wi-2", SessionID: "sess-1-t-test_case-wi-2", Goal: "复核", Status: teamUnitStatusDone,
	})
	if len(teamRecords(t, fixture.nodeStore)) != 2 {
		t.Fatalf("前置：两条记录都必须在盘上")
	}
	if _, err := fixture.runtime.teamCloseHandler(ctx, ""); err != nil {
		t.Fatalf("team_close: %v", err)
	}
	for _, record := range teamRecords(t, fixture.nodeStore) {
		if record.NodeID == "exec-wi-1" || record.NodeID == "test_case-wi-2" {
			t.Fatalf("收口之后不该还有 %q 的单元记录（它描述的是已拆掉的现场）", record.NodeID)
		}
	}
	recovery, err := fixture.runtime.RecoverTeamworkUnits(ctx, "sess-1")
	if err != nil {
		t.Fatalf("RecoverTeamworkUnits: %v", err)
	}
	if !recovery.Resume.Empty() {
		t.Fatalf("收口之后回灌读数必须是空的（不再算残留）：%+v", recovery.Resume)
	}
}

// TestTeamUnitFinishPolicyKeepsTheScene ④：AtTeamClose 的 Finish **不拆现场**——
// 现场与会话的回收只有 Reclaim 一个动作（Immediate 才是"收尾即回收"那一档）。
func TestTeamUnitFinishPolicyKeepsTheScene(t *testing.T) {
	ctx := seeletelemetry.WithSessionID(context.Background(), "sess-1")
	fixture := newTeamHostFixture(t, teamHostPlan(), teamHostBindings())
	// 两个角色的现场都建出来（真实 git worktree），记录也落上。
	for _, nodeID := range []string{"exec-wi-1", "test_case-wi-2"} {
		if wt := fixture.runtime.worktreeMgr.BeginNamed(nodeID); wt == nil {
			t.Fatalf("前置：应当能建出现场 %q", nodeID)
		}
	}
	sceneFor := func(nodeID, role, sessionID, worktreeName string) *teamUnit {
		unit, err := newTeamUnit(fixture.runtime, "sess-1", role, workunit.Scene{
			NodeID: nodeID, SessionID: sessionID, Worktree: worktreeName, TeamID: "t-host", WorkItem: "wi-1",
		}, teamwork.WorkerRequest{
			MainSessionID: "sess-1", Role: role, RoleSessionID: sessionID, WorkItemID: "wi-1", Worktree: worktreeName,
		})
		if err != nil {
			t.Fatalf("newTeamUnit: %v", err)
		}
		return unit
	}
	unit := sceneFor("exec-wi-1", "exec", "sess-1-t-exec-wi-1", "seelex/exec-wi-1")
	saveTeamRecord(t, fixture.nodeStore, sessionstore.NodeSessionRecord{
		NodeID: "exec-wi-1", SessionID: "sess-1-t-exec-wi-1", Goal: "实现", Status: teamUnitStatusDone,
	})

	policy := unit.FinishPolicy()
	if _, ok := policy.(workunit.AtTeamClose); !ok {
		t.Fatalf("teammate 的策略必须是 AtTeamClose（回收唯一入口 = team_close），得到 %T", policy)
	}
	if err := policy.AfterFinish(ctx, unit.host, unit.read); err != nil {
		t.Fatalf("AtTeamClose.AfterFinish: %v", err)
	}
	if _, ok := fixture.runtime.worktreeMgr.Info("exec-wi-1"); !ok {
		t.Fatal("AtTeamClose 不得自己拆现场（Finish 之后现场还要给 leader 审查 / 人工处置用）")
	}
	if len(teamRecords(t, fixture.nodeStore)) != 1 {
		t.Fatal("AtTeamClose 不得清会话记录（记录与现场是同一份事实的两个面）")
	}

	// 对照：Immediate 那一档才在收尾之后回收——而且**只拆自己这一份**。
	if err := (workunit.Immediate{}).AfterFinish(ctx, unit.host, unit.read); err != nil {
		t.Fatalf("Immediate.AfterFinish: %v", err)
	}
	if _, ok := fixture.runtime.worktreeMgr.Info("exec-wi-1"); ok {
		t.Fatal("Immediate 策略应当回收自己这一份现场")
	}
	if _, ok := fixture.runtime.worktreeMgr.Info("test_case-wi-2"); !ok {
		t.Fatal("回收必须只拆自己这一份，不牵连同会话其他人")
	}
	remaining := teamRecords(t, fixture.nodeStore)
	if len(remaining) != 0 {
		t.Fatalf("回收到的那一份记录要一起清（另一个角色的记录本来就不该在盘上）：%+v", remaining)
	}
}
