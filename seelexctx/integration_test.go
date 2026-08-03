package seelexctx_test

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	frameworkSession "github.com/RedHuang-0622/Seele/session"
	"github.com/RedHuang-0622/seelex/seelebridge"
	"github.com/RedHuang-0622/seelex/seelexctx"
	"github.com/RedHuang-0622/seelex/seelexctx/compactor"
	"github.com/RedHuang-0622/seelex/seelexctx/merger"
	"github.com/RedHuang-0622/seelex/seelexctx/provider"
	"github.com/RedHuang-0622/seelex/seelexctx/snapshot"
)

func createTestSession(t *testing.T) (*frameworkSession.Session, *seelebridge.Runtime) {
	t.Helper()
	configPath := t.TempDir() + "/accounts.yaml"
	config := []byte("roles:\n  agent:\n    - model: test-model\n      base_url: http://localhost\n      api_key: test-only\n")
	if err := os.WriteFile(configPath, config, 0o644); err != nil {
		t.Fatal(err)
	}
	runtime, err := seelebridge.NewRuntime(seelebridge.RuntimeConfig{
		AccountsPath: configPath, ToolCallTimeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := runtime.NewMainSession(nil)
	if err != nil {
		t.Fatal(err)
	}
	return sess, runtime
}

func TestIntegration_ExportFull(t *testing.T) {
	eng, agt := createTestSession(t)
	defer agt.Shutdown()
	eng.SetSystemPrompt("test prompt")
	snap := seelexctx.Export(eng)
	if snap == nil {
		t.Fatal("Export returned nil")
	}
	if snap.SourceSessionID == "" {
		t.Fatal("expected SourceSessionID")
	}
}

func TestIntegration_ExportWithGoal(t *testing.T) {
	eng, agt := createTestSession(t)
	defer agt.Shutdown()
	snap := seelexctx.ExportWithGoal(eng, "集成测试")
	if snap.Goal != "集成测试" {
		t.Fatalf("got %q", snap.Goal)
	}
}

func TestIntegration_ImportAppend(t *testing.T) {
	eng1, agt1 := createTestSession(t)
	defer agt1.Shutdown()
	eng2, agt2 := createTestSession(t)
	defer agt2.Shutdown()

	eng1.SetSystemPrompt("父代理角色")
	snap := seelexctx.ExportWithGoal(eng1, "测试继承")
	eng2.SetSystemPrompt("子代理角色")
	seelexctx.Import(eng2, snap)

	prompt := getPrompt(t, eng2)
	if !strings.Contains(prompt, "子代理角色") {
		t.Fatal("original prompt should be preserved")
	}
	if !strings.Contains(prompt, "测试继承") {
		t.Fatal("goal should be appended")
	}
}

func TestIntegration_ProviderImportRoundTrip(t *testing.T) {
	eng1, agt1 := createTestSession(t)
	defer agt1.Shutdown()
	eng2, agt2 := createTestSession(t)
	defer agt2.Shutdown()

	eng1.SetSystemPrompt("父代理")
	eng2.SetSystemPrompt("子代理")

	// Use provider interface
	p := provider.NewEngineProvider(eng1)
	snap, err := p.Export(nil)
	if err != nil {
		t.Fatal(err)
	}
	snap.SetGoal("end-to-end").AddDecision("方案A", "性能更好").AddFinding("需要优化")

	seelexctx.Import(eng2, snap)
	prompt := getPrompt(t, eng2)
	for _, s := range []string{"end-to-end", "方案A", "需要优化"} {
		if !strings.Contains(prompt, s) {
			t.Fatalf("expected %q in prompt", s)
		}
	}
}

func TestIntegration_MergeBackChain(t *testing.T) {
	eng, agt := createTestSession(t)
	defer agt.Shutdown()

	parent := seelexctx.ExportWithGoal(eng, "主任务")
	parent.AddFinding("发现1").AddDecision("决策1", "因为1")
	child := &snapshot.ContextSnapshot{
		SourceSessionID: "child", ExportedAt: time.Now(), Goal: "子任务",
	}
	child.AddFinding("发现2").AddDecision("决策2", "因为2").SetProgress("完成")
	m := merger.NewMerger()
	if err := m.MergeBack(parent, child); err != nil {
		t.Fatal(err)
	}
	if len(parent.Findings) != 2 {
		t.Fatalf("got %d", len(parent.Findings))
	}
	if len(parent.Decisions) != 2 {
		t.Fatalf("got %d", len(parent.Decisions))
	}
	if parent.Progress != "完成" {
		t.Fatalf("got %q", parent.Progress)
	}
}

func TestIntegration_CompactorRealSnapshot(t *testing.T) {
	eng, agt := createTestSession(t)
	defer agt.Shutdown()

	snap := seelexctx.Export(eng)
	snap.SetGoal("压缩测试")
	for i := 0; i < 25; i++ {
		snap.AddFinding(fmt.Sprintf("测试发现 #%d", i))
	}
	c := compactor.NewCompactor()
	r, err := c.Compact(snap, 200)
	if err != nil {
		t.Fatal(err)
	}
	if r.Goal != "压缩测试" {
		t.Fatal("goal should be preserved")
	}
}

func TestIntegration_Validate(t *testing.T) {
	eng, agt := createTestSession(t)
	defer agt.Shutdown()
	snap := seelexctx.ExportWithGoal(eng, "验证测试")
	if err := snap.Validate(); err != nil {
		t.Fatalf("expected valid: %v", err)
	}
}

func TestIntegration_ImportNoExistingPrompt(t *testing.T) {
	eng, agt := createTestSession(t)
	defer agt.Shutdown()
	snap := &snapshot.ContextSnapshot{SourceSessionID: "s", ExportedAt: time.Now(), Goal: "直注入"}
	seelexctx.Import(eng, snap)
	prompt := getPrompt(t, eng)
	if !strings.Contains(prompt, "直注入") {
		t.Fatal("expected goal in prompt")
	}
}

func getPrompt(t *testing.T, eng *frameworkSession.Session) string {
	t.Helper()
	for _, m := range eng.History() {
		if m.Role == "system" && m.Content != nil {
			return *m.Content
		}
	}
	return ""
}
