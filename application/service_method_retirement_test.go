package application_test

import "testing"

func TestRetiredServiceMethodsAreNotPublic(t *testing.T) {
	methods := map[string]bool{}
	for _, method := range exportedServiceMethods() {
		methods[method] = true
	}
	for _, retired := range []string{
		"StageUnitToolOutcome",
		"CommitUnitToolRecord",
		"CloseUnitToolRecord",
		"CloseSubagentToolRecordUnit",
		"ToolRecordDroppedReadings",
		"RestoreRoleAccounts",
		"RestoreRolePermissions",
		"ActiveSkillIDs",
		"GoalBegin",
		"GoalDone",
		"GoalUpdate",
		"HandleSubagentToolEvent",
		"RefreshWorkTableSnapshot",
		"TeamworkBoardViewFor",
		"ClearReviewRequest",
		"Suggestions",
		"SelectAccount",
		"SwitchPlugin",
		"SwitchEffort",
		"LoadMoreHistory",
		"LoadLatestHistory",
		"ReorderQueuedInput",
		"RecallQueuedInput",
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
			t.Errorf("%s must stay off the Service public compatibility surface", retired)
		}
	}
}
