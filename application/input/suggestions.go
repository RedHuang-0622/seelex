// Package input owns input-facing application policies that do not dispatch or
// execute commands, skills, plugins or conversations.
package input

import (
	"fmt"
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
	HasPlugin          func(string) bool
	HasSkill           func(string) bool
	HasVisibleTool     func(string) bool
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

// HasSigilPrefix reports whether input starts with an authoritative sigil.
func HasSigilPrefix(value string) bool {
	_, _, ok := Parse(value)
	return ok
}

// SigilOf returns the leading sigil, or an empty string for ordinary input.
func SigilOf(value string) string {
	trigger, _, ok := Parse(value)
	if !ok {
		return ""
	}
	return trigger
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

// SigilMigrationHint explains the correct sigil when a name exists in another
// input domain. It performs no I/O and returns an empty string when no
// migration target exists.
func (application *Application) SigilMigrationHint(used, name string) string {
	name = strings.TrimSpace(name)
	if application == nil || name == "" {
		return ""
	}
	hints := make([]string, 0, 2)
	if used != SigilPlugin && application.deps.HasPlugin != nil && application.deps.HasPlugin(name) {
		hints = append(hints, fmt.Sprintf("切换插件用 %s%s", SigilPlugin, name))
	}
	if used != SigilSkill && application.deps.HasSkill != nil && application.deps.HasSkill(name) {
		hints = append(hints, fmt.Sprintf("召回 Skill 用 %s%s", SigilSkill, name))
	}
	if len(hints) == 0 {
		return ""
	}
	return "（" + strings.Join(hints, "；") + "）"
}

// UnknownCommandNotice reports an unknown command and adds only applicable,
// catalog-backed recovery hints.
func (application *Application) UnknownCommandNotice(name string) string {
	trimmed := strings.TrimSpace(name)
	notice := fmt.Sprintf("未知命令: %s。输入 %shelp 查看可用命令。", trimmed, SigilCommand)
	hints := make([]string, 0, 2)
	if application != nil && application.deps.HasVisibleTool != nil && application.deps.HasVisibleTool(trimmed) {
		hints = append(hints, fmt.Sprintf("「%s」是模型侧工具（由模型调用、经权限门），不能从输入框直接执行", trimmed))
	}
	if counterpart := application.commandCounterpart(trimmed); counterpart != "" {
		hints = append(hints, fmt.Sprintf("你是想用 %s%s 吗？", SigilCommand, counterpart))
	}
	if len(hints) == 0 {
		return notice
	}
	return notice + "（" + strings.Join(hints, "；") + "）"
}

// commandCounterpart finds the closest registered command. Prefix and
// containment matches win; bounded edit distance handles ordinary typos.
func (application *Application) commandCounterpart(name string) string {
	if application == nil || application.deps.CommandSuggestions == nil {
		return ""
	}
	lower := strings.ToLower(strings.TrimSpace(name))
	if lower == "" {
		return ""
	}
	best, bestScore := "", 0
	for _, suggestion := range application.deps.CommandSuggestions() {
		if suggestion.Kind != "" && suggestion.Kind != SuggestionKindCommand {
			continue
		}
		candidate := strings.ToLower(suggestion.Text)
		if candidate == "" || candidate == lower {
			continue
		}
		matched := strings.HasPrefix(lower, candidate) || strings.Contains(lower, candidate)
		if !matched && len(candidate) <= 16 && len(lower) <= 16 {
			matched = editDistanceAtMost(lower, candidate, 2)
		}
		if matched && len(candidate) > bestScore {
			best, bestScore = candidate, len(candidate)
		}
	}
	return best
}

// editDistanceAtMost is the bounded Levenshtein check used only for command
// recovery. It keeps two rows and exits as soon as every current cell exceeds
// the limit.
func editDistanceAtMost(a, b string, limit int) bool {
	ar, br := []rune(a), []rune(b)
	if diff := len(ar) - len(br); diff > limit || diff < -limit {
		return false
	}
	previous := make([]int, len(br)+1)
	current := make([]int, len(br)+1)
	for index := range previous {
		previous[index] = index
	}
	for row := 1; row <= len(ar); row++ {
		current[0] = row
		rowMin := current[0]
		for column := 1; column <= len(br); column++ {
			cost := 1
			if ar[row-1] == br[column-1] {
				cost = 0
			}
			current[column] = min(
				previous[column]+1,
				min(current[column-1]+1, previous[column-1]+cost),
			)
			if current[column] < rowMin {
				rowMin = current[column]
			}
		}
		if rowMin > limit {
			return false
		}
		copy(previous, current)
	}
	return previous[len(br)] <= limit
}
