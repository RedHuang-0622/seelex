package model

import "testing"

func TestWireToolResultRefRoundTripsThroughCallID(t *testing.T) {
	ref := WireToolResultRef("call_abc")
	if ref != "result:call_abc" {
		t.Fatalf("WireToolResultRef = %q, want result:call_abc", ref)
	}
	callID, ok := WireToolResultCallID(ref)
	if !ok || callID != "call_abc" {
		t.Fatalf("WireToolResultCallID(%q) = %q,%v, want call_abc,true", ref, callID, ok)
	}
}

func TestWireToolResultCallIDRejectsNonWireShape(t *testing.T) {
	cases := []string{
		"",
		"tr-abc",
		"result:",
		"resulted:call_abc",
		"node:n1:payload",
	}
	for _, ref := range cases {
		if callID, ok := WireToolResultCallID(ref); ok {
			t.Errorf("WireToolResultCallID(%q) = %q,true; non-wire shape must not yield a call ID", ref, callID)
		}
	}
}
