package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestStoreDDDMTeamworkModelOwnershipGate is the first M8 slice gate.
func TestStoreDDDMTeamworkModelOwnershipGate(t *testing.T) {
	root := repoRoot()
	for _, name := range []string{"model.go", "validation.go", "README.md"} {
		if _, err := os.Stat(filepath.Join(root, "store", "teamwork", name)); err != nil {
			t.Fatalf("store/teamwork/%s 尚未落位：%v", name, err)
		}
	}
	body, err := os.ReadFile(filepath.Join(root, "store", "teamwork", "model.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, symbol := range []string{"type Event", "type Binding", "func Bindings"} {
		if !strings.Contains(string(body), symbol) {
			t.Errorf("store/teamwork/model.go missing %s", symbol)
		}
	}
	validation, err := os.ReadFile(filepath.Join(root, "store", "teamwork", "validation.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(validation), "func ValidatePlan(") {
		t.Error("store/teamwork/validation.go missing ValidatePlan")
	}
}
