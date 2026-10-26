package application_test

import "testing"

func TestAdminRoleWorkspaceCompatibilityMethodsLiveOutsideService(t *testing.T) {
	methods := map[string]bool{}
	for _, method := range exportedServiceMethods() {
		methods[method] = true
	}
	for _, retired := range []string{
		"AddAccount", "AccountDetails", "UpdateAccount", "DeleteAccount",
		"AccountBalance", "SetAccountWindow",
		"RoleDefaultVector", "SetRoleDefaultVector", "RoleAccount", "SetRoleAccount",
		"RoleInheritMainAgent", "SetRoleInheritMainAgent", "RoleSSHUserSpaces",
		"RoleDefaultSSHUserSpaces", "SetRoleSSHUserSpaces", "SetRoleSessionSSHUserSpaces",
		"RolePermissionVector", "SetRolePermissionVector",
		"CreateRoleSession", "AppendRoleDraft", "ReadRoleDraft", "SyncRoleDraft",
		"AppendRoleSessionRows", "ReadRoleSessionRows", "RoleSnapshot", "AssembleRoleWire",
		"SetLifecycleOrder", "SetRoleLifecycle", "ListRoleSessions",
		"ScheduleRegister", "ScheduleCancel", "ScheduleFire",
		"WorkspaceEnvironments", "SetWorkspaceEnvironment", "DeleteWorkspaceEnvironment",
		"SetDefaultWorkspaceEnvironment", "WorkspaceGitRepos",
		"DeleteSession", "CreateWorkspace", "CreateWorkspaces", "BindWorkspace",
		"UnbindWorkspace", "WorkspaceTree", "WorkspaceFileCount", "WorkspaceGitLog",
		"WorkspaceChanges", "WorkspaceGitCommitDetail", "WorkspaceFileContent",
		"WorkspaceGitCommitFileContent", "WorkspaceWriteFile",
	} {
		if methods[retired] {
			t.Errorf("%s must not remain a Service public compatibility method", retired)
		}
	}
}
