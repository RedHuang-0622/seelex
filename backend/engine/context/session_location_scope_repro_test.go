package context_runtime

import (
	"path/filepath"
	"testing"

	"github.com/RedHuang-0622/seelex/backend/engine/session"
	"github.com/RedHuang-0622/seelex/kernel/state"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
	"github.com/RedHuang-0622/seelex/store"
)

// ── 错误复现：压缩 checkpoint 的落点解析（sessionLocationLocked）───────────────
//
// 现象（本机真实存储，2026-10-14）：会话 seelex-1791954143891366400-2 同时存在于
// project-03b5b8d29af3f44b（event/goal/teamwork）与 project-e3b0c44298fc1c14
// （= sha256("")，默认项目：整份 972 行 message 在一个新建目录里一次性写出，而首行
// 消息 created_at 比该目录早两个多小时）。目录枚举按项目分格 ⇒ 前端把它显示成两个
// 会话，而**没有任何 fork 指令**。
//
// 触发时刻是压缩之后这一跳：
//
//	compactTaskContextFor（coordinator.go）
//	  → persistCompactionCheckpoint
//	    → c.sessions.PersistCurrentSession(location, sessionID)
//	      → SessionSnapshotPort.SaveSessionSnapshotWorkspace：整份事件流 + record
//	        按 location.WorkspaceID **整键替换**
//
// 也就是"整份 transcript 写进哪个项目目录"完全由这个 location 决定。旧实现的
// 定序是：
//
//	Snapshot.SessionWorkspaces[sessionID]（绑定投影）
//	  → Snapshot.CurrentWorkspace（**当前视图工作区**）
//	    → ""（默认项目）
//
// 后两个分支就是"按活跃作用域猜会话归属"：后台会话（或绑定投影这一格还没刷新的
// 会话，现场 workspace_index.json 的 bindings 里根本没有这个会话）猜错一次，这个
// 会话 ID 就在第二个项目目录里长出完整 message head。
//
// 修复后的定序（本用例钉住的契约）：
//
//	绑定投影 → SessionLocator.LocateSessionResolved（会话域：绑定 + 数据实际所在）
//	  → 会话域明确说"找不到" ⇒ **不落盘**（绝不回退活跃视图作用域）
func TestCompactionCheckpointPersistMustNotLandInViewWorkspace(t *testing.T) {
	const ownerProject = "ws-owner" // 会话真正归属的项目
	const viewProject = "ws-view"   // 视图当前项目（活跃写作用域）
	const sessionID = "sess-compaction-scope"

	root := t.TempDir()
	router, err := store.NewRouter(filepath.Join(root, "session-storage.json"), root)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	t.Cleanup(func() { _ = router.Close() })

	seed := store.Commit{Events: []store.Event{{Seq: 1, Role: "user", Content: "owner-side"}}}
	if err := router.SaveCommitWorkspace(ownerProject, sessionID, seed); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	// 视图切到另一个项目；绑定投影里**没有**这个会话（后台会话 / 投影未刷新——现场
	// 正是这样：workspace_index.json 的 bindings 里没有 seelex-1791954143891366400-2）。
	viewSnapshot := model.Snapshot{
		Session:           model.SessionState{ID: "sess-view"},
		CurrentWorkspace:  &model.WorkspaceInfo{ID: viewProject, Name: "seelex", RootPath: `G:\view`},
		SessionWorkspaces: map[string]string{},
	}

	// ① 会话域明确回答"找不到这个会话"：压缩 checkpoint 必须**放弃落盘**——旧实现
	//    会在这一刻按 Snapshot.CurrentWorkspace 把整份快照写进视图项目。
	unresolvedPort := &recordingSessionPort{router: router, seed: seed}
	unresolved := NewCoordinator(Deps{Core: &state.Core{Snapshot: viewSnapshot}, Sessions: unresolvedPort})
	if location, kind := unresolved.sessionLocationLocked(sessionID); kind != sessionLocationUnresolved {
		t.Fatalf("会话域找不到它时结论应为 unresolved，实际 %v（落点 %q）——这正是键漂移的入口。",
			kind, location.WorkspaceID)
	}
	if err := unresolved.persistCompactionCheckpoint(sessionID); err != nil {
		t.Fatalf("归属不明的 checkpoint 落盘应安静跳过，实际报错：%v", err)
	}
	if wrote := unresolvedPort.locations(); len(wrote) != 0 {
		t.Fatalf("归属不明却落了盘（%v）：落到视图/默认项目会让同一会话长出第二个项目目录。", wrote)
	}
	if listed(router, viewProject, sessionID) {
		t.Fatalf("压缩 checkpoint 把会话 %q 写进了视图项目 %q（旧实现回退 Snapshot.CurrentWorkspace 的现场）。", sessionID, viewProject)
	}
	if listed(router, "", sessionID) {
		t.Fatalf("压缩 checkpoint 把会话 %q 写进了默认项目 \"\"（未关联会话）：同一会话在两个项目目录各留一份。", sessionID)
	}

	// ② 绑定投影有它：按绑定落盘，即使视图在别的项目。
	boundPort := &recordingSessionPort{router: router, seed: seed}
	bound := NewCoordinator(Deps{Core: &state.Core{Snapshot: model.Snapshot{
		Session:           model.SessionState{ID: "sess-view"},
		CurrentWorkspace:  &model.WorkspaceInfo{ID: viewProject, Name: "seelex", RootPath: `G:\view`},
		SessionWorkspaces: map[string]string{sessionID: ownerProject},
	}}, Sessions: boundPort})
	if err := bound.persistCompactionCheckpoint(sessionID); err != nil {
		t.Fatalf("persist checkpoint: %v", err)
	}
	if got := boundPort.locations(); len(got) != 1 || got[0] != ownerProject {
		t.Fatalf("绑定投影优先的落点 = %v，want [%s]", got, ownerProject)
	}

	// ③ 绑定投影缺条目、会话域解析得到归属项目（现场那条路径）：按归属落盘，
	//    且**只**在归属项目里出现。
	locatingPort := &recordingSessionPort{
		router: router, seed: seed, locateProject: ownerProject, locateResolved: true,
	}
	locating := NewCoordinator(Deps{Core: &state.Core{Snapshot: viewSnapshot}, Sessions: locatingPort})
	location, kind := locating.sessionLocationLocked(sessionID)
	if kind != sessionLocationResolved || location.WorkspaceID != ownerProject {
		t.Fatalf("会话域解析结果 = (%q, %v)，want (%q, resolved)", location.WorkspaceID, kind, ownerProject)
	}
	if err := locating.persistCompactionCheckpoint(sessionID); err != nil {
		t.Fatalf("persist checkpoint: %v", err)
	}
	if listed(router, viewProject, sessionID) {
		t.Fatalf("压缩 checkpoint 把会话 %q 写进了视图项目 %q：同一会话在两个项目目录各留一份，"+
			"目录枚举按项目分格 ⇒ 前端把它显示成两个会话（没有 fork 指令）。", sessionID, viewProject)
	}
	if listed(router, "", sessionID) {
		t.Fatalf("压缩 checkpoint 把会话 %q 写进了默认项目 \"\"（未关联会话）。", sessionID)
	}
	if !listed(router, ownerProject, sessionID) {
		t.Fatalf("会话 %q 在它自己的项目 %q 里读不到（数据被写去了别处）。", sessionID, ownerProject)
	}

	// ④ 没有会话域解析面（测试替身 / 会话域未装配）：退回"未关联会话"（默认项目），
	//    仍然绝不碰视图项目。生产装配总是带解析面（session_runtime.Coordinator 天然
	//    实现 SessionLocator），这条只为替身与降级装配保留，且与 SessionContextStore
	//    的兜底口径一致（无绑定时按默认项目落盘，而不是按视图猜）。
	const unboundSessionID = "sess-compaction-unbound"
	plainPort := &plainSessionPort{router: router, seed: seed}
	plain := NewCoordinator(Deps{Core: &state.Core{Snapshot: viewSnapshot}, Sessions: plainPort})
	location, kind = plain.sessionLocationLocked(unboundSessionID)
	if kind != sessionLocationFallback || location.WorkspaceID != "" {
		t.Fatalf("无解析面时应退回默认项目（未关联会话），实际 (%q, %v)", location.WorkspaceID, kind)
	}
	if err := plain.persistCompactionCheckpoint(unboundSessionID); err != nil {
		t.Fatalf("persist checkpoint: %v", err)
	}
	if got := plainPort.locations(); len(got) != 1 || got[0] != "" {
		t.Fatalf("无解析面时的落点 = %v，want [\"\"]（未关联会话）", got)
	}
	if listed(router, viewProject, unboundSessionID) {
		t.Fatalf("无解析面时 checkpoint 写进了视图项目 %q（活跃写作用域永远不是会话归属）。", viewProject)
	}
}

