package ssh

// probe_cache.go —— 带缓存的探针：把"连通性结论"变成一次调用可以白读的一格。
//
// 为什么必须有缓存：传输选择是**每次工具调用**都要做一次的判断，而探测是一次真实
// 握手（TCP + 认证，秒级）。不缓存等于每条 bash 命令前都先握一次手——把工具调用
// 的成本翻倍，还会让远端 sshd 里堆满只连不跑的空会话。
//
// 三态语义（与 registry 的 TransportProbeState 同源）：
//   - 没条目（或用例自己说"没探过"）= **未探**：选择器不替人探，按本地执行并留痕；
//   - 条目在 TTL 内 = 有结论（ok / failed），选择器据此决定；
//   - 过期 = 视同没条目（结论会腐烂：网络通了、key 换了、主机重启了）。
//
// 并发：Prober 自带锁，可被多个会话同时调用；不同端点各占一个键，互不污染。

import (
	"context"
	"sync"
	"time"
)

// DefaultProbeTTL 是探测结论的默认寿命。
//
// 60s 的依据：它要短于"人发现连不上并去修"的时间尺度（否则修好了还得再等），又要
// 长于一次任务里连续几十条命令的间隔（否则缓存在实际使用中等于没有）。
const DefaultProbeTTL = 60 * time.Second

// probeEntry 是一条缓存的结论。
type probeEntry struct {
	result ProbeResult
	at     time.Time
}

// Prober 是带 TTL 的探针缓存。
type Prober struct {
	mu      sync.Mutex
	ttl     time.Duration
	probe   func(ctx context.Context, spec Spec) ProbeResult
	now     func() time.Time
	entries map[string]probeEntry
}

// NewProber 造一个探针缓存（probe 为 nil 时用本包的 Probe；ttl<=0 取默认）。
func NewProber(ttl time.Duration, probe func(ctx context.Context, spec Spec) ProbeResult) *Prober {
	if ttl <= 0 {
		ttl = DefaultProbeTTL
	}
	if probe == nil {
		probe = Probe
	}
	return &Prober{ttl: ttl, probe: probe, now: time.Now, entries: map[string]probeEntry{}}
}

// ProbeKey 是缓存键：端点 + 用户（同一台机器换个用户是另一次认证）。
func ProbeKey(spec Spec) string { return spec.Addr() + "|" + spec.User }

// Cached 只读缓存（ok=false = 未探或已过期）。它**不发网络请求**。
func (p *Prober) Cached(spec Spec) (ProbeResult, bool) {
	if p == nil {
		return ProbeResult{}, false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	entry, ok := p.entries[ProbeKey(spec)]
	if !ok {
		return ProbeResult{}, false
	}
	if p.now().Sub(entry.at) > p.ttl {
		delete(p.entries, ProbeKey(spec))
		return ProbeResult{}, false
	}
	return entry.result, true
}

// ProbeNow 当场探一次并写缓存（显式指名者付的那一次）。
func (p *Prober) ProbeNow(ctx context.Context, spec Spec) ProbeResult {
	if p == nil {
		return Probe(ctx, spec)
	}
	result := p.probe(ctx, spec)
	p.mu.Lock()
	p.entries[ProbeKey(spec)] = probeEntry{result: result, at: p.now()}
	p.mu.Unlock()
	return result
}

// Invalidate 丢掉一条结论（端点/凭据变更后由装配层调用）。
func (p *Prober) Invalidate(spec Spec) {
	if p == nil {
		return
	}
	p.mu.Lock()
	delete(p.entries, ProbeKey(spec))
	p.mu.Unlock()
}
