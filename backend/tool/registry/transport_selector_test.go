package registry

// transport_selector_test.go —— 传输选择中间件的验收判据。
//
// 五组用例，各钉一件事：
//
//	1. 六格矩阵（未配 / 配置未启用 / 探针未探 / 探针通 / 探针挂 / 显式指名不可用）
//	   ——每格断言"选了哪一支 + 理由 + 有没有留痕"，降级必须带 reason；
//	2. 显式指名不许降级（失败即错，码固定）；
//	3. 传输错误与权限拒绝**可区分**（结构判据，不是文案判据）；
//	4. 并发两个会话各选各的，互不污染；
//	5. 插入点：用真实 NewRegistryState 装配，证明顺序是
//	   权限 → 传输选择 → 诊断（被拒的调用一个探针都不打）。
//
// 全部用例用假传输/假探针驱动：不碰网络、不碰 sshd、不依赖本机连通性。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	frameworktools "github.com/RedHuang-0622/Seele/tools"
	toolspermission "github.com/RedHuang-0622/Seele/tools/permission"
	"github.com/RedHuang-0622/seelex/backend/limit/policy"
	seelessh "github.com/RedHuang-0622/seelex/backend/tool/ssh"
)

// fakeTransport 的五个能力面实现见 transport_fake_test.go（同一个假面，两份用例共用）。

// selectorHarness 是一套可编排的选择器依赖（六格矩阵用它逐格取值）。
type selectorHarness struct {
	sessionID string
	projectID string
	enabled   bool

	endpoint    SSHEndpoint
	configured  bool
	endpointErr error

	verdict    TransportProbeVerdict
	probed     bool
	probeCalls int32
	probeNow   int32

	dialCalls int32
	dialErr   error
	transport *fakeTransport

	trace *TransportTraceLog
}

func newSelectorHarness() *selectorHarness {
	return &selectorHarness{
		sessionID: "sess-A", projectID: "ws-A", enabled: true,
		endpoint: SSHEndpoint{Host: "deploy.example", Port: 22, User: "deploy"}, configured: true,
		transport: &fakeTransport{name: "ssh-exec"},
		trace:     NewTransportTraceLog(32),
	}
}

func (h *selectorHarness) deps() TransportSelectorDeps {
	return TransportSelectorDeps{
		Enabled:   h.enabled,
		Consumers: TransportConsumers(BashToolName),
		SessionKey: func(context.Context) string {
			return h.sessionID
		},
		Project: func(context.Context, string) string { return h.projectID },
		Endpoint: func(string) (SSHEndpoint, bool, error) {
			return h.endpoint, h.configured, h.endpointErr
		},
		Probe: func(context.Context, string, SSHEndpoint) (TransportProbeVerdict, bool) {
			atomic.AddInt32(&h.probeCalls, 1)
			return h.verdict, h.probed
		},
		ProbeNow: func(context.Context, string, SSHEndpoint) TransportProbeVerdict {
			atomic.AddInt32(&h.probeNow, 1)
			return h.verdict
		},
		Dial: func(context.Context, string, SSHEndpoint) (Transport, error) {
			atomic.AddInt32(&h.dialCalls, 1)
			if h.dialErr != nil {
				return nil, h.dialErr
			}
			return h.transport, nil
		},
		Trace: h.trace,
	}
}

// run 走一遍中间件，并把 handler 看到的执行面原样带回来。
func (h *selectorHarness) run(t *testing.T, deps TransportSelectorDeps, argsJSON string) (TransportSelection, bool, error) {
	t.Helper()
	middleware := TransportSelector(deps)
	if middleware == nil {
		t.Fatal("有消费者时选择器不应为 nil")
	}
	var (
		seen TransportSelection
		held bool
	)
	handler := middleware(BashToolName, frameworktools.ToolMeta{}, frameworktools.HandlerFunc(
		func(ctx context.Context, _ string) (string, error) {
			seen, held = TransportSelectionFromContext(ctx)
			return "ran", nil
		}))
	_, err := handler.Execute(context.Background(), argsJSON)
	return seen, held, err
}

