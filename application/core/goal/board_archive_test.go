package goal

// board_archive_test.go — 钉住 goal 看板存档的**写侧**语义：active/history 分离、
// 收口账本按 goal_id 只追加、冷读（重启）后账本仍在、指纹节流。
//
// 为什么单独立文件：sessionstore_store_test.go 钉的是"goal 第五栈（活栈）随会话
// 恢复"；这里钉的是它旁边那份**派生快照**（metadata/board_goal.json）——弹栈即
// 消失的收口事实只在这里留痕，两者不能互相冒充。

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/RedHuang-0622/seelex/sessionstore"
)

// boardArchiveCtx 是本文件的测试上下文（不借用其它测试文件的包级变量）。
var boardArchiveCtx = context.Background()

// newBoardArchiveRouter 建一个临时会话存储（看板存档落在会话目录的 metadata 下）。
func newBoardArchiveRouter(t *testing.T) *sessionstore.Router {
	t.Helper()
	root := t.TempDir()
	router, err := sessionstore.NewRouter(filepath.Join(root, "session-storage.json"), root)
	if err != nil {
		t.Fatalf("new session router: %v", err)
	}
	t.Cleanup(func() { _ = router.Close() })
	return router
}

// newBoardArchiveStore 装配"会话存储 + 看板存档面"的 goal 域适配器。
// 第二个返回值是存档取用面（断言用），第三个是会话存储（冷读用）。
func newBoardArchiveStore(t *testing.T, router *sessionstore.Router, sessionID string) (
	*ContextStateStore, sessionstore.SessionBoards, *sessionstore.SessionContextStore,
) {
	t.Helper()
	session := sessionstore.NewSessionContextStore(router, sessionID)
	if err := session.Load(boardArchiveCtx); err != nil {
		t.Fatalf("load session context %q: %v", sessionID, err)
	}
	boards, ok := router.BoardsForSession(sessionID)
	if !ok {
		t.Fatal("看板存档取用面未装配（BoardsForSession 返回 false）")
	}
	return NewContextStateStore(session, boards), boards, session
}

// readBoardArchive 读回看板存档（读取失败直接失败：这些用例都先写过）。
func readBoardArchive(t *testing.T, boards sessionstore.SessionBoards) sessionstore.GoalBoardMeta {
	t.Helper()
	meta, err := boards.ReadGoalBoard(boardArchiveCtx)
	if err != nil {
		t.Fatalf("读看板存档: %v", err)
	}
	return meta
}

// historyFor 返回账本里某个 goal 的条目条数（"不会重复进 history"的判据）。
func historyFor(meta sessionstore.GoalBoardMeta, goalID string) int {
	count := 0
	for _, entry := range meta.History {
		if entry.GoalID == goalID {
			count++
		}
	}
	return count
}

