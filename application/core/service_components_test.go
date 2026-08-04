package core

import (
	"reflect"
	"testing"
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
	if service.components.prompts.state != service.serviceState ||
		service.components.context.serviceState != service.serviceState ||
		service.components.history.serviceState != service.serviceState ||
		service.components.sessions.serviceState != service.serviceState ||
		service.components.tasks.serviceState != service.serviceState ||
		service.components.view.serviceState != service.serviceState {
		t.Fatal("service components do not share the assembled state")
	}
	if service.components.context.collaborators.prompts != service.components.prompts ||
		service.components.context.collaborators.sessions != service.components.sessions ||
		service.components.context.collaborators.tasks != service.components.tasks ||
		service.components.context.collaborators.history != service.components.history ||
		service.components.context.collaborators.view != service.components.view {
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
		reflect.TypeOf(promptCoordinator{}),
		reflect.TypeOf(contextCoordinator{}),
		reflect.TypeOf(historySafetyCoordinator{}),
		reflect.TypeOf(sessionCoordinator{}),
		reflect.TypeOf(taskContextCoordinator{}),
		reflect.TypeOf(viewCoordinator{}),
	}
	for _, componentType := range componentTypes {
		for index := 0; index < componentType.NumField(); index++ {
			if componentType.Field(index).Type == serviceType {
				t.Fatalf("%s field %q holds the Service facade", componentType.Name(), componentType.Field(index).Name)
			}
		}
	}
}
