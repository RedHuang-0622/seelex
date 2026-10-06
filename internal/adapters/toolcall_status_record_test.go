package adapters

import (
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/application/model"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// toolcall_status_record_test.go — "工具调用视图词"这一格在**存储面 → 契约枚举**的互锁。
//
// 这一格不新开表：`model.ToolCall.Status` 与工具事件状态同格（`dto.ToolEvent*`：
// running | success | error）。store 那一侧存的是**词**（本层不 import 契约包），
// 所以判据是三条：
//  1. store 写出的词与契约词**逐字相同**，读回同一个词（两包各存一份词表的老毛病：
//     store 换一个写法，读面静默变成"未知"或"成功"）；
//  2. 认不得的词、空串 → `dto.ToolEventUnknown`，**不折成** running/success/error
//     （折成 success 比折不出更糟：看板与详情页会把"读不懂"显示成"成功了"）；
//  3. 本格是闭合的：三个词各自读回自己，别的什么都不认。
func TestToolCallStatusOfRecordLocksTheStoreVocabulary(t *testing.T) {
	storeWord := sessionstore.ConversationToolCallStatusSuccess
	if storeWord != dto.ToolEventSuccess.String() {
		t.Fatalf("store 的词 %q 与契约的词 %q 不一致（两包词表分叉）", storeWord, dto.ToolEventSuccess)
	}
	if got := model.ToolCallStatusOfRecord(storeWord); got != dto.ToolEventSuccess {
		t.Fatalf("store 词 %q 读回成了 %v", storeWord, got)
	}

	for _, word := range []string{"running", "success", "error"} {
		got := model.ToolCallStatusOfRecord(word)
		if got.String() != word {
			t.Errorf("本格的词 %q 读回成了 %q", word, got)
		}
	}

	for _, wire := range []string{"", "completed", "Success", "done", "failed", "unknown-and-worse"} {
		got := model.ToolCallStatusOfRecord(wire)
		if got != dto.ToolEventUnknown {
			t.Errorf("认不得的词 %q 读回成了 %q，必须落 unknown（不是任何已知态）", wire, got)
		}
		if got == dto.ToolEventSuccess {
			t.Fatalf("认不得的词 %q 被折成了成功——读不懂就说不懂，不许替调用方宣布成功", wire)
		}
	}
}
