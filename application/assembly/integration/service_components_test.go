package integration

import (
	assembly "github.com/RedHuang-0622/seelex/application/assembly"
	applicationprompt "github.com/RedHuang-0622/seelex/application/prompt"
	"github.com/RedHuang-0622/seelex/application/session/viewstate"
	"github.com/RedHuang-0622/seelex/kernel/context"
	"github.com/RedHuang-0622/seelex/kernel/task"
	"reflect"
	"testing"
)

func TestNewAssemblesFocusedServiceComponents(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	owners := assembly.ComponentsFor(service)
	defer owners.Runtime.Lifecycle.Shutdown()

	if owners.Conversation.Prompts == nil || owners.Conversation.Context == nil ||
		owners.Conversation.ContextHistory == nil || owners.Session.Runtime == nil ||
		owners.Execution.Tasks == nil || owners.Session.View == nil ||
		owners.Conversation.Submission == nil {
		t.Fatalf("incomplete service component graph: %+v", owners)
	}
	if owners.Conversation.Prompts.Core != service.Core ||
		owners.Conversation.Context.Core != service.Core ||
		owners.Conversation.ContextHistory.Core != service.Core ||
		owners.Session.Runtime.Core != service.Core ||
		owners.Execution.Tasks.Core != service.Core ||
		owners.Session.View.Core != service.Core {
		t.Fatal("service components do not share the assembled state")
	}
	ports := owners.Conversation.Context.Ports()
	if ports.Prompts != owners.Conversation.Prompts ||
		ports.Sessions != owners.Session.Runtime ||
		ports.Tasks != owners.Execution.Tasks ||
		ports.History != owners.Conversation.ContextHistory ||
		ports.View != owners.Session.View {
		t.Fatal("context component was not wired through focused collaborators")
	}
}

// TestServiceFacadeContainsOnlyAssembly 钉住 Service 只由「装配状态 + 组件图」
// 两个字段组成：状态与 owner 图之外不得再长出直挂字段。
func TestServiceFacadeContainsOnlyAssembly(t *testing.T) {
	serviceType := reflect.TypeOf(assembly.Service{})
	if serviceType.NumField() != 2 {
		t.Fatalf("Service has %d fields, want only state and components", serviceType.NumField())
	}
	stateType := serviceType.Field(0).Type
	if stateType.Kind() != reflect.Pointer || stateType.Elem().Name() != "serviceState" {
		t.Fatalf("first Service field = %s, want *serviceState", stateType)
	}
	if got := serviceType.Field(1).Type.Name(); got != "serviceComponents" {
		t.Fatalf("second Service field type = %s, want serviceComponents", got)
	}
}

func TestFocusedComponentsDoNotHoldServiceFacade(t *testing.T) {
	serviceType := reflect.TypeOf((*assembly.Service)(nil))
	componentTypes := []reflect.Type{
		reflect.TypeOf(applicationprompt.Coordinator{}),
		reflect.TypeOf(context_runtime.Coordinator{}),
		reflect.TypeOf(context_runtime.HistoryCoordinator{}),
		reflect.TypeOf(task_context.Coordinator{}),
		reflect.TypeOf(view_state.Coordinator{}),
	}
	for _, componentType := range componentTypes {
		for index := 0; index < componentType.NumField(); index++ {
			if componentType.Field(index).Type == serviceType {
				t.Fatalf("%s field %q holds the Service facade", componentType.Name(), componentType.Field(index).Name)
			}
		}
	}
}

func TestComponentsForReturnsCompleteOwnerGraph(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	owners := assembly.ComponentsFor(service)
	defer owners.Runtime.Lifecycle.Shutdown()

	assertNonNilOwnerGraph(t, reflect.ValueOf(owners), "Components")

	stateType := reflect.TypeOf(assembly.Service{}).Field(0).Type.Elem()
	for _, retired := range []string{"editForkOps", "degradeOnce", "reviewSeq"} {
		if _, exists := stateType.FieldByName(retired); exists {
			t.Fatalf("serviceState 仍持有已归 owner 的字段 %s", retired)
		}
	}
}

func assertNonNilOwnerGraph(t *testing.T, value reflect.Value, path string) {
	t.Helper()
	if !value.IsValid() {
		t.Fatalf("%s is invalid", path)
	}
	switch value.Kind() {
	case reflect.Interface, reflect.Pointer:
		if value.IsNil() {
			t.Fatalf("%s is nil", path)
		}
	case reflect.Struct:
		valueType := value.Type()
		for index := 0; index < value.NumField(); index++ {
			field := valueType.Field(index)
			if field.PkgPath != "" {
				continue
			}
			if field.Type.Kind() == reflect.Func {
				t.Fatalf("%s.%s is a function; Components must contain owners only", path, field.Name)
			}
			assertNonNilOwnerGraph(t, value.Field(index), path+"."+field.Name)
		}
	default:
		t.Fatalf("%s has unsupported kind %s", path, value.Kind())
	}
}
