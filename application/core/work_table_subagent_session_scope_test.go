package core

// E 靶场：子代理行的**归属会话**必须来自"发起 fork 的那个会话"，不是"正在被
// 同步的那个会话"。
//
// 现场症状（用户报告）：别的会话的工作表格里出现本会话的子代理行；反过来本会话
// 「仅本会话」筛不到自己的子代理。根因在同步路径：子代理树是**进程级**的一张树
// （fork 的子代理节点没有归属主会话标记），而 syncTasksFromSourcesFor 把整棵树
// 投影进"被同步会话"的 task scope——视图在 B 时同步 A，就把 B 的子代理写进 A 的
// 分区（还顺带把 A 的行贴上 B 的会话号）。前端按 `row.session_id` 做会话轴
// （「仅本会话」与「实发」都取它），于是行落在谁的表格里完全取决于"谁触发了同步"。
//
// 目标约束（会话粒度）：
//   - 归属 A 的子代理只进 A 的 scope / 台账，归属 B 的只进 B 的；
//   - 视图会话（活跃注册表）同步时同样只收自己的行；
//   - 后台会话同步时不再把视图会话（或任何别的会话）的子代理写进它的分区。

import (
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// subagentSessionScopeFixture 造一个视图会话在 session-a 的服务，并把"进程级
// 子代理树"塞进视图镜像（生产里它来自 Engine.SubAgentTree()，含全部会话的 fork）。
func subagentSessionScopeFixture(t *testing.T) (*fakeRuntime, *Service) {
	t.Helper()
	runtime := &fakeRuntime{currentTaskSession: "session-a"}
	service := newTestService(t, &fakeEngine{sessionID: "session-a"}, withTestRuntime(runtime))
	service.ViewMu.Lock()
	service.Core.Snapshot.Session = SessionState{ID: "session-a"}
	service.Core.Snapshot.Runtime.SubAgentTree = []dto.SubAgentTreeNode{
		{ID: "main", Status: dto.SubAgentRunning},
		{
			ID: "node-a", Goal: "A 的子代理", Status: dto.SubAgentRunning,
			SessionID: "sub-node-a", MainSessionID: "session-a",
		},
		{
			ID: "node-b", Goal: "B 的子代理", Status: dto.SubAgentRunning,
			SessionID: "sub-node-b", MainSessionID: "session-b",
		},
	}
	service.ViewMu.Unlock()
	return runtime, service
}

// 视图路径：活跃会话同步只收自己的子代理行，别的会话的行不得进实时注册表。
func TestSubagentRowsStayOutOfActiveRegistryOfOtherSessions(t *testing.T) {
	runtime, service := subagentSessionScopeFixture(t)

	service.syncTasksFromSources()

	rows := runtime.TaskSnapshotFor("session-a")
	if !hasTaskKey(rows, "subagent:node-a") {
		t.Fatalf("活跃会话丢了自己的子代理行：%+v", rows)
	}
	if hasTaskKey(rows, "subagent:node-b") {
		t.Fatalf("跨会话污染：B 的子代理行进进了 A 的实时注册表（视图会话的行会被前端当成本会话）：%+v", rows)
	}
	// 台账（全局读面，前端数据源）同样只认归属：A 的行走 A 的会话号——
	// 缺键/错键的行会被前端 rowBelongsToViewSession 的空值兜底判成"本会话"。
	ledger := runtime.TaskSnapshot()
	if !hasTaskKey(ledger, "subagent:node-a") {
		t.Fatalf("台账缺 A 的子代理行：%+v", ledger)
	}
	for _, record := range ledger {
		if record.Key != "subagent:node-a" {
			continue
		}
		if record.SessionID != "session-a" {
			t.Fatalf("子代理行归属会话 = %q, want session-a（ledger=%+v）", record.SessionID, ledger)
		}
	}
}

// 后台路径：为 session-a 后台同步时，树里**别的会话**的子代理不得写进 A 的分区。
func TestBackgroundSyncKeepsForeignSubagentRowsOut(t *testing.T) {
	runtime, service := subagentSessionScopeFixture(t)

	// 视图切到 B：A 变成后台会话（它自己的 plan 事件走 For 路径）。
	service.ViewMu.Lock()
	service.Core.Snapshot.Session = SessionState{ID: "session-b"}
	service.ViewMu.Unlock()
	runtime.currentTaskSession = "session-b"

	service.syncTasksFromSourcesFor("session-a")

	background := runtime.TaskSnapshotFor("session-a")
	if hasTaskKey(background, "subagent:node-b") {
		t.Fatalf("跨会话污染：B 的子代理行被同步进了 A 的分区（%+v）", background)
	}
	if !hasTaskKey(background, "subagent:node-a") {
		t.Fatalf("A 的后台同步丢了自己的子代理行（%+v）", background)
	}
}

// 归位后的行必须带上归属会话键（前端会话轴的唯一数据源）；缺键的行会被前端
// 当作"本会话"（rowBelongsToViewSession 的空值兜底），于是每个会话都能看到它。
func TestSubagentWorkItemCarriesOwningSession(t *testing.T) {
	runtime, service := subagentSessionScopeFixture(t)

	service.syncTasksFromSources()
	rows := buildWorkTable(nil, runtime.TaskSnapshot(), nil, nil)
	found := false
	for _, row := range rows {
		if row.ID != "subagent:node-a" {
			continue
		}
		found = true
		if strings.TrimSpace(row.SessionID) != "session-a" {
			t.Fatalf("子代理行归属会话 = %q, want session-a", row.SessionID)
		}
	}
	if !found {
		t.Fatalf("工作表格缺 subagent:node-a 行：%+v", rows)
	}
}

func hasTaskKey(records []dto.TaskRecord, key string) bool {
	for _, record := range records {
		if record.Key == key {
			return true
		}
	}
	return false
}
