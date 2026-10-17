package dto

import (
	"encoding/json"
	"strings"
	"testing"
)

// state_enum_test.go — 状态枚举化（第三批）的判据：**枚举是类型，对外词只有一份**。
//
// 为什么要有这一份：把散落的字符串字面量收成"契约里的一处常量"只解决了一半——常量仍是
// 无类型字符串，谁都能拿一个字面量直接跟它比（`record.State == "running"` 照样编译过）。
// 枚举化后，状态的**类型**把这件事钉死：比较只能发生在枚举之间，写错词是编译错误，不是
// "记录说已完成、看板说还在跑"。
//
// 同时钉住 wire 兼容：JSON / 工具结果 / 看板里仍然必须是 `"running"` 这样的**词**，
// 不是枚举的整数值——枚举只活在进程内。

func TestAsyncStateEnumKeepsWireShape(t *testing.T) {
	words := []struct {
		state AsyncState
		word  string
	}{
		{AsyncStateRunning, "running"},
		{AsyncStateDone, "done"},
		{AsyncStateFailed, "failed"},
		{AsyncStateKilled, "killed"},
		{AsyncStateUnknown, "unknown"},
	}
	for _, entry := range words {
		if got := entry.state.String(); got != entry.word {
			t.Errorf("AsyncState(%d).String() = %q，期望 %q", entry.state, got, entry.word)
		}
		encoded, err := json.Marshal(entry.state)
		if err != nil {
			t.Fatalf("序列化 %s：%v", entry.word, err)
		}
		if string(encoded) != `"`+entry.word+`"` {
			t.Errorf("序列化形状变了：%s，期望 %q（枚举值不许漏到 wire 上）", encoded, entry.word)
		}
		parsed, ok := ParseAsyncState(entry.word)
		if !ok || parsed != entry.state {
			t.Errorf("ParseAsyncState(%q) = %v/%v，期望 %v/true", entry.word, parsed, ok, entry.state)
		}
		decoded := AsyncStateUnknown
		if err := json.Unmarshal(encoded, &decoded); err != nil || decoded != entry.state {
			t.Errorf("读回 %s：%v/%v，期望 %v", encoded, decoded, err, entry.state)
		}
	}
}

// 认不得的词必须报错：静默折成零值会把"读不懂"变成"还在跑"——两者差一整个作业生命周期。
func TestAsyncStateRejectsUnknownWord(t *testing.T) {
	if state, ok := ParseAsyncState("halted"); ok || state != AsyncStateUnknown {
		t.Fatalf("ParseAsyncState(\"halted\") = %v/%v，期望 Unknown/false", state, ok)
	}
	state := AsyncStateRunning
	if err := json.Unmarshal([]byte(`"halted"`), &state); err == nil {
		t.Fatal("认不得的状态词必须报错，不许静默接受")
	}
	if state != AsyncStateRunning {
		t.Fatalf("报错时不许留下半个值（原值应原样不动）：%v", state)
	}
}

// 记录整条走一遍 JSON：字段形状与字符串时代逐字节相同（`"state":"running"`）。
func TestAsyncRunRecordWireRoundTrip(t *testing.T) {
	record := AsyncRunRecord{Handle: "a1", Kind: "process", State: AsyncStateRunning, SessionID: "s1"}
	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("序列化记录：%v", err)
	}
	if !strings.Contains(string(encoded), `"state":"running"`) {
		t.Fatalf("记录里的状态字段形状变了：%s", encoded)
	}
	var decoded AsyncRunRecord
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("读回记录：%v", err)
	}
	if decoded.State != AsyncStateRunning {
		t.Fatalf("读回的状态 = %v，期望 running", decoded.State)
	}
}

func TestPlanRunStatusEnumKeepsWireShape(t *testing.T) {
	words := []struct {
		status PlanRunStatus
		word   string
	}{
		{PlanRunStatusCompleted, "completed"},
		{PlanRunStatusFailed, "failed"},
		{PlanRunStatusAborted, "aborted"},
		{PlanRunStatusUnknown, "unknown"},
	}
	for _, entry := range words {
		if got := entry.status.String(); got != entry.word {
			t.Errorf("PlanRunStatus(%d).String() = %q，期望 %q", entry.status, got, entry.word)
		}
		encoded, err := json.Marshal(entry.status)
		if err != nil {
			t.Fatalf("序列化 %s：%v", entry.word, err)
		}
		if string(encoded) != `"`+entry.word+`"` {
			t.Errorf("序列化形状变了：%s，期望 %q", encoded, entry.word)
		}
		parsed, ok := ParsePlanRunStatus(entry.word)
		if !ok || parsed != entry.status {
			t.Errorf("ParsePlanRunStatus(%q) = %v/%v", entry.word, parsed, ok)
		}
	}
	if status, ok := ParsePlanRunStatus("panicked"); ok || status != PlanRunStatusUnknown {
		t.Fatalf("节点状态词不属于这一格：ParsePlanRunStatus(\"panicked\") = %v/%v", status, ok)
	}
}
