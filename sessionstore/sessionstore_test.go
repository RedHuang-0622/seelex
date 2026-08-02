package sessionstore

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/RedHuang-0622/Seele/types"
)

func messages(count int, marker string) []types.Message {
	result := make([]types.Message, count)
	for index := range result {
		content := fmt.Sprintf("%s-%d", marker, index)
		result[index] = types.Message{Role: "user", Content: &content}
	}
	return result
}

func TestJSONRepositoryCommitsShardGenerationsAtomically(t *testing.T) {
	repository, err := newJSONRepository(t.TempDir(), 0)
	if err != nil {
		t.Fatal(err)
	}
	key := Key{ProjectID: "project", SessionID: "session"}
	first, second := messages(205, "first"), messages(121, "second")
	if err := repository.WriteAtomic(context.Background(), key, first); err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	errors := make(chan error, 40)
	for index := 0; index < 40; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			history, readErr := repository.Read(context.Background(), key)
			if readErr != nil {
				errors <- readErr
				return
			}
			if len(history) != len(first) && len(history) != len(second) {
				errors <- fmt.Errorf("mixed history count %d", len(history))
			}
		}()
	}
	if err := repository.WriteAtomic(context.Background(), key, second); err != nil {
		t.Fatal(err)
	}
	group.Wait()
	close(errors)
	for err := range errors {
		t.Error(err)
	}
	history, err := repository.Read(context.Background(), key)
	if err != nil || len(history) != len(second) || *history[0].Content != "second-0" {
		t.Fatalf("history=%d err=%v", len(history), err)
	}
}

