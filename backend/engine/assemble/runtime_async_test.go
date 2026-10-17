package assemble

import "testing"

// 字段搬运那一列（"每一列都真的到了 Application 侧"）随实现搬进作业域
// （`backend/tool/jobs/projection_test.go`，P21-10）；本文件只留**宿主装配语义**的用例：
// 投影口在 Runtime 还没装配出工具路由（或整个 Runtime 为空）时必须回"没有行/没有信号"。

// Runtime 还没装配出工具路由（或整个 Runtime 为空）时，投影口必须回"没有行/没有信号"，
// 不得 panic——core 每次重投影都会调它。
func TestAsyncPortsAreNilSafeBeforeAssembly(t *testing.T) {
	var nilRuntime *Runtime
	if got := nilRuntime.AsyncRunsSnapshot(); got != nil {
		t.Fatalf("空 Runtime 的快照 = %+v", got)
	}
	if got := nilRuntime.AsyncRunEvents(); got != nil {
		t.Fatalf("空 Runtime 的信号口 = %v", got)
	}
	partial := &Runtime{}
	if got := partial.AsyncRunsSnapshot(); got != nil {
		t.Fatalf("未装配工具路由的快照 = %+v", got)
	}
	if got := partial.AsyncRunEvents(); got != nil {
		t.Fatalf("未装配工具路由的信号口 = %v", got)
	}
}
