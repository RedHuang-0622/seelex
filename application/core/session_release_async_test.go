package core

// 会话销毁即杀后台命令：core 这一道的转发必须有断言用例，不能只靠编译期——
// `contract.RuntimePort` 加了方法只保证"实现了"，不保证"调用点了"。
// 真正被杀的语义在 tools 层测（TestCloseSessionAsyncKillsOnlyOwnSession）；
// 这里只钉"删会话时 core 确实转了这一次"。

import (
	"slices"
	"testing"
)

func TestDeleteSessionReleasesBackgroundRuns(t *testing.T) {
	runtime := &fakeRuntime{}
	service := newTestService(t, &fakeEngine{sessionID: "session-a"}, withTestRuntime(runtime))
	defer service.Shutdown()

	if err := service.DeleteSession("session-gone"); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(runtime.releasedAsyncSessions, "session-gone") {
		t.Fatalf("删会话没转 ReleaseSessionAsync（后台命令会变成无人认领的孤儿进程）: %v",
			runtime.releasedAsyncSessions)
	}
}
