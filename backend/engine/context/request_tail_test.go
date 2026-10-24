package context_runtime

import (
	"strings"
	"testing"
)

func TestComposeRequestTailKeepsEnvironmentAfterInput(t *testing.T) {
	got := composeRequestTail("用户输入", "<!-- work -->\nwork", "<!-- env -->\nenv")
	if !strings.HasPrefix(got, "<!-- work -->") {
		t.Fatalf("工作块应在输入前: %q", got)
	}
	inputAt := strings.Index(got, "用户输入")
	envAt := strings.Index(got, "<!-- env -->")
	if inputAt < 0 || envAt < 0 || envAt <= inputAt {
		t.Fatalf("环境块必须是输入之后的后缀: %q", got)
	}
	if want := "<!-- work -->\nwork\n\n用户输入\n\n<!-- env -->\nenv"; got != want {
		t.Fatalf("tail = %q, want %q", got, want)
	}
}

func TestComposeRequestTailOmitsEmptyBlocks(t *testing.T) {
	if got := composeRequestTail("用户输入", "", ""); got != "用户输入" {
		t.Fatalf("got %q", got)
	}
	if got := composeRequestTail("", "", "<!-- env -->\nenv"); got != "<!-- env -->\nenv" {
		t.Fatalf("got %q", got)
	}
}
