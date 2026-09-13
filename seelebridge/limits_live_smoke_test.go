//go:build limitslive

// seelex × 本地 Seele（limits 限流/并发装配）真实 API 联调冒烟（opt-in）。
//
// 它验证的是"装配形状"而不只是包内单测：
//   - seelex 自己的账号配置加载（config.LoadTolerant）与账号构造（account.ClientFor）；
//   - Seele 的 limits.Assembly 逐账号装饰 agent.Completer；
//   - accountpool 租约 + limits 准入两层同时生效；
//   - 真实 provider 调用与真实 usage 结算；
//   - 运行时 Set 方法在装配后仍然生效；
//   - 边界：账号租约占满 + QueueTimeout=0 → 立即拒绝（零 API 成本）。
//
// 前提（默认构建不受影响，见文件头的 3 条命令）：
//
//	go mod edit -replace=github.com/RedHuang-0622/Seele=G:/Program/go/Seele
//	$env:SEELEX_SMOKE_ACCOUNTS='G:\Program\go\seelex\config\accounts.yaml'
//	go test -tags limitslive ./seelebridge/ -run TestSeelexLimitsLiveSmoke -count=1 -v
//
// 联调结束后复原：go mod edit -dropreplace=github.com/RedHuang-0622/Seele
package seelebridge

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RedHuang-0622/Seele/accountpool"
	"github.com/RedHuang-0622/Seele/agent"
	seelimits "github.com/RedHuang-0622/Seele/limits"
	"github.com/RedHuang-0622/Seele/types"

	"github.com/RedHuang-0622/seelex/seelebridge/account"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/config"
)

