package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
)

func (model Model) handleInteractionKey(message tea.KeyMsg) (tea.Model, tea.Cmd) {
	interaction := model.snapshot.Interaction
	if interaction == nil {
		return model, nil
	}
	switch message.String() {
	case "up":
		if model.interactionSel > 0 {
			model.interactionSel--
		}
		return model, nil
	case "down":
		if model.interactionSel < len(interaction.Options)-1 {
			model.interactionSel++
		}
		return model, nil
	case "enter":
		if model.interactionSel >= 0 && model.interactionSel < len(interaction.Options) {
			return model, resolveInteraction(model.app, interaction.ID, interaction.Options[model.interactionSel].ID)
		}
		return model, nil
	case "esc", "ctrl+c", "ctrl+d":
		return model, resolveInteraction(model.app, interaction.ID, "__CANCEL__")
	default:
		key := message.String()
		if len(key) == 1 && key[0] >= '1' && key[0] <= '9' {
			index := int(key[0] - '1')
			if index < len(interaction.Options) {
				model.interactionSel = index
				return model, resolveInteraction(model.app, interaction.ID, interaction.Options[index].ID)
			}
		}
		return model, nil
	}
}

func resolveInteraction(app AppController, id, optionID string) tea.Cmd {
	return func() tea.Msg {
		return submitResultMsg{err: app.ResolveInteraction(context.Background(), id, optionID)}
	}
}
