package policy

import (
	"errors"
	"reflect"
	"testing"

	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
)

func TestRoleApplicationPrefersExplicitStoredVector(t *testing.T) {
	defaults := &roleDefaultsFake{}
	registry := &permissionRegistryFake{
		vectors: map[string][]string{
			string(dto.RoleTeammate): {},
		},
	}
	layers := permissionLayersFake{
		layers: dto.RoleSettingsLayers{
			UserAllow: dto.RoleSettings{PermissionVector: []string{dto.PermissionTagRW}},
		},
	}
	application := NewRoleApplication(Deps{
		RoleDefaults: func() RoleDefaultsPort { return defaults },
		Registry:     func() PermissionRegistryPort { return registry },
		Layers:       func() PermissionLayerPort { return layers },
	})

	vector, err := application.RolePermissionVector("session-a", string(dto.RoleTeammate))
	if err != nil {
		t.Fatal(err)
	}
	if vector == nil || len(vector) != 0 {
		t.Fatalf("explicit empty vector = %#v, want non-nil empty vector", vector)
	}
}

func TestRoleApplicationFallsBackOnlyWhenUnregistered(t *testing.T) {
	layers := permissionLayersFake{
		layers: dto.RoleSettingsLayers{
			UserAllow: dto.RoleSettings{PermissionVector: []string{dto.PermissionTagRO}},
		},
	}
	application := NewRoleApplication(Deps{
		Registry: func() PermissionRegistryPort { return &permissionRegistryFake{} },
		Layers:   func() PermissionLayerPort { return layers },
	})

	vector, err := application.RolePermissionVector("session-a", string(dto.RoleAgent))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(vector, []string{dto.PermissionTagRO}) {
		t.Fatalf("fallback vector = %#v", vector)
	}
}

func TestRoleApplicationRollsBackRuntimeAccountOnPersistenceFailure(t *testing.T) {
	persistErr := errors.New("persist failed")
	defaults := &roleDefaultsFake{setAccountErr: persistErr}
	runtime := &runtimeAccountFake{}
	application := NewRoleApplication(Deps{
		RoleDefaults: func() RoleDefaultsPort { return defaults },
		Runtime:      func() contract.AccountRoleSelectionPort { return runtime },
	})

	err := application.SetRoleAccount("session-a", string(dto.RoleAgent), "primary")
	if !errors.Is(err, persistErr) {
		t.Fatalf("SetRoleAccount error = %v, want %v", err, persistErr)
	}
	want := []string{"primary", ""}
	if !reflect.DeepEqual(runtime.calls, want) {
		t.Fatalf("runtime calls = %#v, want %#v", runtime.calls, want)
	}
}

func TestRoleApplicationInheritanceAndRemoteUserSpaces(t *testing.T) {
	defaults := &roleDefaultsFake{
		agentPaths: []string{"/home/main"},
	}
	application := NewRoleApplication(Deps{
		RoleDefaults: func() RoleDefaultsPort { return defaults },
	})

	paths, err := application.RoleSSHUserSpaces("session-a", string(dto.RoleSubAgent))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(paths, []string{"/home/main"}) {
		t.Fatalf("inherited paths = %#v", paths)
	}

	err = application.SetRoleSSHUserSpaces("session-a", string(dto.RoleAgent), []string{"/srv/app/../app", "/srv/app"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(defaults.lastPaths, []string{"/srv/app"}) {
		t.Fatalf("normalized paths = %#v", defaults.lastPaths)
	}
}

func TestRoleApplicationFailsLoudlyWithoutRegistry(t *testing.T) {
	application := NewRoleApplication(Deps{})
	if _, err := application.RestoreRolePermissions("session-a", []string{"root"}); !errors.Is(err, ErrRolePermissionsUnavailable) {
		t.Fatalf("RestoreRolePermissions error = %v", err)
	}
}

type roleDefaultsFake struct {
	vectors       map[string][]string
	accounts      map[string]string
	inherit       map[string]bool
	agentPaths    []string
	lastPaths     []string
	setAccountErr error
}

func (fake *roleDefaultsFake) RoleDefault(string, string) ([]string, bool, error) {
	return nil, false, nil
}

func (fake *roleDefaultsFake) SetRoleDefault(string, string, []string) error {
	return nil
}

func (fake *roleDefaultsFake) RoleAccount(sessionID, role string) (string, bool, error) {
	value, ok := fake.accounts[sessionID+"\x00"+role]
	return value, ok, nil
}

func (fake *roleDefaultsFake) SetRoleAccount(sessionID, role, account string) error {
	if fake.setAccountErr != nil {
		return fake.setAccountErr
	}
	if fake.accounts == nil {
		fake.accounts = make(map[string]string)
	}
	fake.accounts[sessionID+"\x00"+role] = account
	return nil
}

func (fake *roleDefaultsFake) RoleInheritance(sessionID, role string) (bool, bool, error) {
	value, ok := fake.inherit[sessionID+"\x00"+role]
	return value, ok, nil
}

func (fake *roleDefaultsFake) SetRoleInheritance(string, string, bool) error {
	return nil
}

func (fake *roleDefaultsFake) RoleSSHUserSpaces(_, role string) ([]string, error) {
	if role == string(dto.RoleAgent) {
		return append([]string(nil), fake.agentPaths...), nil
	}
	return nil, nil
}

func (fake *roleDefaultsFake) RoleDefaultSSHUserSpaces(string, string) ([]string, error) {
	return nil, nil
}

func (fake *roleDefaultsFake) SetRoleSSHUserSpaces(_, _ string, paths []string) error {
	fake.lastPaths = append([]string(nil), paths...)
	return nil
}

func (fake *roleDefaultsFake) SetRoleSessionSSHUserSpaces(_, _ string, paths []string) error {
	fake.lastPaths = append([]string(nil), paths...)
	return nil
}

type permissionRegistryFake struct {
	vectors map[string][]string
}

func (fake *permissionRegistryFake) RolePermissionVectors(string) (map[string][]string, error) {
	out := make(map[string][]string, len(fake.vectors))
	for role, vector := range fake.vectors {
		out[role] = append([]string(nil), vector...)
	}
	return out, nil
}

func (fake *permissionRegistryFake) RolePermissionVector(_ string, role string) ([]string, bool, error) {
	vector, ok := fake.vectors[role]
	return append([]string(nil), vector...), ok, nil
}

func (fake *permissionRegistryFake) SetRolePermissionVector(string, string, []string) error {
	return nil
}

type permissionLayersFake struct {
	layers dto.RoleSettingsLayers
}

func (fake permissionLayersFake) RolePermissionLayers(string, string) dto.RoleSettingsLayers {
	return fake.layers
}

type runtimeAccountFake struct {
	calls []string
}

func (fake *runtimeAccountFake) SetRoleAccount(role, account string) error {
	fake.calls = append(fake.calls, account)
	return nil
}
