package application

import (
	"context"
	"sort"
	"strings"
)

type Suggestion struct {
	Text        string `json:"text"`
	Description string `json:"description,omitempty"`
	Kind        string `json:"kind"`
}

func (service *Service) Suggestions(input string) []Suggestion {
	trigger := ""
	prefix := ""
	switch {
	case strings.HasPrefix(input, "/"):
		trigger, prefix = "/", strings.TrimPrefix(input, "/")
	case strings.HasPrefix(input, "#"):
		trigger, prefix = "#", strings.TrimPrefix(input, "#")
	case strings.HasPrefix(input, "@"):
		trigger, prefix = "@", strings.TrimPrefix(input, "@")
	default:
		return nil
	}
	if strings.Contains(prefix, " ") {
		return nil
	}
	all := make([]Suggestion, 0)
	if trigger == "/" || trigger == "#" {
		for _, skill := range service.deps.Skills.All() {
			all = append(all, Suggestion{Text: skill.Name, Description: skill.Description, Kind: "skill"})
		}
	}
	if trigger == "/" {
		for _, command := range service.commands.All() {
			all = append(all, Suggestion{Text: command.Name(), Description: command.Description(), Kind: "command"})
		}
		for _, tool := range service.deps.Runtime.VisibleTools(context.Background()) {
			all = append(all, Suggestion{Text: tool.Name, Description: tool.Description, Kind: "tool"})
		}
	}
	if trigger == "@" {
		for _, plugin := range service.deps.Plugins.All() {
			all = append(all, Suggestion{Text: plugin.Name, Description: plugin.Description, Kind: "plugin"})
		}
		all = append(all, Suggestion{Text: "off", Description: "停用所有插件", Kind: "plugin"})
	}
	lower := strings.ToLower(prefix)
	filtered := all[:0]
	for _, suggestion := range all {
		if lower == "" || strings.HasPrefix(strings.ToLower(suggestion.Text), lower) {
			filtered = append(filtered, suggestion)
		}
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		priority := map[string]int{"command": 0, "tool": 1, "skill": 2}
		if priority[filtered[i].Kind] != priority[filtered[j].Kind] {
			return priority[filtered[i].Kind] < priority[filtered[j].Kind]
		}
		return filtered[i].Text < filtered[j].Text
	})
	return append([]Suggestion(nil), filtered...)
}
