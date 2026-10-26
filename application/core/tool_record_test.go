package core

// tool_record_test.go — 钉住「工具记录」这一跳（2026-10-15）。
//
// 三件事一起钉：判据（成败 + 原因归并只有一处）、开关（关就是关）、以及**批量落盘**
// （会话粒度、只在提交点写、幂等、每个会话只读回一次）。

import (
	"context"
	"errors"
	"fmt"
	seelsession "github.com/RedHuang-0622/seelex/backend/multiagent/subagent"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"strings"
	"sync"
	"testing"
	"time"
)

// recordingSnapshotStore 是落盘口替身：记下每次批量写，并能吐回一份"旧读数"。
type recordingSnapshotStore struct {
	mu        sync.Mutex
	saves     []dto.ToolRecordSnapshot
	sessions  []string
	loads     int
	persisted *dto.ToolRecordSnapshot
	fail      error
}

func (s *recordingSnapshotStore) SaveToolRecordSnapshot(sessionID string, snapshot dto.ToolRecordSnapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail != nil {
		return s.fail
	}
	s.saves = append(s.saves, snapshot)
	s.sessions = append(s.sessions, sessionID)
	return nil
}

func (s *recordingSnapshotStore) LoadToolRecordSnapshot(string) (dto.ToolRecordSnapshot, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loads++
	if s.persisted == nil {
		return dto.ToolRecordSnapshot{}, false, nil
	}
	return *s.persisted, true, nil
}

func (s *recordingSnapshotStore) savesSnapshot() []dto.ToolRecordSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]dto.ToolRecordSnapshot(nil), s.saves...)
}

func (s *recordingSnapshotStore) loadCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loads
}

// enableToolRecord 打开开关并把落盘口装上（测试里两条装配动作各走一次）。
func enableToolRecord(t *testing.T, service *Service, store *recordingSnapshotStore) {
	t.Helper()
	if _, err := service.SetToolRecordEnabled(true); err != nil {
		t.Fatalf("SetToolRecordEnabled: %v", err)
	}
	if store != nil {
		service.SetToolRecordSnapshotStore(store)
	}
}

func TestToolRecordCountsBothOutcomesAcrossFamilies(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	enableToolRecord(t, service, nil)
	ctx := context.Background()
	// 主代理链的记录落在**视图会话**（handleToolStart/Complete 按 ctx 里的会话号，缺省回落
	// 视图快照会话）；teammate/子代理链按事件自带的主会话路由。两者在真实运行里是同一个
	// 会话，用例因此显式取视图会话号，别自己编一个（编了就会落进另一份累计）。
	sessionID := service.currentViewSessionID()

	service.handleToolStart(ctx, "bash", "tool-ok", `{"command":"ls -la"}`)
	service.handleToolComplete("bash", "tool-ok", "ok", nil, time.Millisecond)
	service.handleToolStart(ctx, "read_file", "tool-bad", `{"path":"nope"}`)
	service.handleToolComplete("read_file", "tool-bad", "", errors.New("read_file: not found"), time.Millisecond)

	service.HandleRoleToolActivity(dto.RoleToolActivity{ID: "role#1", MainSessionID: sessionID, Name: "grep_search", Status: "running"})
	service.HandleRoleToolActivity(dto.RoleToolActivity{
		ID: "role#2", MainSessionID: sessionID, RoleSessionID: "role-impl", Name: "grep_search", Status: "error", Error: "permission denied",
	})
	// teammate 的读数走**阶梯提交**（2026-10-19 方向 C）：先落进角色会话自己的暂存，
	// 这一轮收口时才一次性交给会话累计——所以这里必须显式收口，否则它还没入账。
	frontendOfTestService(service).CloseRoleToolRecordUnit(sessionID, "role-impl")
	service.components.subagent.HandleSubagentToolEvent(seelsession.SubagentToolEvent{ID: "sub#1", NodeID: "node-1", Name: "bash_bg", Status: dto.ToolEventRunning})

	view := service.toolRecords.SessionView(sessionID)
	if !view.Exists {
		t.Fatal("该会话必须有一份累计")
	}
	stats := view.Tools
	if len(stats) != 3 {
		t.Fatalf("三个工具在册（bash 成功一笔 + read_file/grep_search 各失败一笔）：%+v", stats)
	}
	if view.Total != 3 || view.Success != 1 || view.Failure != 2 {
		t.Fatalf("口径不对（running 帧不计；成功也记）：total=%d success=%d failure=%d", view.Total, view.Success, view.Failure)
	}
	for _, stat := range stats {
		switch stat.Tool {
		case "bash":
			if stat.Success != 1 || stat.Failure != 0 {
				t.Fatalf("bash 那笔是成功（工具没报错）：%+v", stat)
			}
		case "read_file":
			if stat.Failure != 1 || len(stat.Reasons) != 1 || stat.Reasons[0].Reason != dto.ToolReasonNotFound {
				t.Fatalf("read_file 的失败原因应归 %s：%+v", dto.ToolReasonNotFound, stat)
			}
		case "grep_search":
			if stat.Failure != 1 || len(stat.Reasons) != 1 || stat.Reasons[0].Reason != dto.ToolReasonPermission {
				t.Fatalf("permission denied 应归 %s：%+v", dto.ToolReasonPermission, stat)
			}
		default:
			t.Fatalf("不该有这个工具：%+v", stat)
		}
	}
}