// TestTransportSelectorSixCellMatrix 钉住六格矩阵：选了哪一支、理由是什么、有没有留痕。
func TestTransportSelectorSixCellMatrix(t *testing.T) {
	t.Run("未配连接→本地且不记降级", func(t *testing.T) {
		h := newSelectorHarness()
		h.configured = false
		selection, held, err := h.run(t, h.deps(), `{}`)
		if err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		if !held || selection.Kind != TransportLocal {
			t.Fatalf("selection = %+v, want local", selection)
		}
		if selection.Reason != ReasonUnconfigured {
			t.Fatalf("reason = %q, want %q", selection.Reason, ReasonUnconfigured)
		}
		if selection.Transport != nil {
			t.Fatal("本地选择不应带 Transport")
		}
		if entries := h.trace.Entries(); len(entries) != 1 || entries[0].Degraded {
			t.Fatalf("trace = %+v, want 一条非降级记录", entries)
		}
	})

	t.Run("配置未启用→本地", func(t *testing.T) {
		h := newSelectorHarness()
		h.enabled = false
		selection, _, err := h.run(t, h.deps(), `{}`)
		if err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		if selection.Kind != TransportLocal || selection.Reason != ReasonEnvDisabled {
			t.Fatalf("selection = %+v, want local/env_disabled", selection)
		}
		if h.transport.Calls() != 0 || atomic.LoadInt32(&h.probeCalls) != 0 {
			t.Fatal("能力关闭时不应探针/建连")
		}
	})

	t.Run("探针未探→降级本地并留痕且不替人探", func(t *testing.T) {
		h := newSelectorHarness()
		selection, _, err := h.run(t, h.deps(), `{}`)
		if err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		if selection.Kind != TransportLocal || selection.Reason != ReasonProbeNotRun {
			t.Fatalf("selection = %+v, want local/probe_not_run", selection)
		}
		if atomic.LoadInt32(&h.probeNow) != 0 {
			t.Fatal("未指名时不许当场探针（连接是花钱的动作）")
		}
		entries := h.trace.Entries()
		if len(entries) != 1 || !entries[0].Degraded {
			t.Fatalf("trace = %+v, want 一条降级记录", entries)
		}
		if entries[0].Reason == "" {
			t.Fatal("降级必须带 reason（否则\"为什么这次在本机跑\"无从追查）")
		}
	})

	t.Run("探针通→ssh", func(t *testing.T) {
		h := newSelectorHarness()
		h.verdict, h.probed = TransportProbeVerdict{State: TransportProbeOK}, true
		selection, _, err := h.run(t, h.deps(), `{}`)
		if err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		if selection.Kind != TransportSSH || selection.Transport == nil {
			t.Fatalf("selection = %+v, want ssh", selection)
		}
		if selection.Reason != ReasonProbeOK {
			t.Fatalf("reason = %q, want %q", selection.Reason, ReasonProbeOK)
		}
		if selection.Endpoint != "deploy.example:22" {
			t.Fatalf("endpoint = %q, want deploy.example:22", selection.Endpoint)
		}
	})

	t.Run("探针挂→降级本地并带上分类", func(t *testing.T) {
		h := newSelectorHarness()
		h.verdict = TransportProbeVerdict{State: TransportProbeFailed, Class: seelessh.ClassAuthRejected}
		h.probed = true
		selection, _, err := h.run(t, h.deps(), `{}`)
		if err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		if selection.Kind != TransportLocal || selection.Reason != ReasonProbeFailed {
			t.Fatalf("selection = %+v, want local/probe_failed", selection)
		}
		if selection.ProbeClass != seelessh.ClassAuthRejected {
			t.Fatalf("probe class = %q, want %q", selection.ProbeClass, seelessh.ClassAuthRejected)
		}
		entries := h.trace.Degradations()
		if len(entries) != 1 || entries[0].ProbeClass != seelessh.ClassAuthRejected {
			t.Fatalf("degradations = %+v, want 一条带分类", entries)
		}
		if h.transport.Calls() != 0 {
			t.Fatal("探针挂了就不该再建连")
		}
	})

	t.Run("显式指名不可用→报错不降级", func(t *testing.T) {
		cases := []struct {
			name      string
			prepare   func(*selectorHarness)
			wantCode  string
			probeCost int32
		}{
			{
				name: "探针挂-认证失败",
				prepare: func(h *selectorHarness) {
					h.verdict = TransportProbeVerdict{State: TransportProbeFailed, Class: seelessh.ClassAuthRejected}
					h.probed = true
				},
				wantCode: CodeSSHAuthFailed,
			},
			{
				name: "探针挂-主机不可信",
				prepare: func(h *selectorHarness) {
					h.verdict = TransportProbeVerdict{State: TransportProbeFailed, Class: seelessh.ClassHostUnknown}
					h.probed = true
				},
				wantCode: CodeSSHHostUntrusted,
			},
			{
				name: "探针挂-超时",
				prepare: func(h *selectorHarness) {
					h.verdict = TransportProbeVerdict{State: TransportProbeFailed, Class: seelessh.ClassTimeout}
					h.probed = true
				},
				wantCode: CodeSSHTimeout,
			},
			{
				name: "探针挂-不可达",
				prepare: func(h *selectorHarness) {
					h.verdict = TransportProbeVerdict{State: TransportProbeFailed, Class: seelessh.ClassUnreachable}
					h.probed = true
				},
				wantCode: CodeSSHUnreachable,
			},
			{
				name: "未探→当场探一次（指名者付费）",
				prepare: func(h *selectorHarness) {
					h.verdict = TransportProbeVerdict{State: TransportProbeFailed, Class: seelessh.ClassUnreachable}
				},
				wantCode:  CodeSSHUnreachable,
				probeCost: 1,
			},
			{
				name:     "未配连接",
				prepare:  func(h *selectorHarness) { h.configured = false },
				wantCode: CodeSSHUnreachable,
			},
			{
				name:     "能力关闭",
				prepare:  func(h *selectorHarness) { h.enabled = false },
				wantCode: CodeSSHEnvDisabled,
			},
			{
				name: "建连失败",
				prepare: func(h *selectorHarness) {
					h.verdict, h.probed = TransportProbeVerdict{State: TransportProbeOK}, true
					h.dialErr = &TransportError{
						Code: CodeSSHTimeout, Msg: "建连超时",
						Err: &ProbeClassError{Class: seelessh.ClassTimeout},
					}
				},
				wantCode: CodeSSHTimeout,
			},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				h := newSelectorHarness()
				tc.prepare(h)
				_, held, err := h.run(t, h.deps(), `{"transport":"ssh"}`)
				if err == nil {
					t.Fatalf("显式指名 ssh 且不可用时必须报错（held=%v），不许降级", held)
				}
				if code := TransportErrorCode(err); code != tc.wantCode {
					t.Fatalf("code = %q, want %q（err = %v）", code, tc.wantCode, err)
				}
				if atomic.LoadInt32(&h.probeNow) != tc.probeCost {
					t.Fatalf("probe-now 次数 = %d, want %d", h.probeNow, tc.probeCost)
				}
				if h.transport.Calls() != 0 {
					t.Fatal("不可用的通路不该真的执行命令")
				}
				if len(h.trace.Degradations()) != 1 {
					t.Fatalf("失败必须留痕：%+v", h.trace.Entries())
				}
			})
		}
	})
}

