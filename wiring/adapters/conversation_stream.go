package adapters

import (
	applicationconversation "github.com/RedHuang-0622/seelex/application/conversation"
	reactstream "github.com/RedHuang-0622/seelex/kernel/react"
)

type conversationStreamPort struct{}

var _ applicationconversation.StreamOutputPort = conversationStreamPort{}

// NewConversationStreamPort exposes visible-output and batching primitives
// through the conversation-owned port.
func NewConversationStreamPort() applicationconversation.StreamOutputPort {
	return conversationStreamPort{}
}

func (conversationStreamPort) NewBatcher(render func([]string), options applicationconversation.StreamBatcherOptions) applicationconversation.DeltaBatcher {
	return reactstream.NewStreamBatcher(render, reactstream.StreamBatcherOptions{
		FlushSize:  options.FlushSize,
		BufferSize: options.BufferSize,
		Interval:   options.Interval,
	})
}

func (conversationStreamPort) NewVisibleOutput(requestID string) applicationconversation.VisibleOutput {
	return reactstream.NewVisibleOutputStream(requestID)
}

func (conversationStreamPort) StripThoughtBlocks(value string) string {
	return reactstream.StripThoughtBlocks(value)
}
