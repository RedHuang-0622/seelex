package seelebridge

import (
	"context"
	"strings"
	"testing"
)

// TestUnboundSessionContextStoreFailsCompactionPush 钉住"未绑定"这一状态在装配层
// 推帧那一跳的**确切后果**：PushCompactionFrame 直接报
// `compaction index: 会话上下文存储未绑定（压缩栈不可用）`。
//
// 为什么要有这条用例：2026-10-04 现场那一帧（归档在会话的 big_tool_result 里）
// 的 readback.note 写的就是这句话，而帧正文却把它读成了"压缩摘要开关未开启"——
// 归因错位的根子在这条错误路径上（同一事实两处换口径，见 compaction_frame.go）。
// 错误文案因此是被外部证据读到的接口：改它就得同时改现场读帧的人。
//
// 它同时是"装配层 store 依赖"的证据面：控制器路径（runtimeCompactStacks）在 store
// 为空时**退回内存栈**、装配层这条路径**硬失败**——两条路径对同一个状态语义不一致，
// 所以任何让会话"有引擎 bundle 却没有 store"的接线缺口（新建会话、切项目另起的
// 会话）都会以这条错误显形，而不是安静地少写一帧。
func TestUnboundSessionContextStoreFailsCompactionPush(t *testing.T) {
	fixture := newCompactionSwitchFixture(t, true)
	// 生产里"未绑定"由 AttachSessionContextStore(nil) 造成（LeaveSession 的解绑，
	// 或某条让会话成为当前会话却从未挂接的路径）。
	fixture.runtime.AttachSessionContextStore(nil)

	if _, err := fixture.runtime.ReadbackCompactionSummary(context.Background(), fixture.session, CompactionFrameRequest{}); err == nil {
		t.Fatal("未绑定时读数应报错（压缩栈不可用），实际成功")
	} else if !strings.Contains(err.Error(), "会话上下文存储未绑定") {
		t.Fatalf("读数失败原因 = %q，want 含『会话上下文存储未绑定』", err.Error())
	}
	_, err := fixture.runtime.PushCompactionFrame(context.Background(), fixture.session, CompactionFrameRequest{
		Overflow:      compactionSwitchOverflow(),
		ReplayHistory: compactionSwitchReplayHistory(),
	})
	if err == nil {
		t.Fatal("未绑定应让推帧失败，实际成功——那这一帧就没有可读回的 segment_id 却装作推上了")
	}
	if !strings.Contains(err.Error(), "会话上下文存储未绑定") {
		t.Fatalf("推帧失败原因 = %q，want 含『会话上下文存储未绑定』（现场帧引用的就是这句）", err.Error())
	}
}