// TestTransportSelectorExplicitLocalAlsoHonored 显式指名 local 时尊重调用方（不探、不连、不算降级）。
func TestTransportSelectorExplicitLocalAlsoHonored(t *testing.T) {
	h := newSelectorHarness()
	h.verdict, h.probed = TransportProbeVerdict{State: TransportProbeOK}, true
	selection, _, err := h.run(t, h.deps(), `{"transport":"local"}`)
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if selection.Kind != TransportLocal || selection.Reason != ReasonExplicitLocal {
		t.Fatalf("selection = %+v, want local/explicit_local", selection)
	}
	if atomic.LoadInt32(&h.probeCalls) != 0 || h.transport.Calls() != 0 {
		t.Fatal("显式指名 local 时不该探针/建连")
	}
	if entries := h.trace.Degradations(); len(entries) != 0 {
		t.Fatalf("显式 local 不是降级：%+v", entries)
	}
}

// TestTransportSelectorExplicitFromContext ctx 指名优先于工具入参（宿主/界面按会话指定）。
func TestTransportSelectorExplicitFromContext(t *testing.T) {
	h := newSelectorHarness()
	h.configured = false
	deps := h.deps()
	middleware := TransportSelector(deps)
	if middleware == nil {
		t.Fatal("选择器不应为 nil")
	}
	handler := middleware(BashToolName, frameworktools.ToolMeta{}, frameworktools.HandlerFunc(
		func(context.Context, string) (string, error) { return "ran", nil }))
	ctx := WithExplicitTransport(context.Background(), TransportSSH)
	_, err := handler.Execute(ctx, `{"transport":"local"}`)
	if TransportErrorCode(err) != CodeSSHUnreachable {
		t.Fatalf("ctx 指名 ssh 应优先于入参 local 并报错，err = %v", err)
	}
}

