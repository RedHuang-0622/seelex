package assembly

import (
	"github.com/RedHuang-0622/seelex/backend/engine/input"
	"testing"
)

func TestNewAssemblesInputRouter(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	defer service.components.lifecycleApplication.Shutdown()
	if _, ok := service.components.input.(*input_router.Router); !ok {
		t.Fatalf("input dispatcher = %T, want *input_router.Router", service.components.input)
	}
}
