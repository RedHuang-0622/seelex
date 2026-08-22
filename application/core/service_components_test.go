package core

import (
	"reflect"
	"testing"

	"github.com/RedHuang-0622/seelex/application/core/context_runtime"
	"github.com/RedHuang-0622/seelex/application/core/prompt_layer"
	"github.com/RedHuang-0622/seelex/application/core/task_context"
	"github.com/RedHuang-0622/seelex/application/core/view_state"
)

func TestNewAssemblesFocusedServiceComponents(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	defer service.Shutdown()

	if service.serviceState == nil {
		t.Fatal("service state was not assembled")
	}
	if service.components.prompts == nil || service.components.context == nil || service.components.history == nil || service.components.sessions == nil || service.components.tasks == nil || service.components.view == nil || service.components.input == nil {
		t.Fatalf("incomplete service component graph: %+v", service.components)
	}
	if service.components.prompts.Core != service.Core ||
		service.components.context.Core != service.Core ||
		service.components.history.Core != service.Core ||
		service.components.sessions.Core != service.Core ||
		service.components.tasks.Core != service.Core ||
		service.components.view.Core != service.Core {
		t.Fatal("service components do not share the assembled state")
	}
	ports := service.components.context.Ports()
	if ports.Prompts != service.components.prompts ||
		ports.Sessions != service.components.sessions ||
		ports.Tasks != service.components.tasks ||
		ports.History != service.components.history ||
		ports.View != service.components.view {
		t.Fatal("context component was not wired through focused collaborators")
	}
}

func TestServiceFacadeContainsOnlyAssembly(t *testing.T) {
	serviceType := reflect.TypeOf(Service{})
	if serviceType.NumField() != 2 {
		t.Fatalf("Service has %d fields, want only state and components", serviceType.NumField())
	}
	if serviceType.Field(0).Type != reflect.TypeOf((*serviceState)(nil)) {
		t.Fatalf("first Service field = %s, want *serviceState", serviceType.Field(0).Type)
	}
	if serviceType.Field(1).Type != reflect.TypeOf(serviceComponents{}) {
		t.Fatalf("second Service field = %s, want serviceComponents", serviceType.Field(1).Type)
	}
}

func TestFocusedComponentsDoNotHoldServiceFacade(t *testing.T) {
	serviceType := reflect.TypeOf((*Service)(nil))
	componentTypes := []reflect.Type{
		reflect.TypeOf(prompt_layer.Coordinator{}),
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
