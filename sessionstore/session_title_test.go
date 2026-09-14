package sessionstore

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestSessionTitleDurableThroughMessageHead：会话标题持久化在 message head 的
// 目录枚举面（head.Meta.Summary）——写穿后可 header-only 读回（不打开消息
// 分片）、目录枚举行自带标题、record 派生面也带标题。这是"目录刷新不再为
// 标题读正文"的存储层前提。
func TestSessionTitleDurableThroughMessageHead(t *testing.T) {
	root := t.TempDir()
	router, err := NewRouter(filepath.Join(root, "session-storage.json"), root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = router.Close() })

	const projectID = "project-title"
	const sessionID = "sess-title"
	const title = "第二轮会话为什么报错"

	// 会话真实存在（一条消息行），否则没有 head 可写。
	if err := router.SaveCommitWorkspace(projectID, sessionID, Commit{
		Events: []Event{{Seq: 1, Role: "user", Content: "第一轮问题"}},
	}); err != nil {
		t.Fatalf("commit: %v", err)
	}

	store := NewSessionGranularStore(router)
	if err := store.SaveSessionTitle(projectID, sessionID, title); err != nil {
		t.Fatalf("save title: %v", err)
	}

	// 1) header-only 读回。
	got, ok, err := store.SessionTitle(projectID, sessionID)
	if err != nil || !ok || got != title {
		t.Fatalf("SessionTitle = %q ok=%v err=%v, want %q", got, ok, err, title)
	}

	// 2) 目录枚举行自带标题（枚举只读 head）。
	infos, err := store.SessionsOf(projectID)
	if err != nil || len(infos) != 1 {
		t.Fatalf("SessionsOf = %+v err=%v", infos, err)
	}
	if infos[0].Title != title {
		t.Fatalf("catalog row title = %q, want %q", infos[0].Title, title)
	}

	// 3) record 派生面（应用读 record）也带标题。
	record, ok, err := store.LoadSession(projectID, sessionID)
	if err != nil || !ok || record.Title != title {
		t.Fatalf("derived record = %+v ok=%v err=%v, want title %q", record, ok, err, title)
	}

	// 4) 空标题不落盘（草稿清理路径不得造会话目录）。
	if err := store.SaveSessionTitle(projectID, "sess-draft", ""); err != nil {
		t.Fatalf("save empty title: %v", err)
	}
	if _, ok, err := store.SessionTitle(projectID, "sess-draft"); err != nil || ok {
		t.Fatalf("empty title created a session head: ok=%v err=%v", ok, err)
	}
	// 5) 还没有落盘布局的会话不因标题写穿而出现（幽灵会话护栏）：标题会在首次
	// 提交/record 落盘时写穿。
	if err := store.SaveSessionTitle(projectID, "sess-absent", "尚未落盘的会话"); err != nil {
		t.Fatalf("save title for absent session: %v", err)
	}
	if _, ok, _ := store.SessionTitle(projectID, "sess-absent"); ok {
		t.Fatal("title write created a session head for an absent session")
	}
	infos, err = store.SessionsOf(projectID)
	if err != nil {
		t.Fatalf("SessionsOf: %v", err)
	}
	if len(infos) != 1 || infos[0].ID != sessionID {
		t.Fatalf("catalog gained a ghost session: %+v", infos)
	}
}

// TestSessionTitleSurvivesMessageHeadRebuild：message head 自愈重建（分片
// 可重建 head，但标题只在 head 里）必须带回标题，否则一次修复就抹掉会话
// 标题，目录刷新退回读正文。
func TestSessionTitleSurvivesMessageHeadRebuild(t *testing.T) {
	root := t.TempDir()
	router, err := NewRouter(filepath.Join(root, "session-storage.json"), root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = router.Close() })

	const projectID = "project-title"
	const sessionID = "sess-title"
	const title = "重建后仍在的标题"

	if err := router.SaveCommitWorkspace(projectID, sessionID, Commit{
		Events: []Event{{Seq: 1, Role: "user", Content: "第一轮问题"}},
	}); err != nil {
		t.Fatalf("commit: %v", err)
	}
	store := NewSessionGranularStore(router)
	if err := store.SaveSessionTitle(projectID, sessionID, title); err != nil {
		t.Fatalf("save title: %v", err)
	}

	// 让 head 校验失败（checksum 破坏，但 payload 仍可解码）：读路径按
	// §2.0 规则 3 从分片重建 head。
	matches, err := filepath.Glob(filepath.Join(root, "*", "project-*", "session-*", "metadata", "message.json"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("locate message head: %v matches=%v", err, matches)
	}
	headPath := matches[0]
	raw, err := os.ReadFile(headPath)
	if err != nil {
		t.Fatalf("read head: %v", err)
	}
	var envelope moduleHeadFile
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("decode head: %v", err)
	}
	envelope.Checksum = "0000000000000000000000000000000000000000000000000000000000000000"
	corrupted, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("encode head: %v", err)
	}
	if err := os.WriteFile(headPath, corrupted, 0o600); err != nil {
		t.Fatalf("write head: %v", err)
	}

	got, ok, err := store.SessionTitle(projectID, sessionID)
	if err != nil || !ok || got != title {
		t.Fatalf("title after head rebuild = %q ok=%v err=%v, want %q", got, ok, err, title)
	}
}
