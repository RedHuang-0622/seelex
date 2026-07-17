package plugin

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoaderReadsManifestAndPrivateSkills(t *testing.T) {
	root := t.TempDir()
	pluginRoot := filepath.Join(root, "mechanical")
	mustPluginWrite(t, filepath.Join(pluginRoot, "server.py"), "print('ok')")
	mustPluginWrite(t, filepath.Join(pluginRoot, "skills", "draw", "SKILL.md"), "# Draw\n")
	mustPluginWrite(t, filepath.Join(pluginRoot, manifestFile), `---
schema_version: 1
name: mechanical
description: Mechanical design
include: [cad_*]
mcp_servers:
  - name: freecad
    transport: stdio
    command: FreeCADCmd
    args: ["./server.py"]
---
# Mechanical
`)

	plugins, err := NewLoader(root).LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(plugins) != 1 || len(plugins[0].Skills) != 1 || len(plugins[0].MCPServers) != 1 {
		t.Fatalf("unexpected plugin: %#v", plugins)
	}
	wantArg := filepath.Join(pluginRoot, "server.py")
	if plugins[0].MCPServers[0].Args[0] != wantArg {
		t.Fatalf("arg=%q want=%q", plugins[0].MCPServers[0].Args[0], wantArg)
	}
}

func TestLoaderRejectsMismatchedNameAndEscapingPath(t *testing.T) {
	tests := []string{
		"---\nname: other\n---\n# x\n",
		"---\nname: safe\nmcp_servers:\n  - name: bad\n    command: ./x\n    args: [../escape]\n---\n# x\n",
		"---\nname: safe\nmcp_servers:\n  - name: bad\n    command: ./x\n    args: ['..\\escape']\n---\n# x\n",
	}
	for i, content := range tests {
		root := t.TempDir()
		mustPluginWrite(t, filepath.Join(root, "safe", manifestFile), content)
		if _, err := NewLoader(root).LoadAll(); err == nil {
			t.Fatalf("case %d should fail", i)
		}
	}
}

func TestLoaderLookupFallbackDescriptionAndErrors(t *testing.T) {
	root := t.TempDir()
	mustPluginWrite(t, filepath.Join(root, "simple", manifestFile), "---\nschema_version: 1\n---\n# Simple plugin\n")
	loader := NewLoader(root)
	loaded, err := loader.Load("simple")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Description != "Simple plugin" {
		t.Fatalf("description=%q", loaded.Description)
	}
	if _, err := loader.Load("missing"); err == nil {
		t.Fatal("missing plugin should fail")
	}
	if _, err := loader.Load("../bad"); err == nil {
		t.Fatal("invalid plugin should fail")
	}

	unsupported := t.TempDir()
	mustPluginWrite(t, filepath.Join(unsupported, "future", manifestFile), "---\nschema_version: 2\n---\n# Future\n")
	if _, err := NewLoader(unsupported).LoadAll(); err == nil {
		t.Fatal("future schema should fail")
	}

	duplicate := t.TempDir()
	mustPluginWrite(t, filepath.Join(duplicate, "dup", manifestFile), "---\nmcp_servers:\n  - name: same\n    command: x\n  - name: same\n    command: y\n---\n# Dup\n")
	if _, err := NewLoader(duplicate).LoadAll(); err == nil {
		t.Fatal("duplicate MCP should fail")
	}
}

func mustPluginWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
