package adapters

import (
	"testing"

	reactstream "github.com/RedHuang-0622/seelex/backend/engine/react"
)

func TestConversationStreamPortMatchesEnginePrimitives(t *testing.T) {
	port := NewConversationStreamPort()
	const input = "before<think>hidden</think>after"
	if got, want := port.StripThoughtBlocks(input), reactstream.StripThoughtBlocks(input); got != want {
		t.Fatalf("StripThoughtBlocks = %q, want %q", got, want)
	}

	portStream := port.NewVisibleOutput("request-a")
	engineStream := reactstream.NewVisibleOutputStream("request-a")
	for _, chunk := range []string{"before<thi", "nk>hidden</thi", "nk>after"} {
		if got, want := portStream.Consume(chunk), engineStream.Consume(chunk); got != want {
			t.Fatalf("Consume(%q) = %q, want %q", chunk, got, want)
		}
	}
}
