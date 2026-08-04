package seelebridge

import (
	"testing"

	toolspermission "github.com/RedHuang-0622/Seele/tools/permission"
)

func TestPermissionGateReportsFullAccessState(t *testing.T) {
	state := &permissionGateState{}
	state.set(toolspermission.PermissionConfig{Mode: toolspermission.ModeManual}, nil)
	if state.fullAccess() {
		t.Fatal("manual permission gate reported full access")
	}
	state.setFullAccess(true)
	if !state.fullAccess() {
		t.Fatal("permission gate did not report enabled full access")
	}
	state.setFullAccess(false)
	if state.fullAccess() {
		t.Fatal("permission gate did not restore manual mode")
	}
}
