package core

// role_permissions.go keeps only the core-side port adapters. Public role
// compatibility entries live in application/compat.

import (
	applicationpolicy "github.com/RedHuang-0622/seelex/application/policy"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
)

// ErrRolePermissionsUnavailable preserves the established error identity.
var ErrRolePermissionsUnavailable = applicationpolicy.ErrRolePermissionsUnavailable

// restoreRoleAccounts applies saved role accounts to the runtime.
func (service *Service) restoreRoleAccounts(sessionID string) error {
	return service.roleApplicationComponent().RestoreRoleAccounts(sessionID)
}

// restoreRolePermissions restores effective settings for the requested roles.
func (service *Service) restoreRolePermissions(sessionID string, roles []string) (map[string]dto.RoleSettings, error) {
	return service.roleApplicationComponent().RestoreRolePermissions(sessionID, roles)
}

func (service *Service) roleApplicationComponent() *applicationpolicy.RoleApplication {
	if service == nil {
		return nil
	}
	return service.components.roleApplication
}

func newServiceRoleApplication(service *Service) *applicationpolicy.RoleApplication {
	return applicationpolicy.NewRoleApplication(applicationpolicy.Deps{
		RoleDefaults: func() applicationpolicy.RoleDefaultsPort {
			if service == nil || service.Deps.Sessions == nil {
				return nil
			}
			port, _ := service.Deps.Sessions.(applicationpolicy.RoleDefaultsPort)
			return port
		},
		Runtime: func() contract.AccountRoleSelectionPort {
			if service == nil || service.Deps.Runtime == nil {
				return nil
			}
			port, _ := service.Deps.Runtime.(contract.AccountRoleSelectionPort)
			return port
		},
		Registry: func() applicationpolicy.PermissionRegistryPort {
			if service == nil || service.Deps.Sessions == nil {
				return nil
			}
			port, _ := service.Deps.Sessions.(applicationpolicy.PermissionRegistryPort)
			return port
		},
		Layers: func() applicationpolicy.PermissionLayerPort {
			if service == nil || service.Deps.Sessions == nil {
				return nil
			}
			port, _ := service.Deps.Sessions.(applicationpolicy.PermissionLayerPort)
			return port
		},
	})
}
