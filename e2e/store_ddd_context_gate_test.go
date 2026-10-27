package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestStoreDDDMContextOwnershipGate is the first M7 slice gate.
func TestStoreDDDMContextOwnershipGate(t *testing.T) {
	root := repoRoot()
	for _, name := range []string{"context.go", "README.md"} {
		if _, err := os.Stat(filepath.Join(root, "store", "context", name)); err != nil {
			t.Fatalf("store/context/%s 尚未落位：%v", name, err)
		}
	}
}

// TestStoreDDDMCompactStackReadFaceGate pins the M7 read-face split: consumers
// that only need compact frames must not depend on the universal context record.
func TestStoreDDDMCompactStackReadFaceGate(t *testing.T) {
	root := repoRoot()
	files := []string{
		"store/session_context.go",
		"backend/engine/context/seelectx/controller.go",
		"backend/engine/context/seelectx/search/search.go",
		"backend/engine/assemble/runtime.go",
		"kernel/context/wiring.go",
	}
	for _, name := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		text := string(body)
		if !strings.Contains(text, "CompactStackSnapshot") {
			t.Errorf("%s does not use the compact-stack read face", name)
		}
	}
	for _, name := range files[1:] {
		path := filepath.Join(root, filepath.FromSlash(name))
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if strings.Contains(string(body), "Snapshot() store.SessionContextRecord") {
			t.Errorf("%s still exposes the universal context record to compact-stack consumers", name)
		}
	}
}

// TestStoreDDDMPromptStackReadFaceGate pins the prompt/local-compaction
// projection: these consumers receive only PromptStackSnapshot and cannot read
// storage-only context fields through SessionContextRecord.
func TestStoreDDDMPromptStackReadFaceGate(t *testing.T) {
	root := repoRoot()
	files := []string{
		"store/session_context.go",
		"backend/engine/context/seelectx/assembler.go",
		"backend/engine/context/seelectx/frame.go",
		"backend/engine/context/seelectx/gap.go",
		"backend/engine/context/seelectx/dag.go",
		"backend/engine/context/seelectx/controller.go",
		"backend/engine/assemble/runtime_context.go",
		"kernel/context/wiring.go",
	}
	for _, name := range files {
		body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		text := string(body)
		if !strings.Contains(text, "PromptStackSnapshot") {
			t.Errorf("%s does not use the prompt-stack read face", name)
		}
		if name != files[0] && strings.Contains(text, "SessionContextRecord") {
			t.Errorf("%s still exposes SessionContextRecord to prompt consumers", name)
		}
	}
}

// TestStoreDDDMContextForkOwnershipGate pins context JSON rewriting in the
// context domain; session fork may read/write the payload but must not own the
// record transformation.
func TestStoreDDDMContextForkOwnershipGate(t *testing.T) {
	root := repoRoot()
	owner, err := os.ReadFile(filepath.Join(root, "store", "context", "fork.go"))
	if err != nil {
		t.Fatalf("read context fork owner: %v", err)
	}
	for _, symbol := range []string{"func ForkState", "func RewriteForkCompactStack"} {
		if !strings.Contains(string(owner), symbol) {
			t.Errorf("store/context/fork.go missing %s", symbol)
		}
	}
	consumer, err := os.ReadFile(filepath.Join(root, "kernel", "session", "fork.go"))
	if err != nil {
		t.Fatalf("read session fork consumer: %v", err)
	}
	text := string(consumer)
	if !strings.Contains(text, "storecontext.ForkState") {
		t.Error("kernel/session/fork.go does not call storecontext.ForkState")
	}
	if strings.Contains(text, "SessionContextRecord") {
		t.Error("kernel/session/fork.go still owns SessionContextRecord rewriting")
	}
}