// TestTransportSelectorNonConsumerPassesThrough 不是消费者的工具一个判断都不做。
func TestTransportSelectorNonConsumerPassesThrough(t *testing.T) {
	h := newSelectorHarness()
	h.verdict, h.probed = TransportProbeVerdict{State: TransportProbeOK}, true
	middleware := TransportSelector(h.deps())
	var sawSelection bool
	handler := middleware("read_file", frameworktools.ToolMeta{}, frameworktools.HandlerFunc(
		func(ctx context.Context, _ string) (string, error) {
			_, sawSelection = TransportSelectionFromContext(ctx)
			return "ok", nil
		}))
	if _, err := handler.Execute(context.Background(), `{}`); err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if sawSelection {
		t.Fatal("非消费者不应被写入执行面")
	}
	if len(h.trace.Entries()) != 0 {
		t.Fatal("非消费者不应留痕（否则留痕会被噪音淹掉）")
	}
}

// TestTransportSelectorWithoutConsumersIsNil 无消费者时返回 nil（链上逐字等于旧行为）。
func TestTransportSelectorWithoutConsumersIsNil(t *testing.T) {
	deps := TransportSelectorDeps{Enabled: true}
	if middleware := TransportSelector(deps); middleware != nil {
		t.Fatal("没有消费者时选择器必须是 nil（不许把 ssh 写进 ctx 而没人读）")
	}
}

// TestTransportErrorsDistinguishableFromPermissionDenial 传输失败与权限拒绝在结构上可区分。
func TestTransportErrorsDistinguishableFromPermissionDenial(t *testing.T) {
	permissionErr := &toolspermission.DenialError{Tool: BashToolName}
	if !errors.Is(permissionErr, frameworktools.ErrPermissionDenied) {
		t.Fatal("前提：权限拒绝 unwrap 到 ErrPermissionDenied")
	}
	if IsTransportErrorCode(permissionErr) {
		t.Fatal("权限拒绝不应携带任何 SSH_ 码")
	}

	h := newSelectorHarness()
	h.configured = false
	_, _, err := h.run(t, h.deps(), `{"transport":"ssh"}`)
	if !IsTransportErrorCode(err, CodeSSHUnreachable) {
		t.Fatalf("err = %v, want SSH_UNREACHABLE", err)
	}
	if errors.Is(err, frameworktools.ErrPermissionDenied) {
		t.Fatal("传输失败不得被识别为权限拒绝")
	}
	for _, code := range []string{
		CodeSSHUnreachable, CodeSSHAuthFailed, CodeSSHHostUntrusted, CodeSSHTimeout, CodeSSHEnvDisabled,
	} {
		if strings.TrimSpace(code) == "" {
			t.Fatal("固定错误码不得为空")
		}
	}
}

