package session_runtime

import (
	"sync"
)

// SessionTransitionManager 是会话过渡锁的 per-session keyed 注册表（G5）：
// 同一 key（会话 ID；空 key = 视图过渡保留 key）的命令串行，不同 key 的
// 命令并行。实现为每个 key 一个 SessionTransitionActor（channel 命令 +
// 单 goroutine，无共享 mutex）；Close 幂等，关闭后 Lock/Unlock 退化为
// 空操作（与既有 actor 语义一致，退出路径不会卡死）。
//
// 现状边界（波 3 台账）：视图指针单例 + 部分进程级引擎副作用（fork 的
// StartSession 活跃别名、全局项目根绑定、legacy Router 写作用域）仍要求
// 影响视图的命令共用视图 key；本注册表为后续把这些副作用 per-session 化
// （G6/端口清理）后放开视图 key 提供了机制。
type SessionTransitionManager struct {
	mu     sync.Mutex
	closed bool
	actors map[string]*SessionTransitionActor
}

// NewSessionTransitionManager 构造空的 per-session 过渡锁注册表。
func NewSessionTransitionManager() *SessionTransitionManager {
	return &SessionTransitionManager{actors: make(map[string]*SessionTransitionActor)}
}

// Lock 返回 keyed locker：同 key 串行、跨 key 并行；关闭后为空操作。
func (manager *SessionTransitionManager) Lock(key string) sync.Locker {
	if key == "" {
		key = viewTransitionKey
	}
	return keyedTransitionLocker{manager: manager, key: key}
}

// lock 阻塞直到获得指定 key 的过渡锁（actor 不存在时按需创建）。
func (manager *SessionTransitionManager) lock(key string) {
	manager.mu.Lock()
	if manager.closed {
		manager.mu.Unlock()
		return
	}
	actor := manager.actors[key]
	if actor == nil {
		actor = NewSessionTransitionActor()
		manager.actors[key] = actor
	}
	manager.mu.Unlock()
	actor.Acquire()
}

// unlock 释放指定 key 的过渡锁（未持有也可调用：空操作）。
func (manager *SessionTransitionManager) unlock(key string) {
	manager.mu.Lock()
	if manager.closed {
		manager.mu.Unlock()
		return
	}
	actor := manager.actors[key]
	manager.mu.Unlock()
	if actor != nil {
		actor.Release()
	}
}

// Close 停止全部 per-key actor（幂等）。契约：调用方保证无活跃持有者；
// Shutdown/测试 teardown 使用。停止后 Lock/Unlock 均为空操作。
func (manager *SessionTransitionManager) Close() {
	if manager == nil {
		return
	}
	manager.mu.Lock()
	if manager.closed {
		manager.mu.Unlock()
		return
	}
	manager.closed = true
	actors := manager.actors
	manager.actors = nil
	manager.mu.Unlock()
	for _, actor := range actors {
		actor.Close()
	}
}

// viewTransitionKey 是影响视图指针/当前视图会话生命周期的命令共用的保留 key
// （空字符串归一；见 SessionTransitionManager 文档）。
const viewTransitionKey = "view"

// keyedTransitionLocker 把 manager 的 keyed actor 适配为 sync.Locker。
type keyedTransitionLocker struct {
	manager *SessionTransitionManager
	key     string
}

func (locker keyedTransitionLocker) Lock()   { locker.manager.lock(locker.key) }
func (locker keyedTransitionLocker) Unlock() { locker.manager.unlock(locker.key) }