// plainSessionPort 是**没有**归属解析面（不实现 SessionLocator）的会话端口替身。
type plainSessionPort struct {
	router *store.Router
	seed   store.Commit
	wrote  []string
}

func (port *plainSessionPort) PersistCurrentSession(location session_runtime.Location, sessionID string) error {
	port.wrote = append(port.wrote, location.WorkspaceID)
	return port.router.SaveCommitWorkspace(location.WorkspaceID, sessionID, port.seed)
}

func (port *plainSessionPort) locations() []string {
	return append([]string(nil), port.wrote...)
}

// recordingSessionPort 是 context 域会话端口的替身：实现落盘（等价于
// PersistCurrentSession → SaveSessionSnapshotWorkspace 的整键替换）+ 会话归属解析
// （生产实现是 session_runtime.Coordinator.LocateSessionResolved）。
type recordingSessionPort struct {
	router         *store.Router
	seed           store.Commit
	locateProject  string
	locateResolved bool
	wrote          []string
}

func (port *recordingSessionPort) PersistCurrentSession(location session_runtime.Location, sessionID string) error {
	port.wrote = append(port.wrote, location.WorkspaceID)
	return port.router.SaveCommitWorkspace(location.WorkspaceID, sessionID, port.seed)
}

func (port *recordingSessionPort) LocateSessionResolved(sessionID string) (session_runtime.Location, bool) {
	if !port.locateResolved {
		return session_runtime.Location{Meta: model.SessionInfo{ID: sessionID}}, false
	}
	return session_runtime.Location{
		WorkspaceID: port.locateProject,
		Meta:        model.SessionInfo{ID: sessionID},
	}, true
}

func (port *recordingSessionPort) locations() []string {
	return append([]string(nil), port.wrote...)
}

// listed 报告项目目录枚举（左侧栏的数据源口径）里是否列出了该会话。
func listed(router *store.Router, projectID, sessionID string) bool {
	for _, meta := range router.ListWorkspace(projectID) {
		if meta.SessionID == sessionID {
			return true
		}
	}
	return false
}
