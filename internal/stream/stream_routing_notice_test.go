package stream

import (
	"context"
	"errors"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"sync"
	"testing"

	"github.com/RedHuang-0622/Seele/agent"

	"github.com/RedHuang-0622/seelex/backend/engine/account"
	"github.com/RedHuang-0622/seelex/internal/model"
)

// ── 路由事实观察（RoutingNotice）────────────────────────────────────────────
//
// 口径：换号回退（账号资格失败 → 改用别的账号）与「未固定账号的池内选定」都要
// 投影给观察者——"说的和跑的不是同一个账号"必须可见，不能只落在进程日志里。

// collectingObserver 收集观察到的路由事实（并发安全）。
type collectingObserver struct {
	mu      sync.Mutex
	notices []RoutingNotice
}

func (o *collectingObserver) observe(notice RoutingNotice) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.notices = append(o.notices, notice)
}

func (o *collectingObserver) collected() []RoutingNotice {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]RoutingNotice(nil), o.notices...)
}

// TestStreamingCompleterObservesFailover 钉住：账号资格失败换号时，观察者
// 收到 failover 路由事实（From=失败账号，To=实际承接账号）；首尝试的 request
// 派发钩子照发（每次请求一条，先于 failover 事实）。
func TestStreamingCompleterObservesFailover(t *testing.T) {
	pool := newStreamPool(t, map[string]agent.Completer{
		"agent-1": &staticStreamCompleter{err: errors.New("ChatClient stream: HTTP 402 payment required (insufficient balance)")},
		"agent-2": &staticStreamCompleter{content: "streamed-by-second"},
	})
	observer := &collectingObserver{}
	completer := &streamingAccountCompleter{pool: pool, selector: pinnedTo("agent-1"), observe: observer.observe}

	content, _, _, err := completer.CompleteStream(context.Background(), nil, nil, nil)
	if err != nil || content != "streamed-by-second" {
		t.Fatalf("换号回退结果 = %q err=%v", content, err)
	}
	notices := observer.collected()
	if len(notices) != 2 {
		t.Fatalf("路由事实条数 = %d, want 2（request 钩子 + failover，%+v）", len(notices), notices)
	}
	first, notice := notices[0], notices[1]
	if first.Reason != ReasonRequest || first.To != "agent-1" || first.Role != string(model.RoleAgent) {
		t.Fatalf("首条路由事实 = %+v, want request 钩子（agent-1，主链路 role=agent）", first)
	}
	if notice.Reason != ReasonFailover || notice.From != "agent-1" || notice.To != "agent-2" {
		t.Fatalf("路由事实 = %+v, want failover agent-1 → agent-2", notice)
	}
	if notice.FailureKind != string(account.FailureQuota) {
		t.Fatalf("失败类别 = %q, want quota_exhausted", notice.FailureKind)
	}
	if notice.Model != "test-model" || notice.Provider != "openai" {
		t.Fatalf("路由属性 = %s/%s, want openai/test-model（账号池 Metadata 投影）", notice.Provider, notice.Model)
	}
}

// TestStreamingCompleterObservesUnpinnedSelection 钉住：请求未固定账号且池里
// 不止一个账号时，观察者收到 unpinned_selection（单账号安装不投影——没有路由
// 分叉可提示）；request 派发钩子照发（每次请求一条）。
func TestStreamingCompleterObservesUnpinnedSelection(t *testing.T) {
	pool := newStreamPool(t, map[string]agent.Completer{
		"agent-1":    &staticStreamCompleter{content: "a"},
		"subagent-1": &staticStreamCompleter{content: "b"},
	})
	observer := &collectingObserver{}
	completer := &streamingAccountCompleter{pool: pool, observe: observer.observe}

	if _, _, _, err := completer.CompleteStream(context.Background(), nil, nil, nil); err != nil {
		t.Fatalf("流式调用失败：%v", err)
	}
	notices := observer.collected()
	if len(notices) != 2 {
		t.Fatalf("路由事实条数 = %d, want 2（request 钩子 + unpinned_selection，%+v）", len(notices), notices)
	}
	if notices[1].Reason != ReasonUnpinnedSelection {
		t.Fatalf("Reason = %q, want %q", notices[1].Reason, ReasonUnpinnedSelection)
	}

	// 单账号：无路由分叉，不投影 unpinned_selection；request 钩子仍要发。
	single := newStreamPool(t, map[string]agent.Completer{"agent-1": &staticStreamCompleter{content: "a"}})
	singleObserver := &collectingObserver{}
	singleCompleter := &streamingAccountCompleter{pool: single, observe: singleObserver.observe}
	if _, _, _, err := singleCompleter.CompleteStream(context.Background(), nil, nil, nil); err != nil {
		t.Fatalf("流式调用失败：%v", err)
	}
	singleNotices := singleObserver.collected()
	if len(singleNotices) != 1 || singleNotices[0].Reason != ReasonRequest {
		t.Fatalf("单账号安装应只发 request 钩子，得到 %+v", singleNotices)
	}
}

// TestStreamingCompleterRequestHookRole 钉住 request 派发钩子的角色口径：
// 主链路（无 SceneScope）= agent；节点请求 = SceneScope.Role（subagent）。
func TestStreamingCompleterRequestHookRole(t *testing.T) {
	pool := newStreamPool(t, map[string]agent.Completer{"subagent-1": &staticStreamCompleter{content: "a"}})
	observer := &collectingObserver{}
	completer := &streamingAccountCompleter{pool: pool, observe: observer.observe}

	nodeCtx := dto.WithSceneScope(context.Background(), dto.SceneScope{SceneID: "n1", Role: model.RoleSubAgent})
	if _, _, _, err := completer.CompleteStream(nodeCtx, nil, nil, nil); err != nil {
		t.Fatalf("流式调用失败：%v", err)
	}
	notices := observer.collected()
	if len(notices) != 1 || notices[0].Reason != ReasonRequest {
		t.Fatalf("路由事实 = %+v, want 单条 request 钩子", notices)
	}
	if notices[0].Role != string(model.RoleSubAgent) || notices[0].NodeID != "n1" {
		t.Fatalf("Role/NodeID = %q/%q, want subagent/n1", notices[0].Role, notices[0].NodeID)
	}
}

// TestStreamingCompleterSilentWithoutObserver 钉住：观察者为 nil 时路由事实
// 路径必须完全静默（既有行为零改变）。
func TestStreamingCompleterSilentWithoutObserver(t *testing.T) {
	pool := newStreamPool(t, map[string]agent.Completer{
		"agent-1": &staticStreamCompleter{err: errors.New("ChatClient stream: HTTP 402 payment required")},
		"agent-2": &staticStreamCompleter{content: "streamed-by-second"},
	})
	completer := &streamingAccountCompleter{pool: pool, selector: pinnedTo("agent-1")}
	if content, _, _, err := completer.CompleteStream(context.Background(), nil, nil, nil); err != nil || content != "streamed-by-second" {
		t.Fatalf("换号回退结果 = %q err=%v", content, err)
	}
}
