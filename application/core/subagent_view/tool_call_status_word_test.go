package subagent_view

import (
	"testing"

	"github.com/RedHuang-0622/Seele/types"
	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// TestSubagentConversationSpeaksTheToolCallCellWords 钉住"工具调用视图词"这一格的取值面：
// 详情页里那条历史工具调用说的是**本格的词**。
//
// 这一格不是新表：同一条 `model.ToolCall.Status` 的其它写方（宿主工具钩子、存档回灌、
// 子代理详情）写的都是 running|success|error，就是契约里的 `dto.ToolEvent*`。写第四个词
// 不会编译报错、也不会让别的用例变红，但严格读方（TUI 那个 `switch`、GUI 的 `is-*` 映射、
// 轨迹视图的 `tool.status === "error" ? … : "success"`）会静默降级成"未知/成功"。
func TestSubagentConversationSpeaksTheToolCallCellWords(t *testing.T) {
	c := testCoordinator()
	adapted := c.adaptSubagentConversation([]types.Message{
		{Role: "assistant", Content: strPtr("读取中"), ToolCallID: "t1", Name: "read_file"},
		{Role: "tool", Content: strPtr("package a"), ToolCallID: "t1", Name: "read_file"},
	})
	if len(adapted) == 0 || adapted[0].Tool == nil {
		t.Fatalf("工具消息必须携带 Tool 摘要：%+v", adapted)
	}
	if got := adapted[0].Tool.Status; got != dto.ToolEventSuccess {
		t.Fatalf("详情页工具行说的是 %q，本格只认 %q/%q/%q（契约 dto.ToolEvent*）",
			got, dto.ToolEventRunning, dto.ToolEventSuccess, dto.ToolEventError)
	}
}
