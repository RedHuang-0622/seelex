package provider

import "testing"

func TestEngineProvider_Name(t *testing.T) {
	p := &EngineProvider{}
	if n := p.Name(); n != "engine" { t.Fatalf("got %q", n) }
}

func TestEngineProvider_NilPanic(t *testing.T) {
	defer func() { recover() }()
	NewEngineProvider(nil)
	t.Fatal("expected panic")
}

func TestEngineProvider_NilPanicWithGoal(t *testing.T) {
	defer func() { recover() }()
	NewEngineProviderWithGoal(nil, "test")
	t.Fatal("expected panic")
}
