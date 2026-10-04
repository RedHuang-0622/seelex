package e2e

// teammate_session_live_wire_test.go — 复现「这件事的会话」这条读面在 GUI 上永远读不到。
//
// 现场：后端读面（`Runtime.TeammateSessionLive` / `Service.TeammateSessionLiveFor`）在
// headless 冒烟里是全绿的——它直接读 Go 结构体。但 GUI 走的是 **Wails 绑定**：返回值经
// `encoding/json` 序列化，前端按 **snake_case** 键读（`renderTeammateLiveSession`：
// `view.session_id / role / live / running / messages / truncated`）。
//
// `dto.TeammateSessionLiveView` 的字段**没有 json tag**，`encoding/json` 于是按 Go 字段名
// 出键（`SessionID` / `Role` / `Running` / `Messages` ...）——前端拿到的是一份"全是 undefined"
// 的对象：`view.running` 为假，渲染件走"执行面不在本进程"的分支；`view.messages` 不是数组，
// teammate 自己的对话一行都画不出来。
//
// 这就是"两端各自都绿、契约在中间断了"：Go 侧用例读结构体、前端 .mjs 用例喂的是 snake_case
// 夹具，两边都发现不了。本用例把**同一份载荷**按 JSON 边界读一遍，钉住前端读的那几个键。

import (
	"encoding/json"
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

func TestTeammateSessionLiveWireShapeMatchesFrontend(t *testing.T) {
	payload, err := json.Marshal(dto.TeammateSessionLiveView{
		SessionID: "sess-main-team-exec-wi-wi-impl",
		Role:      "exec",
		Live:      true,
		Running:   true,
		Messages: []dto.TeammateSessionLiveMessage{
			{Role: "user", Text: "把契约落成代码"},
			{Role: "assistant", Text: "这一轮做完了"},
		},
		Truncated: true,
	})
	if err != nil {
		t.Fatalf("marshal TeammateSessionLiveView: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("unmarshal: %v（%s）", err, payload)
	}
	// 前端 renderTeammateLiveSession 读的键（gui/frontend/dist/team-board-view.js）。
	for _, key := range []string{"session_id", "role", "live", "running", "messages", "truncated"} {
		if _, ok := decoded[key]; !ok {
			t.Errorf("实时读面缺前端要读的键 %q（前面的键：%v）；缺了它前端只能把这一轮画成「读不到」", key, sortedKeys(decoded))
		}
	}
	// 对话行同理：前端按 role/text 画"输入 / teammate"两列。
	messages, ok := decoded["messages"].([]any)
	if !ok || len(messages) != 2 {
		t.Fatalf("messages 不是前端能读的数组：%#v", decoded["messages"])
	}
	first, ok := messages[0].(map[string]any)
	if !ok {
		t.Fatalf("messages[0] 不是对象：%#v", messages[0])
	}
	for _, key := range []string{"role", "text"} {
		if _, ok := first[key]; !ok {
			t.Errorf("对话行缺前端要读的键 %q：%#v", key, first)
		}
	}
}

// sortedKeys 只用于把实际键面打进失败信息（顺序无关，读的人一眼能看出漂移）。
func sortedKeys(object map[string]any) []string {
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	for i := range keys {
		for j := i + 1; j < len(keys); j++ {
			if keys[j] < keys[i] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	return keys
}
