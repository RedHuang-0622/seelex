package agentteam

import (
	"errors"
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// dismiss_test.go — "团队离场"（干完就走人）的验收。
//
// 口径与 Dismiss 的边界一一对应：
//   - 离场 = 删注册表 + 复位顺序（读面据此报"本会话没有团队"，而不是"有团队但没成员"）；
//   - 角色会话子树不动（装配幂等键不变，再次召唤复用同一棵子树）；
//   - 宿主没有离场面时**显式报错**，不静默成功（否则调用方以为人走了，面板里还挂着）。

// RemoveTeamRegistry 让工厂桩具备离场面（DismissPort）。
func (port *fakePort) RemoveTeamRegistry(string) error {
	port.registry = dto.TeamRegistry{}
	port.policy = ""
	port.order = nil
	return nil
}

// barePort 只实现 Port（**不**带离场面），用于钉住"没有离场面必须显式报错"。
type barePort struct {
	registry dto.TeamRegistry
	policy   string
	order    []string
}

func (port *barePort) EnsureRoleSession(string, string, string, uint64) (bool, error) {
	return true, nil
}

func (port *barePort) ReadLifecycleOrder(string) (string, []string, error) {
	return port.policy, append([]string(nil), port.order...), nil
}

func (port *barePort) SetLifecycleOrder(_ string, policy string, roles []string) error {
	port.policy = policy
	port.order = append([]string(nil), roles...)
	return nil
}

func (port *barePort) ReadTeamRegistry(string) (dto.TeamRegistry, error) {
	return port.registry, nil
}

func (port *barePort) WriteTeamRegistry(_ string, registry dto.TeamRegistry) error {
	port.registry = registry
	return nil
}

func TestDismissClearsRegistryAndOrder(t *testing.T) {
	port := newFakePort()
	factory, err := NewFactory(port)
	if err != nil {
		t.Fatalf("NewFactory: %v", err)
	}
	spec := testGoalSpec()
	if _, err := factory.Materialize("sess-1", spec, 0); err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if !port.registry.Configured || len(port.order) == 0 {
		t.Fatalf("装配后应有注册表与顺序：registry=%+v order=%v", port.registry, port.order)
	}

	if err := factory.Dismiss("sess-1"); err != nil {
		t.Fatalf("Dismiss: %v", err)
	}
	if port.registry.Configured || len(port.registry.Roles) != 0 {
		t.Fatalf("离场必须清空注册表（读面据此报未配置）：%+v", port.registry)
	}
	if port.policy != "" || len(port.order) != 0 {
		t.Fatalf("离场必须复位顺序（否则「顺序里挂着未注册角色」会常驻）：policy=%q order=%v", port.policy, port.order)
	}
	// 角色会话子树不受影响：再次召唤复用同一棵（幂等键 =(team_id, role_name)）。
	if len(port.sessions) == 0 {
		t.Fatal("离场不该删掉角色会话子树")
	}
	if _, err := factory.Materialize("sess-1", spec, 0); err != nil {
		t.Fatalf("离场后再次装配：%v", err)
	}
}

func TestDismissWithoutPortFailsExplicitly(t *testing.T) {
	factory, err := NewFactory(&barePort{})
	if err != nil {
		t.Fatalf("NewFactory: %v", err)
	}
	if err := factory.Dismiss("sess-1"); !errors.Is(err, ErrDismissUnsupported) {
		t.Fatalf("没有离场面的宿主必须显式报 ErrDismissUnsupported，got %v", err)
	}
}

func TestDismissRejectsEmptySession(t *testing.T) {
	factory, err := NewFactory(newFakePort())
	if err != nil {
		t.Fatalf("NewFactory: %v", err)
	}
	if err := factory.Dismiss("  "); err == nil {
		t.Fatal("空会话号必须显式报错")
	}
}
