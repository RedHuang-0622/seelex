package assembly

import (
	"testing"
)

// BenchmarkSessionRecoveryPath 对照应用层两条恢复路径：
//   - hot：目标会话已驻留，只重指视图与会话 scope；
//   - cold：重建会话单元并完成 record/transcript 读取、引擎恢复与快照发布。
//
// 存储 I/O 的绝对成本由 store.BenchmarkSessionColdLoad 单独量化；本基准固定
// 使用同一个内存 store 夹具，隔离应用层状态机开销。
func BenchmarkSessionRecoveryPath(b *testing.B) {
	const sessionID = "recovery-bench-session"

	b.Run("hot", func(b *testing.B) {
		store := newPagedSessionStore(sessionID, 316)
		service := newTestService(b, &fakeEngine{}, withTestSessions(store))
		if err := service.components.sessionResume.Resume(sessionID); err != nil {
			b.Fatal(err)
		}
		b.ReportAllocs()
		b.ResetTimer()
		for index := 0; index < b.N; index++ {
			if err := service.components.sessionResume.Resume(sessionID); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("cold", func(b *testing.B) {
		b.ReportAllocs()
		for index := 0; index < b.N; index++ {
			b.StopTimer()
			store := newPagedSessionStore(sessionID, 316)
			service := newTestService(b, &fakeEngine{}, withTestSessions(store))
			b.StartTimer()
			if err := service.components.sessionResume.Resume(sessionID); err != nil {
				b.Fatal(err)
			}
			b.StopTimer()
			service.components.lifecycleApplication.Shutdown()
			b.StartTimer()
		}
	})
}
