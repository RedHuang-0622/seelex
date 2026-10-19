package e2e

// facade_narrow_faces_test.go 是门面五组窄接口的**机械断言**（M5-R10a，契约 §1.2）。
// 本文件由 `tmp/reshape/r10a_gen_faces.py` 生成：清单就是契约文本，改清单＝改契约，必须显式说明。

import (
	"os"
	"os/exec"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/application"
)

// wantFaceMethods 是四组行为面的方法集快照（逐字，排序后比较）。
var wantFaceMethods = map[string][]string{
	"ViewFace": {
		"ActiveSkillIDs",
		"AnyChatRunning",
		"AssembleRoleWire",
		"GetSessionTranscript",
		"GoalGovernanceViewFor",
		"GoalSkillActive",
		"GoalStatusFor",
		"ListRoleSessions",
		"ListSessions",
		"ListSubagentRecovery",
		"LoadLatestHistory",
		"LoadMoreHistory",
		"PerfStats",
		"ProjectBoardTeamPlanFor",
		"ProjectBoardTeamStateFor",
		"ProjectRootFor",
		"PromptLayers",
		"ReadRoleDraft",
		"ReadRoleSessionRows",
		"RoleSnapshot",
		"SearchHistory",
		"SessionInputIndex",
		"SessionMeta",
		"SessionReleaseWindow",
		"SessionStorageConfig",
		"Snapshot",
		"SnapshotOf",
		"SubagentSessionDetail",
		"Subscribe",
		"SubscribeSession",
		"SubscribeSessionWithReplay",
		"TeammateSessionLiveFor",
		"TeammateSessionLivePageFor",
		"TeamworkBoardViewFor",
		"ToolRecordBoard",
		"ToolRecordEnabled",
		"ToolResultContent",
		"WaitCatalogRefresh",
		"WaitForIdle",
		"WorkTableRowsFor",
		"WorkspaceChanges",
		"WorkspaceFileContent",
		"WorkspaceFileCount",
		"WorkspaceGitCommitDetail",
		"WorkspaceGitCommitFileContent",
		"WorkspaceGitLog",
		"WorkspaceGitRepos",
		"WorkspaceTree",
	},
	"CommandFace": {
		"ActivateSession",
		"AddNotice",
		"AppendRoleDraft",
		"AppendRoleSessionRows",
		"ArchiveSession",
		"BeginGracefulShutdown",
		"BeginNewSession",
		"BindWorkspace",
		"CancelAllChats",
		"CancelChat",
		"ClearSubagentTree",
		"CompactContextNow",
		"CreateWorkspace",
		"DeleteSession",
		"ForkSession",
		"ForkSessionLatest",
		"ForkSubagents",
		"GoalBegin",
		"GoalBeginFor",
		"GoalDone",
		"GoalDoneFor",
		"GoalUpdate",
		"GoalUpdateFor",
		"RequestFileReview",
		"ResolveInteraction",
		"ResumeInterruptedSubagents",
		"ResumeSession",
		"ResumeSubagent",
		"Shutdown",
		"Submit",
		"SubmitToSession",
		"SyncRoleDraft",
		"UnbindWorkspace",
		"UnloadSession",
		"UpdateWorkItemStatus",
		"WorkspaceWriteFile",
	},
	"PolicyFace": {
		"ActivateSkill",
		"CancelScheduledTask",
		"ConfigureSessionStorage",
		"CreateRoleSession",
		"ScheduleCancel",
		"ScheduleFire",
		"ScheduleRegister",
		"ScheduleTask",
		"SelectAccount",
		"SetAccountWindow",
		"SetFullAccess",
		"SetLifecycleOrder",
		"SetLoopLimit",
		"SetPermissionTier",
		"SetReasoningEffort",
		"SetRoleLifecycle",
		"SetSessionMeta",
		"SetSessionReleaseWindow",
		"SwitchEffort",
		"SwitchPlugin",
		"TestSessionStorage",
		"UpdateScheduledTask",
	},
	"QueueFace": {
		"RecallQueuedInput",
		"ReorderQueuedInput",
		"Suggestions",
	},
}

// assemblyOnlyMethods 是"不在四组的第三类"：装配注入口（`Set*` / `Apply*`）、工具注册面（`*Handler`）、
// 事件钩子（`Handle*`）、内部发布/落盘编排（`Publish*` / `Refresh*` / `Flush*` / `Record*` / `Prepare*`）
// 与调度装配面。它们仍在 `Service` 上（供 `wiring` 与工具注册表调用），但**不属于前端缝**。
var assemblyOnlyMethods = []string{
	"ApplyToolRecordEnabled",
	"AssembleScheduledRun",
	"ClearReviewRequest",
	"CompactContextHandler",
	"FlushToolRecord",
	"GoalBeginHandler",
	"GoalDoneHandler",
	"GoalStatusHandler",
	"GoalUpdateHandler",
	"HandleRoleToolActivity",
	"HandleSubagentToolEvent",
	"PrepareProviderHistory",
	"PublishRuntimeProjections",
	"ReadCompressedTurnHandler",
	"ReadPlanHandler",
	"ReadToolResultHandler",
	"RecordToolCommandOutcome",
	"RefreshRuntimeSnapshot",
	"RefreshWorkTableSnapshot",
	"SearchHistoryHandler",
	"SetProjectBoardSink",
	"SetToolRecordEnabled",
	"SetToolRecordSnapshotStore",
	"StartScheduledSession",
	"TaskTerminalHandler",
}

