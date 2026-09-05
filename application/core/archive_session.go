package core

// C2 ArchiveSession：把会话置为 archived（record 级粘性状态）。归档只改
// record 状态通道，history/transcript/tool-results/context 四片原样保留，
// 会话从常规目录（分格枚举）隐藏，仍可按 ID 冷读/重开（存储层枚举不丢行）。
// 门控复用 sessionBusy 语义：running/queued/awaiting_approval 一律拒绝，
// 与驱逐前置 flush 同一顺序（先 flush + 释放引擎 bundle，再写状态），保证
// 与驱逐/冷读并发时 record 是完整快照。

import (
	"errors"
	"fmt"
	"strings"
)

// ErrSessionNotFoundArchive 表示目标会话不存在或 record 缺失/版本不兼容。
var ErrSessionNotFoundArchive = errors.New("session not found")

// ArchiveSession 归档指定会话：非 running/queued/awaiting_approval 才允许；
// 驻留会话先 flush 并释放引擎 bundle（INV-G8 驱逐前置语义），随后把
// record.Status 写为 archived 并按项目范围刷新目录（归档行从常规列表过滤）。
func (service *Service) ArchiveSession(sessionID string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return errors.New("session ID is required")
	}
	transition := service.transitionForKey(sessionID)
	transition.Lock()
	defer transition.Unlock()

	service.ViewMu.RLock()
	closed := service.closed
	draining := service.draining
	unit := service.sessions.Unit(sessionID)
	busy := unit != nil && service.sessionBusy(unit)
	restoring := service.isRestoringLocked(sessionID)
	service.ViewMu.RUnlock()
	if closed {
		return errors.New("application is shut down")
	}
	if draining {
		return ErrApplicationDraining
	}
	if busy || restoring {
		return ErrChatRunning
	}

	location := service.components.sessions.LocateSession(sessionID)
	record, ok, err := service.components.sessions.LoadSessionRecord(location, sessionID)
	if err != nil {
		return fmt.Errorf("archive load %q: %w", sessionID, err)
	}
	if !ok || record.ID != sessionID {
		return fmt.Errorf("%w: %q", ErrSessionNotFoundArchive, sessionID)
	}
	if record.Status == SessionStatusArchived {
		return nil
	}

	// 驱逐前置 flush：引擎仍驻留时先持久化完整会话快照，再释放 bundle，
	// 归档 record 因此携带最新五片；Unit/View/Runtime 槽保留（Resident=false，
	// 重开走 cold_load）。
	if engine, routed := service.Deps.Engine.(interface {
		HasSession(string) bool
		UnloadSession(string) error
	}); routed && unit != nil && unit.Resident() && engine.HasSession(sessionID) {
		if err := service.components.sessions.PersistCurrentSession(location, sessionID); err != nil {
			return fmt.Errorf("archive flush %q: %w", sessionID, err)
		}
		if err := engine.UnloadSession(sessionID); err != nil {
			return fmt.Errorf("archive unload %q: %w", sessionID, err)
		}
		service.ViewMu.Lock()
		service.markResidentRecentLocked(sessionID, false)
		filtered := service.residentOrder[:0]
		for _, existing := range service.residentOrder {
			if existing != sessionID {
				filtered = append(filtered, existing)
			}
		}
		service.residentOrder = filtered
		service.ViewMu.Unlock()
	}

	if err := service.components.sessions.MarkSessionArchived(location, sessionID); err != nil {
		return fmt.Errorf("archive %q: %w", sessionID, err)
	}
	// 归档状态属于该项目格子：按项目范围刷新，其它项目列表不受影响。
	service.components.sessions.RequestCatalogRefreshProject(location.WorkspaceID)
	return nil
}
