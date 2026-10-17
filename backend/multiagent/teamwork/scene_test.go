package teamwork

// scene_test.go 是团队现场名单判据的**域级**用例（P21-26：随实现从
// `backend/engine/assemble/runtime_teamwork_scene.go` 搬进本域）。

import (
	"testing"

	"github.com/RedHuang-0622/seelex/store"
)

// TestNodeIDFromWorktreeNameMatchesForwardNaming：反向换算（去前缀）与正向拼装**同一条**命名——
// `WorkItemWorktreeName` / `TeammateWorktreeName` 拼出来的指派名，去前缀必须回到原 nodeID。
func TestNodeIDFromWorktreeNameMatchesForwardNaming(t *testing.T) {
	itemName := WorkItemWorktreeName("exec", "wi-1")
	if got := NodeIDFromWorktreeName(itemName); got != "exec-wi-1" {
		t.Fatalf("反向换算 = %q, want exec-wi-1（正向 = %q）", got, itemName)
	}
	mateName := TeammateWorktreeName("pm")
	if got := NodeIDFromWorktreeName(mateName); got != "pm" {
		t.Fatalf("teammate 级反向换算 = %q, want pm（正向 = %q）", got, mateName)
	}
	if got := NodeIDFromWorktreeName("  "); got != "" {
		t.Fatalf("空白指派名 = 没有键：%q", got)
	}
	// 没有前缀的输入原样返回（不猜、不补前缀）。
	if got := NodeIDFromWorktreeName("exec"); got != "exec" {
		t.Fatalf("没有前缀时原样返回：%q", got)
	}
}

func scenePlanFixture() store.TeamworkPlan {
	return store.TeamworkPlan{
		TeamID: "team-1",
		Members: []store.TeamworkMember{
			{Role: "pm", Worktree: TeammateWorktreeName("pm")},
			{Role: "exec", Worktree: ""}, // 没派过现场：没有键，跳过
		},
		Milestones: []store.TeamworkMilestone{{ID: "m1", Items: []store.TeamworkWorkItem{
			{ID: "wi-1", Role: "exec", Worktree: WorkItemWorktreeName("exec", "wi-1")},
			{ID: "wi-2", Role: "exec", Worktree: WorkItemWorktreeName("exec", "wi-2")},
		}}},
	}
}

// TestSceneIndexReadsPlanThenLedgerAndDedupes：名单 = 计划（成员 → 工作项）后账本（未释放的
// 原始行），按 nodeID 去重保序；空白指派名跳过；已释放的行不认领。
func TestSceneIndexReadsPlanThenLedgerAndDedupes(t *testing.T) {
	bindings := []store.TeamworkBinding{
		{Worktree: WorkItemWorktreeName("exec", "wi-1"), Role: "exec", WorkItem: "wi-1"}, // 与计划重复
		{Worktree: TeammateWorktreeName("qa"), Role: "qa"},                               // teammate 级：WorkItem 留空
		{Worktree: WorkItemWorktreeName("exec", "wi-9"), Role: "exec", WorkItem: "wi-9", Released: true},
		{Worktree: "", Role: "ghost"},
	}
	entries := SceneIndex(scenePlanFixture(), bindings)
	got := make([]string, 0, len(entries))
	for _, entry := range entries {
		got = append(got, entry.NodeID)
	}
	want := []string{"pm", "exec-wi-1", "exec-wi-2", "qa"}
	if len(got) != len(want) {
		t.Fatalf("名单 = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("名单 = %v, want %v（顺序：计划在前、账本在后、去重保序）", got, want)
		}
	}
	// 归属标注：teammate 级行的 WorkItem 留空（折叠读法会把这种行整行丢掉，所以读原始行）。
	if entries[0].Role != "pm" || entries[0].WorkItem != "" {
		t.Fatalf("成员项的归属 = %+v", entries[0])
	}
	if entries[1].Role != "exec" || entries[1].WorkItem != "wi-1" {
		t.Fatalf("工作项项的归属 = %+v", entries[1])
	}
	if entries[3].NodeID != "qa" || entries[3].WorkItem != "" {
		t.Fatalf("teammate 级账本行 = %+v", entries[3])
	}
}

// TestSceneIndexEmptyInputs：没有计划也没有账本 ⇒ 空名单（不猜、不扫目录）。
func TestSceneIndexEmptyInputs(t *testing.T) {
	if got := SceneIndex(store.TeamworkPlan{}, nil); len(got) != 0 {
		t.Fatalf("空输入应当给空名单：%+v", got)
	}
	if got := SceneNodeIDs(nil); got != nil {
		t.Fatalf("空名单的 nodeID 投影应当是 nil：%+v", got)
	}
	entries := SceneIndex(scenePlanFixture(), nil)
	nodeIDs := SceneNodeIDs(entries)
	if len(nodeIDs) != len(entries) || nodeIDs[0] != "pm" {
		t.Fatalf("nodeID 投影必须与名单同序同量：%+v vs %+v", nodeIDs, entries)
	}
}

// TestSceneOwnsNode：记录归属过滤只认名单里的 nodeID（trim 后），空名字不归属任何人。
func TestSceneOwnsNode(t *testing.T) {
	entries := SceneIndex(scenePlanFixture(), nil)
	cases := []struct {
		name   string
		nodeID string
		want   bool
	}{
		{"成员级在册", "pm", true},
		{"工作项级在册", "exec-wi-1", true},
		{"两侧留白仍认", "  exec-wi-2\t", true},
		{"子代理记录不在团队名单里", "node-abc", false},
		{"空名字不归属", "   ", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := SceneOwnsNode(entries, c.nodeID); got != c.want {
				t.Fatalf("SceneOwnsNode(%q) = %v, want %v", c.nodeID, got, c.want)
			}
		})
	}
}
