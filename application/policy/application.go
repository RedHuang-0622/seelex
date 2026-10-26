// Package policy contains application-level role policy use cases. It owns
// orchestration and validation, while persistence and runtime routing remain
// in the injected host ports.
package policy

import (
	"errors"
	"path"
	"strings"

	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
)

// ErrRolePermissionsUnavailable reports that the session port does not expose
// the role permission registry.
var ErrRolePermissionsUnavailable = errors.New("role permissions: session port does not expose the role permission registry")

// RoleDefaultsPort is the role-settings capability exposed by the session
// adapter. All methods are scoped by the supplied session so the adapter can
// resolve the owning project.
type RoleDefaultsPort interface {
	RoleDefault(sessionID, role string) ([]string, bool, error)
	SetRoleDefault(sessionID, role string, vector []string) error
	RoleAccount(sessionID, role string) (string, bool, error)
	SetRoleAccount(sessionID, role, account string) error
	RoleInheritance(sessionID, role string) (bool, bool, error)
	SetRoleInheritance(sessionID, role string, inherit bool) error
	RoleSSHUserSpaces(sessionID, role string) ([]string, error)
	RoleDefaultSSHUserSpaces(sessionID, role string) ([]string, error)
	SetRoleSSHUserSpaces(sessionID, role string, paths []string) error
	SetRoleSessionSSHUserSpaces(sessionID, role string, paths []string) error
}

// PermissionRegistryPort reads and writes session-local role permission
// registrations.
type PermissionRegistryPort interface {
	RolePermissionVectors(sessionID string) (map[string][]string, error)
	RolePermissionVector(sessionID, role string) ([]string, bool, error)
	SetRolePermissionVector(sessionID, role string, vector []string) error
}

// PermissionLayerPort supplies the five-layer role settings used only when a
// session-local registration is absent.
type PermissionLayerPort interface {
	RolePermissionLayers(sessionID, role string) dto.RoleSettingsLayers
}

// Deps contains only the host capabilities needed by role policy use cases.
// Accessor functions preserve call-time port resolution.
type Deps struct {
	RoleDefaults func() RoleDefaultsPort
	Runtime      func() contract.AccountRoleSelectionPort
	Registry     func() PermissionRegistryPort
	Layers       func() PermissionLayerPort
}

// RoleApplication owns role account, permission-vector, inheritance, and SSH
// path application rules.
type RoleApplication struct {
	deps Deps
}

// NewRoleApplication constructs the role policy application.
func NewRoleApplication(deps Deps) *RoleApplication {
	return &RoleApplication{deps: deps}
}

// RoleDefaultVector reads a role's explicit default vector.
func (application *RoleApplication) RoleDefaultVector(sessionID, role string) ([]string, bool, error) {
	port := application.roleDefaults()
	if port == nil {
		return nil, false, ErrRolePermissionsUnavailable
	}
	sessionID, role = strings.TrimSpace(sessionID), strings.TrimSpace(role)
	if role == "" {
		return nil, false, errors.New("role default: role is required")
	}
	vector, explicit, err := port.RoleDefault(sessionID, role)
	if err != nil {
		return nil, false, err
	}
	if explicit {
		if vector == nil {
			return []string{}, true, nil
		}
		return vector, true, nil
	}
	return []string{}, false, nil
}

// SetRoleDefaultVector validates and writes a role's default vector without
// changing existing session-local registrations.
func (application *RoleApplication) SetRoleDefaultVector(sessionID, role string, vector []string) error {
	port := application.roleDefaults()
	if port == nil {
		return ErrRolePermissionsUnavailable
	}
	sessionID, role = strings.TrimSpace(sessionID), strings.TrimSpace(role)
	if role == "" {
		return errors.New("role default: role is required")
	}
	normalized, err := dto.NormalizePermissionVector(vector)
	if err != nil {
		return err
	}
	return port.SetRoleDefault(sessionID, role, normalized)
}

// RoleAccount reads a role's explicit default account.
func (application *RoleApplication) RoleAccount(sessionID, role string) (string, bool, error) {
	port := application.roleDefaults()
	if port == nil {
		return "", false, ErrRolePermissionsUnavailable
	}
	sessionID, role = strings.TrimSpace(sessionID), strings.TrimSpace(role)
	if role == "" {
		return "", false, errors.New("role account: role is required")
	}
	return port.RoleAccount(sessionID, role)
}

