package sessionstore

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

// TestPushCompactBridgesOutsideStoreLock 钉住 §2.8 的两条性质：
//
//  1. compact 通道落盘（bridge）不得在 s.mu 内进行 —— 否则一次写盘就把本会话
//     的全部纯读（Snapshot/SystemPrompt 等）按在磁盘上。用例让纯读在整段
//     PushCompact 期间持续发生（-race 下同时校验无数据竞争）。
//  2. 落盘顺序必须仍等于压栈顺序：帧靠 PrevSegmentID 成链，通道会拒绝乱序帧，
//     bridge 移出 s.mu 后这件事由 bridgeMu 保证（本用例断言通道帧序 = 压栈序）。
func TestPushCompactBridgesOutsideStoreLock(t *testing.T) {
	router := newTestRouter(t)
	const projectID, sessionID = "project-compact-lock", "session-compact-lock"
	const frameCount = 8
	events := make([]Event, 0, frameCount)
	for index := 1; index <= frameCount; index++ {
		events = append(events, Event{
			Role: "user", Content: fmt.Sprintf("q-%d", index), MessageID: fmt.Sprintf("u%d", index),
		})
	}
	if err := router.SaveCommitWorkspace(projectID, sessionID, Commit{Events: events}); err != nil {
		t.Fatal(err)
	}
	// 前置：会话目录必须已是会话存储布局（否则 compact 通道的桥接会静默
	// 早退成 no-op，用例失去区分度）。
	repository, ok := router.jsonRepository()
	if !ok {
		t.Fatal("router 不是 JSON 会话存储布局")
	}
	if !repository.active(Key{ProjectID: projectID, SessionID: sessionID}) {
		t.Fatal("会话目录不是会话存储布局，compact 桥接会早退，用例失去意义")
	}

	store := NewSessionContextStore(router, sessionID)
	store.SetWorkspaceResolver(func() string { return projectID })
	if err := store.Load(context.Background()); err != nil {
		t.Fatal(err)
	}

	// 并发纯读：压帧期间读面必须可用（修前桥接持 s.mu 写锁，读面被写盘阻塞）。
	stop := make(chan struct{})
	var readers sync.WaitGroup
	readers.Add(1)
	go func() {
		defer readers.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_ = store.Snapshot()
				_ = store.GoalAuditSnapshot()
			}
		}
	}()

	previous := ""
	for index := 0; index < frameCount; index++ {
		frame := CompactFrame{
			SegmentID:    fmt.Sprintf("seg-%d", index+1),
			From:         0,
			To:           index + 1,
			EventFrom:    1,
			EventTo:      uint64(index + 1),
			Summary:      "s",
			CompressedAt: time.Now(),
		}
		if index > 0 {
			frame.PrevSegmentID = previous
		}
		if err := store.PushCompact(frame); err != nil {
			t.Fatalf("push compact frame %d: %v", index, err)
		}
		previous = frame.SegmentID
	}
	close(stop)
	readers.Wait()

	frames, handled, err := router.CompactFramesWorkspace(projectID, sessionID)
	if err != nil || !handled {
		t.Fatalf("compact 通道 = handled=%v err=%v", handled, err)
	}
	if len(frames) != frameCount {
		t.Fatalf("通道帧数 = %d, want %d", len(frames), frameCount)
	}
	for index, frame := range frames {
		want := fmt.Sprintf("seg-%d", index+1)
		if frame.SegmentID != want {
			t.Fatalf("通道第 %d 帧 = %q, want %q（落盘序必须 = 压栈序）",
				index, frame.SegmentID, want)
		}
	}
}
