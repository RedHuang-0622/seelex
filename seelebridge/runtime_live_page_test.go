package seelebridge

// runtime_live_page_test.go — node 第一视角实时回放的**分页读法**
// （Runtime.SubagentLiveHistoryPage / dto.SubagentLiveHistoryPage）。
//
// 口径（"只有尾巴"改成"有界窗口 + 分页"）：窗口上限可配（RuntimeConfig.SubagentLiveWindow），
// 上限语义不变（超出丢最旧）；分页只决定"这一页读窗口里的哪一段"，不是第二个事实源。

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// newLiveWindowRuntime 造一个显式配置回放窗口的 Runtime（账号用临时文件，不碰真账号）。
func newLiveWindowRuntime(t *testing.T, window int) *Runtime {
	t.Helper()
	path := filepath.Join(t.TempDir(), "accounts.yaml")
	content := `roles:
  agent:
    - model: test-model
      base_url: http://localhost
      api_key: test-key-not-used
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	runtime, err := NewRuntime(RuntimeConfig{
		AccountsPath:       path,
		ToolCallTimeout:    30 * time.Second,
		SubagentLiveWindow: window,
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	t.Cleanup(func() { runtime.Shutdown() })
	return runtime
}

// TestSubagentLiveWindowSizeNormalizes 钉住窗口上限的归一（0 → 默认 512；<50 → 50）：
// 零值 Runtime（测试直接构造）也必须拿到默认窗口，而不是把窗口剪成 0。
func TestSubagentLiveWindowSizeNormalizes(t *testing.T) {
	cases := []struct {
		configured int
		want       int
	}{
		{0, subagentLiveHistoryCap},
		{-1, subagentLiveHistoryMinWindow},
		{10, subagentLiveHistoryMinWindow},
		{49, subagentLiveHistoryMinWindow},
		{50, 50},
		{4096, 4096},
	}
	for _, item := range cases {
		runtime := &Runtime{subagentLiveWindow: item.configured}
		if got := runtime.subagentLiveWindowSize(); got != item.want {
			t.Errorf("SubagentLiveWindow=%d → %d, want %d", item.configured, got, item.want)
		}
	}
	if got := (*Runtime)(nil).subagentLiveWindowSize(); got != subagentLiveHistoryCap {
		t.Errorf("nil Runtime → %d, want %d", got, subagentLiveHistoryCap)
	}
}

// TestSubagentLiveHistoryPageReadsWholeWindow 在**窗口=50** 的运行时下逐页读：
// 页边界 / has_more / 越界空页 / 逐页合计 = total 都要正确（分页不丢条、不重复）。
func TestSubagentLiveHistoryPageReadsWholeWindow(t *testing.T) {
	const nodeID = "node-paged"
	runtime := newLiveWindowRuntime(t, 50)
	// 订阅一次把分发面启动起来（窗口表与广播循环都在那一步建）。此后直接向广播点投事件
	// ——那就是生产写入路径（阶段/工具/正文增量都经它进窗口）。
	_, _, cancel, err := runtime.SubscribeSubagentLive(nodeID)
	if err != nil {
		t.Fatalf("SubscribeSubagentLive: %v", err)
	}
	defer cancel()
	const pushed = 70
	for turn := 0; turn < pushed; turn++ {
		runtime.broadcastLive(dto.SubagentLiveEvent{
			NodeID: nodeID, Kind: "stage", At: time.Now(),
			Stage: &dto.NodeStageLog{Stage: "turn", NodeID: nodeID, Turn: turn},
		})
	}

	// 上限语义不变：超出丢最旧 → 窗口里只剩最近 50 条（第 20..69 轮）。
	first := runtime.SubagentLiveHistoryPage(nodeID, 0, 20)
	if first.ScopeID != nodeID {
		t.Fatalf("ScopeID = %q, want %q", first.ScopeID, nodeID)
	}
	if first.Total != 50 {
		t.Fatalf("窗口上限没生效：total = %d, want 50（%d 条事件，超出丢最旧）", first.Total, pushed)
	}
	if first.Offset != 0 || first.Limit != 20 {
		t.Fatalf("首页归一 = offset %d / limit %d, want 0/20", first.Offset, first.Limit)
	}
	if len(first.Events) != 20 || !first.HasMore {
		t.Fatalf("首页 = %d 条 / has_more=%v, want 20/true", len(first.Events), first.HasMore)
	}
	if first.Events[0].Stage == nil || first.Events[0].Stage.Turn != 20 {
		t.Fatalf("首页第一条 = %+v, want 第 20 轮（0..19 已被丢最旧）", first.Events[0].Stage)
	}
	for index := 1; index < len(first.Events); index++ {
		if first.Events[index].Stage.Turn <= first.Events[index-1].Stage.Turn {
			t.Fatalf("窗口内顺序必须由旧到新：第 %d 条 turn = %d 不大于前一条 %d",
				index, first.Events[index].Stage.Turn, first.Events[index-1].Stage.Turn)
		}
	}

	// 末页边界：offset=49 只有 1 条，且 has_more=false。
	last := runtime.SubagentLiveHistoryPage(nodeID, 49, 10)
	if last.Offset != 49 || len(last.Events) != 1 || last.HasMore {
		t.Fatalf("末页 = offset %d / %d 条 / has_more=%v, want 49/1/false",
			last.Offset, len(last.Events), last.HasMore)
	}
	if last.Events[0].Stage.Turn != 69 {
		t.Fatalf("末页应是最后一条：turn = %d, want 69", last.Events[0].Stage.Turn)
	}

	// 越界：offset >= total → 空页且 has_more=false（offset 原样回带，调用方看得出自己越界）。
	for _, offset := range []int{50, 10_000} {
		beyond := runtime.SubagentLiveHistoryPage(nodeID, offset, 10)
		if beyond.Offset != offset || beyond.Total != 50 || len(beyond.Events) != 0 || beyond.HasMore {
			t.Fatalf("越界页 offset=%d → %+v, want 空页/total 50/has_more false", offset, beyond)
		}
	}

	// 归一：offset<0 → 0；limit<=0 → 默认 50；limit>窗口 → 收敛到窗口。
	negative := runtime.SubagentLiveHistoryPage(nodeID, -5, 10)
	if negative.Offset != 0 || len(negative.Events) != 10 {
		t.Fatalf("offset<0 未归一：%+v", negative)
	}
	if zero := runtime.SubagentLiveHistoryPage(nodeID, 0, 0); zero.Limit != subagentLivePageDefaultLimit {
		t.Fatalf("limit<=0 → %d, want 默认页 %d", zero.Limit, subagentLivePageDefaultLimit)
	}
	huge := runtime.SubagentLiveHistoryPage(nodeID, 0, 5_000)
	if huge.Limit != 50 || len(huge.Events) != 50 || huge.HasMore {
		t.Fatalf("limit>窗口 未收敛：limit=%d / %d 条 / has_more=%v", huge.Limit, len(huge.Events), huge.HasMore)
	}

	// 逐页读：逐页合计 = total，且没有重复（限制 7 条一页，跨 8 页）。
	seen := make(map[int]bool, 50)
	paged := 0
	pages := 0
	for offset := 0; ; {
		page := runtime.SubagentLiveHistoryPage(nodeID, offset, 7)
		pages++
		paged += len(page.Events)
		for _, event := range page.Events {
			if seen[event.Stage.Turn] {
				t.Fatalf("分页重复投递：turn %d 出现两次", event.Stage.Turn)
			}
			seen[event.Stage.Turn] = true
		}
		if !page.HasMore {
			break
		}
		offset = page.Offset + len(page.Events)
		if pages > 16 {
			t.Fatalf("翻页次数 %d 异常（total 50 / 页 7）", pages)
		}
	}
	if paged != 50 || len(seen) != 50 {
		t.Fatalf("逐页合计 = %d（去重后 %d）, want 50", paged, len(seen))
	}

	// 空节点号 / 没见过的节点：空页，不是崩溃、也不是另一个节点的事件。
	if blank := runtime.SubagentLiveHistoryPage("", 0, 10); blank.ScopeID != "" || len(blank.Events) != 0 {
		t.Fatalf("空节点号应给空页：%+v", blank)
	}
	unknown := runtime.SubagentLiveHistoryPage("node-never-seen", 0, 10)
	if unknown.Total != 0 || len(unknown.Events) != 0 || unknown.HasMore {
		t.Fatalf("没见过的节点应给空页：%+v", unknown)
	}
}
