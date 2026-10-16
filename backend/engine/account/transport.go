package account

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// 流式账号 client 的两道传输层看门狗（2026-09-29 事故修复）。
//
// 事故：seelebridge 用 api.NewChatClient(Timeout: 300) 构造账号 client，而
// api.NewChatClient 把它变成 http.Client.Timeout——**整请求 wall-clock 上限，
// 含 SSE body 读**。现场：22:02:33 起的一条流到 22:07:33 恰好满 300s 被 net/http
// 的 cancelTimerBody 在读 body 时砍断，报出
//
//	ChatClient stream: read SSE: context deadline exceeded
//	(Client.Timeout or context cancellation while reading body)
//
// 这句措辞区分不了「上游真挂了」与「上游仍在正常推进、只是慢」——正常的长推理 /
// 大 tool_call 参数生成同样会被判死（假故障），且失败会升级成整个节点失败。
//
// 修复：账号 client 不再设整请求上限，改由这里两道**进度敏感**的看门狗保护：
//   - streamHeaderTimeout：等响应头的上限（连接/上游受理阶段）；
//   - streamIdleTimeout：两次 body 数据之间允许的最长空闲——只要流还在推进就永不
//     触发；真正停滞的流仍在同一量级（300s）内失败，且报错自带语义、可定位。
//
// 天花板仍在别处：fork/plan 批次有 limits.fork_timeout（默认 2h），工具调用有
// tool_call_timeout——这里只负责「一条流不能无声无息地挂住」。
var (
	streamHeaderTimeout = 60 * time.Second
	streamIdleTimeout   = 300 * time.Second
)

// errStreamStalled 是流空闲看门狗的哨兵错误：文本自带「无数据」语义，经 api 层
// "ChatClient stream: read SSE: %w" 与 stream.go 再包装后依然可读。
var errStreamStalled = errors.New("seelebridge: LLM stream stalled (no data received)")

// newStreamTransport 在 base 上叠加响应头超时与流空闲看门狗。base 为 *http.Transport
// 时克隆后设置 ResponseHeaderTimeout（不污染调用方传入的传输层）。
func newStreamTransport(base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	if transport, ok := base.(*http.Transport); ok {
		clone := transport.Clone()
		clone.ResponseHeaderTimeout = streamHeaderTimeout
		base = clone
	}
	return &idleWatchdogTransport{base: base}
}

type idleWatchdogTransport struct{ base http.RoundTripper }

func (t *idleWatchdogTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := t.base.RoundTrip(request)
	if err != nil || response == nil || response.Body == nil {
		return response, err
	}
	response.Body = newIdleWatchdogBody(response.Body, streamIdleTimeout)
	return response, nil
}

// idleWatchdogBody 在每次 Read 前装一只定时器（与 net/http 的 cancelTimerBody 同构，
// 但判据是「帧间空闲」而不是「请求总时长」）：Read 一返回即停表；空闲超过 idle 才
// 关闭底层连接并让该次 Read 报出可读错误。
type idleWatchdogBody struct {
	body     io.ReadCloser
	idle     time.Duration
	mu       sync.Mutex
	timer    *time.Timer
	timedOut bool
}

func newIdleWatchdogBody(body io.ReadCloser, idle time.Duration) *idleWatchdogBody {
	return &idleWatchdogBody{body: body, idle: idle}
}

func (b *idleWatchdogBody) Read(buffer []byte) (int, error) {
	if b.idle <= 0 {
		return b.body.Read(buffer)
	}
	b.mu.Lock()
	if b.timedOut {
		b.mu.Unlock()
		return 0, b.stalledError()
	}
	// 复用同一只定时器（Reset 而非每次 Read 新建）：SSE 每次 Read 都建表会在热路径
	// 上产生无谓分配。
	if b.timer == nil {
		b.timer = time.AfterFunc(b.idle, b.markStalled)
	} else {
		b.timer.Reset(b.idle)
	}
	b.mu.Unlock()

	read, err := b.body.Read(buffer)

	b.mu.Lock()
	if b.timer != nil {
		b.timer.Stop()
	}
	timedOut := b.timedOut
	b.mu.Unlock()
	// 数据到达即算「活着」：即使 Stop 与定时器触发撞上（窄竞态）也不丢这一帧，
	// 下一次 Read 真无数据时会再次命中看门狗。
	if read > 0 {
		return read, err
	}
	if timedOut {
		return 0, b.stalledError()
	}
	return read, err
}

// markStalled 关闭底层 body，让正在阻塞的 Read 立即返回（错误文本由 Read 统一改写）。
func (b *idleWatchdogBody) markStalled() {
	b.mu.Lock()
	b.timedOut = true
	b.mu.Unlock()
	_ = b.body.Close()
}

func (b *idleWatchdogBody) stalledError() error {
	return fmt.Errorf("%w after %s", errStreamStalled, b.idle)
}

func (b *idleWatchdogBody) Close() error {
	b.mu.Lock()
	if b.timer != nil {
		b.timer.Stop()
	}
	b.mu.Unlock()
	return b.body.Close()
}