func TestSeelexLimitsLiveSmoke(t *testing.T) {
	accountsPath := strings.TrimSpace(os.Getenv("SEELEX_SMOKE_ACCOUNTS"))
	if accountsPath == "" {
		t.Skip("设置 SEELEX_SMOKE_ACCOUNTS 指向 accounts.yaml 才能运行真实 API 联调冒烟")
	}

	loaded, err := config.LoadTolerant(accountsPath)
	if err != nil {
		t.Fatalf("加载账号配置: %v", err)
	}
	if len(loaded.Specs) == 0 {
		t.Fatal("账号配置为空")
	}

	params := seelimits.DefaultParams()
	params.MaxConcurrency = 1
	params.RequestsPerMin = 60
	params.TokensPerMin = 60000
	params.QueueTimeout = seelimits.Duration(30 * time.Second)
	params.Retry = seelimits.RetryPolicy{MaxAttempts: 1}
	params.PerKey = true

	var observed int64
	assembly, err := seelimits.Assemble(params, seelimits.WithObserver(func(seelimits.Observation) {
		observed++
	}))
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}

	// 装配形状：逐账号用 limits 装饰 seelex 自己的 Completer，再进 accountpool。
	pool := accountpool.New[agent.Completer]()
	for _, spec := range loaded.Specs {
		wrapped, err := assembly.WrapComplete(spec.Name, account.ClientFor(spec))
		if err != nil {
			t.Fatalf("包装账号 %q: %v", spec.Name, err)
		}
		capacity := spec.MaxConcurrency
		if capacity <= 0 {
			capacity = 1
		}
		if err := pool.Register(accountpool.Account[agent.Completer]{
			ID:             spec.Name,
			Value:          wrapped,
			MaxConcurrency: capacity,
			Metadata: map[string]string{
				"provider": spec.Provider,
				"model":    spec.Model,
			},
		}); err != nil {
			t.Fatalf("注册账号 %q: %v", spec.Name, err)
		}
	}

	spec := loaded.Specs[0]
	t.Logf("联调账号: name=%s model=%s provider=%s（凭据不打印）", spec.Name, spec.Model, spec.Provider)

	t.Run("并发真实调用走两层装配", func(t *testing.T) {
		const workers = 2
		var waitGroup sync.WaitGroup
		replies := make([]string, workers)
		failures := make([]error, workers)

		for i := 0; i < workers; i++ {
			waitGroup.Add(1)
			go func(index int) {
				defer waitGroup.Done()
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
				defer cancel()

				lease, err := pool.Acquire(ctx, accountpool.AcquireRequest{AccountID: spec.Name})
				if err != nil {
					failures[index] = fmt.Errorf("accountpool 租约失败: %w", err)
					return
				}
				defer lease.Release()

				content := "只回复 pong 两个字母，不要调用任何工具。"
				message, err := lease.Client().Complete(ctx, []types.Message{{Role: "user", Content: &content}}, nil)
				if err != nil {
					failures[index] = err
					return
				}
				if message.Content != nil {
					replies[index] = strings.TrimSpace(*message.Content)
				}
			}(i)
		}
		waitGroup.Wait()

		for index, failure := range failures {
			if failure != nil {
				t.Fatalf("第 %d 个并发请求失败: %v", index, failure)
			}
		}
		for index, reply := range replies {
			if reply == "" {
				t.Fatalf("第 %d 个并发请求返回空内容", index)
			}
			t.Logf("第 %d 个并发回复: %q", index, reply)
		}

		stats := assembly.Snapshot().Stats[spec.Name]
		t.Logf("装配统计: admitted=%d completed=%d in_flight=%d peak=%d estimated=%d actual=%d queue_wait=%s",
			stats.Admitted, stats.Completed, stats.InFlight, stats.PeakInFlight, stats.Estimated, stats.Actual, stats.QueueWait)
		if stats.Admitted != workers || stats.Completed != workers {
			t.Errorf("Admitted/Completed = %d/%d, want %d", stats.Admitted, stats.Completed, workers)
		}
		if stats.InFlight != 0 {
			t.Errorf("InFlight = %d, want 0", stats.InFlight)
		}
		if stats.PeakInFlight > 1 {
			t.Errorf("PeakInFlight = %d, 超过 MaxConcurrency=1", stats.PeakInFlight)
		}
		if stats.Actual <= 0 {
			t.Errorf("Actual = %d, 真实 usage 未结算", stats.Actual)
		}
		if observed == 0 {
			t.Error("观察者没有收到准入事件")
		}
	})

	t.Run("运行时Set方法在装配后生效", func(t *testing.T) {
		if err := assembly.SetConcurrency(3); err != nil {
			t.Fatalf("SetConcurrency: %v", err)
		}
		if err := assembly.SetRate(120, 120000); err != nil {
			t.Fatalf("SetRate: %v", err)
		}
		if err := assembly.SetQueueTimeout(10 * time.Second); err != nil {
			t.Fatalf("SetQueueTimeout: %v", err)
		}
		if err := assembly.SetImageWeight(0.5); err != nil {
			t.Fatalf("SetImageWeight: %v", err)
		}
		if err := assembly.SetRetry(seelimits.RetryPolicy{MaxAttempts: 2, BaseDelay: seelimits.Duration(time.Second)}); err != nil {
			t.Fatalf("SetRetry: %v", err)
		}

		global := assembly.Snapshot().Stats[spec.Name]
		if global.MaxConcurrency != 3 || global.RequestsPerMin != 120 {
			t.Fatalf("全局 Set 未生效: %+v", global)
		}
		if params := assembly.Params(); params.QueueTimeout.Duration() != 10*time.Second || params.Retry.MaxAttempts != 2 {
			t.Fatalf("全局参数未生效: %+v", params)
		}

		// 单账号覆盖：只影响该 key。
		override := seelimits.DefaultParams().WithConcurrency(5).WithRate(240, 240000)
		if err := assembly.SetKeyParams(spec.Name, override); err != nil {
			t.Fatalf("SetKeyParams: %v", err)
		}
		keyStats := assembly.Snapshot().Stats[spec.Name]
		if keyStats.MaxConcurrency != 5 || keyStats.RequestsPerMin != 240 {
			t.Fatalf("单账号覆盖未生效: %+v", keyStats)
		}

		if err := assembly.SetConcurrency(-1); !errors.Is(err, seelimits.ErrInvalidParams) {
			t.Fatalf("SetConcurrency(-1) = %v, want ErrInvalidParams", err)
		}
		if after := assembly.Snapshot().Stats[spec.Name]; after.MaxConcurrency != 5 {
			t.Fatalf("无效设置不应生效: %+v", after)
		}
		t.Logf("运行时参数: 全局并发=%d 单账号并发=%d rpm=%v per_key=%v",
			global.MaxConcurrency, keyStats.MaxConcurrency, keyStats.RequestsPerMin, assembly.Params().PerKey)
	})

	t.Run("边界_零API成本的快速拒绝", func(t *testing.T) {
		fastParams := params.WithQueueTimeout(0).WithConcurrency(1).WithEnabled(true)
		fastAssembly, err := seelimits.Assemble(fastParams)
		if err != nil {
			t.Fatalf("Assemble: %v", err)
		}
		gate, err := fastAssembly.Gate(spec.Name)
		if err != nil {
			t.Fatalf("Gate: %v", err)
		}
		held, err := gate.Acquire(context.Background(), seelimits.Cost{})
		if err != nil {
			t.Fatalf("占位失败: %v", err)
		}
		defer held.Release()

		wrapped, err := fastAssembly.WrapComplete(spec.Name, account.ClientFor(spec))
		if err != nil {
			t.Fatalf("WrapFor: %v", err)
		}
		content := "这条请求必须在准入层被拒绝，不应到达 provider。"
		start := time.Now()
		_, err = wrapped.Complete(context.Background(), []types.Message{{Role: "user", Content: &content}}, nil)
		elapsed := time.Since(start)
		if !errors.Is(err, seelimits.ErrQueueTimeout) {
			t.Fatalf("err = %v, want ErrQueueTimeout", err)
		}
		if elapsed > 200*time.Millisecond {
			t.Errorf("QueueTimeout=0 必须立即失败，实际 %s", elapsed)
		}
		t.Logf("快速拒绝耗时 %s（零 API 成本）", elapsed)
	})
}