// TestTransportSelectorConcurrentSessionsIsolated 并发两个会话各选各的：选择不互相污染。
func TestTransportSelectorConcurrentSessionsIsolated(t *testing.T) {
	// 会话 A：项目 ws-A，探针通 → ssh；会话 B：项目 ws-B，未配连接 → local。
	depsA := newSelectorHarness()
	depsA.sessionID, depsA.projectID = "sess-A", "ws-A"
	depsA.verdict, depsA.probed = TransportProbeVerdict{State: TransportProbeOK}, true

	depsB := newSelectorHarness()
	depsB.sessionID, depsB.projectID = "sess-B", "ws-B"
	depsB.configured = false

	middlewareA, middlewareB := TransportSelector(depsA.deps()), TransportSelector(depsB.deps())
	if middlewareA == nil || middlewareB == nil {
		t.Fatal("两个会话的选择器都应存在")
	}
	const rounds = 32
	var wg sync.WaitGroup
	var failures sync.Map
	record := func(sessionID, format string, args ...any) {
		failures.Store(fmt.Sprintf("%s-%d", sessionID, time.Now().UnixNano()), fmt.Sprintf(format, args...))
	}
	handle := func(middleware frameworktools.MetaMiddleware, wantKind TransportKind, wantProject string, tag string) {
		handler := middleware(BashToolName, frameworktools.ToolMeta{}, frameworktools.HandlerFunc(
			func(ctx context.Context, _ string) (string, error) {
				selection, ok := TransportSelectionFromContext(ctx)
				if !ok {
					record(tag, "执行面缺失")
					return "", nil
				}
				if selection.Kind != wantKind {
					record(tag, "kind=%s want %s", selection.Kind, wantKind)
				}
				if selection.ProjectID != wantProject {
					record(tag, "project=%s want %s（会话归属被污染）", selection.ProjectID, wantProject)
				}
				if wantKind == TransportSSH && selection.Transport == nil {
					record(tag, "ssh 选择却没有传输")
				}
				return "", nil
			}))
		_, _ = handler.Execute(context.Background(), `{}`)
	}

	wg.Add(2 * rounds)
	for index := 0; index < rounds; index++ {
		go func() {
			defer wg.Done()
			handle(middlewareA, TransportSSH, "ws-A", "A")
		}()
		go func() {
			defer wg.Done()
			handle(middlewareB, TransportLocal, "ws-B", "B")
		}()
	}
	wg.Wait()
	failures.Range(func(_, value any) bool {
		t.Errorf("并发选择互不污染失败：%v", value)
		return true
	})
	// 留痕也各自归各自：A 的留痕里不许出现 B 的会话/项目（反过来同理）。
	for _, pair := range []struct {
		log       *TransportTraceLog
		sessionID string
		projectID string
		kind      TransportKind
	}{
		{depsA.trace, "sess-A", "ws-A", TransportSSH},
		{depsB.trace, "sess-B", "ws-B", TransportLocal},
	} {
		entries := pair.log.Entries()
		if len(entries) != rounds {
			t.Fatalf("会话 %s 留痕条数 = %d, want %d", pair.sessionID, len(entries), rounds)
		}
		for _, entry := range entries {
			if entry.SessionID != pair.sessionID || entry.ProjectID != pair.projectID {
				t.Fatalf("留痕串会话：%+v", entry)
			}
			if entry.Kind != pair.kind {
				t.Fatalf("会话 %s 留痕 kind = %s, want %s", pair.sessionID, entry.Kind, pair.kind)
			}
		}
	}
}

