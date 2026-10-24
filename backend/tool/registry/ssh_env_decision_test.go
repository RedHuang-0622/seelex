package registry

import (
	"context"
	"sync/atomic"
	"testing"
)

// 环境选择判据：
//  1. 无端点环境 = 本地，不探针、不建连，也不受全局 SSH 开关影响；
//  2. 有端点环境 = SSH，需要后续探针/建连；是否允许走 SSH 由权限门的 ssh 位决定；
//  3. env 与 transport 相反 = 显式冲突，不静默偏向任何一个。

func environmentDecisionDeps(enabled bool) TransportSelectorDeps {
	return TransportSelectorDeps{
		Enabled:    enabled,
		SessionKey: func(context.Context) string { return "session-1" },
		Project:    func(context.Context, string) string { return "project-1" },
		EnvironmentFor: func(_ string, env string) (TransportEnvironment, bool, error) {
			switch env {
			case "dev":
				return TransportEnvironment{Name: "dev", Kind: TransportLocal}, true, nil
			case "test":
				return TransportEnvironment{
					Name: "test", Kind: TransportSSH,
					Endpoint: SSHEndpoint{Name: "test", Host: "test.example.com", Port: 22, User: "builder"},
				}, true, nil
			default:
				return TransportEnvironment{}, false, nil
			}
		},
	}
}

func TestNamedLocalEnvironmentStaysLocalWithoutProbe(t *testing.T) {
	deps := environmentDecisionDeps(false)
	decision := resolveTransportDecision(context.Background(), deps, "bash", false, "", "dev")
	if decision.Kind != TransportLocal || decision.Reason != ReasonEnvironmentLocal {
		t.Fatalf("decision = %+v, want local/environment_local", decision)
	}
	if decision.NeedsProbe || decision.FailureCode != "" {
		t.Fatalf("本地环境不该探针或失败: %+v", decision)
	}
}

func TestNamedSSHEnvironmentSelectsRemoteBeforeProbe(t *testing.T) {
	deps := environmentDecisionDeps(true)
	decision := resolveTransportDecision(context.Background(), deps, "bash", false, "", "test")
	if decision.Kind != TransportSSH || decision.Reason != ReasonEnvironmentSSH || !decision.NeedsProbe {
		t.Fatalf("decision = %+v, want ssh/environment_ssh + needs probe", decision)
	}
	if decision.Endpoint.Name != "test" || decision.Endpoint.Host != "test.example.com" {
		t.Fatalf("endpoint = %+v", decision.Endpoint)
	}
}

func TestEnvironmentAndTransportConflictIsRejected(t *testing.T) {
	deps := environmentDecisionDeps(true)
	decision := resolveTransportDecision(context.Background(), deps, "bash", true, TransportSSH, "dev")
	if decision.FailureCode != CodeSSHEnvConflict {
		t.Fatalf("failure = %q, want %q", decision.FailureCode, CodeSSHEnvConflict)
	}
	if _, err := bindTransportSelection(context.Background(), deps, "bash", decision); !IsTransportErrorCode(err, CodeSSHEnvConflict) {
		t.Fatalf("local 命中失败码时也不得按成功直通，err=%v", err)
	}
}

func TestUnknownEnvironmentDoesNotFallBack(t *testing.T) {
	deps := environmentDecisionDeps(true)
	decision := resolveTransportDecision(context.Background(), deps, "bash", false, "", "prod")
	if decision.FailureCode != CodeSSHEnvUnknown {
		t.Fatalf("failure = %q, want %q", decision.FailureCode, CodeSSHEnvUnknown)
	}
}

