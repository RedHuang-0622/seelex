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
	} {
		if methods[retired] {
			t.Errorf("%s must stay off the Service public compatibility surface", retired)
		}
	}
}
