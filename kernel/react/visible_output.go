package chat

import protocol "github.com/RedHuang-0622/seelex/contract/protocol"

type VisibleOutputStream = protocol.VisibleOutputStream

func NewVisibleOutputStream(requestID string) *VisibleOutputStream {
	return protocol.NewVisibleOutputStream(requestID)
}

func StripThoughtBlocks(value string) string {
	return protocol.StripThoughtBlocks(value)
}