func TestSQLiteRepositoryRoundTrip(t *testing.T) {
	repository, err := Open(context.Background(), Config{Backend: BackendSQLite, Path: filepath.Join(t.TempDir(), "sessions.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	key := Key{ProjectID: "project", SessionID: "session"}
	if err := repository.WriteAtomic(context.Background(), key, messages(3, "sqlite")); err != nil {
		t.Fatal(err)
	}
	history, err := repository.Read(context.Background(), key)
	if err != nil || len(history) != 3 || *history[2].Content != "sqlite-2" {
		t.Fatalf("history=%v err=%v", history, err)
	}
	listed, err := repository.List(context.Background(), "project")
	if err != nil || len(listed) != 1 || listed[0].SessionID != "session" {
		t.Fatalf("list=%v err=%v", listed, err)
	}
}

func TestRouterPersistsAndSwitchesConfiguredBackend(t *testing.T) {
	root := t.TempDir()
	router, err := NewRouter(filepath.Join(root, "session-storage.json"), root)
	if err != nil {
		t.Fatal(err)
	}
	defer router.Close()
	router.SetWorkspace("project")
	if err := router.Save("session", messages(1, "json")); err != nil {
		t.Fatal(err)
	}
	if err := router.Configure(context.Background(), Config{Backend: BackendSQLite, Path: filepath.Join(root, "sessions.db")}); err != nil {
		t.Fatal(err)
	}
	if router.Config().Backend != BackendSQLite {
		t.Fatalf("backend=%q", router.Config().Backend)
	}
	if err := router.Save("session", messages(1, "sqlite")); err != nil {
		t.Fatal(err)
	}
	history, err := router.Load("session")
	if err != nil || *history[0].Content != "sqlite-0" {
		t.Fatalf("history=%v err=%v", history, err)
	}
}

func TestRouterReadsExplicitWorkspaceWithoutChangingActiveScope(t *testing.T) {
	root := t.TempDir()
	router, err := NewRouter(filepath.Join(root, "session-storage.json"), root)
	if err != nil {
		t.Fatal(err)
	}
	defer router.Close()

	router.SetWorkspace("project-a")
	if err := router.Save("shared", messages(1, "a")); err != nil {
		t.Fatal(err)
	}
	router.SetWorkspace("project-b")
	if err := router.Save("shared", messages(1, "b")); err != nil {
		t.Fatal(err)
	}
	router.SetWorkspace("project-a")

	history, err := router.LoadWorkspace("project-b", "shared")
	if err != nil || len(history) != 1 || *history[0].Content != "b-0" {
		t.Fatalf("project-b history=%v err=%v", history, err)
	}
	if router.Workspace() != "project-a" {
		t.Fatalf("explicit read changed active workspace to %q", router.Workspace())
	}
	listed := router.ListWorkspace("project-b")
	if len(listed) != 1 || listed[0].SessionID != "shared" {
		t.Fatalf("project-b sessions=%v", listed)
	}
}

// TestJSONRepositoryReadRangeWindowed 验证窗口读优化：manifest shard 计数
// 存在时 ReadRange 只解析覆盖目标范围的 shard（尾部窗口读），结果与全量
// 读一致。3 个 shard（250 条消息）尾部 5 条窗口。
func TestJSONRepositoryReadRangeWindowed(t *testing.T) {
	repository, err := newJSONRepository(t.TempDir(), 0)
	if err != nil {
		t.Fatal(err)
	}
	key := Key{ProjectID: "project", SessionID: "session"}
	history := messages(250, "round") // 3 shards（100/100/50）
	if err := repository.WriteAtomic(context.Background(), key, history); err != nil {
		t.Fatal(err)
	}
	// 尾部窗口：offset=245, limit=5 → 消息 245-249（"round-245".."round-249"）。
	window, total, err := repository.ReadRange(context.Background(), key, 245, 5)
	if err != nil {
		t.Fatal(err)
	}
	if total != 250 {
		t.Fatalf("total = %d, want 250", total)
	}
	if len(window) != 5 || *window[0].Content != "round-245" || *window[4].Content != "round-249" {
		t.Fatalf("window = %d msgs [%q..%q], want round-245..round-249", len(window), *window[0].Content, *window[len(window)-1].Content)
	}
	// 跨 shard 边界窗口：offset=95, limit=15 → 覆盖 shard 0/1。
	cross, total, err := repository.ReadRange(context.Background(), key, 95, 15)
	if err != nil {
		t.Fatal(err)
	}
	if total != 250 || len(cross) != 15 || *cross[0].Content != "round-95" || *cross[14].Content != "round-109" {
		t.Fatalf("cross-shard window = %d msgs [%q..%q] total=%d", len(cross), *cross[0].Content, *cross[len(cross)-1].Content, total)
	}
	// 与全量读一致性：任意窗口 == 全量切片。
	all, err := repository.Read(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	for offset, limit := range map[int]int{0: 100, 150: 60, 249: 1, 240: 20} {
		end := offset + limit
		if end > len(all) {
			end = len(all)
		}
		want := append([]types.Message(nil), all[offset:end]...)
		got, _, err := repository.ReadRange(context.Background(), key, offset, limit)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != len(want) || (len(got) > 0 && firstContent(got) != firstContent(want)) {
			t.Fatalf("range [%d,%d) = %d msgs, want %d (first %q)", offset, end, len(got), len(want), firstContent(want))
		}
	}
	// 越界 offset 报错。
	if _, _, err := repository.ReadRange(context.Background(), key, 251, 5); err == nil {
		t.Fatal("offset beyond history must fail")
	}
}

func firstContent(messages []types.Message) string {
	if len(messages) == 0 {
		return ""
	}
	if messages[0].Content == nil {
		return ""
	}
	return *messages[0].Content
}

// TestJSONRepositoryReadRangeTotalOnly 验证 limit<=0 语义：只读 manifest 拿
// 总数、不解析任何 shard（会话切换"先探 total 再尾部窗口读"的依赖）。
func TestJSONRepositoryReadRangeTotalOnly(t *testing.T) {
	repository, err := newJSONRepository(t.TempDir(), 0)
	if err != nil {
		t.Fatal(err)
	}
	key := Key{ProjectID: "project", SessionID: "session"}
	history := messages(250, "round") // 3 shards（100/100/50）
	if err := repository.WriteAtomic(context.Background(), key, history); err != nil {
		t.Fatal(err)
	}
	got, total, err := repository.ReadRange(context.Background(), key, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if total != 250 || len(got) != 0 {
		t.Fatalf("total-only read = %d msgs total=%d, want 0 msgs total=250", len(got), total)
	}
}