// TestGoalBoardArchiveSeparatesActiveFrameFromHistory 验证 active/history 分离：
//   - active = 当前栈顶帧（嵌套压栈时跟着换成新栈顶，被压栈的父目标只是 paused，
//     **不进** history）；
//   - history 只在收口（finish/abort）时追加，且带审计条目的 Status/At/Reason；
//   - 生命周期：首次 open 置 seq=1/opened_at，此后每次变化 seq+1、opened_at 不动。
func TestGoalBoardArchiveSeparatesActiveFrameFromHistory(t *testing.T) {
	router := newBoardArchiveRouter(t)
	store, boards, _ := newBoardArchiveStore(t, router, "session-board-split")
	controller := NewController(Options{Depth: 2, Store: store, Audit: store})

	if _, err := controller.Begin(boardArchiveCtx, BeginRequest{
		Title: "父目标", Acceptance: []string{"go build ./..."},
	}); err != nil {
		t.Fatalf("begin parent: %v", err)
	}
	first := readBoardArchive(t, boards)
	if first.State != sessionstore.BoardStateActive || first.Active == nil {
		t.Fatalf("栈非空 → 看板应为 active 且带当前帧: %+v", first)
	}
	if first.Active.GoalID != "g-1" || first.Active.Title != "父目标" ||
		len(first.Active.Acceptance) != 1 {
		t.Fatalf("当前帧快照应带足看板字段: %+v", first.Active)
	}
	if len(first.History) != 0 {
		t.Fatalf("活体帧不是历史（active/history 分离）: %+v", first.History)
	}
	if first.Seq != 1 || first.OpenedAt == 0 {
		t.Fatalf("首次 open 应置 seq=1/opened_at: seq=%d opened_at=%v", first.Seq, first.OpenedAt)
	}
	openedAt := first.OpenedAt

	if _, err := controller.Begin(boardArchiveCtx, BeginRequest{Title: "子目标"}); err != nil {
		t.Fatalf("begin child: %v", err)
	}
	nested := readBoardArchive(t, boards)
	if nested.Active == nil || nested.Active.GoalID != "g-2" {
		t.Fatalf("active 应换成新栈顶: %+v", nested.Active)
	}
	if len(nested.History) != 0 {
		t.Fatalf("被压栈的父目标只是 paused，不得进账本: %+v", nested.History)
	}
	if nested.Seq != 2 {
		t.Fatalf("栈变化应让 seq 前进到 2: %d", nested.Seq)
	}
	if nested.OpenedAt != openedAt {
		t.Fatalf("opened_at 此后不应移动: %v → %v", openedAt, nested.OpenedAt)
	}

	if _, err := controller.Finish(boardArchiveCtx, FinishRequest{Result: "子目标完成"}); err != nil {
		t.Fatalf("finish child: %v", err)
	}
	closed := readBoardArchive(t, boards)
	if closed.State != sessionstore.BoardStateActive || closed.Active == nil ||
		closed.Active.GoalID != "g-1" {
		t.Fatalf("子目标收口后父目标应回到当前帧: %+v", closed)
	}
	if historyFor(closed, "g-2") != 1 {
		t.Fatalf("收口目标应进账本恰好一条: %+v", closed.History)
	}
	entry := closed.History[0]
	if entry.Status != StatusCompleted.String() || entry.Title != "子目标" {
		t.Fatalf("派生条目应取审计条目的 Status/Title: %+v", entry)
	}
	if entry.ClosedReason == "" {
		t.Fatal("ClosedReason 为空时应按 kind 填 goal.finish（账本条目不得没有原因）")
	}
	if entry.ClosedAt <= 0 {
		t.Fatalf("派生条目应取审计条目的 At: %+v", entry)
	}

	// 父目标也收口 → 栈空 → 看板关闭（closed + closed_at + closed_reason）。
	if _, err := controller.Finish(boardArchiveCtx, FinishRequest{}); err != nil {
		t.Fatalf("finish parent: %v", err)
	}
	empty := readBoardArchive(t, boards)
	if empty.State != sessionstore.BoardStateClosed {
		t.Fatalf("栈空应让看板 closed: %+v", empty)
	}
	if empty.Active != nil {
		t.Fatalf("看板关闭时不留当前帧（退出场，不留空壳）: %+v", empty.Active)
	}
	if empty.ClosedAt <= 0 || empty.ClosedReason == "" {
		t.Fatalf("closed 必须带 closed_at/closed_reason: %+v", empty.BoardLifecycle)
	}
	if historyFor(empty, "g-1") != 1 || historyFor(empty, "g-2") != 1 {
		t.Fatalf("两个收口目标都应在账本里: %+v", empty.History)
	}
	if empty.OpenedAt != openedAt {
		t.Fatalf("opened_at 不应随关闭重置: %v → %v", openedAt, empty.OpenedAt)
	}
}