// TestRegistryStateTransportInsertionPoint 用真实 NewRegistryState 钉插入点：
// 决策（只读）→ 权限 → 建连 → 诊断。
//
// 判据（新链序）：决策段在权限门**之前**跑，所以它会读一次**探针缓存**（纯读，不发
// 网络包）；被拒的调用仍然零探测零建连——ProbeNow（真探）与 Dial/Exec（握手与执行）
// 必须都是 0，诊断也不执行。
func TestRegistryStateTransportInsertionPoint(t *testing.T) {
	t.Run("权限拒绝先于建连", func(t *testing.T) {
		h := newSelectorHarness()
		h.verdict, h.probed = TransportProbeVerdict{State: TransportProbeOK}, true
		gate := sessionScopedGate("sess-deny", toolspermission.PermissionConfig{
			Mode:  toolspermission.ModeManual,
			Rules: []toolspermission.PermissionRule{{ToolName: BashToolName, Action: toolspermission.ActionDeny}},
		}, nil)

		var diagnosed int32
		state := NewRegistryState(5*time.Second, gate, time.Second, nil,
			TransportSelector(h.deps()),
			nil,
			func(_ string, next frameworktools.ToolHandler) frameworktools.ToolHandler {
				return frameworktools.HandlerFunc(func(ctx context.Context, argsJSON string) (string, error) {
					atomic.AddInt32(&diagnosed, 1)
					return next.Execute(ctx, argsJSON)
				})
			})
		state.AddInline(BashToolName, "bash", nil, func(context.Context, string) (string, error) {
			t.Error("被拒绝的工具不应执行")
			return "", nil
		})

		_, err := state.Registry.Dispatch(context.Background(), frameworktools.ToolCall{Name: BashToolName, ArgumentsJSON: `{}`})
		if !errors.Is(err, frameworktools.ErrPermissionDenied) {
			t.Fatalf("err = %v, want ErrPermissionDenied", err)
		}
		if IsTransportErrorCode(err) {
			t.Fatal("权限拒绝不应被当成传输失败")
		}
		if atomic.LoadInt32(&h.probeNow) != 0 || atomic.LoadInt32(&h.dialCalls) != 0 || h.transport.Calls() != 0 {
			t.Fatalf("被拒绝的调用不许真探/建连/执行：probeNow=%d dial=%d calls=%d",
				atomic.LoadInt32(&h.probeNow), atomic.LoadInt32(&h.dialCalls), h.transport.Calls())
		}
		if reads := atomic.LoadInt32(&h.probeCalls); reads != 1 {
			t.Fatalf("决策段只该读一次探针缓存（建连段复用决策，不重算）：probeCalls=%d", reads)
		}
		if atomic.LoadInt32(&diagnosed) != 0 {
			t.Fatal("权限拒绝时诊断不该执行（顺序：决策 → 权限 → 建连 → 诊断）")
		}
	})

	t.Run("放行时传输选择先于诊断完成", func(t *testing.T) {
		h := newSelectorHarness()
		h.verdict, h.probed = TransportProbeVerdict{State: TransportProbeOK}, true
		gate := &PermissionGate{}
		gate.Set(policy.DefaultPermissionConfig(), nil)
		gate.SetFullAccess(true)

		type observation struct {
			kind     TransportKind
			held     bool
			reason   string
			executed bool
		}
		var got observation
		state := NewRegistryState(5*time.Second, gate, time.Second, nil,
			TransportSelector(h.deps()),
			nil,
			func(_ string, next frameworktools.ToolHandler) frameworktools.ToolHandler {
				return frameworktools.HandlerFunc(func(ctx context.Context, argsJSON string) (string, error) {
					// 诊断中间件在传输选择**之后**：这里必须已经能读到一个明确的执行面。
					selection, held := TransportSelectionFromContext(ctx)
					got = observation{kind: selection.Kind, held: held, reason: selection.Reason}
					result, err := next.Execute(ctx, argsJSON)
					got.executed = err == nil
					return result, err
				})
			})
		state.AddInline(BashToolName, "bash", nil, func(context.Context, string) (string, error) {
			return "ran", nil
		})

		_, err := state.Registry.Dispatch(context.Background(), frameworktools.ToolCall{Name: BashToolName, ArgumentsJSON: `{}`})
		if err != nil {
			t.Fatalf("Dispatch: %v", err)
		}
		if !got.held {
			t.Fatal("诊断阶段必须已经能读到执行面（选完才进诊断）")
		}
		if got.kind != TransportSSH || got.reason != ReasonProbeOK {
			t.Fatalf("诊断看到的执行面 = %+v, want ssh/probe_ok", got)
		}
		if !got.executed {
			t.Fatal("handler 应被执行")
		}
	})
}

// TestTransportTraceJSONShape 留痕形状固定（诊断/轨迹读取面按字段名取）。
func TestTransportTraceJSONShape(t *testing.T) {
	log := NewTransportTraceLog(2)
	log.RecordTransportTrace(TransportTrace{Tool: "bash", Kind: TransportLocal, Reason: ReasonProbeFailed, Degraded: true})
	log.RecordTransportTrace(TransportTrace{Tool: "bash", Kind: TransportSSH, Reason: ReasonProbeOK})
	log.RecordTransportTrace(TransportTrace{Tool: "bash", Kind: TransportLocal, Reason: ReasonUnconfigured})
	entries := log.Entries()
	if len(entries) != 2 || entries[0].Reason != ReasonProbeOK {
		t.Fatalf("环形留痕应丢最旧一条，得到 %+v", entries)
	}
	payload, err := json.Marshal(entries[0])
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, field := range []string{"At", "Tool", "Kind", "Reason", "Degraded"} {
		if !strings.Contains(string(payload), field) {
			t.Fatalf("留痕缺少字段 %s：%s", field, payload)
		}
	}
}
