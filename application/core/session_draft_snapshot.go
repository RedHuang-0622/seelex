package core

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/RedHuang-0622/seelex/contract/protocol"
)

// draftSnapshotInterval 是长任务把内存 transcript 后缀写成可恢复草稿的最小
// 周期。短回合不会产生任何 draft 文件 I/O。
var draftSnapshotInterval = 5 * time.Second

// startDraftSnapshotLoop 为一次运行中的回合维护 draft 双指针：
// left = 已写入草稿尾的 seq，right = 当前最新内存 transcript seq。
// 高频消息/工具动作只推进内存 right；定时器才尝试把 (left,right] 写盘。
func (service *Service) startDraftSnapshotLoop(ctx context.Context, sessionID, requestID string) func() {
	if service == nil || service.components.sessions == nil || requestID == "" {
		return func() {}
	}
	left, found := service.draftSnapshotBaseSeq(sessionID, requestID)
	if !found {
		return func() {}
	}
	loopCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(draftSnapshotInterval)
		defer ticker.Stop()
		for {
			select {
			case <-loopCtx.Done():
				return
			case <-ticker.C:
				next, supported, err := service.persistDraftSnapshotSuffix(sessionID, left)
				if err != nil {
					log.Printf("[draft-snapshot] session=%s request=%s: %v", sessionID, requestID, err)
					continue
				}
				if !supported {
					return
				}
				left = next
			}
		}
	}()

	var once sync.Once
	return func() {
		once.Do(func() {
			cancel()
			<-done
		})
	}
}

// draftSnapshotBaseSeq 找到本回合首条 transcript 事件，返回其前一个 seq。
// 这样第一份快照只包含本轮内容，不会把冷加载已有的尾部事件重复写盘。
func (service *Service) draftSnapshotBaseSeq(sessionID, requestID string) (uint64, bool) {
	for _, event := range service.transcriptSnapshot(sessionID) {
		if event.TaskID == requestID && event.Seq > 0 {
			return event.Seq - 1, true
		}
	}
	return 0, false
}

// persistDraftSnapshotSuffix 把当前 transcript 中 seq > left 的稳定后缀追加
// 到草稿尾。返回推进后的左指针；没有新事件时原样返回。
func (service *Service) persistDraftSnapshotSuffix(sessionID string, left uint64) (uint64, bool, error) {
	events := service.transcriptSnapshot(sessionID)
	suffix := make([]model.TranscriptEvent, 0)
	right := left
	for _, event := range events {
		if event.Seq <= left {
			continue
		}
		suffix = append(suffix, event)
		if event.Seq > right {
			right = event.Seq
		}
	}
	if len(suffix) == 0 {
		return left, true, nil
	}
	location := service.components.sessions.LocateSession(sessionID)
	_, supported, err := service.components.sessions.AppendDraftSnapshot(location, sessionID, suffix)
	if err != nil || !supported {
		return left, supported, err
	}
	// appended=0 也推进左指针：这些 seq 已经由一次中途提交或同一份草稿
	// 覆盖，重写只会制造重复检查。
	return right, true, nil
}

func (service *Service) transcriptSnapshot(sessionID string) []model.TranscriptEvent {
	events := service.components.tasks.TranscriptFor(sessionID)
	return append([]model.TranscriptEvent(nil), events...)
}
