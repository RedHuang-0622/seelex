package seelebridge

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/seelebridge/mcp"
	"github.com/RedHuang-0622/seelex/seelebridge/security"
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
		if !hasSuitableBash() {
			t.Skipf("no suitable POSIX bash on this host; skipping bash scoping assertion: result=%q err=%v", result, err)
		}
		t.Fatalf("bash did not use project root: result=%q err=%v", result, err)
	}
}

// hasSuitableBash 报告是否存在可供 bash 工具使用的 POSIX bash（固定 Git 路径
// 或非 WSL 的 PATH bash）。WSL bash 自 9.5.3 起被 bash 工具排除（冷启动慢、
// 弹控制台、输出 localhost 代理警告）；仅剩 WSL bash 的机器上 bash 工具回退
// PowerShell，无法解析 POSIX 语法，相关断言跳过而非失败。
func hasSuitableBash() bool {
	if runtime.GOOS != "windows" {
		return true // 非 Windows 必有 /bin/bash 或 PATH bash
	}
	for _, bash := range []string{
		`C:\Program Files\Git\bin\bash.exe`,
		`C:\Program Files\Git\usr\bin\bash.exe`,
		`C:\Program Files (x86)\Git\bin\bash.exe`,
	} {
		if security.FileExists(bash) {
			return true
		}
	}
	if bash, err := exec.LookPath("bash"); err == nil && !security.IsWSLBash(bash) {
		return true
	}
	return false
}
