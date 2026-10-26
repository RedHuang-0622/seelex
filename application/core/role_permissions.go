package core

// role_permissions.go keeps only the core-side port adapters. Public role
// compatibility entries live in application/compat.

import (
	applicationpolicy "github.com/RedHuang-0622/seelex/application/policy"
	"github.com/RedHuang-0622/seelex/contract"
)

// ErrRolePermissionsUnavailable preserves the established error identity.
var ErrRolePermissionsUnavailable = applicationpolicy.ErrRolePermissionsUnavailable

// restoreRoleAccounts applies saved role accounts to the runtime.
// restoreRolePermissions restores effective settings for the requested roles.
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
