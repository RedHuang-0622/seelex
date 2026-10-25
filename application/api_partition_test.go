package application_test

import (
	"reflect"
	"sort"
	"testing"

	"github.com/RedHuang-0622/seelex/application"
)

// smallApplicationAPIs 是 AP-002 的能力面清单。新增能力面必须显式登记，
// 避免又长回一个完整 Service 接口。
var smallApplicationAPIs = []reflect.Type{
	reflect.TypeOf((*application.ConversationAPI)(nil)).Elem(),
	reflect.TypeOf((*application.SessionAPI)(nil)).Elem(),
	reflect.TypeOf((*application.ExecutionQueryAPI)(nil)).Elem(),
	reflect.TypeOf((*application.ExecutionCommandAPI)(nil)).Elem(),
	reflect.TypeOf((*application.WorkspaceAPI)(nil)).Elem(),
	reflect.TypeOf((*application.GoalAPI)(nil)).Elem(),
	reflect.TypeOf((*application.PolicyAPI)(nil)).Elem(),
	reflect.TypeOf((*application.AdminAPI)(nil)).Elem(),
	reflect.TypeOf((*application.RoleAPI)(nil)).Elem(),
	reflect.TypeOf((*application.SubagentAPI)(nil)).Elem(),
	reflect.TypeOf((*application.StateAPI)(nil)).Elem(),
	reflect.TypeOf((*application.QueueAPI)(nil)).Elem(),
	reflect.TypeOf((*application.LifecycleAPI)(nil)).Elem(),
	reflect.TypeOf((*application.WorkItemQueryAPI)(nil)).Elem(),
	reflect.TypeOf((*application.TeammateQueryAPI)(nil)).Elem(),
	reflect.TypeOf((*application.TeamworkQueryAPI)(nil)).Elem(),
	reflect.TypeOf((*application.SubagentQueryAPI)(nil)).Elem(),
	reflect.TypeOf((*application.TranscriptQueryAPI)(nil)).Elem(),
}

func TestSmallApplicationAPIsStayNarrow(t *testing.T) {
	for _, api := range smallApplicationAPIs {
		if count := api.NumMethod(); count > 20 {
			t.Fatalf("%s 有 %d 个方法；单个入站 API 上限为 20", api.Name(), count)
		}
	}
}

func TestSmallApplicationAPIsCoverCompatibilityFaces(t *testing.T) {
	covered := map[string]bool{}
	for _, api := range smallApplicationAPIs {
		for index := 0; index < api.NumMethod(); index++ {
			covered[api.Method(index).Name] = true
		}
	}
	compatibilityFaces := []reflect.Type{
		reflect.TypeOf((*application.ViewFace)(nil)).Elem(),
		reflect.TypeOf((*application.CommandFace)(nil)).Elem(),
		reflect.TypeOf((*application.PolicyFace)(nil)).Elem(),
		reflect.TypeOf((*application.QueueFace)(nil)).Elem(),
	}
	var missing []string
	for _, face := range compatibilityFaces {
		for index := 0; index < face.NumMethod(); index++ {
			name := face.Method(index).Name
			if !covered[name] {
				missing = append(missing, face.Name()+"."+name)
			}
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("能力小接口未完整覆盖旧兼容面：%v", missing)
	}
}

func TestExecutionQueryAPIIsCoveredByReadOnlyCapabilitySlices(t *testing.T) {
	capabilities := []reflect.Type{
		reflect.TypeOf((*application.WorkItemQueryAPI)(nil)).Elem(),
		reflect.TypeOf((*application.TeammateQueryAPI)(nil)).Elem(),
		reflect.TypeOf((*application.TeamworkQueryAPI)(nil)).Elem(),
		reflect.TypeOf((*application.SubagentQueryAPI)(nil)).Elem(),
		reflect.TypeOf((*application.TranscriptQueryAPI)(nil)).Elem(),
	}
	covered := map[string]bool{}
	for _, capability := range capabilities {
		if capability.NumMethod() > 4 {
			t.Fatalf("%s 有 %d 个方法；执行查询切片上限为 4", capability.Name(), capability.NumMethod())
		}
		for index := 0; index < capability.NumMethod(); index++ {
			name := capability.Method(index).Name
			if covered[name] {
				t.Fatalf("执行查询方法 %s 同时落在多个能力切片中", name)
			}
			covered[name] = true
		}
	}
	combined := reflect.TypeOf((*application.ExecutionQueryAPI)(nil)).Elem()
	var missing []string
	for index := 0; index < combined.NumMethod(); index++ {
		name := combined.Method(index).Name
		if !covered[name] {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("ExecutionQueryAPI 仍有未切分的方法：%v", missing)
	}
}
