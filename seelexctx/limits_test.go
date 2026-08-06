package seelexctx

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

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
	// max_tool_result_chars：未配置 → 默认 20000；显式配置 → 覆盖生效。
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
}

// TestLimitsDurations 验证秒字段 → time.Duration 转换。
func TestLimitsDurations(t *testing.T) {
	full := DefaultLimits()
	toolCall, approval, planDecision, heartbeat, replanWindow, tavily := full.Durations()
	if toolCall != 30*time.Minute || approval != 10*time.Minute || heartbeat != 15*time.Second || tavily != 15*time.Second {
		t.Fatalf("durations = %v %v %v %v", toolCall, approval, heartbeat, tavily)
	}
	if planDecision != 10*time.Second || replanWindow != time.Minute {
		t.Fatalf("durations = %v %v", planDecision, replanWindow)
	}
}
