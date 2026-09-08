package fork

import (
	"encoding/json"
	"testing"
)

// TestInputTimeoutSecDecodes 验证 fork 请求可按任务分配总超时
// （timeout_sec）：长任务省略（走 limits.fork_timeout），简单审查可给
// 1200（20 分钟）等更紧上限。
func TestInputTimeoutSecDecodes(t *testing.T) {
	var input Input
	if err := json.Unmarshal([]byte(`{"timeout_sec":1200,"subagents":[{"id":"review","goal":"审查代码"}]}`), &input); err != nil {
		t.Fatal(err)
	}
	if input.TimeoutSec != 1200 {
		t.Fatalf("timeout_sec = %d, want 1200", input.TimeoutSec)
	}
}
