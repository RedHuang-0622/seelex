package seelebridge

// runtime_teamwork_schema_test.go — 钉住 team_plan 的输入契约是**里程碑口径**：
// 里程碑是屏障（milestones[].depends_on），里程碑内按 Work Item 的 depends_on DAG
// 并行；**没有 stages**（2026-10-04 阶段口径整条退场：属性、required、description
// 里一处都不许再出现——留着属性，leader 就会照旧写阶段，两套顺序语义又长回来）。
//
// 为什么值得钉：schema 同时是提示词面（leader 的 skill 照它调用）与准入面，
// 口径一漂移，"里程碑 + Work Item"这条运行时时序就会被读成"阶段制"。

import (
	"strings"
	"testing"
)

func teamworkPlanSchemaProps(t *testing.T) map[string]interface{} {
	t.Helper()
	properties, ok := teamworkPlanSchema()["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("team_plan schema 应有 properties")
	}
	return properties
}

func TestTeamworkPlanSchemaCarriesMilestoneVocabulary(t *testing.T) {
	properties := teamworkPlanSchemaProps(t)

	// 1) milestones 增 name 与 depends_on：这是屏障口径的两个字段。
	milestones, ok := properties["milestones"].(map[string]interface{})
	if !ok {
		t.Fatal("team_plan schema 应有 milestones")
	}
	milestoneItems, ok := milestones["items"].(map[string]interface{})
	if !ok {
		t.Fatal("milestones 应有 items")
	}
	milestoneProps, ok := milestoneItems["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("milestones.items 应有 properties")
	}
	for _, key := range []string{"name", "depends_on"} {
		if _, exists := milestoneProps[key]; !exists {
			t.Fatalf("里程碑口径缺失：milestones.items.properties 应含 %q", key)
		}
	}

	// 2) after 必须**整条退场**：它是阶段制时代的字段（指向阶段 id）。required 保留，
	// 但它现在是"这个里程碑需要哪些在编角色"，与顺序无关。
	if _, exists := milestoneProps["after"]; exists {
		t.Fatal("after 指向阶段 id，阶段口径退场后这个字段不该还在 schema 里")
	}
	requiredField, ok := milestoneProps["required"].(map[string]interface{})
	if !ok {
		t.Fatal("required 应保留（里程碑需要哪些在编角色）")
	}
	description, _ := requiredField["description"].(string)
	if !strings.Contains(description, "角色") {
		t.Fatalf("required 应说明它是角色声明（不参与顺序判定），得 description=%q", description)
	}
}

// TestTeamworkPlanSchemaHasNoStages：阶段口径整条退场——属性没有、required 没有、
// description 里也不提"历史口径的阶段"（那句话本身就是留在提示词面的一条后门：
// leader 读了会照旧写 stages，而写进去的阶段不再被任何读侧认作顺序事实）。
func TestTeamworkPlanSchemaHasNoStages(t *testing.T) {
	schema := teamworkPlanSchema()

	if _, exists := teamworkPlanSchemaProps(t)["stages"]; exists {
		t.Fatal("stages 属性必须整条删除（阶段口径已退场）")
	}
	required, ok := schema["required"].([]string)
	if !ok {
		t.Fatal("team_plan schema 的 required 应是 []string")
	}
	for _, key := range required {
		if key == "stages" {
			t.Fatal("stages 不该在 required 里")
		}
	}
	// 里程碑是计划的最小形状（校验层也这么要求），所以它必须在 required 里——
	// 否则 leader 会以为可以只写成员不写里程碑。
	found := false
	for _, key := range required {
		if key == "milestones" {
			found = true
		}
	}
	if !found {
		t.Fatalf("milestones 必须在 required 里（计划的最小形状）：%v", required)
	}
	if strings.Contains(teamworkPlanDescription(), "stages[") {
		t.Fatalf("description 里不该再讲 stages 口径：%q", teamworkPlanDescription())
	}
}

func TestTeamworkPlanDescriptionSpeaksMilestoneVocabulary(t *testing.T) {
	description := teamworkPlanDescription()
	for _, keyword := range []string{"milestone", "depends_on", "Work Item"} {
		if !strings.Contains(description, keyword) {
			t.Fatalf("team_plan description 应含里程碑口径关键词 %q，得 %q", keyword, description)
		}
	}
}