func TestUnboundSessionStaysLocalEvenWithProjectEndpoint(t *testing.T) {
	h := newSelectorHarness()
	h.verdict, h.probed = TransportProbeVerdict{State: TransportProbeOK}, true
	deps := h.deps()
	deps.EnvFor = func(context.Context, string) string { return "" }
	selection, held, err := h.run(t, deps, `{}`)
	if err != nil || !held {
		t.Fatalf("selection=%+v held=%v err=%v", selection, held, err)
	}
	if selection.Kind != TransportLocal || selection.Reason != ReasonSessionEnvironmentUnbound {
		t.Fatalf("selection=%+v, want local/%s", selection, ReasonSessionEnvironmentUnbound)
	}
	if atomic.LoadInt32(&h.probeCalls) != 0 || atomic.LoadInt32(&h.dialCalls) != 0 {
		t.Fatal("未固定环境的会话不得读取项目端点、探针或建连")
	}
}

func TestTransportBindingKeepsProjectForUnboundSession(t *testing.T) {
	ctx := WithTransportBinding(context.Background(), TransportBinding{ProjectID: "project-1"})
	binding, ok := TransportBindingFromContext(ctx)
	if !ok || binding.ProjectID != "project-1" || binding.Env != "" {
		t.Fatalf("binding = %+v ok=%v, want project-only binding", binding, ok)
	}
}

func TestTransportSelectionUsesEnvironmentKindEndToEnd(t *testing.T) {
	t.Run("local environment", func(t *testing.T) {
		h := newSelectorHarness()
		h.enabled = false
		deps := h.deps()
		deps.EnvFor = func(context.Context, string) string { return "dev" }
		deps.EnvironmentFor = func(string, string) (TransportEnvironment, bool, error) {
			return TransportEnvironment{Name: "dev", Kind: TransportLocal}, true, nil
		}
		selection, held, err := h.run(t, deps, `{"env":"dev"}`)
		if err != nil || !held || selection.Kind != TransportLocal || selection.Env != "dev" {
			t.Fatalf("selection=%+v held=%v err=%v", selection, held, err)
		}
		if atomic.LoadInt32(&h.probeCalls) != 0 || atomic.LoadInt32(&h.dialCalls) != 0 {
			t.Fatal("本地环境不得探测或建连")
		}
	})

	t.Run("ssh environment", func(t *testing.T) {
		h := newSelectorHarness()
		h.verdict, h.probed = TransportProbeVerdict{State: TransportProbeOK}, true
		deps := h.deps()
		deps.EnvFor = func(context.Context, string) string { return "test" }
		deps.EnvironmentFor = func(string, string) (TransportEnvironment, bool, error) {
			return TransportEnvironment{
				Name: "test", Kind: TransportSSH,
				Endpoint: SSHEndpoint{Name: "test", Host: "test.example.com", Port: 22, User: "qa"},
			}, true, nil
		}
		selection, held, err := h.run(t, deps, `{"env":"test"}`)
		if err != nil || !held || selection.Kind != TransportSSH || selection.Env != "test" {
			t.Fatalf("selection=%+v held=%v err=%v", selection, held, err)
		}
		if selection.Endpoint != "test.example.com:22" || selection.Transport == nil {
			t.Fatalf("remote selection = %+v", selection)
		}
	})
}

func TestToolEnvironmentArgumentCannotOverrideSessionBinding(t *testing.T) {
	h := newSelectorHarness()
	h.enabled = false
	deps := h.deps()
	deps.EnvFor = func(context.Context, string) string { return "dev" }
	deps.EnvironmentFor = func(_ string, env string) (TransportEnvironment, bool, error) {
		if env != "dev" {
			t.Fatalf("selector 读了模型伪造的 env=%q；环境必须只来自会话绑定", env)
		}
		return TransportEnvironment{Name: "dev", Kind: TransportLocal}, true, nil
	}
	selection, _, err := h.run(t, deps, `{"env":"prod"}`)
	if err != nil || selection.Env != "dev" || selection.Kind != TransportLocal {
		t.Fatalf("selection=%+v err=%v", selection, err)
	}
}

func BenchmarkResolveTransportDecisionWithEnvironment(b *testing.B) {
	deps := environmentDecisionDeps(true)
	ctx := context.Background()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = resolveTransportDecision(ctx, deps, "bash", false, "", "test")
	}
}
