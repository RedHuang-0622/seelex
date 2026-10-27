package tui

import "github.com/RedHuang-0622/seelex/application"

func currentSuggestions(model Model) []application.Suggestion {
	return model.deps.Completion.Suggestions(model.textarea.Value())
}
