package plugin

import (
	"context"
	"errors"
	"testing"

	"github.com/RedHuang-0622/seelex/skill"
)

type fakeTools struct {
	defined    map[string]bool
	active     string
	fail       string
	defineFail string
}

func (f *fakeTools) DefinePlugin(name, _ string, _, _ []string) error {
	if name == f.defineFail {
		return errors.New("define failed")
	}
	if f.defined == nil {
		f.defined = make(map[string]bool)
	}
	f.defined[name] = true
	return nil
}
func (f *fakeTools) UndefinePlugin(name string) { delete(f.defined, name) }
func (f *fakeTools) ActivatePlugin(name string) error {
	if name == f.fail {
		return errors.New("activate failed")
	}
	f.active = name
	return nil
}
func (f *fakeTools) DeactivatePlugin()    { f.active = "" }
func (f *fakeTools) ActivePlugin() string { return f.active }

type fakeMCP struct {
	attached   map[string]bool
	fail       string
	detachFail string
}

func (f *fakeMCP) AttachMCPServer(_ context.Context, name, _, _ string, _, _ []string, _ string) error {
	if name == f.fail {
		return errors.New("attach failed")
	}
	if f.attached == nil {
		f.attached = make(map[string]bool)
	}
	f.attached[name] = true
	return nil
}
func (f *fakeMCP) DetachMCP(name string) error {
	if name == f.detachFail {
		return errors.New("detach failed")
	}
	delete(f.attached, name)
	return nil
}

type fakeSkills struct {
	active         string
	fail           string
	publishFail    string
	deactivateFail bool
	published      map[string]bool
}

func (f *fakeSkills) PublishPluginSkills(name string, _ []skill.Skill) error {
	if name == f.publishFail {
		return errors.New("publish failed")
	}
	if f.published == nil {
		f.published = make(map[string]bool)
	}
	f.published[name] = true
	return nil
}
func (f *fakeSkills) ClearPluginSkills(name string) { delete(f.published, name) }
func (f *fakeSkills) ActivatePluginSkills(name string) error {
	if name == f.fail {
		return errors.New("skill activation failed")
	}
	f.active = name
	return nil
}
func (f *fakeSkills) DeactivatePluginSkills() error {
	if f.deactivateFail {
		return errors.New("skill deactivation failed")
	}
	f.active = ""
	return nil
}

func TestManagerActivateAndRollback(t *testing.T) {
	tools := &fakeTools{}
	mcp := &fakeMCP{}
	skills := &fakeSkills{}
	m := NewManager(NewLoader(), tools, mcp, skills)
	m.plugins = map[string]Plugin{
		"old": {Name: "old", MCPServers: []MCPServer{{Name: "one"}}},
		"new": {Name: "new", MCPServers: []MCPServer{{Name: "two"}}},
	}
	if err := m.Activate(context.Background(), "old"); err != nil {
		t.Fatal(err)
	}
	tools.fail = "new"
	if err := m.Activate(context.Background(), "new"); err == nil {
		t.Fatal("activation should fail")
	}
	if tools.active != "old" || skills.active != "old" || !mcp.attached["old__one"] || m.current != "old" {
		t.Fatalf("rollback failed: tools=%q skills=%q mcp=%v current=%q", tools.active, skills.active, mcp.attached, m.current)
	}
}

func TestManagerAttachFailureKeepsPrevious(t *testing.T) {
	tools := &fakeTools{}
	mcp := &fakeMCP{}
	skills := &fakeSkills{}
	m := NewManager(NewLoader(), tools, mcp, skills)
	m.plugins = map[string]Plugin{
		"old": {Name: "old", MCPServers: []MCPServer{{Name: "one"}}},
		"new": {Name: "new", MCPServers: []MCPServer{{Name: "two"}}},
	}
	if err := m.Activate(context.Background(), "old"); err != nil {
		t.Fatal(err)
	}
	mcp.fail = "new__two"
	if err := m.Activate(context.Background(), "new"); err == nil {
		t.Fatal("MCP attach should fail")
	}
	if !mcp.attached["old__one"] || m.current != "old" {
		t.Fatalf("previous plugin not restored: %#v current=%q", mcp.attached, m.current)
	}
}

func TestManagerLoadActivateDeactivateAndList(t *testing.T) {
	root := t.TempDir()
	mustPluginWrite(t, root+"/one/"+manifestFile, "---\nname: one\n---\n# One\n")
	mustPluginWrite(t, root+"/two/"+manifestFile, "---\nname: two\n---\n# Two\n")
	tools := &fakeTools{}
	mcp := &fakeMCP{}
	skills := &fakeSkills{}
	m := NewManager(NewLoader(root), tools, mcp, skills)
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	if len(m.All()) != 2 {
		t.Fatalf("plugins=%#v", m.All())
	}
	if err := m.Activate(context.Background(), "one"); err != nil {
		t.Fatal(err)
	}
	if err := m.Activate(context.Background(), "one"); err != nil {
		t.Fatal(err)
	}
	if current, ok := m.Current(); !ok || current.Name != "one" {
		t.Fatalf("current=%#v ok=%v", current, ok)
	}
	if err := m.Deactivate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.Current(); ok || tools.active != "" || skills.active != "" {
		t.Fatal("plugin remained active")
	}
	if err := m.Activate(context.Background(), "missing"); err == nil {
		t.Fatal("missing plugin should fail")
	}
}

