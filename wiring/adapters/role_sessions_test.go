package adapters

import (
	"strings"
	"testing"
)

func TestRoleSessionsFactoryPreservesUnassembledError(t *testing.T) {
	port := NewRoleSessionsFactory()(nil)
	_, err := port.ListRoleSessions("session")
	if err == nil || !strings.Contains(err.Error(), "role session storage is not assembled") {
		t.Fatalf("ListRoleSessions error = %v, want unassembled error", err)
	}
}
