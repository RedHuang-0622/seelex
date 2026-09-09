// 运行期装配 API：Router 面向运行期（resume/compact/retention/lifecycle）与
// 数据通道（栈）的能力出口。
//
// 通道口径：栈通道由每个后端实现（Repository.stackJournal），不存在「某后端
// 没有栈」的运行期分支；wire 装配 / compact 帧 / retention 建议 / LRU 删除 /
// lifecycle 恢复仍只在会话存储布局生效（非该布局返回 ok=false），把它们做成
// 后端无关是设计稿待办（见 conformance-checklist §4）。
package sessionstore

import (
	"strings"

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

// jsonRepositoryLocked 返回当前 repository（若为 JSON 会话存储布局）。
func (router *Router) jsonRepositoryLocked() (*jsonRepository, bool) {
	repository, ok := router.repository.(*jsonRepository)
	return repository, ok
}

// AssembleWireWorkspace 对会话执行 wire 装配（frame 摘要 + tail + 最近 K
// 条尝试）。非该布局返回 ok=false。
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
// 供 wire 装配；非该布局返回 ok=false）。
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

// ---------- 栈通道（my_design §2.4/§3.2）----------

// StackPush 把一批条目压入指定栈（batchID 为空 = 本次入栈自成一批）。
func (router *Router) StackPush(projectID, sessionID string, kind StackKind, batchID string, items []StackItemInput) (StackMutation, error) {
	return router.stackMutate(projectID, sessionID, kind, stackPushMessage(kind, batchID, items))
}

// StackSetStatus 更新栈条目状态；所属批次全部完成时整批弹栈归档。
func (router *Router) StackSetStatus(projectID, sessionID string, kind StackKind, itemID, status string) (StackMutation, error) {
	return router.stackMutate(projectID, sessionID, kind, stackSetStatusMessage(itemID, status))
}

// StackPopTop 弹出栈顶条目（goal LIFO）。
func (router *Router) StackPopTop(projectID, sessionID string, kind StackKind, itemID, status string) (StackMutation, error) {
	return router.stackMutate(projectID, sessionID, kind, stackPopTopMessage(itemID, status))
}

// StackReplace 用新的条目集合替换 active 栈，并推导出逐条迁移。
func (router *Router) StackReplace(projectID, sessionID string, kind StackKind, items []StackItemInput, closedStatus string) (StackMutation, error) {
	return router.stackMutate(projectID, sessionID, kind, stackReplaceMessage(kind, items, closedStatus))
}

// stackMutate 是栈通道的统一写入口：变更以闭包（actor 消息）交给当前后端的
// 会话提交临界区执行 → 单写者，可变投影不会被第二个 goroutine 同时触碰。
func (router *Router) stackMutate(projectID, sessionID string, kind StackKind, mutate func(*stackState) (StackMutation, error)) (StackMutation, error) {
	var mutation StackMutation
	err := router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		var err error
		mutation, err = stackCommit(repository.stackJournal(), Key{ProjectID: projectID, SessionID: sessionID}, kind, mutate)
		return err
	})
	return mutation, err
}

// StackActive 返回指定栈当前 active 条目（按 seq 升序）。
func (router *Router) StackActive(projectID, sessionID string, kind StackKind) ([]StackItemRecord, error) {
	var rows []StackItemRecord
	err := router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		var err error
		rows, err = stackReadActive(repository.stackJournal(), Key{ProjectID: projectID, SessionID: sessionID}, kind)
		return err
	})
	return rows, err
}

// StackHistory 返回指定栈的归档条目。
func (router *Router) StackHistory(projectID, sessionID string, kind StackKind) ([]StackItemRecord, error) {
	var rows []StackItemRecord
	err := router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		var err error
		rows, err = stackReadHistory(repository.stackJournal(), Key{ProjectID: projectID, SessionID: sessionID}, kind)
		return err
	})
	return rows, err
}

// StackVerify 校验栈通道 head 水位与数据计数一致（M5 巡检的栈通道部分）。
func (router *Router) StackVerify(projectID, sessionID string) error {
	return router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		return stackVerify(repository.stackJournal(), Key{ProjectID: projectID, SessionID: sessionID})
	})
}

// StackStorageStats 返回栈通道的延迟归因：栈锁等待与 active / history / head /
// guide / EVENT 各数据域耗时，用于回答「延迟落在哪个文件」。
func (router *Router) StackStorageStats() (stackJournalStats, error) {
	var stats stackJournalStats
	err := router.withRepository(func(repository Repository, projectID string) error {
		stats = repository.stackJournal().stats()
		return nil
	})
	return stats, err
}

// ForkStacks 按 message 锚把父会话三栈重建进子会话（§8.1 + T-FK-02）。
// fromSeq 必须 ≥ 父会话 LRU watermark（I7），否则显式报错、不产生半成品栈。
func (router *Router) ForkStacks(parentProjectID, parentSessionID, childProjectID, childSessionID string, fromSeq uint64) error {
	return router.withRepositoryAt(childProjectID, func(repository Repository, childProjectID string) error {
		return stackFork(repository.stackJournal(),
			Key{ProjectID: strings.TrimSpace(parentProjectID), SessionID: parentSessionID},
			Key{ProjectID: childProjectID, SessionID: childSessionID}, fromSeq)
	})
}
