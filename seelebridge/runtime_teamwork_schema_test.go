package seelebridge

// runtime_teamwork_schema_test.go — 钉住 team_plan 的输入契约是**里程碑口径**：
// 里程碑是屏障（milestones[].depends_on），里程碑内按 Work Item 的 depends_on DAG
// 并行；stages 只是阶段制时代的历史口径，既不在 required 里、也不承载顺序事实。
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

	// 2) after / required 保留，但必须被标注为历史字段（读旧计划用）。
	for _, legacy := range []string{"after", "required"} {
		field, ok := milestoneProps[legacy].(map[string]interface{})
		if !ok {
			t.Fatalf("历史字段 %q 应保留（旧计划仍可读）", legacy)
		}
		description, _ := field["description"].(string)
		if !strings.Contains(description, "历史") {
			t.Fatalf("%q 应被标注为历史字段，得 description=%q", legacy, description)
		}
	}
}

func TestTeamworkPlanSchemaDropsStagesFromRequired(t *testing.T) {
	schema := teamworkPlanSchema()

	required, ok := schema["required"].([]string)
	if !ok {
		t.Fatal("team_plan schema 的 required 应是 []string")
	}
	for _, key := range required {
		if key == "stages" {
			t.Fatal("stages 是历史口径，不应在 required 里（milestones-only 计划必须合法）")
		}
	}
	// 保留属性，但读法与里程碑一致：属性在、required 不在。
	stages, ok := teamworkPlanSchemaProps(t)["stages"].(map[string]interface{})
	if !ok {
		t.Fatal("stages 属性应保留（历史口径，只为读旧计划）")
	}
	description, _ := stages["description"].(string)
	if !strings.Contains(description, "历史") {
		t.Fatalf("stages 应被标注为历史口径，得 description=%q", description)
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
