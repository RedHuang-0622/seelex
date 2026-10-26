package session

import (
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/contract/protocol"
)

type sshTestSessions struct {
	state model.SessionSSHInfo
}

func (sessions *sshTestSessions) SessionSSHState(string) (model.SessionSSHInfo, error) {
	return sessions.state, nil
}

func (sessions *sshTestSessions) SetSessionSSHState(_ string, state model.SessionSSHInfo) error {
	sessions.state = state
	return nil
}

type sshTestWorkspace struct {
	environments []model.WorkspaceEnvironmentInfo
	boundID      string
}

func (workspace *sshTestWorkspace) SessionWorkspace(string) (model.WorkspaceInfo, bool) {
	return model.WorkspaceInfo{ID: workspace.boundID}, workspace.boundID != ""
}

func (workspace *sshTestWorkspace) WorkspaceEnvironments(string) ([]model.WorkspaceEnvironmentInfo, string, error) {
	return workspace.environments, "", nil
}

func TestValidateAllowedEnvsRejectsNamesOutsideProject(t *testing.T) {
	known := map[string]struct{}{"dev": {}, "test": {}}

	if _, err := ValidateAllowedEnvs(known, []string{"prod"}); err == nil {
		t.Fatal("environment outside project must be rejected")
	}
	if _, err := ValidateAllowedEnvs(known, []string{"dev", "prod"}); err == nil {
		t.Fatal("one unknown environment must reject the whole set")
	}

	got, err := ValidateAllowedEnvs(known, []string{" dev ", "", "test", "dev"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "dev" || got[1] != "test" {
		t.Fatalf("normalized environments = %#v, want [dev test]", got)
	}

	empty, err := ValidateAllowedEnvs(known, nil)
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("empty set = %#v, %v; want non-nil empty slice", empty, err)
	}
}

func TestSetEnvFixesEnvironmentOnce(t *testing.T) {
	sessions := &sshTestSessions{}
	workspace := &sshTestWorkspace{
		boundID: "project-a",
		environments: []model.WorkspaceEnvironmentInfo{
			{Name: "dev"},
			{Name: "test"},
		},
	}
	application := NewSSHApplication(SSHDeps{
		Sessions:  func() SessionSSHPort { return sessions },
		Workspace: func() WorkspacePort { return workspace },
	})

	state, err := application.SetEnv("session-a", "project-a", "test")
	if err != nil || state.CurrentEnv != "test" {
		t.Fatalf("SetEnv = %+v, %v", state, err)
	}
	if _, err := application.SetEnv("session-a", "project-a", "test"); err != nil {
		t.Fatalf("setting same environment must be idempotent: %v", err)
	}
	if _, err := application.SetEnv("session-a", "project-a", "dev"); err == nil {
		t.Fatal("fixed environment must not change")
	}
	if _, err := application.SetEnv("session-a", "project-a", ""); err == nil {
		t.Fatal("fixed environment must not be cleared")
	}
}

func TestSetAllowed(t *testing.T) {
	sessions := &sshTestSessions{}
	workspace := &sshTestWorkspace{
		boundID: "project-a",
		environments: []model.WorkspaceEnvironmentInfo{
			{Name: "dev"},
			{Name: "test"},
		},
	}
	application := NewSSHApplication(SSHDeps{
		Sessions:  func() SessionSSHPort { return sessions },
		Workspace: func() WorkspacePort { return workspace },
	})

	state, err := application.SetAllowed("session-a", []string{" dev ", "test", "dev"})
	if err != nil {
		t.Fatal(err)
	}
	if len(state.AllowedEnvs) != 2 || state.AllowedEnvs[0] != "dev" || state.AllowedEnvs[1] != "test" {
		t.Fatalf("allowed environments = %#v", state.AllowedEnvs)
	}
}

func TestSSHApplicationUnavailable(t *testing.T) {
	application := NewSSHApplication(SSHDeps{})
	if _, err := application.State("session-a"); err != ErrSessionSSHUnavailable {
		t.Fatalf("State error = %v, want ErrSessionSSHUnavailable", err)
	}
	if _, err := application.SetEnv("session-a", "project-a", "dev"); err != ErrSessionSSHUnavailable {
		t.Fatalf("SetEnv error = %v, want ErrSessionSSHUnavailable", err)
	}
}

func TestSSHApplicationRejectsMissingEnvironment(t *testing.T) {
	sessions := &sshTestSessions{}
	workspace := &sshTestWorkspace{
		boundID:      "project-a",
		environments: []model.WorkspaceEnvironmentInfo{{Name: "dev"}},
	}
	application := NewSSHApplication(SSHDeps{
		Sessions:  func() SessionSSHPort { return sessions },
		Workspace: func() WorkspacePort { return workspace },
	})
	if _, err := application.SetEnv("session-a", "project-a", "prod"); err == nil ||
		!strings.Contains(err.Error(), "已经不在项目环境表里") {
		t.Fatalf("missing environment error = %v", err)
	}
}
