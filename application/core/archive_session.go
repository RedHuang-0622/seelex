package core

// archive_session.go — **C2 ArchiveSession** 的宿主适配 + 门面转发。
//
// 业务事实：把会话置为 archived（record 级粘性状态）。归档只改 record 状态通道，
// history/transcript/tool-results/context 四片原样保留，会话从常规目录（分格枚举）
// 隐藏，仍可按 ID 冷读/重开（存储层枚举不丢行）。门控复用会话忙态语义：
// running/queued/awaiting_approval 一律拒绝，与驱逐前置 flush 同一顺序（先 flush +
// 释放引擎 bundle，再写状态），保证与驱逐/冷读并发时 record 是完整快照。
//
// 判据（门控次序 → 读 record → flush/unload/摘驻留序 → 写归档状态 → 释放后台命令 →
// 按项目刷新目录）已于 M5-R6-archive 随接收者转移归位 `middle/residency/archive.go`
// （plan §2.4）；本文件只剩"把宿主读数与存储/引擎落点装成端口"这条装配缝与一行转发。

import (
	"errors"
	"strings"

	"github.com/RedHuang-0622/seelex/middle/residency"
)

// ErrSessionNotFoundArchive 表示目标会话不存在或 record 缺失/版本不兼容。
// 判据在 `middle/residency`；本名字是宿主侧的兼容面，与 `residency.ErrArchiveNotFound`
// 是同一个错误值（`errors.Is` 两个名字都成立）。
var ErrSessionNotFoundArchive = residency.ErrArchiveNotFound

// ArchiveSession 归档指定会话（判据与文案在 `middle/residency`）。
func (service *Service) ArchiveSession(sessionID string) error {
	// 过渡锁仍按宿主口径取（engine 不上报会话路由时退化成全局单飞），trim 后的 ID
	// 同时交给过渡锁与端口装配：落点解析仍留在宿主侧。
	sessionID = strings.TrimSpace(sessionID)
	transition := service.transitionForKey(sessionID)
	transition.Lock()
	defer transition.Unlock()
	return residency.Archive(service.archiveDeps(sessionID), sessionID)
}

// archiveDeps 装配归档用例的宿主面（每次调用构造一次值语义的 Deps；无状态）。
//
// 端口刻意是**函数值**：`ArchiveStore` 的每个动作都要"按会话定位落点"，而落点类型
// （`session_runtime.Location`）是后端域类型，中间层不能命名；结构满足做不到的那一层
// 适配因此只留在装配缝上（AGENTS §8.1/§8.4）。
func (service *Service) archiveDeps(sessionID string) residency.ArchiveDeps {
	sessions := service.components.sessions
	engine, routed := service.Deps.Engine.(residentEngine)
	return residency.ArchiveDeps{
		Gate: func() residency.ArchiveGate { return service.archiveGate(sessionID) },
		Store: residency.ArchiveStore{
			Load: func(sessionID string) (SessionRecord, bool, error) {
				return sessions.LoadSessionRecord(sessions.LocateSession(sessionID), sessionID)
			},
			Flush: func(sessionID string) error {
				return sessions.PersistCurrentSession(sessions.LocateSession(sessionID), sessionID)
			},
			MarkArchived: func(sessionID string) error {
				return sessions.MarkSessionArchived(sessions.LocateSession(sessionID), sessionID)
			},
			RefreshProject: func(sessionID string) {
				sessions.RequestCatalogRefreshProject(sessions.LocateSession(sessionID).WorkspaceID)
			},
		},
		Engine: residency.ArchiveEngine{
			Resident: func(sessionID string) bool {
				service.ViewMu.RLock()
				defer service.ViewMu.RUnlock()
				unit := service.sessions.Unit(sessionID)
				return unit != nil && unit.Resident()
			},
			Loaded: func(sessionID string) bool {
				return routed && engine.HasSession(sessionID)
			},
			Unload: func(sessionID string) error {
				return engine.UnloadSession(sessionID)
			},
			ForgetResident: func(sessionID string) {
				service.ViewMu.Lock()
				service.forgetResidentLocked(sessionID)
				service.ViewMu.Unlock()
			},
		},
		ReleaseAsync: service.releaseSessionAsync,
	}
}

// archiveGate 在视图锁内取归档门控的四个读数，并带上宿主自己的错误值。
//
// 调用方已持会话过渡锁；判据（哪个轴先拒绝）在 `middle/residency`，这里只负责读宿主状态。
func (service *Service) archiveGate(sessionID string) residency.ArchiveGate {
	service.ViewMu.RLock()
	gate := residency.ArchiveGate{
		Closed:    service.closed,
		Draining:  service.draining,
		Busy:      service.sessionBusy(service.sessions.Unit(sessionID)),
		Restoring: service.isRestoringLocked(sessionID),
	}
	service.ViewMu.RUnlock()
	if gate.Closed {
		gate.ErrClosed = errors.New("application is shut down")
	}
	if gate.Draining {
		gate.ErrDraining = ErrApplicationDraining
	}
	if gate.Busy || gate.Restoring {
		gate.ErrBusy = ErrChatRunning
	}
	return gate
}