// 批量落盘：只在提交点写一次；没有再记过就不重写（dirty 门）；写的是整份快照。
func TestToolRecordFlushIsBatchAndIdempotent(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	store := &recordingSnapshotStore{}
	enableToolRecord(t, service, store)
	ctx := context.Background()
	sessionID := service.currentViewSessionID()

	service.handleToolStart(ctx, "bash", "tool-1", `{"command":"ls"}`)
	service.handleToolComplete("bash", "tool-1", "ok", nil, time.Millisecond)
	service.handleToolStart(ctx, "bash", "tool-2", `{"command":"nope"}`)
	service.handleToolComplete("bash", "tool-2", "", errors.New("boom"), time.Millisecond)

	service.FlushToolRecord(sessionID)
	saves := store.savesSnapshot()
	if len(saves) != 1 {
		t.Fatalf("一次批量落盘应只写一行整份快照：%d", len(saves))
	}
	if len(saves[0].Tools) != 1 || saves[0].Tools[0].Success != 1 || saves[0].Tools[0].Failure != 1 {
		t.Fatalf("快照内容不对：%+v", saves[0].Tools)
	}
	// 没有再记过 ⇒ 不重写（避免"每开一次会话重写一遍同样的读数"）。
	service.FlushToolRecord(sessionID)
	if len(store.savesSnapshot()) != 1 {
		t.Fatalf("没有新读数时不得重写：%d", len(store.savesSnapshot()))
	}
	// 再记一笔后再冲 ⇒ 第二行（整份，不是增量）。
	service.handleToolStart(ctx, "bash", "tool-3", `{"command":"ls"}`)
	service.handleToolComplete("bash", "tool-3", "ok", nil, time.Millisecond)
	service.FlushToolRecord(sessionID)
	saves = store.savesSnapshot()
	if len(saves) != 2 || saves[1].Tools[0].Success != 2 || saves[1].Tools[0].Failure != 1 {
		t.Fatalf("第二行必须是整份最新读数：%+v", saves)
	}
}

// 落盘失败不回抛（会话提交不该因为读数写不进去而失败），错误留给看板显示。
func TestToolRecordFlushFailureSurfacesOnBoard(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	store := &recordingSnapshotStore{fail: errors.New("disk full")}
	enableToolRecord(t, service, store)
	service.handleToolStart(context.Background(), "bash", "tool-x", `{"command":"ls"}`)
	service.handleToolComplete("bash", "tool-x", "ok", nil, time.Millisecond)
	service.FlushToolRecord(service.currentViewSessionID())
	if !strings.Contains(service.ToolRecordBoard().LedgerError, "disk full") {
		t.Fatalf("落盘失败必须显示在告警那格：%+v", service.ToolRecordBoard())
	}
}

// 冷启动读回：看板读之前懒加载一次，且**每个会话只读一次**（不每次读盘）。
func TestToolRecordLoadsPersistedReadingOnce(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	persisted := dto.ToolRecordSnapshot{At: time.Now().UTC(), Tools: []dto.ToolRecordStat{
		{Tool: "bash", Success: 7, Failure: 2, Total: 9, Reasons: []dto.ToolRecordReason{{Reason: dto.ToolReasonTimeout, Count: 2}}},
	}}
	store := &recordingSnapshotStore{persisted: &persisted}
	enableToolRecord(t, service, store)

	board := service.ToolRecordBoard()
	if board.Total != 9 || board.Success != 7 || board.Failure != 2 {
		t.Fatalf("看板必须显示落盘读数（重启后接着看）：%+v", board)
	}
	if len(board.Tools) != 1 || board.Tools[0].Reasons[0].Reason != dto.ToolReasonTimeout {
		t.Fatalf("失败原因也要读回：%+v", board.Tools)
	}
	_ = service.ToolRecordBoard()
	if store.loadCount() != 1 {
		t.Fatalf("每个会话只该读回一次：%d", store.loadCount())
	}
}

