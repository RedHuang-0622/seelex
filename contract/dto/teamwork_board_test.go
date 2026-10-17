package dto

// teamwork_board_test.go — 团队看板 DTO 的 **wire 形状**（JSON 键）钉子。
//
// 为什么在 DTO 这一层单钉：前端（gui/frontend/dist/team-board-view.js 的
// renderTeamBoard / renderTeamQueue）与编排回执的消费方都按**这些键名**读写；Go 侧
// 改名/改 tag 是完全合法的重构，而 wire 上就是静默变空——两端各自的用例还会全绿。
// 所以键集合按契约逐键断言（多一个键也报出来：多出来的键同样是契约漂移）。

import (
	"encoding/json"
	"reflect"
	"sort"
	"testing"
)

// pluginAssemblyWireKeys 是 dto.PluginAssemblyView 的**冻结键集合**（2026-10-05，
// leader 冻结的契约）：前 11 个与编排回执（team_plan 的 assemblies[] / team_dispatch 的
// plugin_face）逐键相同；后 3 个是**失灵读数**，同样沿用回执既有键名。
var pluginAssemblyWireKeys = []string{
	"role", "mode", "plugins", "plugin_count",
	"skill_count", "skill_catalog_runes", "skill_catalog_tokens_est",
	"plugin_face_tools", "total_tools",
	"plugin_face_faulted", "plugin_face_missing", "plugin_face_note",
	"yellow", "yellow_reason",
}

func decodeKeys(t *testing.T, value any) []string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	var fields map[string]any
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatalf("decode %s: %v", encoded, err)
	}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func wantSorted(keys []string) []string {
	sorted := append([]string(nil), keys...)
	sort.Strings(sorted)
	return sorted
}

// TestPluginAssemblyViewWireKeysAreFrozen 逐键断言装配读数的 wire 形状。
func TestPluginAssemblyViewWireKeysAreFrozen(t *testing.T) {
	full := PluginAssemblyView{
		Role: "exec", Mode: "replace", Plugins: []string{"docs"}, PluginCount: 1,
		SkillCount: 2, SkillCatalogRunes: 100, SkillCatalogTokensEst: 25,
		PluginFaceTools: 3, TotalTools: 81,
		PluginFaceFaulted: true, PluginFaceMissing: []string{"docs"}, PluginFaceNote: "失灵",
		Yellow: true, YellowReason: "超 6k",
	}
	if got := decodeKeys(t, full); !reflect.DeepEqual(got, wantSorted(pluginAssemblyWireKeys)) {
		t.Fatalf("装配读数的 wire 键漂了：\n got %v\nwant %v", got, wantSorted(pluginAssemblyWireKeys))
	}

	// 全字段 omitempty：全零值是一个**空对象**，不是一个塞满 0 的读数。
	if got := decodeKeys(t, PluginAssemblyView{}); len(got) != 0 {
		t.Fatalf("全零值不该下发任何键（全部 omitempty），得到 %v", got)
	}
}

// TestTeamworkMemberViewCarriesAssemblyGates 钉成员行的装配两格：
//   - 有声明就下发 `plugins`（声明面）；
//   - 有读数就下发 `assembly`（生效面，**非 nil** 才算给得出读数）；
//   - 两样都没有时**两个键都不出现**（前端据此不显示装配格，而不是渲染一个 0/0 空壳）。
func TestTeamworkMemberViewCarriesAssemblyGates(t *testing.T) {
	// ① 只有既有字段：两个新键都不该出现（新增字段不得污染老载荷）。
	plain := TeamworkMemberView{Role: "pm", Status: "free"}
	for _, key := range []string{"plugins", "assembly"} {
		for _, got := range decodeKeys(t, plain) {
			if got == key {
				t.Fatalf("没有装配事实时不该下发 %q：%v", key, decodeKeys(t, plain))
			}
		}
	}

	// ② 声明面 + 生效面同时在：键都在，且 assembly 的键就是上面那一份。
	withAssembly := TeamworkMemberView{
		Role: "exec", Status: "free",
		Plugins:  []string{"docs"},
		Assembly: &PluginAssemblyView{Role: "exec", Mode: "replace", Plugins: []string{"docs"}, PluginCount: 1},
	}
	encoded, err := json.Marshal(withAssembly)
	if err != nil {
		t.Fatalf("encode member: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &raw); err != nil {
		t.Fatalf("decode raw: %v", err)
	}
	assemblyRaw, ok := raw["assembly"]
	if !ok {
		t.Fatalf("有读数必须下发 assembly 键：%s", encoded)
	}
	if got := decodeKeys(t, json.RawMessage(assemblyRaw)); !reflect.DeepEqual(got, wantSorted([]string{"role", "mode", "plugins", "plugin_count"})) {
		t.Fatalf("成员行里的 assembly 键与上面那一份不一致：%v", got)
	}
	if _, ok := raw["plugins"]; !ok {
		t.Fatalf("声明面非空必须下发 plugins：%s", encoded)
	}

	// ③ 空集：语义写进 mode（inherit-host），而**不是**靠缺失 plugins 暗示——
	// 前端拿到的必须是"这个人不覆盖宿主装配"这句话，不是"没有装配这一格"。
	inherit := TeamworkMemberView{Role: "pm", Status: "free", Assembly: &PluginAssemblyView{Role: "pm", Mode: "inherit-host"}}
	encoded, err = json.Marshal(inherit)
	if err != nil {
		t.Fatalf("encode inherit member: %v", err)
	}
	// 解到**新的** map（复用同一个 map 会让上一次的键残留，断言就成了空断言）。
	var inheritRaw map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &inheritRaw); err != nil {
		t.Fatalf("decode inherit raw: %v", err)
	}
	if _, ok := inheritRaw["plugins"]; ok {
		t.Fatalf("空集声明不下发 plugins（空 = 不覆盖）：%s", encoded)
	}
	var inheritAssembly map[string]any
	if err := json.Unmarshal(inheritRaw["assembly"], &inheritAssembly); err != nil {
		t.Fatalf("空集必须仍有 assembly 读数（读出 inherit-host）：%s", encoded)
	}
	if inheritAssembly["mode"] != "inherit-host" {
		t.Fatalf("空集必须把语义写出来（inherit-host），得到 %v", inheritAssembly)
	}
}
