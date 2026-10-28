package integration

import (
	"fmt"
	"testing"

	assembly "github.com/RedHuang-0622/seelex/application/assembly"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	task_context "github.com/RedHuang-0622/seelex/kernel/task"
)

// turn_status_single_word_test.go — 「一次回合（请求）现在处在哪一步」这件事，
// 仓库里曾经有**两套词表**：
//
//   - 可见面：`model.TaskStatus`（旧名；progressing | completed | needs_user_decision | …），
//     住在 `Snapshot.Task.Status`，GUI/TUI 直接渲染；
//   - 执行面：`task_context.Status*`（running | completed | …），住在
//     `TaskExecutionState.Status` 并**落盘**在 `TaskContextProjection.Status`。
//
// 两套只在"进行中"这个词上分叉（progressing vs running），于是两者之间长出了一处
// 手写映射（`task_context_state.go` 的旧名 `model.TaskStatus("running")` 那两行）。
// 分叉的代价不是"多写一行"，而是**改一处忘一处没人报**：`IsContinuableStatus`
// 按执行面的词判，谁要是拿可见面的词去喂它，永远判否。
//
// 这一条用例把判据钉在**同格一个词**上：同一个在飞回合，可见面与存档面必须说同一个词。

func TestTurnStatusSpeaksOneWordOnSnapshotAndArchive(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	components := assembly.ComponentsFor(service)

	service.ViewMu.Lock()
	service.Core.Snapshot.Chat = model.ChatState{Running: true, RequestID: "task-1"}
	components.Execution.Tasks.BeginTask("task-1", "audit the repository", "high", nil, model.TaskCheckpoint{})
	components.Execution.Tasks.SetTaskStateLocked("task-1", model.TurnProgressing, "Task is in progress.")
	service.ViewMu.Unlock()

	// 可见面走 `Snapshot()`（自取读锁）；存档面走 `TaskProjectionLocked`（要调用方持读锁）。
	// **不要**在持有 `ViewMu.RLock()` 时调 `Snapshot()`：`sync.RWMutex` 不可重入——目录刷新
	// worker 在 `Coordinator.publishCatalogMirror` 里排队等写锁时，嵌套的 RLock 会永久阻塞
	// （P20 现场：3 并发跑本包，第一轮就挂死在这个嵌套上）。
	visible := components.Session.Snapshot.Snapshot().Task
	service.ViewMu.RLock()
	projection := components.Execution.Tasks.TaskProjectionLocked(service.Core.Snapshot.Session.ID)
	service.ViewMu.RUnlock()

	if visible == nil {
		t.Fatal("在飞回合必须有可见状态面（Snapshot.Task）")
	}
	if projection == nil {
		t.Fatal("在飞回合必须有存档投影（TaskContextProjection）")
	}
	// fmt.Sprint 取的是各自的"对外词"：枚举走 String()，字符串字段就是它本身。
	if got, want := fmt.Sprint(projection.Status), fmt.Sprint(visible.Status); got != want {
		t.Fatalf("同一个回合状态说了两个词：存档面 %q、可见面 %q —— 这就是平行词表，"+
			"两处只差一个手写映射，没有编译器看得见", got, want)
	}
}

// 落盘面读回：合并前的存档里"进行中"写的是 "running"，合并后写 "progressing"。
// 老记录必须照样读得回来（读旧文件不许炸），而且"进行中"不许被读成某个终态、
// 认不得的词也不许。
func TestTurnStatusOfRecordKeepsLegacyRunningAndNeverFoldsToTerminal(t *testing.T) {
	cases := []struct {
		word string
		want string
	}{
		{"progressing", model.TurnProgressing.String()},
		{"running", model.TurnProgressing.String()}, // 合并前那一格的词（老存档）
		{"idle", model.TurnIdle.String()},
		{"completed", model.TurnCompleted.String()},
		{"failed", model.TurnFailed.String()},
		{"", model.TurnUnknown.String()},              // 空 = 老记录没这个字段
		{"half-exploded", model.TurnUnknown.String()}, // 认不得 = 说认不得，不折成某个已知状态
	}
	for _, testCase := range cases {
		if got := task_context.TurnStatusOfRecord(testCase.word).String(); got != testCase.want {
			t.Errorf("TurnStatusOfRecord(%q) = %q，期望 %q", testCase.word, got, testCase.want)
		}
	}
	if got := task_context.TurnStatusOfRecord("half-exploded"); got == model.TurnCompleted || got == model.TurnFailed || got == model.TurnInterrupted {
		t.Fatalf("认不得的落盘词被折成了终态：%v", got)
	}
}
