package teamwork

// board_archive_test.go 是团队看板存档生命周期判据的**域级**用例（P21-24：随实现从
// `backend/engine/assemble/runtime_teamwork_board_archive.go` 与 `..._board_close.go` 搬进本域）。

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/store"
)

// stubBoardRepository 是存档仓库替身：内存一份 meta + 可注入的读错误。
type stubBoardRepository struct {
	meta    store.TeamBoardMeta
	exists  bool
	readErr error
	writes  []store.TeamBoardMeta
}

func (s *stubBoardRepository) ReadTeamBoard(context.Context, store.Key) (store.TeamBoardMeta, error) {
	if s.readErr != nil {
		return store.TeamBoardMeta{}, s.readErr
	}
	if !s.exists {
		return store.TeamBoardMeta{}, fs.ErrNotExist
	}
	return s.meta, nil
}

func (s *stubBoardRepository) WriteTeamBoard(_ context.Context, _ store.Key, meta store.TeamBoardMeta) error {
	s.meta = meta
	s.exists = true
	s.writes = append(s.writes, meta)
	return nil
}

// 目标看板那一半在本用例里不被触达，但接口是完整的（不做"只实现一半"的替身）。
func (s *stubBoardRepository) WriteGoalBoard(context.Context, store.Key, store.GoalBoardMeta) error {
	return nil
}

func (s *stubBoardRepository) ReadGoalBoard(context.Context, store.Key) (store.GoalBoardMeta, error) {
	return store.GoalBoardMeta{}, fs.ErrNotExist
}

func archivePlanFixture(version int) store.TeamworkPlan {
	return store.TeamworkPlan{TeamID: "team-1", Version: version}
}

// TestNextBoardMetaOpensNewVersionAndClosesOld：三条生命周期规则——无存档新开一版、
// 版本不同**先关旧后开新**（旧版 closed_reason=team.replan）、同版本只推进 seq 与 updated_at。
func TestNextBoardMetaOpensNewVersionAndClosesOld(t *testing.T) {
	now := time.Unix(1700000000, 0)
	payload := json.RawMessage(`{"milestones":[{"id":"m1"}]}`)

	// ① 无存档 → 新开一版 active（seq=1、opened_at=now、指纹已算）。
	fresh := &stubBoardRepository{}
	meta, write, err := NextBoardMeta(context.Background(), fresh, store.Key{}, archivePlanFixture(1), payload, now)
	if err != nil || !write {
		t.Fatalf("首次写应当 write=true：%v %v", write, err)
	}
	if meta.State != store.BoardStateActive || meta.Seq != 1 || meta.OpenedAt != now.Unix() ||
		meta.Fingerprint != BoardFingerprint(payload) || meta.Version != 1 {
		t.Fatalf("新开一版的 meta = %+v", meta)
	}
	if writeErr := fresh.WriteTeamBoard(context.Background(), store.Key{}, meta); writeErr != nil {
		t.Fatal(writeErr)
	}

	// ② 同版本 + 同载荷 → §7 节流（不写盘）。
	if _, write, err = NextBoardMeta(context.Background(), fresh, store.Key{}, archivePlanFixture(1), payload, now); err != nil || write {
		t.Fatalf("载荷不变应当节流：write=%v err=%v", write, err)
	}

	// ③ 同版本 + 载荷变了 → 续接 opened_at、只推进 seq/updated_at。
	changed := json.RawMessage(`{"milestones":[{"id":"m1"},{"id":"m2"}]}`)
	later := now.Add(time.Minute)
	oldSeq := fresh.meta.Seq
	meta, write, err = NextBoardMeta(context.Background(), fresh, store.Key{}, archivePlanFixture(1), changed, later)
	if err != nil || !write {
		t.Fatalf("同版本刷新应当写：%v %v", write, err)
	}
	if meta.Seq != oldSeq+1 || meta.OpenedAt != now.Unix() || meta.UpdatedAt != later.Unix() {
		t.Fatalf("同版本刷新必须续接 opened_at 且只推进 seq/updated_at：%+v", meta)
	}
	if writeErr := fresh.WriteTeamBoard(context.Background(), store.Key{}, meta); writeErr != nil {
		t.Fatal(writeErr)
	}

	// ④ 版本不同 → 旧版显式收口（team.replan）后再开新版（seq 回到 1）。
	meta, write, err = NextBoardMeta(context.Background(), fresh, store.Key{}, archivePlanFixture(2), payload, later)
	if err != nil || !write {
		t.Fatalf("换版本应当写：%v %v", write, err)
	}
	if meta.Version != 2 || meta.Seq != 1 {
		t.Fatalf("新版 meta = %+v", meta)
	}
	if len(fresh.writes) != 3 {
		t.Fatalf("写序应当是 新开 → 刷新 → 关旧：%d 次", len(fresh.writes))
	}
	closed := fresh.writes[2]
	if closed.State != store.BoardStateClosed || closed.ClosedReason != store.BoardCloseTeamReplan {
		t.Fatalf("旧版必须显式收口为 team.replan：%+v", closed)
	}

	// ⑤ 已退场的那一版不再续命：新开一版。
	retired := &stubBoardRepository{exists: true, meta: store.TeamBoardMeta{
		BoardLifecycle: store.BoardLifecycle{State: store.BoardStateClosed, Seq: 9},
		Version:        1,
	}}
	meta, write, err = NextBoardMeta(context.Background(), retired, store.Key{}, archivePlanFixture(1), payload, now)
	if err != nil || !write || meta.Seq != 1 || meta.State != store.BoardStateActive {
		t.Fatalf("已退场的存档应当新开一版：%+v write=%v err=%v", meta, write, err)
	}

	// ⑥ 读不出来（非 NotExist）与"没有存档"同解：按第一次写处理。
	broken := &stubBoardRepository{readErr: errors.New("io error")}
	if _, write, err = NextBoardMeta(context.Background(), broken, store.Key{}, archivePlanFixture(1), payload, now); err != nil || !write {
		t.Fatalf("存档读失败应当按第一次写处理：%v %v", write, err)
	}
}

