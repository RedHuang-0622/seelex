package adapters

import (
	"path/filepath"
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/application/model"
	"github.com/RedHuang-0622/seelex/session"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// TestSessionPermissionTierRoundTrip：会话级权限档位设置经适配层落到真实存储
// （项目级会话元数据 blob），并且**与展示元数据互不覆盖**——"取消置顶/清别名"不得
// 顺手抹掉档位，改档位也不得动别名/排序位。这条链是"档位跨重启记住"的物理面。
func TestSessionPermissionTierRoundTrip(t *testing.T) {
	root := t.TempDir()
	router, err := sessionstore.NewRouter(filepath.Join(root, "session-storage.json"), root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = router.Close() })

	const projectID = "project-setting"
	const sessionID = "sess-setting"
	const otherSessionID = "sess-setting-other"
	for _, id := range []string{sessionID, otherSessionID} {
		if err := router.SaveCommitWorkspace(projectID, id, sessionstore.Commit{
			Events: []sessionstore.Event{{Seq: 1, Role: "user", Content: "seed"}},
		}); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}

	port := SessionPort{
		Manager: session.NewManager().WithRouter(router),
		Meta:    sessionstore.NewSessionMetaStore(router),
	}
	port.SetWorkspaceResolver(func(string) string { return projectID })

	// 写档位 → 读回（同一会话键）。
	if err := port.SetSessionPermissionTier(sessionID, dto.PermissionTierFull); err != nil {
		t.Fatalf("SetSessionPermissionTier: %v", err)
	}
	if tier, err := port.SessionPermissionTier(sessionID); err != nil || tier != dto.PermissionTierFull {
		t.Fatalf("SessionPermissionTier = %q err=%v, want full", tier, err)
	}
	// 未选择的会话必须为空（不串会话）。
	if tier, err := port.SessionPermissionTier(otherSessionID); err != nil || tier != "" {
		t.Fatalf("other session tier = %q err=%v, want empty（不得串会话）", tier, err)
	}

	// 写入展示元数据不得抹掉档位（两个写面共用一份 blob，但字段互不覆盖）。
	if err := port.SetSessionMeta(sessionID, model.SessionMeta{Pinned: true, Alias: "线上排查", SortOrder: 3}); err != nil {
		t.Fatalf("SetSessionMeta: %v", err)
	}
	if tier, err := port.SessionPermissionTier(sessionID); err != nil || tier != dto.PermissionTierFull {
		t.Fatalf("展示元数据写入后档位 = %q err=%v, want full", tier, err)
	}

	// 清展示元数据（零值）同样不得抹掉档位，且展示字段确实复位。
	if err := port.SetSessionMeta(sessionID, model.SessionMeta{}); err != nil {
		t.Fatalf("clear display meta: %v", err)
	}
	if meta, err := port.SessionMeta(sessionID); err != nil || meta.Pinned || meta.Alias != "" || meta.SortOrder != 0 {
		t.Fatalf("display meta after clear = %+v err=%v, want zero", meta, err)
	}
	if tier, err := port.SessionPermissionTier(sessionID); err != nil || tier != dto.PermissionTierFull {
		t.Fatalf("清展示元数据后档位 = %q err=%v, want full", tier, err)
	}

	// 清档位：只剩档位字段时条目整条消失（不留空记录）。
	if err := port.SetSessionPermissionTier(sessionID, ""); err != nil {
		t.Fatalf("clear tier: %v", err)
	}
	if tier, err := port.SessionPermissionTier(sessionID); err != nil || tier != "" {
		t.Fatalf("cleared tier = %q err=%v, want empty", tier, err)
	}
	metas, err := sessionstore.NewSessionMetaStore(router).Meta(projectID)
	if err != nil {
		t.Fatalf("meta read: %v", err)
	}
	if _, ok := metas[sessionID]; ok {
		t.Fatalf("clear 后仍留空条目: %+v", metas[sessionID])
	}
}
