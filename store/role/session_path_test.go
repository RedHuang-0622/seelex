package role

import (
	"encoding/json"
	"path/filepath"
	"testing"

	protocol "github.com/RedHuang-0622/seelex/contract/protocol"
	storeroot "github.com/RedHuang-0622/seelex/store/root"
	storesession "github.com/RedHuang-0622/seelex/store/session"
)

// TestSessionRootMainReusesMainRoot 钉住"main 角色就是主会话本身"：不建第二份子树。
func TestSessionRootMainReusesMainRoot(t *testing.T) {
	mainRoot := filepath.Join(t.TempDir(), "sessions", "main")
	if got := SessionRoot(mainRoot, RoleMain, "role-session-main"); got != mainRoot {
		t.Fatalf("SessionRoot(main) = %q, want %q（main 恒等于主会话根）", got, mainRoot)
	}
}

// TestSessionRootPrefixByRole 钉住目录名前缀：tl → goal_<hash>，其它 → role_<hash>。
func TestSessionRootPrefixByRole(t *testing.T) {
	mainRoot := filepath.Join(t.TempDir(), "sessions", "main")
	cases := []struct {
		roleName      string
		roleSessionID string
		wantPrefix    string
	}{
		{roleName: RoleTL, roleSessionID: "role-session-tl", wantPrefix: "goal_"},
		{roleName: "reviewer", roleSessionID: "role-session-reviewer", wantPrefix: "role_"},
		{roleName: "goalplan", roleSessionID: "role-session-goalplan", wantPrefix: "role_"},
	}
	for _, entry := range cases {
		entry := entry
		t.Run(entry.roleName, func(t *testing.T) {
			got := SessionRoot(mainRoot, entry.roleName, entry.roleSessionID)
			if filepath.Dir(got) != mainRoot {
				t.Fatalf("角色子树必须直接挂在主会话根下：got %q, mainRoot %q", got, mainRoot)
			}
			want := filepath.Join(mainRoot, entry.wantPrefix+storeroot.Hash(entry.roleSessionID))
			if got != want {
				t.Fatalf("SessionRoot = %q, want %q", got, want)
			}
			if filepath.Base(got) != entry.wantPrefix+storeroot.Hash(entry.roleSessionID) {
				t.Fatalf("目录名 = %q, want %q+hash", filepath.Base(got), entry.wantPrefix)
			}
		})
	}
}

// TestSessionRootHashSuffixMatchesRootHash 钉住 hash 判据同源：不得在本包复制一份哈希。
func TestSessionRootHashSuffixMatchesRootHash(t *testing.T) {
	mainRoot := "C:/sessions/main"
	roleSessionID := "role-session-abc"
	got := SessionRoot(mainRoot, "reviewer", roleSessionID)
	want := "role_" + storeroot.Hash(roleSessionID)
	if filepath.Base(got) != want {
		t.Fatalf("hash 后缀 = %q, want %q", filepath.Base(got), want)
	}
}

// TestSessionCommitIDIsStable 钉住角色会话创建身份的字符串形态（旧盘上已存在）。
func TestSessionCommitIDIsStable(t *testing.T) {
	first := SessionCommitID(RoleTL, "role-session-tl")
	second := SessionCommitID(RoleTL, "role-session-tl")
	if first != second {
		t.Fatalf("SessionCommitID 不稳定：%q != %q", first, second)
	}
	want := "role-session-tl-" + storeroot.Hash("role-session-tl")
	if first != want {
		t.Fatalf("SessionCommitID = %q, want %q", first, want)
	}
	if other := SessionCommitID(RoleTL, "role-session-other"); other == first {
		t.Fatalf("不同 roleSessionID 必须得到不同提交身份：%q", other)
	}
}

// TestBackupCommitIDFollowsRows 钉住备份身份：同一批行稳定、内容变了就变。
func TestBackupCommitIDFollowsRows(t *testing.T) {
	rows := []protocol.TranscriptEvent{
		{Seq: 1, Role: "assistant", Content: "hello", MessageID: "m1"},
		{Seq: 2, Role: "tool", ToolCallID: "c1", Name: "bash", Content: "ok"},
	}
	first := BackupCommitID("reviewer", "role-session-r", rows)
	if second := BackupCommitID("reviewer", "role-session-r", rows); second != first {
		t.Fatalf("BackupCommitID 不稳定：%q != %q", first, second)
	}
	want := "role-backup-reviewer-" + storeroot.Hash("role-session-r|"+storesession.MessageRowsCommitID(rows))
	if first != want {
		t.Fatalf("BackupCommitID = %q, want %q", first, want)
	}
	changed := []protocol.TranscriptEvent{
		{Seq: 1, Role: "assistant", Content: "hello!", MessageID: "m1"},
		{Seq: 2, Role: "tool", ToolCallID: "c1", Name: "bash", Content: "ok"},
	}
	if BackupCommitID("reviewer", "role-session-r", changed) == first {
		t.Fatal("行内容变化后 BackupCommitID 必须变化")
	}
	if BackupCommitID("reviewer", "role-session-other", rows) == first {
		t.Fatal("roleSessionID 变化后 BackupCommitID 必须变化")
	}
	if BackupCommitID("other", "role-session-r", rows) == first {
		t.Fatal("roleName 变化后 BackupCommitID 必须变化")
	}
}

// TestJoinPayloadKeepsFieldNames 钉住 role_session 结构化事件的负载 wire（四个键逐字保持）。
func TestJoinPayloadKeepsFieldNames(t *testing.T) {
	payload := JoinPayload("main-session", RoleTL, "role-session-tl", 7)
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("JoinPayload 必须是合法 JSON：%v", err)
	}
	want := map[string]any{
		"main_session_id": "main-session",
		"role_name":       RoleTL,
		"role_session_id": "role-session-tl",
		"join_seq_id":     float64(7),
	}
	if len(decoded) != len(want) {
		t.Fatalf("负载键集合 = %v, want %v", decoded, want)
	}
	for key, expected := range want {
		got, ok := decoded[key]
		if !ok {
			t.Fatalf("负载缺键 %q：%v", key, decoded)
		}
		if got != expected {
			t.Fatalf("负载 %q = %#v, want %#v", key, got, expected)
		}
	}
	// join_seq_id 必须回环成数值（不是字符串），下游按 uint64 解析。
	if _, ok := decoded["join_seq_id"].(float64); !ok {
		t.Fatalf("join_seq_id 必须是数值，实际 %T", decoded["join_seq_id"])
	}
}
