package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// session_ssh_state_test.go 钉住会话级 SSH 状态的四件事：
//  1. 缺省是"什么都不给"：没有记录 = 没有当前环境、没有放权（SSH 在会话里默认不可用）；
//  2. 追加写、按行回放、同一会话取最后一行（后写覆盖先写，历史仍留在文件里）；
//  3. 坏行跳过：一行坏掉不该让整份状态读不出来（读不出来时方向是收紧）；
//  4. 换一个 store 实例也能读回来（真落盘，不是进程内缓存）。

func openSessionSSHRouter(t *testing.T) *Router {
	t.Helper()
	dir := t.TempDir()
	router, err := NewRouter(filepath.Join(dir, "session-storage.json"))
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	t.Cleanup(func() { _ = router.Close() })
	return router
}

func sessionSSHFileOnDisk(t *testing.T, router *Router) string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(router.repository.(*jsonRepository).root, "project-*", sessionSSHFileName))
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("会话 SSH 状态文件 = %v, want 恰好一份", matches)
	}
	return matches[0]
}

func TestSessionSSHStateDefaultsToNothingGranted(t *testing.T) {
	router := openSessionSSHRouter(t)
	store := NewSessionSSHStateStore(router)

	state, err := store.State("", "session-a")
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	if state.CurrentEnv != "" {
		t.Fatalf("缺省不该有当前环境: %q", state.CurrentEnv)
	}
	if state.AllowedEnvs == nil || len(state.AllowedEnvs) != 0 {
		t.Fatalf("缺省放权集合必须是空切片（不是 nil、不是有值）: %#v", state.AllowedEnvs)
	}
}

func TestSessionSSHStateAppendsAndLastWins(t *testing.T) {
	router := openSessionSSHRouter(t)
	store := NewSessionSSHStateStore(router)

	if err := store.Put("", SessionSSHState{SessionID: "session-a", AllowedEnvs: []string{"dev"}}); err != nil {
		t.Fatalf("Put #1: %v", err)
	}
	if err := store.Put("", SessionSSHState{SessionID: "session-a", AllowedEnvs: []string{"dev", "test"}, CurrentEnv: "test"}); err != nil {
		t.Fatalf("Put #2: %v", err)
	}
	// 别的会话不受影响
	if err := store.Put("", SessionSSHState{SessionID: "session-b", AllowedEnvs: []string{"prod"}}); err != nil {
		t.Fatalf("Put #3: %v", err)
	}

	// 换一个实例读（真落盘）
	reopened := NewSessionSSHStateStore(router)
	state, err := reopened.State("", "session-a")
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	if state.CurrentEnv != "test" {
		t.Fatalf("当前环境 = %q, want test", state.CurrentEnv)
	}
	if len(state.AllowedEnvs) != 2 || state.AllowedEnvs[0] != "dev" || state.AllowedEnvs[1] != "test" {
		t.Fatalf("放权集合 = %#v, want [dev test]", state.AllowedEnvs)
	}
	other, err := reopened.State("", "session-b")
	if err != nil {
		t.Fatalf("State b: %v", err)
	}
	if len(other.AllowedEnvs) != 1 || other.AllowedEnvs[0] != "prod" {
		t.Fatalf("session-b 放权 = %#v, want [prod]", other.AllowedEnvs)
	}

	// 文件是 JSONL：三行，一行一条变更（历史留着）
	raw, err := os.ReadFile(sessionSSHFileOnDisk(t, router))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	lines := 0
	for _, line := range splitLines(string(raw)) {
		if line == "" {
			continue
		}
		lines++
		var record SessionSSHState
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("第 %d 行不是合法 JSON: %v", lines, err)
		}
	}
	if lines != 3 {
		t.Fatalf("JSONL 行数 = %d, want 3（每次 Put 一行，不覆盖历史）", lines)
	}
}

func TestSessionSSHStateSkipsBrokenLines(t *testing.T) {
	router := openSessionSSHRouter(t)
	store := NewSessionSSHStateStore(router)
	if err := store.Put("", SessionSSHState{SessionID: "session-a", AllowedEnvs: []string{"dev"}}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	path := sessionSSHFileOnDisk(t, router)
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	if _, err := file.WriteString("{这行是坏的\n"); err != nil {
		t.Fatalf("WriteString: %v", err)
	}
	_ = file.Close()

	state, err := store.State("", "session-a")
	if err != nil {
		t.Fatalf("State 不该因坏行失败: %v", err)
	}
	if len(state.AllowedEnvs) != 1 || state.AllowedEnvs[0] != "dev" {
		t.Fatalf("坏行之后仍要读回最后一条好行: %#v", state.AllowedEnvs)
	}
}

func TestSessionSSHStateConcurrentWrites(t *testing.T) {
	router := openSessionSSHRouter(t)
	store := NewSessionSSHStateStore(router)

	var wg sync.WaitGroup
	for index := 0; index < 8; index++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			sessionID := "session-" + string(rune('a'+index))
			for round := 0; round < 5; round++ {
				if err := store.Put("", SessionSSHState{SessionID: sessionID, AllowedEnvs: []string{"dev"}}); err != nil {
					t.Errorf("Put(%s): %v", sessionID, err)
					return
				}
			}
		}(index)
	}
	wg.Wait()

	for index := 0; index < 8; index++ {
		sessionID := "session-" + string(rune('a'+index))
		state, err := store.State("", sessionID)
		if err != nil {
			t.Fatalf("State(%s): %v", sessionID, err)
		}
		if len(state.AllowedEnvs) != 1 || state.AllowedEnvs[0] != "dev" {
			t.Fatalf("%s 放权 = %#v, want [dev]", sessionID, state.AllowedEnvs)
		}
	}
}

// splitLines 是测试用的小工具（不引 strings.Split 以避免与生产路径混淆）。
func splitLines(text string) []string {
	lines := make([]string, 0, 8)
	current := make([]byte, 0, 64)
	for index := 0; index < len(text); index++ {
		if text[index] == '\n' {
			lines = append(lines, string(current))
			current = current[:0]
			continue
		}
		current = append(current, text[index])
	}
	if len(current) > 0 {
		lines = append(lines, string(current))
	}
	return lines
}