// TestGoalBoardArchiveHistoryIsAppendOnlyPerGoal 验证账本**只追加不重写**：收口后
// 再触发多次刷新（Save + 终态审计刷新 + 后续目标变更）都不会给同一个 goal 追加
// 第二条，也不会回改既有条目的字段。
func TestGoalBoardArchiveHistoryIsAppendOnlyPerGoal(t *testing.T) {
	router := newBoardArchiveRouter(t)
	store, boards, _ := newBoardArchiveStore(t, router, "session-board-append")
	controller := NewController(Options{Depth: 2, Store: store, Audit: store})

	if _, err := controller.Begin(boardArchiveCtx, BeginRequest{Title: "第一轮"}); err != nil {
		t.Fatalf("begin first: %v", err)
	}
	if _, err := controller.Update(boardArchiveCtx, UpdateRequest{
		ProgressKind: ProgressMilestone, ProgressContent: "已接线",
	}); err != nil {
		t.Fatalf("update first: %v", err)
	}
	if _, err := controller.Finish(boardArchiveCtx, FinishRequest{Reason: "验收通过"}); err != nil {
		t.Fatalf("finish first: %v", err)
	}
	after := readBoardArchive(t, boards)
	if historyFor(after, "g-1") != 1 {
		t.Fatalf("收口应恰好进账本一条: %+v", after.History)
	}
	firstEntry := after.History[0]

	// 后续目标变更（新 Save）与"重放同一份栈投影"都不得让 g-1 再进一次。
	if _, err := controller.Begin(boardArchiveCtx, BeginRequest{Title: "第二轮"}); err != nil {
		t.Fatalf("begin second: %v", err)
	}
	if err := store.Save(boardArchiveCtx, controller.Status().Stack); err != nil {
		t.Fatalf("replay save: %v", err)
	}
	replayed := readBoardArchive(t, boards)
	if historyFor(replayed, "g-1") != 1 {
		t.Fatalf("重放/后续变更不得重复追加: %+v", replayed.History)
	}
	if replayed.History[0] != firstEntry {
		t.Fatalf("既有账本条目不得被重写: %+v → %+v", firstEntry, replayed.History[0])
	}
	if replayed.Active == nil || replayed.Active.GoalID != "g-2" {
		t.Fatalf("当前帧应是第二轮目标: %+v", replayed.Active)
	}
}

// TestGoalBoardArchiveSurvivesColdRead 验证**重启快照**语义：换一个会话存储/
// 适配器实例（模拟重启）冷读，收口账本仍在——它不依赖进程内的
// goal.Controller.History，也不靠活栈（活栈里收口目标已经消失）。
func TestGoalBoardArchiveSurvivesColdRead(t *testing.T) {
	router := newBoardArchiveRouter(t)
	store, _, _ := newBoardArchiveStore(t, router, "session-board-cold")
	controller := NewController(Options{Depth: 1, Store: store, Audit: store})
	if _, err := controller.Begin(boardArchiveCtx, BeginRequest{Title: "冷读目标"}); err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := controller.Update(boardArchiveCtx, UpdateRequest{
		ProgressKind: ProgressFinding, ProgressContent: "复核了契约",
	}); err != nil {
		t.Fatalf("update: %v", err)
	}
	if _, err := controller.Finish(boardArchiveCtx, FinishRequest{Reason: "已交付"}); err != nil {
		t.Fatalf("finish: %v", err)
	}

	// 冷读：全新实例 + 全新适配器（模拟进程重启后重新打开同一会话目录）。
	warmStore, warmBoards, warmSession := newBoardArchiveStore(t, router, "session-board-cold")
	meta := readBoardArchive(t, warmBoards)
	if len(meta.History) != 1 || meta.History[0].GoalID != "g-1" {
		t.Fatalf("冷读后收口账本应仍在: %+v", meta.History)
	}
	if meta.History[0].Title != "冷读目标" || meta.History[0].ClosedReason != "已交付" {
		t.Fatalf("账本条目应带标题与收口原因: %+v", meta.History[0])
	}
	if meta.State != sessionstore.BoardStateClosed {
		t.Fatalf("收口后冷读应看到 closed 看板: %+v", meta)
	}
	// 冷读的活栈仍是空的：收口目标不会被"复活"成活目标。
	restored := NewController(Options{Depth: 1, Store: warmStore, Audit: warmStore})
	if err := restored.Reload(boardArchiveCtx); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if status := restored.Status(); status.Active != nil || len(status.Stack) != 0 {
		t.Fatalf("收口目标不得回到活栈: %+v", status)
	}
	if persisted := warmSession.GoalStackSnapshot(); len(persisted) != 0 {
		t.Fatalf("活栈通道也应为空: %+v", persisted)
	}
}

// failingBoardRepository 是写必失败的存档面（验证"写失败怎么算"的显式口径）。
// 它实现 sessionstore.SessionBoards（Key 已烘焙）：读侧按"没有存档"降级。
type failingBoardRepository struct {
	writes int
}

func (r *failingBoardRepository) WriteGoalBoard(context.Context, sessionstore.GoalBoardMeta) error {
	r.writes++
	return errors.New("测试用：存档只读")
}

func (r *failingBoardRepository) ReadGoalBoard(context.Context) (sessionstore.GoalBoardMeta, error) {
	return sessionstore.GoalBoardMeta{}, fs.ErrNotExist
}

