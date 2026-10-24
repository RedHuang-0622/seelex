package registry

// path_face_root_test.go —— 路径门**按已定执行面取根**的验收判据（本工作项核心）。
//
// 四组判据，各钉一件事：
//
//	1. 判据按面分派：无决策 = 本地面（旧口径，越界拒）；决策 = ssh → 不做本地根判定，
//	   把越界候选交给权限门（RemotePathHandoff）；决策 = local → 仍按项目根拒。
//	2. 远端面上相对路径不按本地根判（它是远端相对路径）；本地面照旧拒。
//	3. 端到端（真实 NewRegistryState）：会话固定 SSH + 远端绝对路径 → 不再被本地根
//	   误拒，且路径门确实跑在决策之后。
//	4. 显式指名不可用仍报错不降级；决策段不探不建连（探针费在建连段付）。
//
// 全部用假传输/假探针驱动：不碰网络、不碰 sshd。

import (
	"context"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	frameworktools "github.com/RedHuang-0622/Seele/tools"
	"github.com/RedHuang-0622/seelex/backend/limit/gate"
	"github.com/RedHuang-0622/seelex/backend/limit/policy"
	seelessh "github.com/RedHuang-0622/seelex/backend/tool/ssh"
)

// boundScope 绑一个会话根，并返回（scope, 根目录, 本地根之外的绝对路径）。
//
// "远端路径"在用例里取"本地根之外的绝对路径"：路径门旧口径误拒的正是这一类别，
// 与 /etc/nginx.conf 在判定上是同一件事（都是不在本地项目根内的绝对路径）。
func boundScope(t *testing.T, sessionID string) (*security.ProjectScope, string, string) {
	t.Helper()
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "remote", "app.conf")
	scope := security.NewProjectScope()
	if err := scope.BindFor(sessionID, root); err != nil {
		t.Fatalf("bind scope: %v", err)
	}
	return scope, root, outside
}

func faceDeps(scope *security.ProjectScope, sessionID string) Deps {
	return Deps{ProjectScope: scope, SessionKey: func(context.Context) string { return sessionID }}
}

// sshDecisionContext 造一个"决策段已经选中 ssh"的 ctx（决策段的产出形状）。
func sshDecisionContext(ctx context.Context) context.Context {
	return WithTransportDecision(ctx, TransportDecision{
		Decided: true, Kind: TransportSSH, ProjectID: "ws-A", Feasible: true,
		Endpoint: SSHEndpoint{Host: "deploy.example", Port: 22, User: "deploy"},
		Reason:   ReasonProbeOK,
	})
}

// localDecisionContext 造一个"决策落在本地（降级）"的 ctx。
func localDecisionContext(ctx context.Context) context.Context {
	return WithTransportDecision(ctx, TransportDecision{
		Decided: true, Kind: TransportLocal, ProjectID: "ws-A",
		Reason: ReasonProbeNotRun, Degraded: true,
	})
}