// TestBoardFingerprintIsStable：同一份载荷 → 同一个值；不同载荷 → 不同值。
func TestBoardFingerprintIsStable(t *testing.T) {
	if BoardFingerprint([]byte("a")) != BoardFingerprint([]byte("a")) {
		t.Fatal("同一份载荷必须同指纹")
	}
	if BoardFingerprint([]byte("a")) == BoardFingerprint([]byte("b")) {
		t.Fatal("不同载荷不该同指纹")
	}
}

// TestReadBoardArchiveOnlyRestoresActive：只有 active 且快照可解、且有里程碑时才恢复；
// 恢复出来的看板必须标 Recovered=true 与 Stale=true（句柄来自上一个进程）。
func TestReadBoardArchiveOnlyRestoresActive(t *testing.T) {
	snapshot, err := json.Marshal(boardArchiveViewFixture())
	if err != nil {
		t.Fatal(err)
	}
	active := &stubBoardRepository{exists: true, meta: store.TeamBoardMeta{
		BoardLifecycle: store.BoardLifecycle{State: store.BoardStateActive},
		Snapshot:       snapshot,
	}}
	recovered := ReadBoardArchive(active, store.Key{})
	if recovered == nil || !recovered.Recovered || !recovered.Stale || len(recovered.Milestones) != 1 {
		t.Fatalf("恢复出的看板 = %+v", recovered)
	}

	cases := map[string]*stubBoardRepository{
		"已退场": {exists: true, meta: store.TeamBoardMeta{
			BoardLifecycle: store.BoardLifecycle{State: store.BoardStateClosed}, Snapshot: snapshot,
		}},
		"没有存档": {},
		"快照坏了": {exists: true, meta: store.TeamBoardMeta{
			BoardLifecycle: store.BoardLifecycle{State: store.BoardStateActive}, Snapshot: json.RawMessage(`{bad`),
		}},
		"没有编排": {exists: true, meta: store.TeamBoardMeta{
			BoardLifecycle: store.BoardLifecycle{State: store.BoardStateActive}, Snapshot: json.RawMessage(`{"team_id":"t"}`),
		}},
	}
	for name, repository := range cases {
		if got := ReadBoardArchive(repository, store.Key{}); got != nil {
			t.Fatalf("%s：不该恢复出看板，得到 %+v", name, got)
		}
	}
	if ReadBoardArchive(nil, store.Key{}) != nil {
		t.Fatal("没有存档面 = 不恢复")
	}
}

// TestSealBoardIsIdempotent：封板把在册那一版关掉（可顺带刷新快照）；已 closed / 无存档都是
// 幂等的无事路径（"没有在册的一版就没有可关的东西"，不是失败）；读失败必须上抛。
func TestSealBoardIsIdempotent(t *testing.T) {
	now := time.Unix(1700000000, 0)
	payload := json.RawMessage(`{"milestones":[{"id":"m1"}]}`)

	repository := &stubBoardRepository{exists: true, meta: store.TeamBoardMeta{
		BoardLifecycle: store.BoardLifecycle{State: store.BoardStateActive, Seq: 3},
	}}
	if err := SealBoard(context.Background(), repository, store.Key{}, payload, now); err != nil {
		t.Fatal(err)
	}
	sealed := repository.meta
	if sealed.State != store.BoardStateClosed || sealed.ClosedReason != store.BoardCloseTeamClose ||
		sealed.ClosedAt != now.Unix() || string(sealed.Snapshot) != string(payload) {
		t.Fatalf("封板结果 = %+v", sealed)
	}
	// 再封一次：幂等（不再写盘）。
	before := len(repository.writes)
	if err := SealBoard(context.Background(), repository, store.Key{}, payload, now); err != nil {
		t.Fatal(err)
	}
	if len(repository.writes) != before {
		t.Fatal("已 closed 的存档不该再写")
	}
	// 没有存档 / 没有存档面：无事路径。
	if err := SealBoard(context.Background(), &stubBoardRepository{}, store.Key{}, nil, now); err != nil {
		t.Fatalf("没有存档不该报错：%v", err)
	}
	if err := SealBoard(context.Background(), nil, store.Key{}, nil, now); err != nil {
		t.Fatalf("没有存档面不该报错：%v", err)
	}
	// 读失败（非 NotExist）必须上抛：收口方据此保留"可重试"状态。
	if err := SealBoard(context.Background(), &stubBoardRepository{readErr: errors.New("io error")}, store.Key{}, nil, now); err == nil {
		t.Fatal("读失败必须上抛")
	}
}

// boardArchiveViewFixture 造一份"有里程碑"的看板投影（存档恢复的最小形状）。
func boardArchiveViewFixture() map[string]any {
	return map[string]any{
		"team_id":    "team-1",
		"version":    1,
		"milestones": []any{map[string]any{"id": "m1", "name": "实现"}},
	}
}
