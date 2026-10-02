package mcp

import (
	"testing"

	tools "github.com/RedHuang-0622/Seele/tools"
	types "github.com/RedHuang-0622/Seele/types"
)

// fakeProvider 是 providerSource 的测试替身：只有描述与 server 名，没有连接。
type fakeProvider struct {
	servers []string
	entries []tools.ToolEntry
}

func (p *fakeProvider) ProviderName() string  { return "mcp" }
func (p *fakeProvider) ServerNames() []string { return p.servers }
func (p *fakeProvider) Tools() []tools.ToolEntry {
	return append([]tools.ToolEntry(nil), p.entries...)
}

func entry(name, description string) tools.ToolEntry {
	return tools.ToolEntry{Definition: types.Tool{
		Type: "function",
		Function: types.ToolFunction{
			Name:        name,
			Description: description,
			Parameters:  map[string]interface{}{"type": "object"},
		},
	}}
}

func TestNotesProviderAppendsServerNotes(t *testing.T) {
	provider := &fakeProvider{
		servers: []string{"playwright"},
		entries: []tools.ToolEntry{entry("browser_navigate", "Navigate to a URL")},
	}
	decorated := &notesProvider{inner: provider, notes: map[string][]string{
		"playwright": {"only url-form calls return results"},
	}}

	got := decorated.Tools()
	want := "Navigate to a URL\n\nonly url-form calls return results"
	if got[0].Definition.Function.Description != want {
		t.Fatalf("description = %q, want %q", got[0].Definition.Function.Description, want)
	}
	if decorated.ProviderName() != "mcp" {
		t.Fatalf("ProviderName must stay the inner one, got %q", decorated.ProviderName())
	}
}

// 没有注记时必须是纯透传：描述、provider 名、条目数都不许被动。
func TestNotesProviderPassthroughWithoutNotes(t *testing.T) {
	provider := &fakeProvider{
		servers: []string{"playwright"},
		entries: []tools.ToolEntry{entry("browser_navigate", "Navigate to a URL")},
	}
	for _, notes := range []map[string][]string{nil, {}, {"other": {"x"}}} {
		got := (&notesProvider{inner: provider, notes: notes}).Tools()
		if got[0].Definition.Function.Description != "Navigate to a URL" {
			t.Fatalf("notes=%v: description changed to %q", notes, got[0].Definition.Function.Description)
		}
	}
}

// 多 server：框架给 entry 名加 `server__` 前缀，注记只落在自己那个 server 上。
func TestNotesProviderScopesNotesPerServer(t *testing.T) {
	provider := &fakeProvider{
		servers: []string{"playwright", "chrome_devtools"},
		entries: []tools.ToolEntry{
			entry("playwright__browser_navigate", "Navigate"),
			entry("chrome_devtools__navigate_page", "Navigate"),
		},
	}
	decorated := &notesProvider{inner: provider, notes: map[string][]string{
		"playwright": {"url form only"},
	}}

	got := decorated.Tools()
	if got[0].Definition.Function.Description != "Navigate\n\nurl form only" {
		t.Errorf("playwright tool not annotated: %q", got[0].Definition.Function.Description)
	}
	if got[1].Definition.Function.Description != "Navigate" {
		t.Errorf("other server's tool must stay untouched: %q", got[1].Definition.Function.Description)
	}
}

// 空白注记折叠掉：多行 folded scalar 带来的缩进/换行不该出现在描述里。
func TestAppendNotesSkipsBlankAndFoldsMultiple(t *testing.T) {
	got := appendNotes("Base", []string{"  ", "first", "", "second "})
	if got != "Base\n\nfirst\n\nsecond" {
		t.Fatalf("appendNotes = %q", got)
	}
	if blank := appendNotes("Base", []string{"", "   "}); blank != "Base" {
		t.Fatalf("all-blank notes must not add separators: %q", blank)
	}
}

func TestSetNotesKeepsRegisteredNotesOnLaterAttach(t *testing.T) {
	manager := &Manager{}
	manager.setNotes("playwright", []string{"  registered  "})
	manager.setNotes("playwright", nil) // 连接时 cfg 没带注记 → 保留登记时那份
	got := manager.notesSnapshot()
	if len(got["playwright"]) != 1 || got["playwright"][0] != "registered" {
		t.Fatalf("notes = %v, want the registered note trimmed", got)
	}
	if manager.notesSnapshot()["playwright"] == nil {
		t.Fatal("snapshot must carry the note")
	}
	// 快照是副本：改它不影响 manager 状态。
	got["playwright"][0] = "mutated"
	if again := manager.notesSnapshot(); again["playwright"][0] != "registered" {
		t.Fatalf("snapshot aliasing: %v", again)
	}
}