func (r *failingBoardRepository) WriteTeamBoard(context.Context, sessionstore.TeamBoardMeta) error {
	return errors.New("测试用：存档只读")
}

func (r *failingBoardRepository) ReadTeamBoard(context.Context) (sessionstore.TeamBoardMeta, error) {
	return sessionstore.TeamBoardMeta{}, fs.ErrNotExist
}

// TestGoalBoardArchiveWriteFailurePolicy 钉住写失败的**显式决定**（§7）：
//   - active 方向尽力而为：存档写不进去不阻断 goal 变更（活栈通道才是权威）；
//   - close 方向必须上报：写不进去就不能当关闭——否则重启后看板会把一个已经
//     收口的目标复活成活目标（假的活目标比没有看板更糟）；
//   - 两个方向都不改变**栈的保存语义**：栈照常落盘（这里 close 后活栈为空）。
func TestGoalBoardArchiveWriteFailurePolicy(t *testing.T) {
	router := newBoardArchiveRouter(t)
	session := sessionstore.NewSessionContextStore(router, "session-board-failure")
	if err := session.Load(boardArchiveCtx); err != nil {
		t.Fatalf("load session context: %v", err)
	}
	boards := &failingBoardRepository{}
	store := NewContextStateStore(session, boards)
	controller := NewController(Options{Depth: 1, Store: store, Audit: store})

	if _, err := controller.Begin(boardArchiveCtx, BeginRequest{Title: "存档写不进去"}); err != nil {
		t.Fatalf("active 方向的存档失败不应阻断 goal 变更: %v", err)
	}
	if boards.writes == 0 {
		t.Fatal("写侧应当尝试过写存档（不是静默跳过）")
	}
	if persisted := session.GoalStackSnapshot(); len(persisted) != 1 {
		t.Fatalf("栈必须照常落盘（存档失败不改栈的保存语义）: %+v", persisted)
	}

	if _, err := controller.Finish(boardArchiveCtx, FinishRequest{Reason: "收口"}); err == nil {
		t.Fatal("close 写不进去必须上报：不能静默把这次关闭当已存档")
	}
	if persisted := session.GoalStackSnapshot(); len(persisted) != 0 {
		t.Fatalf("栈仍按域状态弹空（存档失败不回滚栈）: %+v", persisted)
	}
}

// stubBoardRepository 是一份"内容可以是任意的、写进去就丢"的存档面：用来验证
// **存档内容不参与任何判定**（I1）——读侧把存档当兜底数据，而不是当事实。
type stubBoardRepository struct {
	meta sessionstore.GoalBoardMeta
}

func (r *stubBoardRepository) WriteGoalBoard(context.Context, sessionstore.GoalBoardMeta) error {
	return nil // 写进去就丢：本用例只看领域侧的结果受不受存档影响。
}

func (r *stubBoardRepository) ReadGoalBoard(context.Context) (sessionstore.GoalBoardMeta, error) {
	return r.meta, nil
}

func (r *stubBoardRepository) WriteTeamBoard(context.Context, sessionstore.TeamBoardMeta) error {
	return nil
}

func (r *stubBoardRepository) ReadTeamBoard(context.Context) (sessionstore.TeamBoardMeta, error) {
	return sessionstore.TeamBoardMeta{}, fs.ErrNotExist
}

