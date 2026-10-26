package core

import (
	"context"

	applicationadmin "github.com/RedHuang-0622/seelex/application/admin"
	view_state "github.com/RedHuang-0622/seelex/application/session/viewstate"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/protocol"
)

// newServiceAdminApplication adapts Service's narrow hosts to the admin
// account application. Account validation, runtime mutation, projection
// publication and persistence remain owned by their existing implementations.
func newServiceAdminApplication(service *Service) *applicationadmin.AccountApplication {
	return applicationadmin.NewAccountApplication(applicationadmin.AccountDeps{
		Admin: func() contract.AccountAdminPort {
			if service == nil {
				return nil
			}
			return service.Deps.AccountAdmin
		},
		Balance: func() contract.AccountBalancePort {
			if service == nil || service.Deps.Runtime == nil {
				return nil
			}
			port, _ := service.Deps.Runtime.(contract.AccountBalancePort)
			return port
		},
		Select: func(name string) bool {
			return service.Deps.Runtime.SelectAccount(name)
		},
		Window: func(name string, window int) (int, error) {
			runtimePort, _ := service.Deps.Runtime.(contract.AccountWindowPort)
			return service.components.accountWindowPolicy.Set(
				runtimePort,
				service.Deps.AccountWindowStore,
				name,
				window,
			)
		},
		Projection: serviceAdminProjectionPort{service: service},
	})
}

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
	return port.service.Snapshot().Runtime
}

func (port serviceAdminProjectionPort) PublishRuntimeChanged(revision uint64, sessionID string, runtime model.RuntimeState) {
	port.service.publishSessionEvent(EventRuntimeChanged, revision, "", sessionID, runtime)
}

func (port serviceAdminProjectionPort) AddNotice(message string) {
	port.service.components.view.AddNotice(message)
}
