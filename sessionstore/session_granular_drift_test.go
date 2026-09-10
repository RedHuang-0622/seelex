package sessionstore

import (
	"testing"
)

// TestDefaultProjectSessionPersistDoesNotFollowActiveScope 复现「未关联
// （默认项目）会话随视图切换发生键漂移」：B 在默认项目 "" 落盘首轮后，用户
// 切到带项目会话 A（Router 活跃作用域变 project-A），B 的下一轮落盘仍以
// location.WorkspaceID="" 提交——数据必须继续落在默认项目，而不是漂移到
// project-A。否则重启后 B 的记录分裂在两个项目（A 项目里有 B、默认项目里
// 只剩旧轮），目录/加载/删除全部错键（manifest 找不到 / A、B 同收消息）。
func TestDefaultProjectSessionPersistDoesNotFollowActiveScope(t *testing.T) {
	for _, backend := range []Backend{BackendJSON, BackendSQLite} {
		t.Run(string(backend), func(t *testing.T) {
			router := newSessionGranularRouter(t, backend)
			store := NewSessionGranularStore(router)

			const bID = "sess-unbound-B"
			router.SetWorkspace("") // 默认项目
			first := Commit{}
			if backend == BackendJSON {
				// v8 JSON 布局（S11）：正文以事件行落库（provider 整段缓存退役）。
				first = Commit{Events: []Event{{Role: "user", Content: "first-turn-0", MessageID: "b-first"}}}
			} else {
				first = Commit{ProviderHistory: messages(1, "first-turn")}
			}
			if err := store.SaveCommit("", bID, first); err != nil {
				t.Fatalf("save B first turn: %v", err)
			}

			// 视图切到带项目会话 A（活跃写作用域 = project-A）。
			router.SetWorkspace("project-A")
			second := Commit{}
			if backend == BackendJSON {
				second = Commit{Events: []Event{
					{Role: "user", Content: "second-turn-0", MessageID: "b-second-0"},
					{Role: "user", Content: "second-turn-1", MessageID: "b-second-1"},
				}}
			} else {
				second = Commit{ProviderHistory: messages(2, "second-turn")}
			}
			// B 的落库 location.WorkspaceID=""（LocateSession 对默认项目会话
			// 返回空项目）：必须仍落在默认项目。
			if err := store.SaveCommit("", bID, second); err != nil {
				t.Fatalf("save B second turn: %v", err)
			}

			// 视图再切回默认作用域（重启/草稿）：B 的完整记录应可在默认项目
			// 读到最新两轮，且 project-A 不得出现 B。
			router.SetWorkspace("")
			defaultListed, err := store.SessionsOf("")
			if err != nil {
				t.Fatalf("SessionsOf(default): %v", err)
			}
			foundDefault := false
			for _, info := range defaultListed {
				if info.ID == bID {
					foundDefault = true
					break
				}
			}
			if !foundDefault {
				t.Fatalf("B missing from default project after view switch; listed=%+v", defaultListed)
			}
			history, total, err := store.HistoryRange("", bID, 0, 0)
			if err != nil {
				t.Fatalf("history range default: %v", err)
			}
			if total < 2 {
				t.Fatalf("B default-project history total=%d, want >=2（第二轮漂移到了 project-A）; msgs=%d", total, len(history))
			}
			projectAListed, err := store.SessionsOf("project-A")
			if err != nil {
				t.Fatalf("SessionsOf(project-A): %v", err)
			}
			for _, info := range projectAListed {
				if info.ID == bID {
					t.Fatalf("B leaked into project-A: %+v", projectAListed)
				}
			}
		})
	}
}
