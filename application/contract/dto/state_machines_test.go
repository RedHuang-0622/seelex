package dto

import (
	"encoding/json"
	"fmt"
	"testing"
)

// state_machines_test.go — "状态机枚举统一"的判据：**每一格都过同一套口径**。
//
// 这一批要收的是"状态机没有枚举统一"：仓库里曾经是每格一套写法（有的 `type X string`
// 加散落字面量、有的干脆是 `string` 字段直接跟字面量比）。统一后的形状是：
//
//	type XState uint8              // 取值只能从下面那组常量来
//	var xStateWords = [...]string{...}   // 枚举 ↔ 对外词，唯一一份
//	String / Parse / MarshalJSON / UnmarshalJSON → 全部转调 stateCodec（唯一一份实现）
//
// 所以这份用例也共用一套判据：任意一格的任意取值都必须
//   ① `String()` 与 `json.Marshal` 给出**同一个对外词**（枚举整数值不许漏到 wire 上）
//   ② 那个词能被 Parse 读回同一个枚举值
//   ③ 认不得的词 Parse 判否、JSON 读回**报错**（不静默折成零值）
//
// 每条判据都要能独立失败——所以样本是"逐格逐词"的表，不是一句"表格对得上"。

type stateWireCase struct {
	grid  string
	state fmt.Stringer
	word  string
	parse func(string) (fmt.Stringer, bool)
	read  func([]byte) error // 往枚举值里读回，认不得的词必须给 err
}

func assertStateWire(t *testing.T, c stateWireCase) {
	t.Helper()
	if got := c.state.String(); got != c.word {
		t.Errorf("[%s] String() = %q，期望 %q", c.grid, got, c.word)
	}
	encoded, err := json.Marshal(c.state)
	if err != nil {
		t.Fatalf("[%s] 序列化 %s: %v", c.grid, c.word, err)
	}
	if string(encoded) != `"`+c.word+`"` {
		t.Errorf("[%s] wire 形状变了：%s（期望 %q：枚举整数值不许漏到 wire 上）", c.grid, encoded, c.word)
	}
	parsed, ok := c.parse(c.word)
	if !ok || parsed.String() != c.word {
		t.Errorf("[%s] Parse(%q) = %v/%v", c.grid, c.word, parsed, ok)
	}
	if _, ok := c.parse("__not_a_state_word__"); ok {
		t.Errorf("[%s] 认不得的词必须判否：Parse(\"__not_a_state_word__\") 认了", c.grid)
	}
	if err := c.read([]byte(`"__not_a_state_word__"`)); err == nil {
		t.Errorf("[%s] JSON 读回认不得的词必须报错（不许静默折成零值）", c.grid)
	}
	if err := c.read(encoded); err != nil {
		t.Errorf("[%s] 自己写出去的词必须能读回：%v", c.grid, err)
	}
}

// 格 A：记录状态（子代理节点的生命周期；词也是**落盘**的那一份）。
func TestRecordStatusEnumWire(t *testing.T) {
	cases := []struct {
		state SubAgentNodeStatus
		word  string
	}{
		{SubAgentQueued, "queued"},
		{SubAgentRunning, "running"},
		{SubAgentDone, "done"},
		{SubAgentFailed, "failed"},
		{SubAgentInterrupted, "interrupted"},
		{SubAgentUnknown, "unknown"},
	}
	for _, entry := range cases {
		state := entry.state
		assertStateWire(t, stateWireCase{
			grid:  "记录状态",
			state: state,
			word:  entry.word,
			parse: func(text string) (fmt.Stringer, bool) {
				parsed, ok := ParseSubAgentNodeStatus(text)
				return parsed, ok
			},
			read: func(data []byte) error { return json.Unmarshal(data, &state) },
		})
	}
}

// 格 B：工具事件状态（子代理工具调用的 running | success | error）。
func TestToolEventStatusEnumWire(t *testing.T) {
	cases := []struct {
		state ToolEventStatus
		word  string
	}{
		{ToolEventRunning, "running"},
		{ToolEventSuccess, "success"},
		{ToolEventError, "error"},
		{ToolEventUnknown, "unknown"},
	}
	for _, entry := range cases {
		state := entry.state
		assertStateWire(t, stateWireCase{
			grid:  "工具事件状态",
			state: state,
			word:  entry.word,
			parse: func(text string) (fmt.Stringer, bool) {
				parsed, ok := ParseToolEventStatus(text)
				return parsed, ok
			},
			read: func(data []byte) error { return json.Unmarshal(data, &state) },
		})
	}
}

