package switching

import (
	"errors"
	"testing"

	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
)

// 本文件钉 fork 的门控次序与两个纯助手（M5-R6-fork 接收者转移）。

func TestForkGateOrderAndUnassembled(t *testing.T) {
	closedErr := errors.New("application is shut down")
	drainingErr := errors.New("draining")
	runningErr := errors.New("chat is already running")
	gate := ForkGate{ErrClosed: closedErr, ErrDraining: drainingErr, ErrRunning: runningErr}

	if err := gate.Reject(); err != nil {
		t.Fatalf("三轴都不成立应放行，got %v", err)
	}
	if err := (ForkGate{Closed: true, ErrClosed: closedErr}).Reject(); !errors.Is(err, closedErr) {
		t.Fatalf("关闭应返回宿主错误值，got %v", err)
	}
	if err := (ForkGate{Draining: true, ErrDraining: drainingErr}).Reject(); !errors.Is(err, drainingErr) {
		t.Fatalf("排空应返回宿主错误值，got %v", err)
	}
	if err := (ForkGate{Running: true, ErrRunning: runningErr}).Reject(); !errors.Is(err, runningErr) {
		t.Fatalf("父会话在跑应返回宿主错误值，got %v", err)
	}
	// 次序：三者同时成立时拿到的必须是"关闭"那条。
	both := ForkGate{Closed: true, Draining: true, Running: true,
		ErrClosed: closedErr, ErrDraining: drainingErr, ErrRunning: runningErr}
	if err := both.Reject(); !errors.Is(err, closedErr) {
		t.Fatalf("关闭优先，got %v", err)
	}
	// 装配缺陷必须响亮失败，而不是静默放行 fork。
	if err := (ForkGate{Running: true}).Reject(); !errors.Is(err, ErrForkGateUnassembled) {
		t.Fatalf("缺错误值应报 ErrForkGateUnassembled，got %v", err)
	}
}

func TestMaxTranscriptEventSeq(t *testing.T) {
	if got := MaxTranscriptEventSeq(nil); got != 0 {
		t.Fatalf("空流应为 0，got %d", got)
	}
	events := []model.TranscriptEvent{{Seq: 3}, {Seq: 17}, {Seq: 9}}
	if got := MaxTranscriptEventSeq(events); got != 17 {
		t.Fatalf("应取最大 Seq，got %d", got)
	}
}

func TestDeepCopyRecordBreaksParentChildAliasing(t *testing.T) {
	tool := &model.ToolCall{ID: "call-1"}
	record := model.SessionRecord{
		Conversation: model.ConversationRecord{Messages: []model.Message{{ID: "m1", Tool: tool}}},
		Tasks:        []dto.TaskRecord{{ID: "t1"}},
		Checkpoints:  []model.TaskCheckpoint{{Version: 1}},
		ToolResults:  []model.ToolResultRef{{Ref: "tr-1"}},
		PlanStack:    []model.SessionPlanFrame{{ID: "f1"}},
		Execution:    model.SessionExecutionRecord{ReadFiles: []model.ReadFileRef{{Path: "a"}}},
	}
	cloned := DeepCopyRecord(record, func(stack []model.SessionPlanFrame) []model.SessionPlanFrame {
		out := make([]model.SessionPlanFrame, len(stack))
		copy(out, stack)
		return out
	})

	// 消息切片与其中 Tool 指针都必须是新对象（改子不影响父）。
	cloned.Conversation.Messages[0].ID = "m2"
	*cloned.Conversation.Messages[0].Tool = model.ToolCall{ID: "call-2"}
	if record.Conversation.Messages[0].ID != "m1" || record.Conversation.Messages[0].Tool.ID != "call-1" {
		t.Fatal("消息与其 Tool 引用不得与父共享")
	}
	// 四个派生切片各自独立。
	cloned.Tasks[0].ID = "t2"
	cloned.Checkpoints[0].Version = 2
	cloned.ToolResults[0].Ref = "tr-2"
	cloned.Execution.ReadFiles[0].Path = "b"
	if record.Tasks[0].ID != "t1" || record.Checkpoints[0].Version != 1 ||
		record.ToolResults[0].Ref != "tr-1" || record.Execution.ReadFiles[0].Path != "a" {
		t.Fatal("Tasks/Checkpoints/ToolResults/ReadFiles 必须新建切片")
	}
	// PlanStack 由宿主注入的克隆器负责（这里验证它被调用）。
	cloned.PlanStack[0].ID = "f2"
	if record.PlanStack[0].ID != "f1" {
		t.Fatal("PlanStack 应交由注入的克隆器深拷贝")
	}
	// 未注入克隆器时保持原值（宿主没装配该能力 = 不假装拷贝过）。
	uncloned := DeepCopyRecord(record, nil)
	if len(uncloned.PlanStack) != 1 {
		t.Fatalf("未注入克隆器时应保留原 PlanStack，got %+v", uncloned.PlanStack)
	}
}
