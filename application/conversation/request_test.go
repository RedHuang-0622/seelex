package conversation_test

import (
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/application/conversation"
	"github.com/RedHuang-0622/seelex/application/prompt"
	"github.com/RedHuang-0622/seelex/contract/dto"
)

// envelopeDeps 用最小信封实现替代装配注入的真实编码，只验证 Combine 自己的
// 「还原正文 → 重新包装」判据。
func envelopeDeps() conversation.RequestDeps {
	const prefix = "<display:"
	return conversation.RequestDeps{
		ParseModelEnvelope: func(input string) (string, int, bool) {
			if !strings.HasPrefix(input, prefix) {
				return "", 0, false
			}
			end := strings.Index(input, ">\n")
			if end < 0 {
				return "", 0, false
			}
			return input[len(prefix):end], end + 2, true
		},
		WrapModelInput: func(display, body string) string {
			return prefix + display + ">\n" + body
		},
	}
}

func TestSelectedSkillLayersKeepsOnlySkillKind(t *testing.T) {
	layers := []prompt.PromptLayer{
		{Kind: "base", Name: "base", Text: "system"},
		{Kind: "skill", Name: "review", Text: "Check correctness.\nReport evidence."},
		{Kind: "skill", Name: "security", Text: "Inspect inputs."},
	}
	selected := conversation.SelectedSkillLayers(layers)
	if len(selected) != 2 || selected[0].Name != "review" || selected[1].Name != "security" {
		t.Fatalf("selected Skills = %#v", selected)
	}
}

func TestRequestCombinePreservesDisplayAndFrozenSkillLayers(t *testing.T) {
	turns := conversation.NewRequestApplication(envelopeDeps())
	first := turns.NewRequest("plain", nil)
	second := turns.NewRequest("$review focused", []prompt.PromptLayer{
		{Kind: "skill", Name: "review", Text: "review prompt"},
	})
	combined := turns.Combine([]conversation.Request{first, second})

	if got, want := combined.DisplayInput, "plain\n---\n$review focused"; got != want {
		t.Fatalf("display = %q, want %q", got, want)
	}
	for _, expected := range []string{"plain", "$review focused"} {
		if !strings.Contains(combined.ModelInput, expected) {
			t.Fatalf("combined model input missing %q:\n%s", expected, combined.ModelInput)
		}
	}
	if strings.Count(combined.ModelInput, "$review focused") != 1 {
		t.Fatalf("combined model input must carry the batch body once:\n%s", combined.ModelInput)
	}
	if strings.Contains(combined.ModelInput, "review prompt") {
		t.Fatalf("Skill body leaked into model input:\n%s", combined.ModelInput)
	}
	if len(combined.Skills) != 1 || combined.Skills[0].Name != "review" {
		t.Fatalf("combined trusted Skills = %#v", combined.Skills)
	}
}

// TestRequestCombineUnwrapsAndRewrapsEnvelopes 钉住已带信封的队列批次：
// 正文要按批次重新拼接，display 必须整体重算，不能把旧信封逐段带进模型输入。
func TestRequestCombineUnwrapsAndRewrapsEnvelopes(t *testing.T) {
	deps := envelopeDeps()
	turns := conversation.NewRequestApplication(deps)
	combined := turns.Combine([]conversation.Request{
		{DisplayInput: "first", ModelInput: deps.WrapModelInput("first", "first")},
		{DisplayInput: "second", ModelInput: deps.WrapModelInput("second", "second")},
	})

	if got, want := combined.DisplayInput, "first\n---\nsecond"; got != want {
		t.Fatalf("display = %q, want %q", got, want)
	}
	if got, want := combined.ModelInput, deps.WrapModelInput(combined.DisplayInput, "first\n---\nsecond"); got != want {
		t.Fatalf("model input = %q, want %q", got, want)
	}
}

func TestRequestCombineMergesSkillsByName(t *testing.T) {
	turns := conversation.NewRequestApplication(envelopeDeps())
	combined := turns.Combine([]conversation.Request{
		{DisplayInput: "a", ModelInput: "a", Skills: []prompt.PromptLayer{{Kind: "skill", Name: "review", Text: "v1"}}},
		{DisplayInput: "b", ModelInput: "b", Skills: []prompt.PromptLayer{
			{Kind: "skill", Name: "review", Text: "v2"},
			{Kind: "skill", Name: "security", Text: "s"},
		}},
	})
	if len(combined.Skills) != 2 {
		t.Fatalf("combined Skills = %#v, want review+security only", combined.Skills)
	}
	if combined.Skills[0].Name != "review" || combined.Skills[0].Text != "v2" {
		t.Fatalf("duplicate Skill layer must be replaced by the latest, got %#v", combined.Skills[0])
	}
	if combined.Skills[1].Name != "security" {
		t.Fatalf("appended Skill layer = %#v", combined.Skills[1])
	}
}

// TestRequestCombineKeepsImagesWithTheBatch 钉住"排队期间附的图跟着这一批走"：
// 队列晋升会把多条排队输入拼成一条消息（正文用 `---` 分隔），图片必须同样拼起来
// 并去重——否则用户排了两条带图的输入，晋升后只看得见其中一条的图。
func TestRequestCombineKeepsImagesWithTheBatch(t *testing.T) {
	turns := conversation.NewRequestApplication(envelopeDeps())
	first := turns.NewRequestWithImages("第一句", nil, []dto.ComposerImage{{Ref: "media:aa", Name: "a.png"}})
	second := turns.NewRequestWithImages("第二句", nil, []dto.ComposerImage{
		{Ref: "media:aa", Name: "a.png"},
		{Ref: "media:bb", Name: "b.png"},
	})
	combined := turns.Combine([]conversation.Request{first, second})
	if len(combined.Images) != 2 {
		t.Fatalf("合并后的图片 = %+v, want 2 条（aa 去重 + bb）", combined.Images)
	}
	if combined.Images[0].Ref != "media:aa" || combined.Images[1].Ref != "media:bb" {
		t.Fatalf("顺序没保留: %+v", combined.Images)
	}
}

// TestNewRequestDropsBlankAndRepeatedImageRefs 钉住入参归一：空 ref 丢掉、同 ref
// 只留一条（内容寻址下同 ref 就是同一张图，模型不该看两遍）。
func TestNewRequestDropsBlankAndRepeatedImageRefs(t *testing.T) {
	turns := conversation.NewRequestApplication(envelopeDeps())
	request := turns.NewRequestWithImages("看图", nil, []dto.ComposerImage{
		{Ref: "  "},
		{Ref: " media:aa "},
		{Ref: "media:aa"},
	})
	if len(request.Images) != 1 || request.Images[0].Ref != "media:aa" {
		t.Fatalf("归一后的图片 = %+v", request.Images)
	}
	if plain := turns.NewRequest("没有图", nil); plain.Images != nil {
		t.Fatalf("没有图时必须是 nil（不是空切片），实际 %#v", plain.Images)
	}
}
