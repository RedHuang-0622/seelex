package seelebridge

import (
	"encoding/json"
	"testing"
)

func FuzzNormalizePlanLoadArguments(f *testing.F) {
	for _, seed := range []string{
		`{"entry":"inspect","nodes":{"inspect":{"input":"read"}},"edges":{}}`,
		`{"entry":"inspect","nodes":[{"id":"inspect","input":"read"},{"id":"verify","input":"test"}],"edges":["verify"]}`,
		`{"entry":"a","nodes":{"a":{"input":"x"},"b":{"input":"y"}},"edges":"a -> b"}`,
		`{}`,
		`not-json`,
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		if len(input) > 1<<20 {
			t.Skip()
		}
		canonical, err := NormalizePlanLoadArguments(input)
		if err != nil {
			return
		}
		if !json.Valid([]byte(canonical)) {
			t.Fatalf("normalizer returned invalid JSON: %q", canonical)
		}
		second, err := NormalizePlanLoadArguments(canonical)
		if err != nil {
			t.Fatalf("canonical output is not accepted: %v", err)
		}
		if second != canonical {
			t.Fatalf("normalization is not idempotent:\nfirst:  %s\nsecond: %s", canonical, second)
		}
	})
}
