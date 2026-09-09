// 运行期装配 API：Router 面向运行期（resume/compact/retention/lifecycle）
// 的 能力出口。非 JSON/后端返回 ok=false，上层回退旧链路，保证 AB
// 双链路并存。
package sessionstore

import (
	"github.com/RedHuang-0622/Seele/types"
)

// RetentionAdvisory 是 retention 水位建议（运行期只读，不自动删）。
type RetentionAdvisory struct {
	Layout     string `json:"layout"`
	FrameCount int    `json:"frame_count"`
	Threshold  int    `json:"threshold"`
	RawBytes   uint64 `json:"raw_bytes"`
	AlertBytes uint64 `json:"alert_bytes"`
	Eligible   bool   `json:"eligible"`
	Mode       string `json:"mode"`
}

// jsonRepositoryAccessor 返回当前 repository（若为 JSON）。
func (router *Router) jsonRepositoryLocked() (*jsonRepository, bool) {
	repository, ok := router.repository.(*jsonRepository)
	return repository, ok
}

// AssembleWireWorkspace 对 会话执行 wire 装配（frame 摘要 + tail + 最近 K
// 条尝试）。非 会话返回 ok=false。
func (router *Router) AssembleWireWorkspace(projectID, sessionID string, budget, k int) ([]types.Message, bool, error) {
	router.mu.RLock()
	repository, ok := router.jsonRepositoryLocked()
	router.mu.RUnlock()
	if !ok {
		return nil, false, nil
	}
	return repository.assembleWireWorkspace(Key{ProjectID: projectID, SessionID: sessionID}, budget, k)
}

// CommitCompactFrameWorkspace 把运行期压缩帧写入 compact 通道（帧摘要
// 供 wire 装配；非 v8 返回 ok=false）。
func (router *Router) CommitCompactFrameWorkspace(projectID, sessionID string, frame CompactFrame) (bool, error) {
	router.mu.RLock()
	repository, ok := router.jsonRepositoryLocked()
	router.mu.RUnlock()
	if !ok {
		return false, nil
	}
	return repository.commitCompactFrameWorkspace(Key{ProjectID: projectID, SessionID: sessionID}, frame)
}

// RetentionAdvisoryWorkspace 返回会话 retention 水位建议（v8；非 v8 返回
// Layout=legacy 的零值建议）。
func (router *Router) RetentionAdvisoryWorkspace(projectID, sessionID string) (RetentionAdvisory, error) {
	router.mu.RLock()
	repository, ok := router.jsonRepositoryLocked()
	router.mu.RUnlock()
	if !ok {
		return RetentionAdvisory{Layout: "legacy"}, nil
	}
	return repository.retentionAdvisoryWorkspace(Key{ProjectID: projectID, SessionID: sessionID})
}

// LRUDeleteWorkspace 用户确认后删除 watermark 前连续前缀（v8；manual 模式
// 未确认返回 ErrRetentionRequiresConfirm）。
func (router *Router) LRUDeleteWorkspace(projectID, sessionID string, upToSeq uint64, confirmed bool) (bool, error) {
	router.mu.RLock()
	repository, ok := router.jsonRepositoryLocked()
	router.mu.RUnlock()
	if !ok {
		return false, nil
	}
	return repository.lruDeleteWorkspace(Key{ProjectID: projectID, SessionID: sessionID}, upToSeq, confirmed)
}

// LifecycleRecoverWorkspace 重启恢复 lifecycle 队列（发送未确认项回
// queued；message 已发布项出队）。返回恢复条数 + ok。
func (router *Router) LifecycleRecoverWorkspace(projectID, sessionID string) (int, bool, error) {
	router.mu.RLock()
	repository, ok := router.jsonRepositoryLocked()
	router.mu.RUnlock()
	if !ok {
		return 0, false, nil
	}
	return repository.lifecycleRecoverWorkspace(Key{ProjectID: projectID, SessionID: sessionID})
}