// TestPathGateJudgesByDecidedFace：同一个远端绝对路径，按决策的面得到相反结论。
func TestPathGateJudgesByDecidedFace(t *testing.T) {
	scope, _, outside := boundScope(t, "sess-face")
	gated := PathGate(faceDeps(scope, "sess-face"))
	args := `{"path":` + quoteJSON(outside) + `}`

	t.Run("没有决策时按本地面判（越界拒）", func(t *testing.T) {
		ran := 0
		handler := gated(ReadFileToolName, frameworktools.ToolMeta{},
			frameworktools.HandlerFunc(func(context.Context, string) (string, error) {
				ran++
				return "ok", nil
			}))
		_, err := handler.Execute(context.Background(), args)
		if err == nil || !strings.Contains(err.Error(), "bound project") {
			t.Fatalf("本地面越界应直接拒，err=%v", err)
		}
		if ran != 0 {
			t.Fatalf("被拒的调用不得进入 handler：ran=%d", ran)
		}
	})

	t.Run("决策为 local 时仍按项目根拒", func(t *testing.T) {
		ran := 0
		handler := gated(ReadFileToolName, frameworktools.ToolMeta{},
			frameworktools.HandlerFunc(func(context.Context, string) (string, error) {
				ran++
				return "ok", nil
			}))
		_, err := handler.Execute(localDecisionContext(context.Background()), args)
		if err == nil || !strings.Contains(err.Error(), "bound project") {
			t.Fatalf("本地面（含降级）越界必须拒，err=%v", err)
		}
		if ran != 0 {
			t.Fatalf("被拒的调用不得进入 handler：ran=%d", ran)
		}
	})

	t.Run("决策为 ssh 时不按本地根判，改把候选交给权限门", func(t *testing.T) {
		var (
			ran      int
			handoff  RemotePathHandoff
			heldOff  bool
			seenPath string
		)
		handler := gated(ReadFileToolName, frameworktools.ToolMeta{},
			frameworktools.HandlerFunc(func(ctx context.Context, argsJSON string) (string, error) {
				ran++
				seenPath = argsJSON
				handoff, heldOff = RemotePathHandoffFromContext(ctx)
				return "ok", nil
			}))
		if _, err := handler.Execute(sshDecisionContext(context.Background()), args); err != nil {
			t.Fatalf("远端面不该被本地根拒，err=%v", err)
		}
		if ran != 1 {
			t.Fatalf("远端面应放行到 handler：ran=%d", ran)
		}
		if seenPath != args {
			t.Fatalf("路径不得被改写：%s", seenPath)
		}
		if !heldOff || len(handoff.Candidates) != 1 || handoff.Candidates[0] != outside {
			t.Fatalf("越界候选应作为交接事实交给 SSH 路径白名单：%+v held=%v", handoff, heldOff)
		}
	})
}

// TestPathGateRemoteFaceKeepsRelativePathsUnjudged：远端面上相对路径不按本地根判。
func TestPathGateRemoteFaceKeepsRelativePathsUnjudged(t *testing.T) {
	scope, _, _ := boundScope(t, "sess-rel")
	gated := PathGate(faceDeps(scope, "sess-rel"))
	args := `{"path":` + quoteJSON(filepath.Join("..", "outside", "secret.txt")) + `}`
	handler := gated(ReadFileToolName, frameworktools.ToolMeta{},
		frameworktools.HandlerFunc(func(context.Context, string) (string, error) { return "ok", nil }))

	if _, err := handler.Execute(context.Background(), args); err == nil {
		t.Fatal("本地面上的越界相对路径必须拒（不能等到 handler 才拒，否则先弹一次没用的审批）")
	}
	if _, err := handler.Execute(sshDecisionContext(context.Background()), args); err != nil {
		t.Fatalf("远端面上的相对路径是**远端相对路径**，不该按本地根判：%v", err)
	}
}

// TestFixedSSHEnvironmentNotRejectedByLocalRoot 是端到端判据：会话固定 SSH 环境后，
// 远端绝对路径不再被本地根误拒；并且路径门确实跑在决策段之后。
func TestFixedSSHEnvironmentNotRejectedByLocalRoot(t *testing.T) {
	h := newSelectorHarness()
	h.verdict, h.probed = TransportProbeVerdict{State: TransportProbeOK}, true
	scope, _, outside := boundScope(t, h.sessionID)

	deps := bindHarnessEnvironment(h, h.deps(), "prod", TransportSSH)
	deps.Consumers = TransportConsumers(ReadFileToolName)
	// 路径门外面套一层探针：进入路径门时就**必须**已经能读到决策（链序的一部分）。
	realGate := PathGate(faceDeps(scope, h.sessionID))
	spyGate := func(name string, meta frameworktools.ToolMeta, next frameworktools.ToolHandler) frameworktools.ToolHandler {
		inner := realGate(name, meta, next)
		return frameworktools.HandlerFunc(func(ctx context.Context, argsJSON string) (string, error) {
			decision, ok := TransportDecisionFromContext(ctx)
			if !ok || !decision.Remote() {
				t.Errorf("路径门之前必须已经有执行面决策（链序：事件 → 决策 → 路径门）：%+v ok=%v", decision, ok)
			}
			return inner.Execute(ctx, argsJSON)
		})
	}

	gate := &PermissionGate{}
	gate.Set(policy.DefaultPermissionConfig(), nil)
	gate.SetFullAccess(true)

	var surface string
	state := NewRegistryState(5*time.Second, gate, time.Second,
		spyGate, TransportSelector(deps), nil, nil)
	state.AddInline(ReadFileToolName, "read_file", nil, func(ctx context.Context, _ string) (string, error) {
		surface, _ = ExecutionFace(ctx)
		return "ran", nil
	})

	args := `{"path":` + quoteJSON(outside) + `}`
	if _, err := state.Registry.Dispatch(context.Background(),
		frameworktools.ToolCall{Name: ReadFileToolName, ArgumentsJSON: args}); err != nil {
		t.Fatalf("固定 SSH 环境时远端绝对路径不该被拒：%v", err)
	}
	if surface != string(TransportSSH) {
		t.Fatalf("执行面 = %q, want ssh（会话固定环境）", surface)
	}
	if atomic.LoadInt32(&h.dialCalls) != 1 {
		t.Fatalf("探针通的调用应建连一次：dialCalls=%d", atomic.LoadInt32(&h.dialCalls))
	}
}

