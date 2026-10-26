// Package input owns input-facing application policies that do not dispatch or
// execute commands, skills, plugins or conversations.
package input

import (
	"sort"
	"strings"
)

const (
	SigilCommand = "/"
	SigilPlugin  = "#"
	SigilSkill   = "$"
)

var sigils = []string{SigilCommand, SigilPlugin, SigilSkill}

const (
	SuggestionKindCommand = "command"
	SuggestionKindSkill   = "skill"
	SuggestionKindPlugin  = "plugin"
)

// Suggestion is a directly executable input candidate.
type Suggestion struct {
	Text        string `json:"text"`
	Description string `json:"description,omitempty"`
	Kind        string `json:"kind"`
}

// Deps supplies the authoritative command, Skill and Plugin directories.
type Deps struct {
	CommandSuggestions func() []Suggestion
	SkillSuggestions   func() []Suggestion
	PluginSuggestions  func() []Suggestion
}

// Application shapes prefix suggestions without owning any directory state.
type Application struct {
	deps Deps
}

// New constructs the input suggestion application.
func New(deps Deps) *Application {
	return &Application{deps: deps}
}

// Sigils returns a copy of the authoritative sigil order.
func Sigils() []string {
	return append([]string(nil), sigils...)
}

// Parse splits an input sigil from its candidate prefix.
func Parse(value string) (trigger, prefix string, ok bool) {
	for _, sigil := range sigils {
		if strings.HasPrefix(value, sigil) {
			return sigil, strings.TrimPrefix(value, sigil), true
		}
	}
	return "", "", false
}

// Suggestions combines candidates from the domain directories, filters by the
// typed prefix and applies the stable command → skill → plugin order.
func (application *Application) Suggestions(value string) []Suggestion {
	trigger, prefix, ok := Parse(value)
	if !ok || strings.Contains(prefix, " ") {
		return nil
	}
	all := make([]Suggestion, 0)
	switch trigger {
	case SigilCommand:
		all = append(all, application.deps.CommandSuggestions()...)
		all = append(all, application.deps.SkillSuggestions()...)
	case SigilPlugin:
		all = append(all, application.deps.PluginSuggestions()...)
	case SigilSkill:
		all = append(all, application.deps.SkillSuggestions()...)
	}
	lower := strings.ToLower(prefix)
	filtered := all[:0]
	for _, suggestion := range all {
		if lower == "" || strings.HasPrefix(strings.ToLower(suggestion.Text), lower) {
			filtered = append(filtered, suggestion)
		}
	}
	priority := map[string]int{
		SuggestionKindCommand: 0,
		SuggestionKindSkill:   1,
		SuggestionKindPlugin:  2,
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		if priority[filtered[i].Kind] != priority[filtered[j].Kind] {
			return priority[filtered[i].Kind] < priority[filtered[j].Kind]
		}
		return filtered[i].Text < filtered[j].Text
	})
	return append([]Suggestion(nil), filtered...)
}
