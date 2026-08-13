package seelebridge

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/seelebridge/mcp"
)

func TestFrameworkMCPValidation(t *testing.T) {
	tests := []struct {
		name string
		cfg  MCPServer
		ok   bool
	}{
		{name: "stdio inferred", cfg: MCPServer{Name: "fs", Command: "npx"}, ok: true},
		{name: "sse inferred", cfg: MCPServer{Name: "web", URL: "http://localhost"}, ok: true},
		{name: "empty name", cfg: MCPServer{Command: "x"}},
		{name: "missing command", cfg: MCPServer{Name: "x", Transport: "stdio"}},
		{name: "invalid transport", cfg: MCPServer{Name: "x", Transport: "http"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := mcp.ToFramework(tt.cfg)
			if (err == nil) != tt.ok {
				t.Fatalf("err=%v ok=%v", err, tt.ok)
			}
		})
	}
}
func TestRuntimeBuiltinsAndMCPEmptyState(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	runtime.RegisterBuiltins()
	runtime.RegisterTool("read_file", "unsafe override", map[string]interface{}{"type": "object"}, func(context.Context, string) (string, error) {
		return "unsafe", nil
	})
	if len(runtime.AllTools()) == 0 || runtime.Agent() == nil {
		t.Fatal("builtins or Agent accessor missing")
	}
	registered := make(map[string]bool)
	for _, tool := range runtime.AllTools() {
		registered[tool.Name] = true
	}
	for _, name := range []string{"plan_load", "plan_run", "plan_status", "plan_export", "plan_clear"} {
		if !registered[name] {
			t.Errorf("initial builtin tools are missing %q", name)
		}
	}
	if names := runtime.MCPServerNames(); len(names) != 0 {
		t.Fatalf("unexpected MCP servers: %v", names)
	}
	if err := runtime.DetachMCP("missing"); err != nil {
		t.Fatal(err)
	}
	if err := runtime.RefreshMCP(context.Background(), "missing"); err == nil {
		t.Fatal("refreshing missing MCP should fail")
	}
}
func TestRuntimeProjectScopedToolsUseBoundProject(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	runtime.RegisterBuiltins()

	projectA := t.TempDir()
	projectB := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectA, "marker.txt"), []byte("project-a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectB, "marker.txt"), []byte("project-b"), 0o644); err != nil {
		t.Fatal(err)
	}
	if result, err := runtime.Agent().DirectDispatch(context.Background(), "read_file", `{"path":"marker.txt"}`); err == nil || result == "unsafe" {
		t.Fatal("unbound read_file must fail closed")
	}
	if err := runtime.BindProjectRoot(projectA); err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Agent().DirectDispatch(context.Background(), "read_file", `{"path":"marker.txt"}`)
	if err != nil || result != "project-a" {
		t.Fatalf("project A read = %q, err=%v", result, err)
	}
	if _, err := runtime.Agent().DirectDispatch(context.Background(), "read_file", `{"path":"../marker.txt"}`); err == nil {
		t.Fatal("read_file traversal must fail")
	}
	if err := runtime.BindProjectRoot(projectB); err != nil {
		t.Fatal(err)
	}
	result, err = runtime.Agent().DirectDispatch(context.Background(), "read_file", `{"path":"marker.txt"}`)
	if err != nil || result != "project-b" {
		t.Fatalf("project B read = %q, err=%v", result, err)
	}
	result, err = runtime.Agent().DirectDispatch(context.Background(), "bash", `{"command":"pwd && ls -la","timeout":10}`)
	if err != nil || !strings.Contains(result, filepath.Base(projectB)) {
		t.Fatalf("bash did not use project root: result=%q err=%v", result, err)
	}
}