// SetRoleAccount applies a role account to the runtime before persisting it
// and rolls the runtime back when persistence fails.
func (application *RoleApplication) SetRoleAccount(sessionID, role, account string) error {
	port := application.roleDefaults()
	if port == nil {
		return ErrRolePermissionsUnavailable
	}
	sessionID, role, account = strings.TrimSpace(sessionID), strings.TrimSpace(role), strings.TrimSpace(account)
	if role == "" || account == "" {
		return errors.New("role account: role and account are required")
	}
	runtimePort := application.runtime()
	if runtimePort != nil {
		if err := runtimePort.SetRoleAccount(role, account); err != nil {
			return err
		}
	}
	if err := port.SetRoleAccount(sessionID, role, account); err != nil {
		if runtimePort != nil {
			_ = runtimePort.SetRoleAccount(role, "")
		}
		return err
	}
	return nil
}

// RestoreRoleAccounts applies all explicitly stored role accounts to the
// runtime. Missing runtime support is a supported no-op.
func (application *RoleApplication) RestoreRoleAccounts(sessionID string) error {
	port := application.roleDefaults()
	if port == nil {
		return ErrRolePermissionsUnavailable
	}
	runtimePort := application.runtime()
	if runtimePort == nil {
		return nil
	}
	for _, role := range dto.AccountRoleVocabulary() {
		account, ok, err := port.RoleAccount(sessionID, role)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		if err := runtimePort.SetRoleAccount(role, account); err != nil {
			return err
		}
	}
	return nil
}

// RoleInheritMainAgent reports whether a role inherits mainagent settings.
func (application *RoleApplication) RoleInheritMainAgent(sessionID, role string) (bool, error) {
	port := application.roleDefaults()
	if port == nil {
		return false, ErrRolePermissionsUnavailable
	}
	role = strings.TrimSpace(role)
	inherit, explicit, err := port.RoleInheritance(sessionID, role)
	if err != nil {
		return false, err
	}
	if explicit {
		return inherit, nil
	}
	return role == string(dto.RoleSubAgent) || role == string(dto.RoleTeammate), nil
}

// SetRoleInheritMainAgent updates inheritance for subagent or teammate only.
func (application *RoleApplication) SetRoleInheritMainAgent(sessionID, role string, inherit bool) error {
	port := application.roleDefaults()
	if port == nil {
		return ErrRolePermissionsUnavailable
	}
	role = strings.TrimSpace(role)
	if role != string(dto.RoleSubAgent) && role != string(dto.RoleTeammate) {
		return errors.New("role inheritance: only subagent and teammate can inherit mainagent")
	}
	return port.SetRoleInheritance(sessionID, role, inherit)
}

// RoleSSHUserSpaces reads the effective SSH path scope for a role. Inherited
// subagent and teammate roles use mainagent's scope.
func (application *RoleApplication) RoleSSHUserSpaces(sessionID, role string) ([]string, error) {
	port := application.roleDefaults()
	if port == nil {
		return nil, ErrRolePermissionsUnavailable
	}
	sessionID, role = strings.TrimSpace(sessionID), strings.TrimSpace(role)
	if role == string(dto.RoleSubAgent) || role == string(dto.RoleTeammate) {
		inherit, explicit, err := port.RoleInheritance(sessionID, role)
		if err != nil {
			return nil, err
		}
		if !explicit || inherit {
			return port.RoleSSHUserSpaces(sessionID, string(dto.RoleAgent))
		}
	}
	return port.RoleSSHUserSpaces(sessionID, role)
}

// RoleDefaultSSHUserSpaces reads a role's cross-session default SSH paths.
func (application *RoleApplication) RoleDefaultSSHUserSpaces(sessionID, role string) ([]string, error) {
	port := application.roleDefaults()
	if port == nil {
		return nil, ErrRolePermissionsUnavailable
	}
	sessionID, role = strings.TrimSpace(sessionID), strings.TrimSpace(role)
	if role == "" {
		return nil, errors.New("role user spaces: role is required")
	}
	return port.RoleDefaultSSHUserSpaces(sessionID, role)
}

// SetRoleSSHUserSpaces writes normalized default SSH paths for a role.
func (application *RoleApplication) SetRoleSSHUserSpaces(sessionID, role string, paths []string) error {
	port := application.roleDefaults()
	if port == nil {
		return ErrRolePermissionsUnavailable
	}
	role = strings.TrimSpace(role)
	if role == "" {
		return errors.New("role user spaces: role is required")
	}
	normalized, err := normalizeRemoteUserSpaces(paths)
	if err != nil {
		return err
	}
	return port.SetRoleSSHUserSpaces(sessionID, role, normalized)
}

// SetRoleSessionSSHUserSpaces writes a normalized session-local SSH path
// override for one agent role.
func (application *RoleApplication) SetRoleSessionSSHUserSpaces(sessionID, role string, paths []string) error {
	port := application.roleDefaults()
	if port == nil {
		return ErrRolePermissionsUnavailable
	}
	sessionID, role = strings.TrimSpace(sessionID), strings.TrimSpace(role)
	if sessionID == "" || role == "" {
		return errors.New("role user spaces: session and role are required")
	}
	normalized, err := normalizeRemoteUserSpaces(paths)
	if err != nil {
		return err
	}
	return port.SetRoleSessionSSHUserSpaces(sessionID, role, normalized)
}

