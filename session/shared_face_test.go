package session

import (
	"reflect"
	"testing"
)

// TestGlobalSharedFaceBounded（T5.5 静态断言）：G=(V,registry,bus,services)
// 之外无共享可变状态——会话注册表与 V 已收进 actor（Domain 只有 channel
// 字段，无 mutex/共享可变容器）；每会话状态只属自身。
func TestGlobalSharedFaceBounded(t *testing.T) {
	typ := reflect.TypeOf(Domain{})
	got := make(map[string]reflect.Type, typ.NumField())
	for index := 0; index < typ.NumField(); index++ {
		field := typ.Field(index)
		got[field.Name] = field.Type
	}
	want := map[string]reflect.Type{
		"cmds":   reflect.TypeOf(make(chan domainCmd, 1)),
		"stopCh": reflect.TypeOf(make(chan struct{}, 1)),
		"done":   reflect.TypeOf(make(chan struct{}, 1)),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Domain 共享面字段 = %v, want %v（G 之外不得有共享可变状态）", got, want)
	}
}
