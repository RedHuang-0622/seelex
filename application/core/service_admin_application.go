package core

import (
	"context"

	view_state "github.com/RedHuang-0622/seelex/application/session/viewstate"
	"github.com/RedHuang-0622/seelex/contract/protocol"
)

type serviceAdminProjectionPort struct {
	service *Service
}

func (port serviceAdminProjectionPort) Collect(ctx context.Context) view_state.RuntimeStateProjection {
	return port.service.components.view.CollectRuntimeProjection(ctx)
}

func (port serviceAdminProjectionPort) LockView() {
	port.service.ViewMu.Lock()
}

func (port serviceAdminProjectionPort) SetRuntimeAccount(name string) {
	port.service.Core.Snapshot.Runtime.Account = name
}

func (port serviceAdminProjectionPort) ApplyRuntimeProjectionLocked(projection view_state.RuntimeStateProjection) {
	port.service.components.view.ApplyRuntimeProjectionLocked(projection)
}

func (port serviceAdminProjectionPort) BumpLocked() uint64 {
	return port.service.components.view.BumpLocked()
}

func (port serviceAdminProjectionPort) UnlockView() {
	port.service.ViewMu.Unlock()
}

func (port serviceAdminProjectionPort) CurrentViewSessionID() string {
	return port.service.currentViewSessionID()
}

func (port serviceAdminProjectionPort) RuntimeSnapshot() model.RuntimeState {
	return port.service.components.frontendSnapshot.Snapshot().Runtime
}

func (port serviceAdminProjectionPort) PublishRuntimeChanged(revision uint64, sessionID string, runtime model.RuntimeState) {
	port.service.publishSessionEvent(EventRuntimeChanged, revision, "", sessionID, runtime)
}

func (port serviceAdminProjectionPort) AddNotice(message string) {
	port.service.components.view.AddNotice(message)
}
