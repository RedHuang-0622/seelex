package history

import (
	"testing"

	"github.com/RedHuang-0622/seelex/middle/protocol/model"
)

// 本文件钉窗口装配判据：接缝（行身份）对位、窗口边界与起点推算、空页/无锚的保守行为。
// 门面级端到端用例（冷回读 / 分页读回 / 与磁盘读面的坐标口径）仍在 application/core。

func row(id, role, content string) model.Message {
	return model.Message{ID: id, Role: role, Content: content}
}

func ids(rows []model.Message) []string {
	out := make([]string, 0, len(rows))
	for _, message := range rows {
		out = append(out, message.ID)
	}
	return out
}

func equalIDs(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func TestReplaceTailJoinsHotTailByRowIdentity(t *testing.T) {
	page := []model.Message{row("a", "user", "1"), row("b", "assistant", "2")}
	memory := []model.Message{row("b", "assistant", "2"), row("c", "user", "3")} // 热尾：b 已在页里，c 未落盘
	rows, start := ReplaceTail(page, memory, 0, 3, 10)
	if !equalIDs(ids(rows), []string{"a", "b", "c"}) {
		t.Fatalf("热尾应接在页尾之后：%v", ids(rows))
	}
	// 页尾接上了热尾 ⇒ 右界是内存窗口右界（memoryStart + len(memory) = 2），visible=3 ⇒ start=0。
	if start != 0 {
		t.Fatalf("start = %d, want 0", start)
	}
}

func TestReplaceTailWithoutSeamUsesVisibleTotal(t *testing.T) {
	page := []model.Message{row("a", "user", "1")}
	memory := []model.Message{row("z", "user", "z")} // 页尾不在内存窗口里：接不上
	rows, start := ReplaceTail(page, memory, 5, 7, 10)
	if !equalIDs(ids(rows), []string{"a"}) {
		t.Fatalf("接不上热尾时应以页为准：%v", ids(rows))
	}
	if start != 6 { // end = 可见总数 7，visible = 1
		t.Fatalf("start = %d, want 6（窗口右界 = 可见总数）", start)
	}
}

func TestReplaceTailEmptyVisibleKeepsWindow(t *testing.T) {
	rows, start := ReplaceTail(nil, nil, 4, 4, 10)
	if rows != nil || start != 4 {
		t.Fatalf("一页可见行都没读到时窗口原地不动：rows=%v start=%d", ids(rows), start)
	}
}

func TestPrependUsesWindowFirstRowAsSeam(t *testing.T) {
	page := []model.Message{row("x", "user", "x"), row("y", "assistant", "y"), row("a", "user", "a")}
	memory := []model.Message{row("a", "user", "a"), row("b", "assistant", "b")}
	rows, start := Prepend(page, memory, 10, 10)
	if !equalIDs(ids(rows), []string{"x", "y", "a", "b"}) {
		t.Fatalf("接缝之前的行应前置到窗口头部：%v", ids(rows))
	}
	if start != 8 {
		t.Fatalf("start = %d, want 8（窗口整体后退 2 行）", start)
	}
}

func TestPrependWithoutSeamStaysPut(t *testing.T) {
	page := []model.Message{row("x", "user", "x")}
	memory := []model.Message{row("a", "user", "a")} // 窗口首行不在页里
	rows, start := Prepend(page, memory, 3, 10)
	if rows != nil || start != 3 {
		t.Fatalf("接缝对不上就不动窗口：rows=%v start=%d", ids(rows), start)
	}
}

func TestPrependIntoEmptyWindowTakesPageTail(t *testing.T) {
	page := []model.Message{row("x", "user", "x"), row("y", "assistant", "y"), row("z", "user", "z")}
	rows, start := Prepend(page, nil, 5, 2)
	if !equalIDs(ids(rows), []string{"y", "z"}) {
		t.Fatalf("无对位锚时取页尾窗口：%v", ids(rows))
	}
	if start != 3 {
		t.Fatalf("start = %d, want 3（memoryStart - 窗口行数）", start)
	}
}

func TestRowIdentityMatching(t *testing.T) {
	rows := []model.Message{
		row("", "tool", ""),
		{Role: "tool", Tool: &model.ToolCall{ID: "call-1", Name: "bash"}},
		row("m-2", "assistant", "hi"),
	}
	if IndexOfSameRow(rows, row("m-2", "assistant", "hi")) != 2 {
		t.Fatal("同 ID 的行应被认出")
	}
	// 不带 ID、也不带内容与工具调用的空 tool 行**对位不到任何行**（空占位互相认同就会把接缝挪位）。
	if IndexOfSameRow(rows, row("", "tool", "")) != -1 {
		t.Fatal("空占位不得参与对位")
	}
	if !ContainsSameRow(rows, model.Message{Role: "tool", Tool: &model.ToolCall{ID: "call-1"}}) {
		t.Fatal("工具行应按调用 ID 对位")
	}
	// 无 ID 的普通行退回内容对位；两条空占位不对位。
	if !SameByContent(row("", "assistant", "same"), row("", "assistant", "same")) {
		t.Fatal("内容相同应视为同一行（旧格式历史行没有稳定 ID）")
	}
	if SameByContent(row("", "assistant", ""), row("", "assistant", "")) {
		t.Fatal("空占位不得互相认同（认错就把接缝挪位）")
	}
	if HasRowIdentity(model.Message{}) {
		t.Fatal("完全空的行没有身份")
	}
}

func TestDurableRowsSkipsSystem(t *testing.T) {
	rows := DurableRows([]model.Message{
		row("s", "system", "prompt"),
		row("u", "user", "hi"),
	})
	if !equalIDs(ids(rows), []string{"u"}) {
		t.Fatalf("system 引导行不占 durable 下标空间：%v", ids(rows))
	}
}
