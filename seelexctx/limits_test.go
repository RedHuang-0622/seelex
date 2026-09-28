package seelexctx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestLoadLimitsCompactionBudgetRatios：压缩预算比例是**装配参数**（seelex.yaml
// limits 段），不再硬编码在 task_context.newContextBudget——判据（软/硬/目标）
// 与单条输入外置阈值必须能被同一份配置调，且缺字段回退默认（8/95/98/80/50）。
func TestLoadLimitsCompactionBudgetRatios(t *testing.T) {
	def := DefaultLimits()
	if def.ContextSafetyReserveDivisor != 8 || def.ContextSoftPercent != 95 ||
		def.ContextHardPercent != 98 || def.ContextTargetPercent != 80 ||
		def.ContextSingleItemPercent != 50 {
		t.Fatalf("压缩预算默认比例 = %+v", def)
	}

	path := filepath.Join(t.TempDir(), "seele.yaml")
	content := "limits:\n" +
		"  context_safety_reserve_divisor: 4\n" +
		"  context_soft_percent: 60\n" +
		"  context_hard_percent: 85\n" +
		"  context_target_percent: 45\n" +
		"  context_single_item_percent: 30\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadLimits(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ContextSafetyReserveDivisor != 4 || loaded.ContextSoftPercent != 60 ||
		loaded.ContextHardPercent != 85 || loaded.ContextTargetPercent != 45 ||
		loaded.ContextSingleItemPercent != 30 {
		t.Fatalf("yaml 里的压缩预算比例未生效 = %+v", loaded)
	}

	// 只写一部分：LoadLimits 返回原始解析结果（未写字段为 0），默认值由
	// WithDefaults 补（limits.Apply 走的正是这条链）——不能把 0 当"用户要 0"。
	partial := filepath.Join(t.TempDir(), "partial.yaml")
	if err := os.WriteFile(partial, []byte("limits:\n  context_soft_percent: 70\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err = LoadLimits(partial)
	if err != nil {
		t.Fatal(err)
	}
	merged := loaded.WithDefaults()
	if merged.ContextSoftPercent != 70 || merged.ContextHardPercent != 98 ||
		merged.ContextTargetPercent != 80 || merged.ContextSingleItemPercent != 50 ||
		merged.ContextSafetyReserveDivisor != 8 {
		t.Fatalf("部分配置必须与默认合并 = %+v", merged)
	}
}

// TestLoadLimitsDefaults 验证缺失文件/缺失 limits 段 → 完整默认值。
func TestLoadLimitsDefaults(t *testing.T) {
	limits, err := LoadLimits(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if limits != DefaultLimits() {
		t.Fatalf("missing file must yield full defaults, got %+v", limits)
	}
	if limits.ToolCallTimeoutSec != 1800 || limits.ApprovalTimeoutSec != 600 || limits.PlanNodeMaxLoops != 15 {
		t.Fatalf("defaults = %+v", limits)
	}
	// docker 自动恢复默认：开启 + 60s 启动等待。
	if limits.DisableDockerAutoStart || limits.DockerStartTimeoutSec != 60 {
		t.Fatalf("docker defaults = %+v", limits)
	}
	// fork 宽松预算默认：2h 超时（循环数复用 effort，无独立字段）。
	if limits.ForkTimeoutSec != 7200 {
		t.Fatalf("fork defaults = %+v", limits)
	}
	// 文件存在但没有 limits 段 → 同样走完整默认。
	path := filepath.Join(t.TempDir(), "seele.yaml")
	if err := os.WriteFile(path, []byte("window:\n  rounds: 0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	limits, err = LoadLimits(path)
	if err != nil || limits != DefaultLimits() {
		t.Fatalf("missing limits section = %+v err=%v", limits, err)
	}
}

// TestLoadLimitsParsesAndOverrides 验证部分配置只覆盖指定字段，
// 其余字段经 WithDefaults 补默认。
func TestLoadLimitsParsesAndOverrides(t *testing.T) {
	path := filepath.Join(t.TempDir(), "seele.yaml")
	content := "limits:\n  tool_call_timeout: 0\n  approval_timeout: 900\n  plan_node_max_loops: 30\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	limits, err := LoadLimits(path)
	if err != nil {
		t.Fatal(err)
	}
	if limits.ToolCallTimeoutSec != 0 || limits.ApprovalTimeoutSec != 900 || limits.PlanNodeMaxLoops != 30 {
		t.Fatalf("parsed = %+v", limits)
	}
	full := limits.WithDefaults()
	if full.ToolCallTimeoutSec != 0 { // 0 保留为显式"无限制"，不补默认
		t.Fatalf("explicit zero must be kept, got %d", full.ToolCallTimeoutSec)
	}
	if full.ApprovalTimeoutSec != 900 || full.HeartbeatIntervalSec != 15 || full.HistoryWindow != 200 {
		t.Fatalf("merged = %+v", full)
	}
	if full.WorkTableRows != DefaultLimits().WorkTableRows || DefaultLimits().WorkTableRows != 200 {
		t.Fatalf("work table rows default = %d, want 200", DefaultLimits().WorkTableRows)
	}
	// limits 段存在但未写 tool_call_timeout → 0 = 无限制（文档语义）。
	partialPath := filepath.Join(t.TempDir(), "seele.yaml")
	if err := os.WriteFile(partialPath, []byte("limits:\n  approval_timeout: 300\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	partial, err := LoadLimits(partialPath)
	if err != nil {
		t.Fatal(err)
	}
	if partial.ToolCallTimeoutSec != 0 || partial.ApprovalTimeoutSec != 300 {
		t.Fatalf("partial section = %+v", partial)
	}
	// max_tool_result_chars：未配置 → 默认 60000；显式配置 → 覆盖生效。
	if merged := partial.WithDefaults(); merged.MaxToolResultChars != DefaultLimits().MaxToolResultChars {
		t.Fatalf("default tool result chars = %d, want %d", merged.MaxToolResultChars, DefaultLimits().MaxToolResultChars)
	}
	overridePath := filepath.Join(t.TempDir(), "seele.yaml")
	if err := os.WriteFile(overridePath, []byte("limits:\n  max_tool_result_chars: 30000\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	overridden, err := LoadLimits(overridePath)
	if err != nil {
		t.Fatal(err)
	}
	if got := overridden.WithDefaults().MaxToolResultChars; got != 30000 {
		t.Fatalf("override tool result chars = %d, want 30000", got)
	}
}

// TestLoadLimitsRejectsNegative 验证负值显式报错（避免静默吞掉错误配置）。
func TestLoadLimitsRejectsNegative(t *testing.T) {
	path := filepath.Join(t.TempDir(), "seele.yaml")
	if err := os.WriteFile(path, []byte("limits:\n  evidence_chars: -1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadLimits(path); err == nil {
		t.Fatal("negative value must be rejected")
	}
	// max_tool_result_chars 负值同样显式报错。
	path = filepath.Join(t.TempDir(), "seele.yaml")
	if err := os.WriteFile(path, []byte("limits:\n  max_tool_result_chars: -1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadLimits(path); err == nil {
		t.Fatal("negative max_tool_result_chars must be rejected")
	}
	// docker_start_timeout 负值显式报错。
	path = filepath.Join(t.TempDir(), "seele.yaml")
	if err := os.WriteFile(path, []byte("limits:\n  docker_start_timeout: -1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadLimits(path); err == nil {
		t.Fatal("negative docker_start_timeout must be rejected")
	}
	// work_table_rows 负值显式报错。
	path = filepath.Join(t.TempDir(), "seele.yaml")
	if err := os.WriteFile(path, []byte("limits:\n  work_table_rows: -1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadLimits(path); err == nil {
		t.Fatal("negative work_table_rows must be rejected")
	}
}

// TestLoadLimitsRejectsSoftAtOrAboveHard：软线必须**严格低于**硬线。这条约束此前只写在
// config/seelex.yaml 的注释里，代码不校验（LoadLimits 只查了每项落在 [0,100]），配反了
// 会让自主折叠每轮抢跑——症状是"一轮对话压一次"，而配置里看不出任何异常。
//
// 判定必须落在**生效值**上：只写 soft: 100（hard 缺省 98）与只把 hard 调到 90（soft 缺省
// 95）都是非法组合，而原始解析结果里"另一侧是 0"，只看原始值这两种写法都会溜过去。
func TestLoadLimitsRejectsSoftAtOrAboveHard(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		content string
		wantErr bool
	}{
		{"soft == hard", "limits:\n  context_soft_percent: 90\n  context_hard_percent: 90\n", true},
		{"soft > hard", "limits:\n  context_soft_percent: 96\n  context_hard_percent: 90\n", true},
		{"soft 100，hard 缺省（98）", "limits:\n  context_soft_percent: 100\n", true},
		{"hard 90，soft 缺省（95）", "limits:\n  context_hard_percent: 90\n", true},
		{"soft < hard", "limits:\n  context_soft_percent: 60\n  context_hard_percent: 85\n", false},
		{"只写 soft 70（< 缺省 hard 98）", "limits:\n  context_soft_percent: 70\n", false},
	} {
		path := filepath.Join(t.TempDir(), "seele.yaml")
		if err := os.WriteFile(path, []byte(testCase.content), 0o600); err != nil {
			t.Fatal(err)
		}
		limits, err := LoadLimits(path)
		if testCase.wantErr {
			if err == nil {
				t.Fatalf("%s：必须报错，实际通过（%+v）", testCase.name, limits)
			}
			// 报错必须同时点名两个键（只说"配置非法"等于让用户去猜是哪一对）。
			if message := err.Error(); !strings.Contains(message, "context_soft_percent") ||
				!strings.Contains(message, "context_hard_percent") {
				t.Fatalf("%s：报错没有点名两个键：%v", testCase.name, err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s：不该报错：%v", testCase.name, err)
		}
	}
}

// TestLimitsDurations 验证秒字段 → time.Duration 转换。
func TestLimitsDurations(t *testing.T) {
	full := DefaultLimits()
	toolCall, approval, planDecision, heartbeat, replanWindow, searchTimeout := full.Durations()
	if toolCall != 30*time.Minute || approval != 10*time.Minute || heartbeat != 15*time.Second || searchTimeout != 15*time.Second {
		t.Fatalf("durations = %v %v %v %v", toolCall, approval, heartbeat, searchTimeout)
	}
	if planDecision != 10*time.Second || replanWindow != time.Minute {
		t.Fatalf("durations = %v %v", planDecision, replanWindow)
	}
}

// TestLimitsSearchTimeoutAlias 验证旧字段 tavily_timeout 仍兼容，且
// search_timeout 优先于旧字段。
func TestLimitsSearchTimeoutAlias(t *testing.T) {
	path := filepath.Join(t.TempDir(), "seele.yaml")
	if err := os.WriteFile(path, []byte("limits:\n  tavily_timeout: 30\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	limits, err := LoadLimits(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := limits.WithDefaults().SearchTimeoutSec; got != 30 {
		t.Fatalf("tavily_timeout alias should map to search_timeout 30, got %d", got)
	}

	path = filepath.Join(t.TempDir(), "seele.yaml")
	if err := os.WriteFile(path, []byte("limits:\n  search_timeout: 25\n  tavily_timeout: 30\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	limits, err = LoadLimits(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := limits.WithDefaults().SearchTimeoutSec; got != 25 {
		t.Fatalf("search_timeout should take precedence, got %d", got)
	}
}

// TestLimitsAsyncExecDefaultsOff 验证作业面的开关语义：缺省（配置文件缺失、
// limits 段缺 async_exec、或块在但 enabled 未写）= 关闭；只有显式 enabled: true
// 才打开。关闭是安全侧（作业工具都不注册、旧入参 background 直接拒绝），所以缺省
// 必须是关——能力不可实施时拒绝，不静默降级成同步执行。
func TestLimitsAsyncExecDefaultsOff(t *testing.T) {
	limits, err := LoadLimits(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if limits.WithDefaults().AsyncExec.Enabled {
		t.Fatal("配置文件缺失时后台命令切片必须关闭")
	}

	absent := filepath.Join(t.TempDir(), "seele.yaml")
	if err := os.WriteFile(absent, []byte("limits:\n  tool_call_timeout: 60\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	limits, err = LoadLimits(absent)
	if err != nil {
		t.Fatal(err)
	}
	if full := limits.WithDefaults(); full.AsyncExec.Enabled || full.ToolCallTimeoutSec != 60 {
		t.Fatalf("缺 async_exec 段时必须关闭且不影响其它字段: %+v", full.AsyncExec)
	}

	enabled := filepath.Join(t.TempDir(), "seele.yaml")
	if err := os.WriteFile(enabled, []byte("limits:\n  async_exec:\n    enabled: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	limits, err = LoadLimits(enabled)
	if err != nil {
		t.Fatal(err)
	}
	if !limits.WithDefaults().AsyncExec.Enabled {
		t.Fatal("显式 enabled: true 必须打开后台命令切片")
	}
}