// TestUnboundSessionStillJudgesLocalFace：未固定命名环境 → local，越界路径直接拒。
func TestUnboundSessionStillJudgesLocalFace(t *testing.T) {
	h := newSelectorHarness()
	scope, _, outside := boundScope(t, h.sessionID)
	deps := h.deps()
	deps.Consumers = TransportConsumers(ReadFileToolName)

	gate := &PermissionGate{}
	gate.Set(policy.DefaultPermissionConfig(), nil)
	gate.SetFullAccess(true)

	ran := 0
	state := NewRegistryState(5*time.Second, gate, time.Second,
		PathGate(faceDeps(scope, h.sessionID)), TransportSelector(deps), nil, nil)
	state.AddInline(ReadFileToolName, "read_file", nil, func(context.Context, string) (string, error) {
		ran++
		return "ran", nil
	})

	_, err := state.Registry.Dispatch(context.Background(),
		frameworktools.ToolCall{Name: ReadFileToolName, ArgumentsJSON: `{"path":` + quoteJSON(outside) + `}`})
	if err == nil || !strings.Contains(err.Error(), "bound project") {
		t.Fatalf("未固定环境按本地面执行时越界路径须直接拒，err=%v", err)
	}
	if ran != 0 {
		t.Fatalf("被拒的调用不得执行 handler：ran=%d", ran)
	}
	if atomic.LoadInt32(&h.dialCalls) != 0 || h.transport.Calls() != 0 {
		t.Fatal("本地调用不该建连/执行")
	}
}

// TestExplicitSSHUnavailableStillErrorsWithoutDowngrade：显式指名而不可用 → 报错，
// 不降级（本地跑一条命令与远端跑一条命令是两件事）。
func TestExplicitSSHUnavailableStillErrorsWithoutDowngrade(t *testing.T) {
	h := newSelectorHarness()
	h.configured = false // 项目没配连接
	scope, _, outside := boundScope(t, h.sessionID)
	deps := h.deps()
	deps.Consumers = TransportConsumers(ReadFileToolName)

	gate := &PermissionGate{}
	gate.Set(policy.DefaultPermissionConfig(), nil)
	gate.SetFullAccess(true)

	ran := 0
	state := NewRegistryState(5*time.Second, gate, time.Second,
		PathGate(faceDeps(scope, h.sessionID)), TransportSelector(deps), nil, nil)
	state.AddInline(ReadFileToolName, "read_file", nil, func(context.Context, string) (string, error) {
		ran++
		return "ran", nil
	})

	_, err := state.Registry.Dispatch(context.Background(), frameworktools.ToolCall{
		Name:          ReadFileToolName,
		ArgumentsJSON: `{"path":` + quoteJSON(outside) + `,"transport":"ssh"}`,
	})
	if !IsTransportErrorCode(err, CodeSSHUnreachable) {
		t.Fatalf("显式指名不可用应报固定码 SSH_UNREACHABLE，err=%v", err)
	}
	if ran != 0 {
		t.Fatalf("不降级：handler 不得执行（ran=%d）", ran)
	}
	if atomic.LoadInt32(&h.probeNow) != 0 || h.transport.Calls() != 0 {
		t.Fatalf("未配连接不该探针/执行：probeNow=%d calls=%d", atomic.LoadInt32(&h.probeNow), h.transport.Calls())
	}
}

