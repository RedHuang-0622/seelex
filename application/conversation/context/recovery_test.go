package context

import (
	"errors"
	"testing"
)

func TestRetryableAfterRecoveryOnlyAcceptsPreExecutionRejections(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"context", errors.New("context window exceeds limit"), true},
		{"tool pairing", errors.New(`ChatClient stream: HTTP 400: {"error":{"message":"An assistant message with 'tool_calls' must be followed by tool messages responding to each 'tool_call_id'. (insufficient tool messages following tool_calls message)"}}`), true},
		{"timeout", errors.New("HTTP 504: timeout_error"), false},
		{"server", errors.New("HTTP 503: server_error"), false},
		{"nil", nil, false},
	}
	for _, testCase := range cases {
		if got := RetryableAfterRecovery(testCase.err); got != testCase.want {
			t.Fatalf("%s: got %v want %v", testCase.name, got, testCase.want)
		}
	}
}

func TestNonEmptyProviderInputKeepsRequestOrSuppliesRecoveryNote(t *testing.T) {
	if got := NonEmptyProviderInput("  continue  "); got != "  continue  " {
		t.Fatalf("non-empty input changed: %q", got)
	}
	if got := NonEmptyProviderInput(" \n "); got == "" {
		t.Fatal("empty input did not receive recovery note")
	}
}
