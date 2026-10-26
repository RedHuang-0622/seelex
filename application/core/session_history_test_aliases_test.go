package core

import (
	"github.com/RedHuang-0622/seelex/contract"
	protocol "github.com/RedHuang-0622/seelex/contract/protocol"
)

func (service *Service) pointViewAtRestoringShell(sessionID string) bool {
	return service.components.sessionHistoryRuntime.PointViewAtRestoringShell(sessionID)
}

func adaptEngineMessage(msg contract.EngineMessage, stripThoughtBlocks func(string) string) protocol.Message {
	if stripThoughtBlocks == nil {
		stripThoughtBlocks = func(value string) string { return value }
	}
	return messageFromEngineHistory(msg, stripThoughtBlocks)
}

func messageFromEngineHistory(msg contract.EngineMessage, stripThoughtBlocks func(string) string) protocol.Message {
	content := msg.Content
	if msg.Role == "assistant" || msg.Role == "tool" {
		content = stripThoughtBlocks(content)
	}
	return protocol.Message{Role: msg.Role, Content: content, ReasoningContent: msg.ReasoningContent}
}

func isVisibleHistoryMessage(message contract.EngineMessage) bool {
	return message.Role != "system"
}
