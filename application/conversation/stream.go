package conversation

import "time"

// VisibleOutput consumes streamed model output and strips provider-only
// reasoning blocks before text reaches the visible conversation.
type VisibleOutput interface {
	Consume(chunk string) string
	RequestID() string
}

// DeltaBatcher accepts streamed chunks and flushes visible deltas.
type DeltaBatcher interface {
	OnChunk(chunk string)
	Flush() error
	FlushPending() error
}

// StreamBatcherOptions is the bounded buffering policy.
type StreamBatcherOptions struct {
	FlushSize  int
	BufferSize int
	Interval   time.Duration
}

// StreamOutputPort is the consumer port for streamed visible output.
type StreamOutputPort interface {
	NewBatcher(render func([]string), options StreamBatcherOptions) DeltaBatcher
	NewVisibleOutput(requestID string) VisibleOutput
	StripThoughtBlocks(value string) string
}
