package sessionstore

import (
	"path/filepath"
	"strings"
	"testing"
)

// 未发送草稿（composer draft）的持久化判据（S20/§2.5.4）：
//
//   - 草稿正文落在 sessionstore 的 lifecycle 草稿通道（会话目录下
//     input/draft.json），与消息/事件通道分离——草稿编辑不写消息日志；
//   - 目录枚举的 Status=draft 判据就是这份草稿文件的存在性：写了草稿的行才是
//     草稿候选（跨重启恢复的数据源），清掉草稿后行必须回到 idle（否则留幽灵行）；
//   - record 通道在 v8 已退役（正文被丢弃），因此草稿正文只能靠这条通道回来。
func TestComposerDraftChannelDrivesDirectoryStatus(t *testing.T) {
	router := newTestRouter(t)
	store := NewSessionGranularStore(router)
	const projectID = "project-draft"
	const sessionID = "draft-1"

	// 会话先以一条 commit 出现在项目索引里（草稿会话的前身：早分配 SID + 空索引）。
	if err := router.SaveCommitWorkspace(projectID, sessionID, Commit{}); err != nil {
		t.Fatalf("save commit: %v", err)
	}

	// 未发送草稿：正文进草稿通道，目录行标 draft。
	if err := store.SaveComposerDraft(projectID, sessionID, "未发送的草稿正文"); err != nil {
		t.Fatalf("save composer draft: %v", err)
	}
	text, found, err := store.LoadComposerDraft(projectID, sessionID)
	if err != nil || !found || text != "未发送的草稿正文" {
		t.Fatalf("load composer draft = %q found=%v err=%v", text, found, err)
	}
	row, ok := granularRow(t, store, projectID, sessionID)
	if !ok {
		t.Fatalf("draft session %q missing from SessionsOf(%q)", sessionID, projectID)
	}
	if row.Status != StatusDraft {
		t.Fatalf("directory status with a draft = %q, want draft", row.Status)
	}

	// record 通道退役：正文不进 record（v8 派生 record 里没有 composer 字段）。
	payload, err := store.LoadRecordRaw(projectID, sessionID)
	if err != nil {
		t.Fatalf("load record: %v", err)
	}
	if strings.Contains(string(payload), "未发送的草稿正文") {
		t.Fatalf("draft text leaked into the retired record channel: %s", payload)
	}

	// 清草稿：文件被删除 → 目录行回到 idle（不留幽灵草稿行）。
	if err := store.SaveComposerDraft(projectID, sessionID, ""); err != nil {
		t.Fatalf("clear composer draft: %v", err)
	}
	if text, found, err := store.LoadComposerDraft(projectID, sessionID); err != nil || found || text != "" {
		t.Fatalf("cleared composer draft = %q found=%v err=%v", text, found, err)
	}
	row, ok = granularRow(t, store, projectID, sessionID)
	if !ok {
		t.Fatalf("session %q missing from SessionsOf(%q) after clearing the draft", sessionID, projectID)
	}
	if row.Status == StatusDraft {
		t.Fatalf("directory status after clearing the draft = %q, want not draft", row.Status)
	}
}

// TestComposerDraftSurvivesRouterReopen：草稿正文跨"重新打开存储"存活（重启恢复
// 的前提），且路由层面读回同一份正文。
func TestComposerDraftSurvivesRouterReopen(t *testing.T) {
	root := t.TempDir()
	indexPath := filepath.Join(root, "session-storage.json")
	const projectID = "project-draft"
	const sessionID = "draft-2"

	first, err := NewRouter(indexPath, root)
	if err != nil {
		t.Fatalf("open router: %v", err)
	}
	if err := NewSessionGranularStore(first).SaveComposerDraft(projectID, sessionID, "跨重开存活的草稿"); err != nil {
		t.Fatalf("save composer draft: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close router: %v", err)
	}

	second, err := NewRouter(indexPath, root)
	if err != nil {
		t.Fatalf("reopen router: %v", err)
	}
	defer func() { _ = second.Close() }()
	text, found, err := NewSessionGranularStore(second).LoadComposerDraft(projectID, sessionID)
	if err != nil || !found || text != "跨重开存活的草稿" {
		t.Fatalf("reopened composer draft = %q found=%v err=%v", text, found, err)
	}
}

func granularRow(t *testing.T, store *SessionGranularStore, projectID, sessionID string) (SessionInfo, bool) {
	t.Helper()
	infos, err := store.SessionsOf(projectID)
	if err != nil {
		t.Fatalf("SessionsOf(%q): %v", projectID, err)
	}
	for _, info := range infos {
		if info.ID == sessionID {
			return info, true
		}
	}
	return SessionInfo{}, false
}
