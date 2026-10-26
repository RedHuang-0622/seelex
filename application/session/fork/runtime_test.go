package sessionfork

import "testing"

func TestRuntimeLifecycleCallbacks(t *testing.T) {
	closed, draining := false, false
	runtime := NewRuntime(RuntimeDeps{
		Closed:   func() bool { return closed },
		Draining: func() bool { return draining },
	})
	if runtime.closed() || runtime.draining() {
		t.Fatal("runtime lifecycle should start open/idle")
	}
	closed, draining = true, true
	if !runtime.closed() || !runtime.draining() {
		t.Fatal("runtime did not observe lifecycle callbacks")
	}
}