// 格 C（回执状态）**刻意不枚举**：`asyncPayload.Status` 这一个字段里同时装着
//   - 我们自己写的五种回执词（accepted | observed | killed | already_finished | retired），
//   - 以及 Seele `jobs.Manager` 生产的词（progress | finished，见 manager.Status 的返回载荷）。
//
// 把框架的词收进我们的契约枚举，就是"拿别人的词表当自己的"：框架加一个词，我们的 Unmarshal
// 就报错。所以这一格的口径是**边界字段**——保持字符串 + 工具侧只用我们自己那五个词 +
// 源码门禁登记白名单理由（见 e2e/subagent_status_vocabulary_gate_test.go 的 receipt 条目）。
// 这一条是清点写方之后改的口径（原计划"连 job_manage 的四种 op 一起收"被证据推翻）。

// 格 D：task 状态（工作表格条目的生命周期）。
func TestTaskStatusEnumWire(t *testing.T) {
	cases := []struct {
		status TaskStatus
		word   string
	}{
		{TaskPending, "pending"},
		{TaskQueued, "queued"},
		{TaskRunning, "running"},
		{TaskDoing, "doing"},
		{TaskCompleted, "completed"},
		{TaskFailed, "failed"},
		{TaskRetry, "retry"},
		{TaskInterrupted, "interrupted"},
		{TaskStatusUnknown, "unknown"},
	}
	for _, entry := range cases {
		status := entry.status
		assertStateWire(t, stateWireCase{
			grid:  "task 状态",
			state: status,
			word:  entry.word,
			parse: func(text string) (fmt.Stringer, bool) {
				parsed, ok := ParseTaskStatus(text)
				return parsed, ok
			},
			read: func(data []byte) error { return json.Unmarshal(data, &status) },
		})
	}
}

// 格 E：回合状态（用户这一次请求的生命周期；可见面 `Snapshot.Task.Status` 与
// 存档面 `TaskContextProjection.Status` 同格——历史上前者用 progressing、后者用
// running，两套词表靠一处手写映射接着）。
func TestTurnStatusEnumWire(t *testing.T) {
	cases := []struct {
		status TurnStatus
		word   string
	}{
		{TurnUnknown, "unknown"},
		{TurnIdle, "idle"},
		{TurnProgressing, "progressing"},
		{TurnCompleted, "completed"},
		{TurnNeedsUserDecision, "needs_user_decision"},
		{TurnBlocked, "blocked"},
		{TurnInterrupted, "interrupted"},
		{TurnFailed, "failed"},
	}
	for _, entry := range cases {
		status := entry.status
		assertStateWire(t, stateWireCase{
			grid:  "回合状态",
			state: status,
			word:  entry.word,
			parse: func(text string) (fmt.Stringer, bool) {
				parsed, ok := ParseTurnStatus(text)
				return parsed, ok
			},
			read: func(data []byte) error { return json.Unmarshal(data, &status) },
		})
	}
	// 边界：另一格（工作表条目 task 状态）的词不许被这一格认下——两格都叫 "task"，
	// 混用就是"同名不同机器"的老毛病。
	for _, foreign := range []string{"doing", "retry", "pending"} {
		if _, ok := ParseTurnStatus(foreign); ok {
			t.Errorf("回合状态认下了工作表条目的词 %q", foreign)
		}
	}
}

// 落盘那一格：老形状（字符串）的记录必须能读回，认不得的词**不炸**、也不被当成终态。
func TestRecordStatusReadsOldShapedRecords(t *testing.T) {
	var legacy struct {
		NodeID string             `json:"node_id"`
		Status SubAgentNodeStatus `json:"status"`
	}
	if err := json.Unmarshal([]byte(`{"node_id":"n1","status":"running"}`), &legacy); err != nil {
		t.Fatalf("老形状（字符串状态）记录读不回来：%v", err)
	}
	if legacy.Status != SubAgentRunning {
		t.Fatalf("读回的状态 = %v，期望 running", legacy.Status)
	}
	if err := json.Unmarshal([]byte(`{"node_id":"n2","status":"half-exploded"}`), &legacy); err == nil {
		t.Fatal("认不得的落盘词必须显式报错（不许静默当成某个已知状态）")
	}
}
