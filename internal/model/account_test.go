package model

import (
	"strings"
	"testing"
)

func TestResolveAccountSpecDoesNotCrossRoleForCredential(t *testing.T) {
	specs := []AccountSpec{
		{Name: "agent-1", Role: RoleAgent, APIKey: "agent-key"},
		{Name: "subagent-1", Role: RoleSubAgent, APIKey: ""},
	}
	if _, err := ResolveAccountSpec(specs, RoleSubAgent); err == nil ||
		!strings.Contains(err.Error(), "no account has api_key") {
		t.Fatalf("subagent 空密钥必须报错，实际 err=%v（不得偷偷用 agent 的密钥）", err)
	}
}

func TestResolveAccountSpecFallsBackOnlyWhenRoleBlockIsAbsent(t *testing.T) {
	specs := []AccountSpec{
		{Name: "agent-1", Role: RoleAgent, APIKey: "agent-key"},
	}
	got, err := ResolveAccountSpec(specs, RoleSubAgent)
	if err != nil || got.Name != "agent-1" || got.APIKey != "agent-key" {
		t.Fatalf("未配置 subagent 时应回退 agent，实际 %+v err=%v", got, err)
	}
}

func TestResolveAccountSpecDoesNotUseUnrelatedRole(t *testing.T) {
	specs := []AccountSpec{
		{Name: "subagent-1", Role: RoleSubAgent, APIKey: "sub-key"},
	}
	if _, err := ResolveAccountSpec(specs, RoleAgent); err == nil {
		t.Fatal("没有 agent 档时不得拿 subagent 的密钥冒充 agent")
	}
}
