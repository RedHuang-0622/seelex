package ssh

// transport.go —— 传输执行面：把"一次调用跑一条命令"接到本包的连接基座。
//
// 生态位刻意很窄：registry 的传输选择器选中 ssh 之后，拿到的是这里的一个值，
// 它的 Run 就是"新连一次 → 跑一条 → 关掉"。为什么每次调用都新连（不复用连接）：
// Client 明确非并发安全（一条连接 = 一个远端会话通道），而工具调用是全并发的；
// 复用得再仔细，也会把"两条命令互相踩"变成偶发故障，代价远大于一次握手的开销。
//
// 命令来源的纪律（与 exec.go 同一条，不在调用方复制）：
//   - 单行 ASCII → Inline（唯一的"一次性参数"形态）；
//   - 多行或含非 ASCII → Stdin（内容通道，字节透明，绕开三层代码页与引号）；
//   - 空命令 → 硬失败（不是"跑了个空脚本成功"）。

import (
	"context"
	"strings"
)

// Backend 是 ssh 执行面：一组连接参数 + 一次一次的连接。
//
// 它不是 registry.Transport 的实现，而是它的**底座**：接口适配在 registry 侧
// （registry/ssh_transport.go），因为本包不许反向依赖 backend/tool/registry
// （见 README 的"依赖方向"）。
type Backend struct {
	// Spec 是一条连接的完整输入（项目连接配置折出来的那一行，不含口令本体）。
	Spec Spec
}

// NewBackend 造一个 ssh 执行面。
func NewBackend(spec Spec) *Backend { return &Backend{Spec: spec} }

// Run 执行一条命令：新连一次、跑完关掉。连接期的失败带分段归因（DialError），
// 由上层翻成固定错误码；执行期的失败按 Result.Status 三态回报（不判业务成功）。
func (b *Backend) Run(ctx context.Context, command, workdir string) (Result, error) {
	req, err := commandRequest(command, workdir)
	if err != nil {
		return Result{Status: StatusFailed, ExitCode: -1}, err
	}
	client, err := Dial(ctx, b.Spec)
	if err != nil {
		return Result{Status: StatusFailed, ExitCode: -1}, err
	}
	defer func() { _ = client.Close() }()
	return client.Run(ctx, req)
}

// Probe 探一次（只连不写）：选择器要的就是这一个结论。
func (b *Backend) Probe(ctx context.Context) ProbeResult { return Probe(ctx, b.Spec) }

// commandRequest 把"脚本文本"归一成一次执行请求（命令来源纪律的唯一实现）。
//
// 判据用"是否需要内容通道"而不是"用户喜欢哪种"：非 ASCII 与换行经一次性参数会被
// 多层代码页/引号转换**静默**损坏，而损坏后的脚本仍能被远端解析——那是最贵的一类
// 事故（docs/arch/ssh-tool.md §9 的硬约束 1）。
func commandRequest(command, workdir string) (Request, error) {
	if strings.TrimSpace(command) == "" {
		return Request{}, codeErr(CodeNoCommand, "命令正文为空：没有任何东西可执行")
	}
	req := Request{Workdir: workdir}
	if isInlineSafe(command) {
		req.Inline = command
		return req, nil
	}
	// 多行 / 含非 ASCII：走 stdin（字节透明，CRLF 与中文都原样到对端）。
	req.Stdin = command
	return req, nil
}

// isInlineSafe 报告命令能否安全地走内联（单行 + 全 ASCII + 不含 NUL）。
func isInlineSafe(command string) bool {
	if strings.ContainsAny(command, "\r\n\x00") {
		return false
	}
	for _, r := range command {
		if r > 0x7f {
			return false
		}
	}
	return true
}