func TestManagerSkillFailureRollsBack(t *testing.T) {
	tools := &fakeTools{}
	mcp := &fakeMCP{}
	skills := &fakeSkills{}
	m := NewManager(NewLoader(), tools, mcp, skills)
	m.plugins = map[string]Plugin{"old": {Name: "old"}, "new": {Name: "new"}}
	if err := m.Activate(context.Background(), "old"); err != nil {
		t.Fatal(err)
	}
	skills.fail = "new"
	if err := m.Activate(context.Background(), "new"); err == nil {
		t.Fatal("skill activation should fail")
	}
	if m.current != "old" || tools.active != "old" || skills.active != "old" {
		t.Fatalf("rollback failed current=%q tools=%q skills=%q", m.current, tools.active, skills.active)
	}
}

func TestManagerSerializesConcurrentActivation(t *testing.T) {
	tools := &fakeTools{}
	mcp := &fakeMCP{}
	skills := &fakeSkills{}
	m := NewManager(NewLoader(), tools, mcp, skills)
	m.plugins = map[string]Plugin{"one": {Name: "one"}, "two": {Name: "two"}}
	done := make(chan error, 2)
	go func() { done <- m.Activate(context.Background(), "one") }()
	go func() { done <- m.Activate(context.Background(), "two") }()
	for i := 0; i < 2; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if m.current != "one" && m.current != "two" {
		t.Fatalf("unexpected current plugin %q", m.current)
	}
}

func TestManagerLoadRollsBackDefinitionsAndSkills(t *testing.T) {
	root := t.TempDir()
	mustPluginWrite(t, root+"/one/"+manifestFile, "---\nname: one\n---\n# One\n")
	mustPluginWrite(t, root+"/two/"+manifestFile, "---\nname: two\n---\n# Two\n")
	tools := &fakeTools{}
	skills := &fakeSkills{publishFail: "two"}
	m := NewManager(NewLoader(root), tools, &fakeMCP{}, skills)

	if err := m.Load(); err == nil {
		t.Fatal("load should fail")
	}
	if len(tools.defined) != 0 || len(skills.published) != 0 || len(m.plugins) != 0 {
		t.Fatalf("load left partial state: tools=%v skills=%v plugins=%v", tools.defined, skills.published, m.plugins)
	}
}

func TestManagerDetachFailureKeepsPreviousPlugin(t *testing.T) {
	tools := &fakeTools{}
	mcp := &fakeMCP{}
	skills := &fakeSkills{}
	m := NewManager(NewLoader(), tools, mcp, skills)
	m.plugins = map[string]Plugin{
		"old": {Name: "old", MCPServers: []MCPServer{{Name: "one"}, {Name: "two"}}},
		"new": {Name: "new", MCPServers: []MCPServer{{Name: "three"}}},
	}
	if err := m.Activate(context.Background(), "old"); err != nil {
		t.Fatal(err)
	}
	mcp.detachFail = "old__one"
	if err := m.Activate(context.Background(), "new"); err == nil {
		t.Fatal("switch should fail when previous MCP cannot detach")
	}
	if m.current != "old" || tools.active != "old" || skills.active != "old" {
		t.Fatalf("previous plugin not active: current=%q tools=%q skills=%q", m.current, tools.active, skills.active)
	}
	if !mcp.attached["old__one"] || !mcp.attached["old__two"] || mcp.attached["new__three"] {
		t.Fatalf("MCP rollback failed: %v", mcp.attached)
	}
}

func TestManagerDeactivateSkillFailureRollsBack(t *testing.T) {
	tools := &fakeTools{}
	mcp := &fakeMCP{}
	skills := &fakeSkills{}
	m := NewManager(NewLoader(), tools, mcp, skills)
	m.plugins = map[string]Plugin{
		"old": {Name: "old", MCPServers: []MCPServer{{Name: "one"}}},
	}
	if err := m.Activate(context.Background(), "old"); err != nil {
		t.Fatal(err)
	}
	skills.deactivateFail = true
	if err := m.Deactivate(context.Background()); err == nil {
		t.Fatal("deactivation should fail")
	}
	if m.current != "old" || tools.active != "old" || skills.active != "old" || !mcp.attached["old__one"] {
		t.Fatalf("deactivation rollback failed: current=%q tools=%q skills=%q mcp=%v", m.current, tools.active, skills.active, mcp.attached)
	}
}
