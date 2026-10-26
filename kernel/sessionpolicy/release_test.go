package switching

import "testing"

// 本文件钉释放面的两条判据（M5-R6-release-surface 接收者转移）：
// 「会话忙不忙」的四个轴，以及「受保护集」的四条来源。

func TestBusyFollowsFourAxes(t *testing.T) {
	for _, tc := range []struct {
		name  string
		state BusyState
		want  bool
	}{
		{"全空闲", BusyState{}, false},
		{"待审批", BusyState{PendingApprovals: 1}, true},
		{"回合运行中", BusyState{Running: true}, true},
		{"排队输入", BusyState{QueuedCount: 2}, true},
		{"在飞请求", BusyState{PendingRequests: 1}, true},
		{"恰好一个排队也算忙", BusyState{QueuedCount: 1}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Busy(tc.state); got != tc.want {
				t.Fatalf("Busy(%+v) = %v, want %v", tc.state, got, tc.want)
			}
		})
	}
}

func TestProtectedCoversViewRestoringAndBusy(t *testing.T) {
	base := ProtectedInput{SessionID: "s1", ViewSessionID: "view-1"}
	if Protected(base) {
		t.Fatal("空闲的非视图会话不该受保护（否则 LRU 永远放不掉）")
	}
	for _, tc := range []struct {
		name  string
		input ProtectedInput
	}{
		{"空会话 ID", ProtectedInput{SessionID: "", ViewSessionID: "view-1"}},
		{"当前视图会话", ProtectedInput{SessionID: "view-1", ViewSessionID: "view-1"}},
		{"正在恢复", ProtectedInput{SessionID: "s1", ViewSessionID: "view-1", Restoring: true}},
		{"待审批", ProtectedInput{SessionID: "s1", ViewSessionID: "view-1", State: BusyState{PendingApprovals: 1}}},
		{"运行中", ProtectedInput{SessionID: "s1", ViewSessionID: "view-1", State: BusyState{Running: true}}},
		{"排队", ProtectedInput{SessionID: "s1", ViewSessionID: "view-1", State: BusyState{QueuedCount: 1}}},
		{"在飞", ProtectedInput{SessionID: "s1", ViewSessionID: "view-1", State: BusyState{PendingRequests: 1}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !Protected(tc.input) {
				t.Fatalf("%s 应判为受保护：%+v", tc.name, tc.input)
			}
		})
	}
}

// TestProtectedEmptyViewSessionIsNotSpecial：视图会话 ID 为空时不该把任意会话都判成视图会话。
func TestProtectedEmptyViewSessionIsNotSpecial(t *testing.T) {
	if Protected(ProtectedInput{SessionID: "s1", ViewSessionID: ""}) {
		t.Fatal("视图 ID 为空时只有空会话 ID 会被保护")
	}
	if Protected(ProtectedInput{SessionID: "", ViewSessionID: ""}) != true {
		t.Fatal("空会话 ID 仍应受保护")
	}
}
