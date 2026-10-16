package session

import (
	"strings"
	"testing"

	frameworkSession "github.com/RedHuang-0622/Seele/session"
)

// P21-5 域级用例：工厂的装配缺口与入参判据（端到端建会话仍由宿主用例
// `backend/engine/assemble/runtime_subagent_session_test.go` 经薄转发覆盖，一行未改）。

func TestSessionFactoryRequiresComponentsAndSessionID(t *testing.T) {
	var nilFactory *SessionFactory
	if _, err := nilFactory.NewWithID("node-1", nil); err == nil ||
		!strings.Contains(err.Error(), "runtime is nil") {
		t.Fatalf("nil 工厂应报装配缺口：%v", err)
	}
	factory := NewSessionFactory(SessionFactoryDeps{
		Components: func() frameworkSession.SessionComponents { return frameworkSession.SessionComponents{} },
	})
	if _, err := factory.NewWithID("   ", nil); err == nil ||
		!strings.Contains(err.Error(), "session ID is required") {
		t.Fatalf("空会话 ID 必须报错：%v", err)
	}
}

func TestSessionFactoryCountToleratesMissingRegistry(t *testing.T) {
	var nilFactory *SessionFactory
	if got := nilFactory.Count(); got != 0 {
		t.Fatalf("nil 工厂计数 = %d, want 0", got)
	}
	factory := NewSessionFactory(SessionFactoryDeps{})
	if got := factory.Count(); got != 0 {
		t.Fatalf("未装配注册表计数 = %d, want 0", got)
	}
}
