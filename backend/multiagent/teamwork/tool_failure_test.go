package teamwork

import (
	"errors"
	"strings"
	"testing"
)

func TestWorkItemToolFailureTrackerCountsOnlyConsecutiveExplicitErrors(t *testing.T) {
	policy := ToolFailurePolicy{Enabled: true, Threshold: 3}
	tracker := NewWorkItemToolFailureTracker("wi-1", policy)
	if tracker.Observe("read_file", nil) {
		t.Fatal("成功调用不得触发停止")
	}
	if tracker.Observe("bash", errors.New("exit status 1")) {
		t.Fatal("第一次失败不应触发停止")
	}
	if tracker.Observe("write_file", nil) {
		t.Fatal("任何成功调用都必须清零连续计数")
	}
	if tracker.ConsecutiveFailures() != 0 {
		t.Fatalf("成功之后连续失败计数应清零，得到 %d", tracker.ConsecutiveFailures())
	}
	tracker.Observe("bash", errors.New("timeout"))
	tracker.Observe("bash", errors.New("permission denied"))
	if !tracker.Observe("bash", errors.New("exit status 2")) {
		t.Fatal("threshold=3 时第三次失败才触发")
	}
	if !tracker.Stopped() {
		t.Fatal("达到阈值必须停止当前 worker")
	}
	err := tracker.Error()
	var typed *WorkItemToolFailureError
	if !errors.As(err, &typed) {
		t.Fatalf("必须返回结构化错误，得到 %T %v", err, err)
	}
	if typed.WorkItemID != "wi-1" || typed.Threshold != 3 || typed.Tool != "bash" {
		t.Fatalf("错误必须带 item/threshold/tool: %+v", typed)
	}
	if !strings.Contains(err.Error(), "team_message") {
		t.Fatalf("返回 leader 的提示必须指向 team_message: %v", err)
	}
}

func TestWorkItemToolFailureTrackerDisabledIsZeroBehavior(t *testing.T) {
	tracker := NewWorkItemToolFailureTracker("wi-1", ToolFailurePolicy{Enabled: false, Threshold: 1})
	for index := 0; index < 10; index++ {
		if tracker.Observe("bash", errors.New("fail")) {
			t.Fatal("关闭策略时不得停止 worker")
		}
	}
	if tracker.ConsecutiveFailures() != 0 || tracker.Stopped() || tracker.Error() != nil {
		t.Fatalf("关闭策略必须零计数零错误: %+v", tracker)
	}
}

func TestWorkItemToolFailureErrorKeepsArtifacts(t *testing.T) {
	tracker := NewWorkItemToolFailureTracker("wi-2", ToolFailurePolicy{Enabled: true, Threshold: 1})
	tracker.Observe("bash", errors.New("exit status 7"))
	err := tracker.Error()
	for _, contract := range []string{"worktree", "Session", "保留", "不自动"} {
		if !strings.Contains(err.Error(), contract) {
			t.Fatalf("错误必须明确现场保留且不自动回滚，缺 %q: %v", contract, err)
		}
	}
}
