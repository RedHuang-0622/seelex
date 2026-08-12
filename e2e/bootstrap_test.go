package e2e

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/e2e/scenario"
)

func TestOfflineApplicationBootstrapComposition(t *testing.T) {
	value, err := scenario.LoadFile(filepath.Join(repoRoot(), "e2e/fixtures/approval-chat.json"))
	if err != nil {
		t.Fatal(err)
	}
	runner, err := scenario.NewHarnessRunner(value)
	if err != nil {
		t.Fatalf("compose offline application harness: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := runner.Run(ctx)
	if err != nil {
		t.Fatalf("run offline bootstrap scenario: %v", err)
	}
	if result.PassedSteps != len(value.Steps) || result.Snapshot.Chat.Running {
		t.Fatalf("offline bootstrap result = %#v", result)
	}
}
