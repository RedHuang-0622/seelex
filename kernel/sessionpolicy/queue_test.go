package switching

import (
	"errors"
	"strings"
	"testing"
)

// 本文件钉队列编辑面的判据（M5-R6-queue-edit 接收者转移）：门禁次序与装配缺陷、
// 目标会话解析与三条拒绝文案、索引越界文案、以及哨兵身份。

func TestQueueEditGateOrderAndUnassembled(t *testing.T) {
	closedErr := errors.New("application is shut down")
	drainingErr := errors.New("application is draining")
	errs := QueueGateErrors{Closed: closedErr, Draining: drainingErr}

	if err := QueueEditGate(false, false, errs); err != nil {
		t.Fatalf("既未关闭也未排空应放行，got %v", err)
	}
	if err := QueueEditGate(true, false, errs); !errors.Is(err, closedErr) {
		t.Fatalf("关闭应返回宿主错误值，got %v", err)
	}
	if err := QueueEditGate(false, true, errs); !errors.Is(err, drainingErr) {
		t.Fatalf("排空应返回宿主错误值，got %v", err)
	}
	if err := QueueEditGate(true, true, errs); !errors.Is(err, closedErr) {
		t.Fatalf("关闭优先于排空，got %v", err)
	}
	if err := QueueEditGate(true, false, QueueGateErrors{}); !errors.Is(err, ErrQueueGateUnassembled) {
		t.Fatalf("缺错误值应报 ErrQueueGateUnassembled，got %v", err)
	}
	if err := QueueEditGate(false, true, QueueGateErrors{Closed: closedErr}); !errors.Is(err, ErrQueueGateUnassembled) {
		t.Fatalf("排空轴缺错误值也要响亮失败，got %v", err)
	}
}

func TestQueueTargetSession(t *testing.T) {
	if got := QueueTargetSession("", "view-1"); got != "view-1" {
		t.Fatalf("空 sessionID 应按视图会话，got %q", got)
	}
	if got := QueueTargetSession("  s1 ", "view-1"); got != "s1" {
		t.Fatalf("sessionID 应先 trim，got %q", got)
	}
	if got := QueueTargetSession("   ", "view-1"); got != "view-1" {
		t.Fatalf("全空白也应按视图会话，got %q", got)
	}
}

func TestQueueTargetError(t *testing.T) {
	err := QueueTargetError("view-1", "view-1", false, false)
	if !errors.Is(err, ErrQueueNotRunning) || !strings.Contains(err.Error(), "session \"view-1\"") {
		t.Fatalf("视图会话未注册应报 NotRunning 且带会话 ID，got %v", err)
	}
	err = QueueTargetError("s2", "view-1", false, false)
	if !errors.Is(err, ErrQueueSessionNotFound) || !strings.Contains(err.Error(), "session \"s2\"") {
		t.Fatalf("未知会话应报 SessionNotFound，got %v", err)
	}
	err = QueueTargetError("s3", "view-1", true, false)
	if !errors.Is(err, ErrQueueNotRunning) {
		t.Fatalf("没有回合在跑应报 NotRunning，got %v", err)
	}
	if err := QueueTargetError("s3", "view-1", true, true); err != nil {
		t.Fatalf("运行中应放行，got %v", err)
	}
}

func TestQueueIndexErrorsKeepTextAndIdentity(t *testing.T) {
	reorder := ReorderIndexError(2, 5)
	if !errors.Is(reorder, ErrQueueIndexOutOfRange) || reorder.Error() != "queued input index out of range: from=2 to=5" {
		t.Fatalf("调换越界文案必须保留，got %q", reorder.Error())
	}
	recall := RecallIndexError(7)
	if !errors.Is(recall, ErrQueueIndexOutOfRange) || recall.Error() != "queued input index out of range: index=7" {
		t.Fatalf("撤回越界文案必须保留，got %q", recall.Error())
	}
}