// TestGoalDomainDecisionsIgnoreBoardArchive 钉住 I1：**领域判定不吃存档**。
//
// 存档里放一份与活体完全不符的内容（另一份 active 帧 + 别人的收口账本），活体侧
// 的 goal 变更与状态必须一个字都不变——"JSON 与活体不一致时以 JSON 为准"是本设计
// 明令禁止的读法（口径 §1）。存档只是下游扇出：写它、读它合并账本，都不回灌
// Controller，也不影响 Status/栈/审计。
func TestGoalDomainDecisionsIgnoreBoardArchive(t *testing.T) {
	router := newBoardArchiveRouter(t)
	session := sessionstore.NewSessionContextStore(router, "session-board-hostile")
	if err := session.Load(boardArchiveCtx); err != nil {
		t.Fatalf("load session context: %v", err)
	}
	boards := &stubBoardRepository{meta: sessionstore.GoalBoardMeta{
		BoardLifecycle: sessionstore.BoardLifecycle{
			Kind: sessionstore.BoardKindGoal, State: sessionstore.BoardStateActive, Seq: 9,
		},
		Active: &sessionstore.GoalBoardActive{GoalID: "g-99", Title: "存档里冒充的当前目标", Status: "active"},
		History: []sessionstore.GoalBoardHistory{
			{GoalID: "g-98", Title: "存档里别人的收口", Status: "completed", ClosedReason: "goal.finish", ClosedAt: 1},
		},
	}}
	store := NewContextStateStore(session, boards)
	controller := NewController(Options{Depth: 2, Store: store, Audit: store})

	began, err := controller.Begin(boardArchiveCtx, BeginRequest{Title: "真实目标"})
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if began.ID != "g-1" || began.Title != "真实目标" {
		t.Fatalf("goal 编号/标题应由 Controller 的会话计数器给出，不受存档影响: %+v", began)
	}
	if _, err := controller.Update(boardArchiveCtx, UpdateRequest{
		ProgressKind: ProgressMilestone, ProgressContent: "真实打点",
	}); err != nil {
		t.Fatalf("update: %v", err)
	}
	status := controller.Status()
	if status.Active == nil || status.Active.ID != "g-1" {
		t.Fatalf("当前帧必须是活体的那一帧（不得被存档里的 g-99 冒充）: %+v", status.Active)
	}
	if len(status.Stack) != 1 {
		t.Fatalf("活动栈只由活栈通道给出: %+v", status.Stack)
	}
	if len(status.Active.Progress) != 1 || status.Active.Progress[0].Content != "真实打点" {
		t.Fatalf("打点流水不得混入存档内容: %+v", status.Active.Progress)
	}
	// 审计账本同理：存档里的 g-98 不得出现在会话的 goal 审计里。
	for _, entry := range session.GoalAuditSnapshot() {
		if entry.GoalID == "g-98" || entry.GoalID == "g-99" {
			t.Fatalf("存档内容不得混进 goal 审计账本: %+v", entry)
		}
	}
}

// TestGoalBoardArchiveThrottlesUnchangedPayload 验证**指纹节流**：载荷内容
// （去掉 seq/updated_at 后）不变就不写盘——seq/updated_at 都不前进；内容真的变了
// 才写。
func TestGoalBoardArchiveThrottlesUnchangedPayload(t *testing.T) {
	router := newBoardArchiveRouter(t)
	store, boards, _ := newBoardArchiveStore(t, router, "session-board-throttle")
	controller := NewController(Options{Depth: 2, Store: store, Audit: store})
	if _, err := controller.Begin(boardArchiveCtx, BeginRequest{Title: "节流目标"}); err != nil {
		t.Fatalf("begin: %v", err)
	}
	before := readBoardArchive(t, boards)

	// 同一份栈投影再存一次：载荷与上次完全相同 → 不写盘。
	if err := store.Save(boardArchiveCtx, controller.Status().Stack); err != nil {
		t.Fatalf("repeat save: %v", err)
	}
	after := readBoardArchive(t, boards)
	if after.Seq != before.Seq {
		t.Fatalf("内容不变不应写盘（seq 前进了）: %d → %d", before.Seq, after.Seq)
	}
	if after.UpdatedAt != before.UpdatedAt {
		t.Fatalf("内容不变不应写盘（updated_at 前进了）: %v → %v", before.UpdatedAt, after.UpdatedAt)
	}
	if after.Fingerprint != before.Fingerprint || after.Fingerprint == "" {
		t.Fatalf("指纹应稳定且非空: %q → %q", before.Fingerprint, after.Fingerprint)
	}

	// 内容真变了（追加一条打点）→ 必须写盘，seq 前进。
	if _, err := controller.Update(boardArchiveCtx, UpdateRequest{
		ProgressKind: ProgressRisk, ProgressContent: "还有一处未覆盖",
	}); err != nil {
		t.Fatalf("update: %v", err)
	}
	changed := readBoardArchive(t, boards)
	if changed.Seq != before.Seq+1 {
		t.Fatalf("内容变化应写盘并让 seq 前进: %d → %d", before.Seq, changed.Seq)
	}
	if changed.Active == nil || len(changed.Active.Progress) != 1 {
		t.Fatalf("新载荷应带上追加的打点: %+v", changed.Active)
	}
}
