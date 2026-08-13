package tools

import (
	"testing"

	toolspermission "github.com/RedHuang-0622/Seele/tools/permission"
)

func TestPermissionGateReportsFullAccessState(t *testing.T) {
	state := &PermissionGate{}
	state.Set(toolspermission.PermissionConfig{Mode: toolspermission.ModeManual}, nil)
	if state.FullAccess() {
		t.Fatal("manual permission gate reported full access")
	}
	state.SetFullAccess(true)
	if !state.FullAccess() {
		t.Fatal("permission gate did not report enabled full access")
	}
	state.SetFullAccess(false)
	if state.FullAccess() {
		t.Fatal("permission gate did not restore manual mode")
	}
}
