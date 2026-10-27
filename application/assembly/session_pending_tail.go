package assembly

// session_pending_tail.go — 草稿尾部恢复的**宿主适配 + 门面转发**。
//
// 判据（决策表与四条 system 提示文案）已在 M5-R6-pending-tail 随接收者转移归位
// `middle/switch.RecoverPendingTail`（plan §2.4 的 `session_pending_tail.go` → `middle/switch/`）。
// 本文件只剩宿主面：会话是否在跑（在飞写者）、定位会话后调存储端口的探测/恢复，
// 以及把结论作为 system 行补进可见投影这一步（写面）。
//
// 时序（历史结论，逐字保留）：调用必须在读可见正文**之前**——读者的可见性闸门就是发布点
// （message head.LastSeq），先恢复再读，恢复出来的行才能在本次加载里就进可见会话。

import (
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/kernel/sessionpolicy"
)

// recoverPendingMessageTailAtLoad 在会话冷加载路径上探测/恢复草稿尾，返回要补进可见会话的
// system 提示（"" = 静默）。
func (service *Service) recoverPendingMessageTailAtLoad(sessionID string) string {
	if service == nil || service.components.sessions == nil {
		return ""
	}
	return switching.RecoverPendingTail(service.pendingTailDeps(), sessionID)
}

// pendingTailDeps 装配草稿尾决策的宿主面（每次调用构造一次值语义的 Deps；无状态）。
func (service *Service) pendingTailDeps() switching.PendingTailDeps {
	return switching.PendingTailDeps{
		Running: service.pendingTailSessionRunning,
		Probe: func(sessionID string) (dto.PendingMessageTailReport, bool, error) {
			location := service.components.sessions.LocateSession(sessionID)
			return service.components.sessions.PendingMessageTail(location, sessionID)
		},
		Recover: func(sessionID string) (dto.PendingMessageTailReport, bool, error) {
			location := service.components.sessions.LocateSession(sessionID)
			return service.components.sessions.RecoverPendingMessageTail(location, sessionID)
		},
	}
}

// pendingTailSessionRunning 报告目标会话是否正在跑（在飞写者）。
func (service *Service) pendingTailSessionRunning(sessionID string) bool {
	service.ViewMu.Lock()
	defer service.ViewMu.Unlock()
	return service.sessionUnitLocked(sessionID).ChatState().Running
}

// appendPendingTailNoticeLocked 把草稿尾恢复的结论作为一条 system 行补进目标会话的可见投影
// （调用方持 Core.ViewMu，且在可见投影安装之后调用——过早追加会被 SetSessionViewLocked 的整体
// 替换抹掉）。用户由此在会话里直接看到"恢复了多少行"或"有内容但没能安全恢复"，而不是只在日志里。
func (service *Service) appendPendingTailNoticeLocked(sessionID, notice string) {
	if notice == "" {
		return
	}
	service.components.view.AppendMessageLockedFor(sessionID, "system", notice, nil)
}