// TestDecisionStageNeitherProbesNorDials：决策段只读——显式指名 + 未探时也不当场探，
// 探测费留给建连段（被拒的调用因此仍零探针零建连）。
func TestDecisionStageNeitherProbesNorDials(t *testing.T) {
	h := newSelectorHarness() // probed=false：未探
	middleware := TransportSelector(h.deps())
	handler := middleware(BashToolName, frameworktools.ToolMeta{}, frameworktools.HandlerFunc(
		func(ctx context.Context, _ string) (string, error) {
			decision, ok := TransportDecisionFromContext(ctx)
			if !ok || !decision.Remote() || !decision.NeedsProbe {
				t.Fatalf("决策段应给出「意图 ssh、待探针」的事实：%+v ok=%v", decision, ok)
			}
			if _, held := TransportSelectionFromContext(ctx); held {
				t.Fatal("决策段不得写 selection（那一格只在建连段落地）")
			}
			return "ok", nil
		}))

	ctx := withTransportStage(WithExplicitTransport(context.Background(), TransportSSH), transportStageDecision)
	if _, err := handler.Execute(ctx, `{}`); err != nil {
		t.Fatalf("决策段不该失败（报错留在建连段）：%v", err)
	}
	if atomic.LoadInt32(&h.probeNow) != 0 || atomic.LoadInt32(&h.dialCalls) != 0 || h.transport.Calls() != 0 {
		t.Fatalf("决策段不许真探/建连/执行：probeNow=%d dial=%d calls=%d",
			atomic.LoadInt32(&h.probeNow), atomic.LoadInt32(&h.dialCalls), h.transport.Calls())
	}
}

// TestExplicitSSHPaysProbeInDialStage：探针费在建连段付——通了才建连，挂了就报错。
func TestExplicitSSHPaysProbeInDialStage(t *testing.T) {
	t.Run("未探且探通→建连一次", func(t *testing.T) {
		h := newSelectorHarness()
		h.probed, h.verdict = false, TransportProbeVerdict{State: TransportProbeOK}
		middleware := TransportSelector(h.deps())
		var kind TransportKind
		handler := middleware(BashToolName, frameworktools.ToolMeta{}, frameworktools.HandlerFunc(
			func(ctx context.Context, _ string) (string, error) {
				selection, ok := TransportSelectionFromContext(ctx)
				if !ok {
					t.Fatal("建连段必须写出 selection")
				}
				kind = selection.Kind
				return "ok", nil
			}))
		if _, err := handler.Execute(WithExplicitTransport(context.Background(), TransportSSH), `{}`); err != nil {
			t.Fatalf("显式指名 + 探通应走到 ssh：%v", err)
		}
		if kind != TransportSSH || atomic.LoadInt32(&h.probeNow) != 1 || atomic.LoadInt32(&h.dialCalls) != 1 {
			t.Fatalf("kind=%q probeNow=%d dial=%d，want ssh/1/1", kind, atomic.LoadInt32(&h.probeNow), atomic.LoadInt32(&h.dialCalls))
		}
	})

	t.Run("未探且探挂→报错不降级", func(t *testing.T) {
		h := newSelectorHarness()
		h.probed = false
		h.verdict = TransportProbeVerdict{State: TransportProbeFailed, Class: seelessh.ClassAuthRejected}
		middleware := TransportSelector(h.deps())
		handler := middleware(BashToolName, frameworktools.ToolMeta{}, frameworktools.HandlerFunc(
			func(context.Context, string) (string, error) {
				t.Error("探测失败的显式指名不得执行 handler")
				return "", nil
			}))
		_, err := handler.Execute(WithExplicitTransport(context.Background(), TransportSSH), `{}`)
		if !IsTransportErrorCode(err, CodeSSHAuthFailed) {
			t.Fatalf("err = %v, want SSH_AUTH_FAILED", err)
		}
		if atomic.LoadInt32(&h.dialCalls) != 0 {
			t.Fatal("探测失败不该建连")
		}
	})
}
