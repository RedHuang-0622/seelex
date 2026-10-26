package core

// role_permissions.go keeps only the core-side port adapters and one-line
// Service compatibility entries. Role policy rules live in application/policy.

import (
	applicationpolicy "github.com/RedHuang-0622/seelex/application/policy"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
)

// ErrRolePermissionsUnavailable preserves the established error identity.
var ErrRolePermissionsUnavailable = applicationpolicy.ErrRolePermissionsUnavailable

// RoleDefaultVector reads a role's explicit default permission vector.
func (service *Service) RoleDefaultVector(sessionID, role string) ([]string, bool, error) {
	return service.roleApplicationComponent().RoleDefaultVector(sessionID, role)
}

// SetRoleDefaultVector writes a role's default permission vector.
func (service *Service) SetRoleDefaultVector(sessionID, role string, vector []string) error {
	return service.roleApplicationComponent().SetRoleDefaultVector(sessionID, role, vector)
}

// RoleAccount reads a role's explicit default account.
func (service *Service) RoleAccount(sessionID, role string) (string, bool, error) {
	return service.roleApplicationComponent().RoleAccount(sessionID, role)
}

// SetRoleAccount applies and persists a role's default account.
func (service *Service) SetRoleAccount(sessionID, role, account string) error {
	return service.roleApplicationComponent().SetRoleAccount(sessionID, role, account)
}

// restoreRoleAccounts applies saved role accounts to the runtime.
func (service *Service) restoreRoleAccounts(sessionID string) error {
	return service.roleApplicationComponent().RestoreRoleAccounts(sessionID)
}

// RoleInheritMainAgent reports whether a role inherits mainagent settings.
func (service *Service) RoleInheritMainAgent(sessionID, role string) (bool, error) {
	return service.roleApplicationComponent().RoleInheritMainAgent(sessionID, role)
}

// SetRoleInheritMainAgent updates role inheritance.
func (service *Service) SetRoleInheritMainAgent(sessionID, role string, inherit bool) error {
	return service.roleApplicationComponent().SetRoleInheritMainAgent(sessionID, role, inherit)
}

// RoleSSHUserSpaces reads a role's effective SSH path scope.
func (service *Service) RoleSSHUserSpaces(sessionID, role string) ([]string, error) {
	return service.roleApplicationComponent().RoleSSHUserSpaces(sessionID, role)
}

// RoleDefaultSSHUserSpaces reads a role's default SSH path scope.
func (service *Service) RoleDefaultSSHUserSpaces(sessionID, role string) ([]string, error) {
	return service.roleApplicationComponent().RoleDefaultSSHUserSpaces(sessionID, role)
}

// SetRoleSSHUserSpaces writes a role's default SSH path scope.
func (service *Service) SetRoleSSHUserSpaces(sessionID, role string, paths []string) error {
	return service.roleApplicationComponent().SetRoleSSHUserSpaces(sessionID, role, paths)
}

// SetRoleSessionSSHUserSpaces writes a session-local SSH path scope.
func (service *Service) SetRoleSessionSSHUserSpaces(sessionID, role string, paths []string) error {
	return service.roleApplicationComponent().SetRoleSessionSSHUserSpaces(sessionID, role, paths)
}

// restoreRolePermissions restores effective settings for the requested roles.
func (service *Service) restoreRolePermissions(sessionID string, roles []string) (map[string]dto.RoleSettings, error) {
	return service.roleApplicationComponent().RestoreRolePermissions(sessionID, roles)
}

// RolePermissionVector reads one effective role permission vector.
func (service *Service) RolePermissionVector(sessionID, role string) ([]string, error) {
	return service.roleApplicationComponent().RolePermissionVector(sessionID, role)
}

// SetRolePermissionVector writes a session-local role permission vector.
func (service *Service) SetRolePermissionVector(sessionID, role string, vector []string) error {
	return service.roleApplicationComponent().SetRolePermissionVector(sessionID, role, vector)
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
