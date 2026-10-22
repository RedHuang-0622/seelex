package adapters

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/backend/engine/plugin"
	"github.com/RedHuang-0622/seelex/backend/engine/skill"
)

// TestAdaptPluginCarriesSkillCatalog 钉住插件页「按插件看 Skill」的数据面：
// PluginInfo 必须随插件带上它自己的 Skill 目录（名字 + 描述），且 **不泄露 Prompt 正文**。
//
// 为什么需要这条：插件页面改成「只看不切」之后，Skill 列表跟着**停留位**走，不能再只靠
// runtime.skills（那是当前激活插件的合并视图）。如果 adaptPlugin 漏掉 Skills，前端只会
// 静默回落到 runtime.skills，页面看起来"能用"，但看别的插件时列的是当前插件的技能。
func TestAdaptPluginCarriesSkillCatalog(t *testing.T) {
	t.Parallel()

	info := adaptPlugin(plugin.Plugin{
		Name:        "artist",
		Description: "visual",
		Prompt:      "secret prompt",
		Skills: []skill.Skill{
			{Name: "motion-director", Description: "taste layer", Prompt: "hidden body"},
			{Name: "motion2code", Description: "svg+gsap", Prompt: "hidden body 2"},
		},
	})

	if info.Name != "artist" || info.Description != "visual" {
		t.Fatalf("plugin identity lost: %#v", info)
	}
	if len(info.Skills) != 2 {
		t.Fatalf("skills = %d, want 2", len(info.Skills))
	}
	if info.Skills[0].Name != "motion-director" || info.Skills[1].Name != "motion2code" {
		t.Fatalf("skill order/names lost: %#v", info.Skills)
	}
	if info.Skills[0].Description != "taste layer" {
		t.Fatalf("skill description lost: %#v", info.Skills[0])
	}

	// Prompt 是 `json:"-"`：插件页只读目录，不把技能正文带进前端。
	raw, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(raw), "hidden body") || strings.Contains(string(raw), "secret prompt") {
		t.Fatalf("prompt leaked into JSON: %s", raw)
	}
	if !strings.Contains(string(raw), `"skills":[`) {
		t.Fatalf("skills must be serialized for the plugin page: %s", raw)
	}
}
