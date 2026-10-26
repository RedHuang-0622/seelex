package core

import applicationconversation "github.com/RedHuang-0622/seelex/application/conversation"
import "github.com/RedHuang-0622/seelex/application/prompt"

type chatRequest = applicationconversation.Request

func chatRequestDisplays(requests []chatRequest) []string {
	return applicationconversation.ChatRequestDisplays(requests)
}

func queuedInputRefs(queue []chatRequest) []string {
	return applicationconversation.QueuedInputRefs(queue)
}

func selectedSkillLayers(layers []prompt.PromptLayer) []prompt.PromptLayer {
	return applicationconversation.SelectedSkillLayers(layers)
}