// 断言 1（编译期结构满足）写在 `application/faces.go` 的 `var _ ViewFace = (*Service)(nil)` 一族里；
// 这里再跑一遍运行时快照，保证接口方法集与本文件的冻结清单逐字一致。
func TestFacadeFacesMethodSetsAreFrozen(t *testing.T) {
	faces := map[string]reflect.Type{
		"ViewFace":    reflect.TypeOf((*application.ViewFace)(nil)).Elem(),
		"CommandFace": reflect.TypeOf((*application.CommandFace)(nil)).Elem(),
		"PolicyFace":  reflect.TypeOf((*application.PolicyFace)(nil)).Elem(),
		"QueueFace":   reflect.TypeOf((*application.QueueFace)(nil)).Elem(),
	}
	for name, face := range faces {
		got := methodNamesOf(face)
		want := append([]string(nil), wantFaceMethods[name]...)
		sort.Strings(want)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s 方法集漂移（改方法集必须显式改本清单）：\n got=%v\nwant=%v", name, got, want)
		}
	}
}

// 断言 2（本片追加，理由：防止新方法绕过四组）：`Service` 的公开方法 = 四组 ∪ 装配面清单。
func TestFacadeEveryExportedMethodBelongsToAFace(t *testing.T) {
	covered := map[string]bool{}
	for _, members := range wantFaceMethods {
		for _, member := range members {
			covered[member] = true
		}
	}
	for _, member := range assemblyOnlyMethods {
		covered[member] = true
	}
	var unclassified []string
	for _, method := range methodNamesOf(reflect.TypeOf((*application.Service)(nil))) {
		if !covered[method] {
			unclassified = append(unclassified, method)
		}
	}
	if len(unclassified) > 0 {
		t.Fatalf("这些公开方法既不在四组也不在装配面清单里：%v\n（要么归类、要么显式登记到 assemblyOnlyMethods）", unclassified)
	}
}

// 断言 3：A 组别名集合逐字等于 `application.FaceTypeNames`。
//
// 反射列不出"一个包里有哪些类型别名"（Go 反射没有包级命名空间），因此这里**解析事实源源码**
// `application/application.go` 的 `type (...)` 块（与契约 §1.4 的清单口径同一处），再与清单比对。
func TestFacadeTypeAliasSetMatchesFaceTypeNames(t *testing.T) {
	source, err := os.ReadFile("../application/application.go")
	if err != nil {
		t.Skipf("读不到事实源源码：%v", err)
	}
	block := regexp.MustCompile(`(?ms)^type \(\r?\n(.*?)^\)\r?\n`).FindSubmatch(source)
	if block == nil {
		t.Fatal("application/application.go 里找不到 `type (` 别名块")
	}
	names := regexp.MustCompile(`(?m)^\t([A-Z]\w*)\s*=`).FindAllStringSubmatch(string(block[1]), -1)
	got := make([]string, 0, len(names))
	for _, match := range names {
		got = append(got, match[1])
	}
	sort.Strings(got)
	want := append([]string(nil), application.FaceTypeNames...)
	if !sort.StringsAreSorted(want) {
		t.Fatalf("FaceTypeNames 必须按字母序（清单即契约文本）：%v", want)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("A 组别名集合漂移：\n got=%v\nwant=%v", got, want)
	}
}

// 断言 4：前端零改动（I5）——`gui/` 与 `tui/` 在本批里不得出现 diff。没有 git 就跳过。
func TestFacadeFrontendHasNoDiff(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git in PATH")
	}
	output, err := exec.Command("git", "diff", "--name-only", "HEAD", "--", "gui", "tui").CombinedOutput()
	if err != nil {
		t.Skipf("git diff 不可用（%v）：%s", err, output)
	}
	if strings.TrimSpace(string(output)) != "" {
		t.Fatalf("前端必须零改动（I5），实际有 diff：\n%s", output)
	}
}

// methodNamesOf 返回类型（或接口）的公开方法名（排序）。
func methodNamesOf(value reflect.Type) []string {
	if value.Kind() == reflect.Ptr {
		value = value.Elem()
	}
	names := make([]string, 0, value.NumMethod())
	for index := 0; index < value.NumMethod(); index++ {
		names = append(names, value.Method(index).Name)
	}
	sort.Strings(names)
	return names
}
