package input

import (
	"reflect"
	"testing"
)

func TestSuggestionsCombineAndSortDomains(t *testing.T) {
	application := New(Deps{
		CommandSuggestions: func() []Suggestion {
			return []Suggestion{{Text: "help", Kind: SuggestionKindCommand}}
		},
		SkillSuggestions: func() []Suggestion {
			return []Suggestion{{Text: "review", Kind: SuggestionKindSkill}}
		},
		PluginSuggestions: func() []Suggestion {
			return []Suggestion{{Text: "plugin-a", Kind: SuggestionKindPlugin}}
		},
	})

	got := application.Suggestions("/")
	want := []Suggestion{
		{Text: "help", Kind: SuggestionKindCommand},
		{Text: "review", Kind: SuggestionKindSkill},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Suggestions(/) = %#v, want %#v", got, want)
	}
}

func TestSuggestionsRejectArgumentsAndUnknownSigils(t *testing.T) {
	application := New(Deps{
		CommandSuggestions: func() []Suggestion {
			t.Fatal("directories must not be read for rejected input")
			return nil
		},
	})
	if got := application.Suggestions("/help now"); got != nil {
		t.Fatalf("argument suggestions = %#v, want nil", got)
	}
	if got := application.Suggestions("normal text"); got != nil {
		t.Fatalf("unknown sigil suggestions = %#v, want nil", got)
	}
}

func TestParsePreservesSigilOrder(t *testing.T) {
	for _, value := range []string{SigilCommand, SigilPlugin, SigilSkill} {
		trigger, prefix, ok := Parse(value + "candidate")
		if !ok || trigger != value || prefix != "candidate" {
			t.Fatalf("Parse(%q) = (%q, %q, %v)", value+"candidate", trigger, prefix, ok)
		}
	}
}

func TestSuggestionsExcludeModelSideToolsFromExecutablePanel(t *testing.T) {
	application := New(Deps{
		CommandSuggestions: func() []Suggestion {
			return []Suggestion{
				{Text: "compact", Kind: SuggestionKindCommand},
				{Text: "help", Kind: SuggestionKindCommand},
			}
		},
		SkillSuggestions: func() []Suggestion {
			return []Suggestion{{Text: "review", Kind: SuggestionKindSkill}}
		},
		HasVisibleTool: func(name string) bool {
			return name == "compact_context" || name == "read"
		},
	})

	for _, suggestion := range application.Suggestions("/") {
		if suggestion.Text == "compact_context" || suggestion.Text == "read" {
			t.Fatalf("模型侧工具进入了可执行面板：%+v", suggestion)
		}
		if suggestion.Kind != SuggestionKindCommand && suggestion.Kind != SuggestionKindSkill {
			t.Fatalf("不可执行域进入可执行面板：%+v", suggestion)
		}
	}
}
