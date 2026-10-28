package session

// conclusion_test.go 是子代理结论投影的**域级**用例（P21-14）：正反两向必须互为逆，
// 事件形状（时间基序号 / 终态映射 / 会话定位挂主会话）与"跳过读不懂的条目"的口径都钉在这里。

import (
	"encoding/json"
	"testing"
	"time"

	frameworkevent "github.com/RedHuang-0622/Seele/event"

	"github.com/RedHuang-0622/seelex/store"
	storeworkunit "github.com/RedHuang-0622/seelex/store/workunit"
)

func conclusionRecord() storeworkunit.Record {
	return storeworkunit.Record{
		SchemaVersion: storeworkunit.SchemaVersion,
		NodeID:        "n1", SessionID: "node-abc", Status: "failed",
		Goal: "实现渲染件", Summary: "半途而废", Error: "provider 402", ResultJSON: json.RawMessage(`{"k":"v"}`),
	}
}

// TestConclusionEventShape 钉住事件形状：**结论跟随 mainagent**（定位挂主会话）、
// 节点号进 Scope、序号取时间基、终态 done/failed 映射到框架事件状态。
func TestConclusionEventShape(t *testing.T) {
	at := time.Unix(1700000000, 123)
	event := ConclusionEvent("sess-main", conclusionRecord(), at)

	if event.Source != ConclusionEventSource || event.Type != frameworkevent.TypeLifecycle {
		t.Fatalf("source/type = %q/%q", event.Source, event.Type)
	}
	if event.Sequence != uint64(at.UnixNano()) || !event.OccurredAt.Equal(at) {
		t.Fatalf("序号必须取时间基（按会话可排序）：seq=%d at=%v", event.Sequence, event.OccurredAt)
	}
	if event.Scope.NodeID != "n1" {
		t.Fatalf("Scope.NodeID = %q, want n1（读侧据此重建树节点）", event.Scope.NodeID)
	}
	if event.Status != frameworkevent.StatusFailed {
		t.Fatalf("带失败状态的记录应投影为 failed 事件，得到 %q", event.Status)
	}
	if len(event.Locations) != 1 || event.Locations[0].Kind != "agent.runtime" ||
		event.Locations[0].IDs["session_id"] != "sess-main" {
		t.Fatalf("结论必须落在**主会话**的事件库里：%#v", event.Locations)
	}

	done := ConclusionEvent("sess-main", storeworkunit.Record{NodeID: "n2", Status: "done"}, at)
	if done.Status != frameworkevent.StatusCompleted {
		t.Fatalf("done 应投影为 completed，得到 %q", done.Status)
	}
	// 认不出的状态词按"非失败"处理（与既有 nodeStateOfRecord 兜底一致）：不静默判死。
	unknown := ConclusionEvent("sess-main", storeworkunit.Record{NodeID: "n3", Status: "??"}, at)
	if unknown.Status != frameworkevent.StatusCompleted {
		t.Fatalf("未知状态词应投影为 completed（不判死），得到 %q", unknown.Status)
	}
}

// TestConclusionsRoundTripIsLossless 是**正反两向互为逆**的钉子：投影成事件、序列化、
// 再从事件库条目读回来，必须得到同一批记录（恢复链正是靠这条往返重建树节点）。
func TestConclusionsRoundTripIsLossless(t *testing.T) {
	record := conclusionRecord()
	event := ConclusionEvent("sess-main", record, time.Unix(1700000000, 0))
	payload, err := json.Marshal(store.EventLogEntry{Seq: event.Sequence, Payload: mustJSON(t, event)})
	if err != nil {
		t.Fatal(err)
	}
	entry := store.EventLogEntry{}
	if err := json.Unmarshal(payload, &entry); err != nil {
		t.Fatal(err)
	}

	records := ConclusionsFromEvents([]store.EventLogEntry{entry})
	if len(records) != 1 {
		t.Fatalf("读回 %d 条, want 1", len(records))
	}
	got := records[0]
	if got.NodeID != record.NodeID || got.SessionID != record.SessionID || got.Status != record.Status ||
		got.Goal != record.Goal || got.Summary != record.Summary || got.Error != record.Error ||
		string(got.ResultJSON) != string(record.ResultJSON) {
		t.Fatalf("往返丢字段：\nwant %+v\ngot  %+v", record, got)
	}
	if got.SchemaVersion != storeworkunit.SchemaVersion {
		t.Fatalf("重建的记录必须带当前 schema 版本：%d", got.SchemaVersion)
	}
}

// TestConclusionsFromEventsSkipsForeignAndBrokenEntries：事件库是 A/B 类事实共用的追加日志，
// 恢复链只认自己那一族——不是事件 / 不是本 Source / 载荷坏 / 没带节点号都必须**跳过而不报错**。
func TestConclusionsFromEventsSkipsForeignAndBrokenEntries(t *testing.T) {
	good := ConclusionEvent("sess-main", conclusionRecord(), time.Unix(1700000000, 0))
	entries := []store.EventLogEntry{
		{Seq: 1, Payload: json.RawMessage(`{"not":"an event"}`)},
		{Seq: 2, Payload: json.RawMessage(`not json at all`)},
		{Seq: 3, Payload: mustJSON(t, frameworkevent.Event{Source: "workplan.runner", Content: []byte(`{}`)})},
		{Seq: 4, Payload: mustJSON(t, frameworkevent.Event{Source: ConclusionEventSource})},
		// Content 不是对象（合法 JSON，但解不成结论）⇒ 跳过。
		{Seq: 5, Payload: json.RawMessage(`{"source":"` + ConclusionEventSource + `","content":[1,2]}`)},
		{Seq: 6, Payload: mustJSON(t, frameworkevent.Event{Source: ConclusionEventSource, Content: []byte(`{"status":"done"}`)})},
		{Seq: 7, Payload: mustJSON(t, good)},
	}
	records := ConclusionsFromEvents(entries)
	if len(records) != 1 || records[0].NodeID != "n1" {
		t.Fatalf("只应读出一条结论记录，得到 %+v", records)
	}
}

// TestTakeoverPredicateOnlyTakesOverDeadOwners：接管判据是"记录的主人已不在本进程"——
// 还活着的节点不许被记录快照覆盖成中断；没有内存判据（nil 读数）时按记录恢复。
func TestTakeoverPredicateOnlyTakesOverDeadOwners(t *testing.T) {
	live := map[string]bool{"n1": true}
	predicate := TakeoverPredicate(func(nodeID string) bool { return live[nodeID] })
	if predicate(storeworkunit.Record{NodeID: "n1"}) {
		t.Fatal("本进程仍在跑的节点不得被记录接管（否则会被覆盖成 interrupted）")
	}
	if !predicate(storeworkunit.Record{NodeID: "n2"}) {
		t.Fatal("主人不在本进程的记录必须按崩溃口径接管")
	}
	if !TakeoverPredicate(nil)(storeworkunit.Record{NodeID: "n1"}) {
		t.Fatal("没有内存判据时按记录恢复（既有语义）")
	}
}

func mustJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return raw
}
