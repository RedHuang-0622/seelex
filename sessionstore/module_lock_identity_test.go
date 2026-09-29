package sessionstore

// module_lock_identity_test.go — 模块锁身份验收（2026-09-29 锁面审计 §2.10）。
//
// sessionstore 的锁口径是「每个模块一把锁 → 模块间互不阻塞」。这条不变式有两个
// 可判定的面：① 枚举值与锁**一对一**（没有两个模块共用一把锁）；② 共用必然表现为
// 互相阻塞。所以这里一条查身份、一条查行为，两条都不赌调度、不用 sleep 判绿。

import (
	"context"
	"sync"
	"testing"
	"time"
)

// allStorageModules 是 storageModule 的全集。新增枚举必须同步这里——漏掉的模块会在
// TestModuleLocksAreDistinct 里缺席，而 mutexFor 的 default 会在测试里直接失败
// （见 TestUnmappedModuleLockPanicsInsteadOfAliasing）。
func allStorageModules() []storageModule {
	return []storageModule{
		moduleMessage, moduleEvent, moduleCompact,
		moduleStackPlan, moduleStackTask, moduleStackGoal,
		moduleLifecycle, moduleRetention, moduleSubagent,
		moduleSystem, moduleCheckpoint, moduleMedia,
	}
}

// TestModuleLocksAreDistinct 钉住「模块 ↔ 锁」一对一。
//
// 修前的形态：media 没有 case，落到 mutexFor 的 default → 与 message 共用
// messageMu。后果有两层：截图/媒体读与消息提交互相串行（与 sessionstore/README.md
// 的"media 与 message 各自加锁"相反），以及任何"在 message 临界区内读写媒体"的
// 调用即不可重入自锁（埋点，等到有人踩才炸）。
func TestModuleLocksAreDistinct(t *testing.T) {
	store, key := messageFixture(t, 0)
	owner := map[*sync.Mutex]storageModule{}
	for _, mod := range allStorageModules() {
		lock := store.mu(key, mod)
		if previous, exists := owner[lock]; exists {
			t.Fatalf("模块 %q 与 %q 共用同一把锁（%p）——模块锁必须一对一", mod, previous, lock)
		}
		owner[lock] = mod
	}
}

// TestUnmappedModuleLockPanicsInsteadOfAliasing 是上一条的护栏：未映射的模块必须
// **显式失败**，不许再猜一把锁出来。旧行为（返回 messageMu）把"加了枚举忘了加
// case"这种编译期可见的错误藏成运行期的锁别名。
func TestUnmappedModuleLockPanicsInsteadOfAliasing(t *testing.T) {
	store, key := messageFixture(t, 0)
	defer func() {
		if recover() == nil {
			t.Fatal("未映射的模块没有显式失败——它又会被静默别名成 messageMu")
		}
	}()
	_ = store.mu(key, storageModule("half-written-module"))
}

// TestMediaReadNotBlockedByHeldMessageLock 钉住行为面：他人持 messageMu（= 正在
// 提交）期间，媒体读必须照常返回。
//
// 修前 media 就是 messageMu：这条会一直挂住（媒体读排在提交后面）。判据用超时护栏
// ——修前必然挡住、修后必然立刻返回，因此不需要 sleep 赌时序。
func TestMediaReadNotBlockedByHeldMessageLock(t *testing.T) {
	router := newLockProfileRouter(t, t.TempDir())
	const projectID, sessionID = "project-media-lock", "session-media-lock"
	if err := router.SaveCommitWorkspace(projectID, sessionID, Commit{Events: []Event{
		{Role: "user", Content: "seed", MessageID: "seed"},
	}}); err != nil {
		t.Fatal(err)
	}
	repository, ok := router.jsonRepository()
	if !ok {
		t.Fatal("router 不是 JSON 会话存储布局")
	}
	key := Key{ProjectID: projectID, SessionID: sessionID}

	messageLock := repository.layout.mu(key, moduleMessage)
	messageLock.Lock()
	done := make(chan error, 1)
	go func() {
		_, err := repository.ListMedia(context.Background(), key)
		done <- err
	}()
	select {
	case err := <-done:
		messageLock.Unlock()
		if err != nil {
			t.Fatalf("ListMedia: %v", err)
		}
	case <-time.After(3 * time.Second):
		messageLock.Unlock()
		t.Fatal("他人持 messageMu 期间媒体读被挡住——media 与 message 是同一把锁")
	}
}
