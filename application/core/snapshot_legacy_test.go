package core

import protocol "github.com/RedHuang-0622/seelex/contract/protocol"

// Snapshot is a test-only compatibility shim for the existing core integration
// suite. Production frontends use viewstate.SnapshotApplication through compat.
func (service *Service) Snapshot() protocol.Snapshot {
	return service.components.frontendSnapshot.Snapshot()
}
