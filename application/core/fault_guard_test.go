package core

import (
	applicationlifecycle "github.com/RedHuang-0622/seelex/application/lifecycle"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"
)

func TestFaultGuardApplicationIsAssembled(t *testing.T) {
	components := reflect.TypeOf(serviceComponents{})
	field, ok := components.FieldByName("faultGuardApplication")
	if !ok {
		t.Fatal("serviceComponents 未装配 faultGuardApplication")
	}
	if field.Type != reflect.TypeOf((*applicationlifecycle.FaultGuardApplication)(nil)) {
		t.Fatalf("faultGuardApplication 字段类型 = %s", field.Type)
	}
	if _, exists := reflect.TypeOf(serviceState{}).FieldByName("degradeOnce"); exists {
		t.Fatal("degradeOnce 应归 faultGuardApplication，不再属于 serviceState")
	}
}

func TestFaultGuardFacadeOnlyDelegatesToApplication(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法定位测试源码")
	}
	path := filepath.Join(filepath.Dir(file), "fault_guard.go")
	parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"degradeAndRequestExit": false,
		"handleLifecycleFault":  false,
	}
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Recv == nil || function.Name == nil || !functionHasReceiver(function, "Service") {
			continue
		}
		if _, tracked := want[function.Name.Name]; !tracked {
			continue
		}
		want[function.Name.Name] = true
		if len(function.Body.List) != 1 {
			t.Fatalf("%s 有 %d 条语句，want 一行 faultGuardApplication 转发", function.Name.Name, len(function.Body.List))
		}
	}
	for name, found := range want {
		if found {
			t.Fatalf("core %s forwarder must be removed", name)
		}
	}
}

// TestDegradeAndRequestExitNotifiesHost：降级退出必须幂等、标记 closed/
// draining，并向订阅者发布进程级退出事件（host 据此关窗/退出）。
func TestDegradeAndRequestExitNotifiesHost(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	defer frontendOfTestService(service).Shutdown()
	sub := frontendOfTestService(service).Subscribe(16)
	defer sub.Close()

	service.components.faultGuardApplication.DegradeAndRequestExit("probe")
	service.components.faultGuardApplication.DegradeAndRequestExit("probe again") // 幂等

	service.ViewMu.RLock()
	closed := service.closed
	draining := service.draining
	service.ViewMu.RUnlock()
	if !closed || !draining {
		t.Fatalf("degraded state = closed:%v draining:%v, want both true", closed, draining)
	}
	deadline := time.After(2 * time.Second)
	for {
		select {
		case event := <-sub.Events:
			if event.Kind == EventExitRequested {
				return
			}
		case <-deadline:
			t.Fatal("host never received EventExitRequested after degrade")
		}
	}
}

// TestLifecycleFaultPanicsAfterDegrade：生命周期消费者中的故障不再被静默
// 吞掉——先降级退出，再重新抛出 panic（fail-fast），宿主进程及时退出而
// 不是带着半坏状态继续运行。
func TestLifecycleFaultPanicsAfterDegrade(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	defer frontendOfTestService(service).Shutdown()

	var panicked any
	func() {
		defer func() { panicked = recover() }()
		service.safeLifecycleCall(func() { panic("concurrency-fault-probe") })
	}()
	if panicked == nil || panicked != "concurrency-fault-probe" {
		t.Fatalf("panic not rethrown after degrade: %v", panicked)
	}
	service.ViewMu.RLock()
	closed := service.closed
	service.ViewMu.RUnlock()
	if !closed {
		t.Fatal("service did not enter degraded state before panic")
	}
}
