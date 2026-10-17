package store

// sessionstore_workspace_race_test.go 钉住 Router 写作用域的读写口径：
// SetWorkspace（会话切换/回合重绑）与按当前作用域取后端的读面必须并发安全。
// 判据是 -race 下的并发用例（普通跑不判红，-race 跑前是 DATA RACE）。

import (
	"fmt"
	"sync"
	"testing"
)

func TestWorkspaceSwitchIsSafeAgainstRepositoryReaders(t *testing.T) {
	router := newTestRouter(t)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for index := 0; index < 200; index++ {
			router.SetWorkspace(fmt.Sprintf("project-%d", index%3))
		}
	}()
	go func() {
		defer wg.Done()
		for index := 0; index < 200; index++ {
			if _, ok := router.BoardsFor(); !ok {
				t.Errorf("活跃后端必须可用")
				return
			}
		}
	}()
	wg.Wait()
}
