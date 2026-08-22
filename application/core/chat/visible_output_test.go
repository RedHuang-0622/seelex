package chat

import "testing"

func TestVisibleOutputStreamSuppressesSplitThoughtBlock(t *testing.T) {
	stream := NewVisibleOutputStream("request-1")
	if got := stream.Consume("visible<th"); got != "visible" {
		t.Fatalf("first chunk = %q", got)
	}
	if got := stream.Consume("ink>private</think> answer"); got != " answer" {
		t.Fatalf("second chunk = %q", got)
	}
}
