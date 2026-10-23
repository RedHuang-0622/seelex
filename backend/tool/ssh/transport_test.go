package ssh

// transport_test.go —— 执行面的两条本地判据（不连网、不依赖 sshd）：
//   1. 命令来源纪律：多行 / 非 ASCII 必须走 stdin，单行 ASCII 才走内联；
//   2. 探针缓存：未探 ≠ 探通，TTL 过期后重新变回"没结论"，键按端点+用户分。
//
// 真实的连接行为（四段超时、指纹、认证）由 ssh_test.go 的进程内 sshd 夹具负责，
// 本文件刻意只测"不需要对端"的那部分：这样它在任何机器上都能跑绿。

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCommandRequestRoutesMultilineAndNonASCIIToStdin(t *testing.T) {
	cases := []struct {
		name       string
		command    string
		wantInline string
		wantStdin  string
	}{
		{name: "单行 ASCII 走内联", command: "ls -la /tmp", wantInline: "ls -la /tmp"},
		{name: "多行走 stdin", command: "echo a\necho b", wantStdin: "echo a\necho b"},
		{name: "CRLF 走 stdin（不归一行尾）", command: "echo a\r\necho b", wantStdin: "echo a\r\necho b"},
		{name: "中文走 stdin", command: "echo 你好", wantStdin: "echo 你好"},
		{name: "路径含中文走 stdin", command: "cat -- /tmp/报告.txt", wantStdin: "cat -- /tmp/报告.txt"},
		{name: "空字节走 stdin", command: "printf a\x00b", wantStdin: "printf a\x00b"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := commandRequest(tc.command, "/srv")
			if err != nil {
				t.Fatalf("commandRequest: %v", err)
			}
			if req.Workdir != "/srv" {
				t.Fatalf("workdir = %q, want /srv", req.Workdir)
			}
			if req.Inline != tc.wantInline {
				t.Fatalf("inline = %q, want %q", req.Inline, tc.wantInline)
			}
			if req.Stdin != tc.wantStdin {
				t.Fatalf("stdin = %q, want %q", req.Stdin, tc.wantStdin)
			}
			if req.Inline != "" && req.Stdin != "" {
				t.Fatal("命令来源必须互斥")
			}
		})
	}
}

func TestCommandRequestRejectsEmptyCommand(t *testing.T) {
	for _, command := range []string{"", "   ", "\n", "\t\n "} {
		if _, err := commandRequest(command, ""); err == nil {
			t.Fatalf("空命令 %q 必须硬失败（不是\"跑了个空脚本成功\"）", command)
		} else if !strings.Contains(err.Error(), CodeNoCommand) {
			t.Fatalf("err = %v, want code %s", err, CodeNoCommand)
		}
	}
}

func TestIsInlineSafe(t *testing.T) {
	safe := []string{"pwd", "ls -la", "git status --short", "echo 'a b'"}
	for _, command := range safe {
		if !isInlineSafe(command) {
			t.Fatalf("%q 应为可内联", command)
		}
	}
	unsafe := []string{"echo a\nb", "echo a\rb", "echo 你好", "printf a\x00b"}
	for _, command := range unsafe {
		if isInlineSafe(command) {
			t.Fatalf("%q 不该被判定为可内联（会被多层代码页静默损坏）", command)
		}
	}
}

// TestProberThreeStates 未探 / 有结论 / 过期 三态与"键按端点+用户分"。
func TestProberThreeStates(t *testing.T) {
	var (
		mu      sync.Mutex
		probes  int
		verdict = ProbeResult{OK: true, Class: ClassOK}
	)
	clock := time.Now()
	prober := NewProber(50*time.Millisecond, func(context.Context, Spec) ProbeResult {
		mu.Lock()
		defer mu.Unlock()
		probes++
		return verdict
	})
	prober.now = func() time.Time { return clock }

	spec := Spec{ID: "p1", Hostname: "deploy.example", Port: 22, User: "deploy"}

	// 未探：没有结论（选择器据此降级并留痕，而不是"假装通"）。
	if _, ok := prober.Cached(spec); ok {
		t.Fatal("首次读缓存不应有结论")
	}
	mu.Lock()
	if probes != 0 {
		mu.Unlock()
		t.Fatal("读缓存不许发网络请求")
	}
	mu.Unlock()

	// 当场探一次 → 有结论。
	if result := prober.ProbeNow(context.Background(), spec); !result.OK {
		t.Fatalf("ProbeNow = %+v, want ok", result)
	}
	if result, ok := prober.Cached(spec); !ok || !result.OK {
		t.Fatalf("Cached = (%+v, %v), want (ok, true)", result, ok)
	}

	// 换个用户就是另一次认证：键不同，结论不共享。
	other := spec
	other.User = "root"
	if _, ok := prober.Cached(other); ok {
		t.Fatal("不同用户不该共享探测结论")
	}

	// 过期 → 重新变回"没结论"（结论会腐烂：网络通了、key 换了、主机重启了）。
	clock = clock.Add(2 * DefaultProbeTTL)
	if _, ok := prober.Cached(spec); ok {
		t.Fatal("过期结论必须作废")
	}

	// Invalidate 显式丢弃。
	verdict = ProbeResult{OK: false, Class: ClassUnreachable}
	prober.ProbeNow(context.Background(), spec)
	prober.Invalidate(spec)
	if _, ok := prober.Cached(spec); ok {
		t.Fatal("Invalidate 后不应有结论")
	}
	mu.Lock()
	defer mu.Unlock()
	if probes != 2 {
		t.Fatalf("探针次数 = %d, want 2（读缓存不探）", probes)
	}
}

// TestProberConcurrentSessions 并发读同一缓存不串结论（-race 下同时验证锁）。
func TestProberConcurrentSessions(t *testing.T) {
	prober := NewProber(time.Minute, func(_ context.Context, spec Spec) ProbeResult {
		if spec.Hostname == "ok.example" {
			return ProbeResult{OK: true, Class: ClassOK}
		}
		return ProbeResult{OK: false, Class: ClassUnreachable}
	})
	okSpec := Spec{Hostname: "ok.example", User: "deploy"}
	badSpec := Spec{Hostname: "bad.example", User: "deploy"}

	var wg sync.WaitGroup
	for index := 0; index < 16; index++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			if result := prober.ProbeNow(context.Background(), okSpec); !result.OK {
				t.Errorf("ok 端点结论被污染：%+v", result)
			}
		}()
		go func() {
			defer wg.Done()
			if result := prober.ProbeNow(context.Background(), badSpec); result.OK {
				t.Errorf("bad 端点结论被污染：%+v", result)
			}
		}()
	}
	wg.Wait()

	if result, ok := prober.Cached(okSpec); !ok || !result.OK {
		t.Fatalf("Cached(ok) = (%+v, %v)", result, ok)
	}
	if result, ok := prober.Cached(badSpec); !ok || result.OK {
		t.Fatalf("Cached(bad) = (%+v, %v)", result, ok)
	}
}