// 关就是关：开关关着时一个字节都不记、也不落盘。
func TestToolRecordDisabledRecordsNothing(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	store := &recordingSnapshotStore{}
	service.SetToolRecordSnapshotStore(store)
	if applied, err := service.SetToolRecordEnabled(false); err != nil || applied {
		t.Fatalf("关掉开关失败：applied=%v err=%v", applied, err)
	}
	service.handleToolStart(context.Background(), "bash", "tool-y", `{"command":"ls"}`)
	service.handleToolComplete("bash", "tool-y", "ok", nil, time.Millisecond)
	service.FlushToolRecord("sess-off")
	if len(store.savesSnapshot()) != 0 {
		t.Fatalf("关着计数时不得落盘：%+v", store.savesSnapshot())
	}
}

// 命令面（退出码）是**另一条**成败判据：工具没报错，但命令退非零 = 一次失败。
func TestToolCommandOutcomeCountsNonZeroExitAsFailure(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	enableToolRecord(t, service, nil)
	const sessionID = "sess-cmd"
	service.RecordToolCommandOutcome(sessionID, "bash", 0)
	service.RecordToolCommandOutcome(sessionID, "bash", 1)

	view := service.toolRecords.SessionView(sessionID)
	if view.Total != 2 || view.Success != 1 || view.Failure != 1 {
		t.Fatalf("退出码口径：total=%d success=%d failure=%d", view.Total, view.Success, view.Failure)
	}
	stats := view.Tools
	if len(stats[0].Reasons) != 1 || stats[0].Reasons[0].Reason != dto.ToolReasonCommandFailed {
		t.Fatalf("非零退出码归 %s：%+v", dto.ToolReasonCommandFailed, stats[0].Reasons)
	}
}

// 未知/空状态词不记（认不得的词不该被当成成功）。
func TestToolRecordIgnoresUnknownStatusWords(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	enableToolRecord(t, service, nil)
	const sessionID = "sess-unknown"
	for _, status := range []string{"", "unknown", "weird"} {
		service.HandleRoleToolActivity(dto.RoleToolActivity{ID: "r" + status, MainSessionID: sessionID, Name: "bash", Status: status})
	}
	if view := service.toolRecords.SessionView(sessionID); view.Total != 0 {
		t.Fatalf("认不得的状态词不得记：%d", view.Total)
	}
}

// 每会话一份：两个会话的读数互不串（切会话看到的是自己的）。
func TestToolRecordIsPerSession(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	enableToolRecord(t, service, nil)
	for index, sessionID := range []string{"sess-a", "sess-b"} {
		for count := 0; count <= index; count++ {
			service.RecordToolCommandOutcome(sessionID, "bash", 0)
		}
	}
	if view := service.toolRecords.SessionView("sess-a"); view.Total != 1 {
		t.Fatalf("sess-a 只该有 1 笔：%d", view.Total)
	}
	if view := service.toolRecords.SessionView("sess-b"); view.Total != 2 {
		t.Fatalf("sess-b 只该有 2 笔：%d", view.Total)
	}
}

// 每条记录都要有会话归属：没有归属的调用记不进任何会话（别造出"没有主人的读数"）。
func TestToolRecordWithoutSessionIsDropped(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	enableToolRecord(t, service, nil)
	service.RecordToolCommandOutcome("  ", "bash", 1)
	if diagnostics := service.toolRecords.Diagnostics(); diagnostics.SessionCount != 0 {
		t.Fatalf("无会话归属的调用不该建出累计：%d", diagnostics.SessionCount)
	}
}

func TestToolRecordFlushWithoutStoreIsNoop(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	if _, err := service.SetToolRecordEnabled(true); err != nil {
		t.Fatalf("SetToolRecordEnabled: %v", err)
	}
	service.RecordToolCommandOutcome("sess-nostore", "bash", 1)
	service.FlushToolRecord("sess-nostore") // 没装落盘口：空操作，不 panic
	if view := service.toolRecords.SessionView("sess-nostore"); view.Total != 1 {
		t.Fatalf("没装落盘口也要保住内存读数：%d", view.Total)
	}
	_ = fmt.Sprintf("%v", service.ToolRecordBoard())
}
