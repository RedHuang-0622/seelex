package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/seelebridge"
	"github.com/RedHuang-0622/seelex/skill"
)

// newSkillToolHarness 构造 skill_activate 工具测试台：真实 skill.Registry +
// 真实 runtime（DirectDispatch 免权限），activate 注入桩记录调用名。
func newSkillToolHarness(t *testing.T) (*seelebridge.Runtime, *skill.Registry, *[]string) {
	t.Helper()
	temp := t.TempDir()
	accountsPath := filepath.Join(temp, "accounts.yaml")
	minimalAccounts := `defaults:
  provider: openai
  context_window: 200000
  max_tokens: 8192
  timeout: 120s
  temperature: 0
roles:
  agent:
    - model: test-model
      base_url: http://127.0.0.1:9/v1
      api_key: test-only-key
mcp_servers: []
`
	if err := os.WriteFile(accountsPath, []byte(minimalAccounts), 0o600); err != nil {
		t.Fatal(err)
	}
	runtime, err := seelebridge.NewRuntime(seelebridge.RuntimeConfig{
		AccountsPath:    accountsPath,
		StorePath:       filepath.Join(temp, "runtime"),
		ToolCallTimeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	t.Cleanup(runtime.Shutdown)
	skills := initSkillSystem()
	var activated []string
	registerSkillActivateTool(runtime, skills, func(name string) (skill.Skill, error) {
		activated = append(activated, name)
		if name != "s1" && name != "s2" && name != "s3" {
			return skill.Skill{}, os.ErrNotExist
		}
		return skill.Skill{Name: name, Prompt: "instructions:" + name}, nil
	})
	return runtime, skills, &activated
}

// TestSkillActivateToolCandidatesFollowPluginSwitch：候选技能列表随插件切换
// 切换（activePlugin 作用域）——s1 在 p1 下可激活，切到 p2 后不可用，p2 的
// s3 成为新候选。
func TestSkillActivateToolCandidatesFollowPluginSwitch(t *testing.T) {
	runtime, skills, activated := newSkillToolHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	dispatch := func(name, args string) (string, error) {
		return runtime.Agent().DirectDispatch(ctx, name, args)
	}

	skills.SetPluginSkills("p1", []skill.Skill{
		{Name: "s1", Description: "d1", Prompt: "p1-s1"},
		{Name: "s2", Description: "d2", Prompt: "p1-s2"},
	})
	if err := skills.ActivatePluginSkills("p1"); err != nil {
		t.Fatal(err)
	}

	// p1：s1 激活成功，正文回传
	got, err := dispatch("skill_activate", `{"name":"s1","reason":"need s1"}`)
	if err != nil {
		t.Fatalf("skill_activate s1: %v", err)
	}
	if !strings.Contains(got, `"status":"activated"`) || !strings.Contains(got, `"description":"d1"`) || !strings.Contains(got, "instructions:s1") {
		t.Fatalf("activate s1 result: %s", got)
	}
	if len(*activated) != 1 || (*activated)[0] != "s1" {
		t.Fatalf("activator calls = %v", *activated)
	}

	// p1：未知技能 → 返回当前插件候选清单
	got, err = dispatch("skill_activate", `{"name":"ghost"}`)
	if err != nil {
		t.Fatalf("skill_activate ghost: %v", err)
	}
	if !strings.Contains(got, `"status":"unknown_skill"`) || !strings.Contains(got, "s1") || !strings.Contains(got, "s2") || strings.Contains(got, "s3") {
		t.Fatalf("unknown result must list p1 candidates: %s", got)
	}

	// 切到 p2：候选换成 p2 的技能表
	skills.SetPluginSkills("p2", []skill.Skill{{Name: "s3", Description: "d3", Prompt: "p2-s3"}})
	if err := skills.ActivatePluginSkills("p2"); err != nil {
		t.Fatal(err)
	}
	got, err = dispatch("skill_activate", `{"name":"s1"}`)
	if err != nil {
		t.Fatalf("skill_activate s1 after switch: %v", err)
	}
	if !strings.Contains(got, `"status":"unknown_skill"`) || !strings.Contains(got, `"available":["s3"]`) || strings.Contains(got, "s2") {
		t.Fatalf("after switch s1 must be unknown and only s3 listed: %s", got)
	}
	if len(*activated) != 1 {
		t.Fatalf("s1 must not be activatable after switch: %v", *activated)
	}
	got, err = dispatch("skill_activate", `{"name":"s3"}`)
	if err != nil {
		t.Fatalf("skill_activate s3: %v", err)
	}
	if !strings.Contains(got, `"status":"activated"`) || !strings.Contains(got, "instructions:s3") {
		t.Fatalf("activate s3 result: %s", got)
	}
	if len(*activated) != 2 || (*activated)[1] != "s3" {
		t.Fatalf("activator calls after switch = %v", *activated)
	}
}

// TestSkillActivateToolValidation：空名报错；json 可解析。
func TestSkillActivateToolValidation(t *testing.T) {
	runtime, _, _ := newSkillToolHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := runtime.Agent().DirectDispatch(ctx, "skill_activate", `{"name":""}`); err == nil {
		t.Fatal("empty name must error")
	}
	// 解析坏 json 报错而非 panic
	if _, err := runtime.Agent().DirectDispatch(ctx, "skill_activate", `{`); err == nil {
		t.Fatal("bad json must error")
	}
}

// TestSkillActivateToolActivateErrorSurfaced：activate 返回错误 → status=error
// （模型可读原因，不炸工具）。
func TestSkillActivateToolActivateErrorSurfaced(t *testing.T) {
	runtime, skills, _ := newSkillToolHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	skills.SetPluginSkills("p1", []skill.Skill{{Name: "boom", Prompt: "x"}})
	if err := skills.ActivatePluginSkills("p1"); err != nil {
		t.Fatal(err)
	}
	got, err := runtime.Agent().DirectDispatch(ctx, "skill_activate", `{"name":"boom"}`)
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if !strings.Contains(got, `"status":"error"`) {
		t.Fatalf("expected error status, got %s", got)
	}
}
