package switching

import (
	"errors"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/contract/dto"
)

// 本文件钉草稿尾恢复的**决策表**（M5-R6-pending-tail 接收者转移）：
// 运行中不抢发布点、clean/未知状态静默、recoverable 恢复并报数、gap 只报告不清盘。

func probeDeps(report dto.PendingMessageTailReport) PendingTailDeps {
	return PendingTailDeps{
		Probe: func(string) (dto.PendingMessageTailReport, bool, error) { return report, true, nil },
	}
}

func TestRecoverPendingTailSkipsRunningSession(t *testing.T) {
	probed := false
	deps := PendingTailDeps{
		Running: func(string) bool { return true },
		Probe: func(string) (dto.PendingMessageTailReport, bool, error) {
			probed = true
			return dto.PendingMessageTailReport{Status: dto.PendingTailRecoverable}, true, nil
		},
	}
	if notice := RecoverPendingTail(deps, "s1"); notice != "" {
		t.Fatalf("运行中的会话不得抢发布点（应静默），got %q", notice)
	}
	if probed {
		t.Fatal("运行中连探测都不该做（探测也要读发布点）")
	}
}

func TestRecoverPendingTailSilentForCleanAndUnknown(t *testing.T) {
	for _, status := range []string{"", dto.PendingTailClean, dto.PendingTailRecovered, dto.PendingTailDiscarded} {
		if notice := RecoverPendingTail(probeDeps(dto.PendingMessageTailReport{Status: status}), "s1"); notice != "" {
			t.Fatalf("状态 %q 应静默，got %q", status, notice)
		}
	}
	// 未装配存储 / 非 v8 布局（ok=false）同样静默。
	deps := PendingTailDeps{Probe: func(string) (dto.PendingMessageTailReport, bool, error) {
		return dto.PendingMessageTailReport{}, false, nil
	}}
	if notice := RecoverPendingTail(deps, "s1"); notice != "" {
		t.Fatalf("能力未启用应静默，got %q", notice)
	}
}

func TestRecoverPendingTailRecoversAndReportsRows(t *testing.T) {
	deps := probeDeps(dto.PendingMessageTailReport{Status: dto.PendingTailRecoverable, RowCount: 3, HeadSeq: 10})
	deps.Recover = func(string) (dto.PendingMessageTailReport, bool, error) {
		return dto.PendingMessageTailReport{Status: dto.PendingTailRecovered, RowCount: 3, HeadSeq: 13}, true, nil
	}
	notice := RecoverPendingTail(deps, "s1")
	if !strings.Contains(notice, "Recovered 3 uncommitted row(s)") ||
		!strings.Contains(notice, "publish point 10 → 13") {
		t.Fatalf("恢复成功的提示应报行数与发布点前后值，got %q", notice)
	}
}

func TestRecoverPendingTailReportsGapWithoutTouchingDisk(t *testing.T) {
	recovered := false
	deps := probeDeps(dto.PendingMessageTailReport{
		Status: dto.PendingTailGap, RowCount: 2, TailFrom: 7, HeadSeq: 5,
	})
	deps.Recover = func(string) (dto.PendingMessageTailReport, bool, error) {
		recovered = true
		return dto.PendingMessageTailReport{}, true, nil
	}
	notice := RecoverPendingTail(deps, "s1")
	if !strings.Contains(notice, "cannot be recovered safely") || !strings.Contains(notice, "kept on disk") {
		t.Fatalf("gap 应只报告（明说仍在磁盘），got %q", notice)
	}
	if recovered {
		t.Fatal("gap 不得触发恢复/清理（红线 3：不发布、不清理、不猜测）")
	}
}

func TestRecoverPendingTailReportsProbeAndRecoverFailures(t *testing.T) {
	deps := PendingTailDeps{Probe: func(string) (dto.PendingMessageTailReport, bool, error) {
		return dto.PendingMessageTailReport{}, false, errors.New("io error")
	}}
	if notice := RecoverPendingTail(deps, "s1"); !strings.Contains(notice, "Failed to probe") {
		t.Fatalf("探测失败要如实报告，got %q", notice)
	}

	deps = probeDeps(dto.PendingMessageTailReport{Status: dto.PendingTailRecoverable, RowCount: 4})
	deps.Recover = func(string) (dto.PendingMessageTailReport, bool, error) {
		return dto.PendingMessageTailReport{}, false, errors.New("locked")
	}
	notice := RecoverPendingTail(deps, "s1")
	if !strings.Contains(notice, "could not be recovered") || !strings.Contains(notice, "still on disk") {
		t.Fatalf("恢复失败要明说内容仍在磁盘，got %q", notice)
	}

	// 探测与恢复之间状态已变（ok=false 或状态不是 recovered）：不宣称恢复。
	deps.Recover = func(string) (dto.PendingMessageTailReport, bool, error) {
		return dto.PendingMessageTailReport{Status: dto.PendingTailDiscarded}, true, nil
	}
	if notice := RecoverPendingTail(deps, "s1"); notice != "" {
		t.Fatalf("竞态窗口应静默，got %q", notice)
	}
}
