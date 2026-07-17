package tui

import "github.com/RedHuang-0622/seelex/application"

func currentSuggestions(model Model) []application.Suggestion {
	return model.app.Suggestions(model.textarea.Value())
}
