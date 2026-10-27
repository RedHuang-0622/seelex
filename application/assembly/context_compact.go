package assembly

import conversationcontext "github.com/RedHuang-0622/seelex/application/conversation/context"

// ContextCompactionResult is the stable public compaction receipt.
type ContextCompactionResult = conversationcontext.Result

// CompactionGateTiming is one gate timing in the receipt.
type CompactionGateTiming = conversationcontext.GateTiming

func compactionRangeLabel(result ContextCompactionResult) string {
	return conversationcontext.CompactionRangeLabel(result)
}

func compactionRecordNote(result ContextCompactionResult) string {
	return conversationcontext.CompactionRecordNote(result)
}
