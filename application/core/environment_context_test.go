package core

import (
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/protocol"
)

type environmentTraceWorkspace struct {
	*fakeWorkspace
	environments []model.WorkspaceEnvironmentInfo
	defaultEnv   string
}

func (workspace *environmentTraceWorkspace) WorkspaceEnvironments(string) ([]model.WorkspaceEnvironmentInfo, string, error) {
	return append([]model.WorkspaceEnvironmentInfo(nil), workspace.environments...), workspace.defaultEnv, nil
}

func (*environmentTraceWorkspace) SetWorkspaceEnvironment(string, string, model.WorkspaceEnvironmentChange) (model.WorkspaceEnvironmentInfo, error) {
	return model.WorkspaceEnvironmentInfo{}, nil
}

func (*environmentTraceWorkspace) DeleteWorkspaceEnvironment(string, string) error { return nil }

func (*environmentTraceWorkspace) SetDefaultWorkspaceEnvironment(string, string) error {
	return nil
}

type environmentTraceSessions struct {
	fakeSessions
	state model.SessionSSHInfo
}

func (sessions *environmentTraceSessions) SessionSSHState(sessionID string) (model.SessionSSHInfo, error) {
	sessions.state.SessionID = sessionID
	return sessions.state, nil
}

func (sessions *environmentTraceSessions) SetSessionSSHState(_ string, state model.SessionSSHInfo) error {
	sessions.state = state
	return nil
}

func TestRenderEnvironmentTraceBlockListsModeAddressAndCurrent(t *testing.T) {
	block := renderEnvironmentTraceBlock("/workspace/app", "dev", "test", []model.WorkspaceEnvironmentInfo{
		{Name: "prod", Host: "prod.example.com", Port: 2222, User: "deploy", RemoteRoot: "/srv/app"},
		{Name: "dev", Note: "local checkout"},
		{Name: "test", Host: "test.example.com", Port: 22, User: "qa", RemoteRoot: "/srv/test"},
	})
	if !strings.HasPrefix(block, environmentTraceMarkerOpen) || !strings.HasSuffix(block, environmentTraceMarkerClose) {
		t.Fatalf("环境块必须带标记: %q", block)
	}
	for _, want := range []string{
		"- dev · local · /workspace/app · 本会话不可切换 / 项目默认",
		"- test · ssh · qa@test.example.com:22 · remote_root=/srv/test · 当前固定",
		"- prod · ssh · deploy@prod.example.com:2222 · remote_root=/srv/app",
		"本会话固定环境：test",
		"subagent / teammate 继承同一绑定",
		"agent 不得通过工具参数切换环境或传输",
	} {
		if !strings.Contains(block, want) {
			t.Fatalf("环境块缺 %q:\n%s", want, block)
		}
	}
	if strings.Contains(block, "password") || strings.Contains(block, "secret") {
		t.Fatalf("环境块不得泄露凭据字段: %s", block)
	}
}

func TestRenderEnvironmentTraceBlockSkipsEmptyTable(t *testing.T) {
	if got := renderEnvironmentTraceBlock("/workspace/app", "", "", nil); got != "" {
		t.Fatalf("空环境表不该注入空壳: %q", got)
	}
}

func TestRenderEnvironmentTraceBlockWithoutBindingStaysLocal(t *testing.T) {
	block := renderEnvironmentTraceBlock("/workspace/app", "", "", []model.WorkspaceEnvironmentInfo{
		{Name: "dev"},
	})
	if !strings.Contains(block, "本会话未绑定命名环境") || !strings.Contains(block, "- dev · local · /workspace/app") {
		t.Fatalf("block = %s", block)
	}
}

func TestEnvironmentTraceBlockForUsesProjectAndSessionState(t *testing.T) {
	workspace := &environmentTraceWorkspace{
		fakeWorkspace: newFakeWorkspace(),
		defaultEnv:    "dev",
		environments: []model.WorkspaceEnvironmentInfo{
			{Name: "dev", Note: "local"},
			{Name: "test", Host: "test.example.com", Port: 22, User: "qa", RemoteRoot: "/srv/test"},
		},
	}
	project, err := workspace.Create("app", "/workspace/app", "")
	if err != nil {
		t.Fatal(err)
	}
	workspace.BindSession("sess-env", project.ID)
	sessions := &environmentTraceSessions{state: model.SessionSSHInfo{SessionID: "sess-env", CurrentEnv: "test"}}
	service := newTestService(t, &fakeEngine{},
		func(deps *contract.Dependencies) { deps.Workspace = workspace },
		withTestSessions(sessions),
	)

	block := service.environmentTraceBlockFor("sess-env")
	if !strings.Contains(block, "- dev · local · /workspace/app") ||
		!strings.Contains(block, "- test · ssh · qa@test.example.com:22 · remote_root=/srv/test · 当前固定") {
		t.Fatalf("service block = %s", block)
	}
}