// RestoreRolePermissions restores effective settings for the requested roles:
// a stored registration wins, otherwise the five-layer resolver is used.
func (application *RoleApplication) RestoreRolePermissions(sessionID string, roles []string) (map[string]dto.RoleSettings, error) {
	port := application.registry()
	if port == nil {
		return nil, ErrRolePermissionsUnavailable
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, errors.New("role permissions: session id is required")
	}
	stored, err := port.RolePermissionVectors(sessionID)
	if err != nil {
		return nil, err
	}
	restored := make(map[string]dto.RoleSettings, len(roles))
	for _, raw := range roles {
		role := strings.TrimSpace(raw)
		if role == "" {
			continue
		}
		vector, explicit := stored[role]
		restored[role] = application.resolveRoleSettings(sessionID, role, vector, explicit, true)
	}
	return restored, nil
}

// RolePermissionVector reads one effective permission vector.
func (application *RoleApplication) RolePermissionVector(sessionID, role string) ([]string, error) {
	port := application.registry()
	if port == nil {
		return nil, ErrRolePermissionsUnavailable
	}
	sessionID, role = strings.TrimSpace(sessionID), strings.TrimSpace(role)
	if sessionID == "" || role == "" {
		return nil, errors.New("role permissions: session id and role are required")
	}
	vector, stored, err := port.RolePermissionVector(sessionID, role)
	if err != nil {
		return nil, err
	}
	if stored {
		return application.resolveRoleSettings(sessionID, role, vector, true, false).PermissionVector, nil
	}
	return application.resolveRoleSettings(sessionID, role, nil, false, true).PermissionVector, nil
}

// SetRolePermissionVector validates and writes a session-local role vector.
func (application *RoleApplication) SetRolePermissionVector(sessionID, role string, vector []string) error {
	port := application.registry()
	if port == nil {
		return ErrRolePermissionsUnavailable
	}
	sessionID, role = strings.TrimSpace(sessionID), strings.TrimSpace(role)
	if sessionID == "" || role == "" {
		return errors.New("role permissions: session id and role are required")
	}
	normalized, err := dto.NormalizePermissionVector(vector)
	if err != nil {
		return err
	}
	return port.SetRolePermissionVector(sessionID, role, normalized)
}

func (application *RoleApplication) resolveRoleSettings(
	sessionID, role string,
	storedVector []string,
	stored bool,
	resolveLayerMetadata bool,
) dto.RoleSettings {
	settings := dto.RoleSettings{}
	if layers := application.layers(); resolveLayerMetadata && layers != nil {
		settings = dto.ResolveRoleSettings(layers.RolePermissionLayers(sessionID, role).Ordered()...)
	}
	if stored {
		settings.PermissionVector = storedVector
	}
	if settings.PermissionVector == nil {
		settings.PermissionVector = []string{}
	}
	return settings
}

func (application *RoleApplication) roleDefaults() RoleDefaultsPort {
	if application == nil || application.deps.RoleDefaults == nil {
		return nil
	}
	return application.deps.RoleDefaults()
}

func (application *RoleApplication) runtime() contract.AccountRoleSelectionPort {
	if application == nil || application.deps.Runtime == nil {
		return nil
	}
	return application.deps.Runtime()
}

func (application *RoleApplication) registry() PermissionRegistryPort {
	if application == nil || application.deps.Registry == nil {
		return nil
	}
	return application.deps.Registry()
}

func (application *RoleApplication) layers() PermissionLayerPort {
	if application == nil || application.deps.Layers == nil {
		return nil
	}
	return application.deps.Layers()
}

func normalizeRemoteUserSpaces(paths []string) ([]string, error) {
	if len(paths) > 64 {
		return nil, errors.New("role user spaces: at most 64 paths are allowed")
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(paths))
	for _, raw := range paths {
		item := strings.TrimSpace(raw)
		if item == "" {
			continue
		}
		if len(item) > 512 || !strings.HasPrefix(item, "/") {
			return nil, errors.New("role user spaces: every path must be an absolute remote POSIX path")
		}
		clean := path.Clean(item)
		if clean == "/" {
			return nil, errors.New("role user spaces: root path is too broad; list specific directories")
		}
		if _, exists := seen[clean]; exists {
			continue
		}
		seen[clean] = struct{}{}
		out = append(out, clean)
	}
	if len(out) == 0 {
		return nil, errors.New("role user spaces: list at least one absolute remote path")
	}
	return out, nil
}
