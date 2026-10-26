package sessionhistory

import (
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/contract"
)

func TestRuntimeHistoryVisibilityAndMessageProjection(t *testing.T) {
	runtime := &Runtime{displayInput: strings.TrimSpace}
	user := contract.EngineMessage{Role: "user", Content: "  hello  "}
	if !runtime.IsVisibleHistoryMessage(user) {
		t.Fatal("user row should be visible")
	}
	message := runtime.AdaptEngineMessage(user)
	if message.Content != "hello" {
		t.Fatalf("content = %q", message.Content)
	}
	if runtime.IsVisibleHistoryMessage(contract.EngineMessage{Role: "system", Content: "prompt"}) {
		t.Fatal("system row must remain hidden")
	}
}
