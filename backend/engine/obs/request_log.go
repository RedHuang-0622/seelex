// Package seelebridge 的 LLM 请求记录中间件（诊断/冒烟用，默认关闭）。
//
// 用途：在真实进程里记录“每次实际发给 provider 的请求”的顺序与内容指纹
// （role + content + tool-call 的 sha256，不落原文），供 headless 冒烟把
// 运行期请求顺序与落盘/重启恢复后的存储内容做匹配（2026-09-08 上下文
// 恢复复查）。仅当设置 SEELEX_REQUEST_LOG=<jsonl 路径> 时启用并包装
// Completer/StreamCompleter；未设置时零开销、不改变任何装配行为。
package obs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/RedHuang-0622/Seele/agent"
	"github.com/RedHuang-0622/Seele/types"

	seeletelemetry "github.com/RedHuang-0622/seelex/internal/telemetry"
)

// RequestLogEnv 是启用请求记录的文件路径环境变量（JSONL append）。
const RequestLogEnv = "SEELEX_REQUEST_LOG"

// requestMessageDigest 是单条消息的内容指纹（只含 role + hash + 工具名，
// 不落任何正文/参数原文，避免诊断日志泄漏会话内容）。
type requestMessageDigest struct {
	Role      string   `json:"role"`
	Hash      string   `json:"hash"`
	ToolCalls []string `json:"tool_calls,omitempty"`
}

// requestLogEntry 是一次 LLM 请求的顺序与内容指纹记录。
type requestLogEntry struct {
	Pid       int                    `json:"pid"`
	Seq       uint64                 `json:"seq"`
	At        string                 `json:"at"`
	SessionID string                 `json:"session_id,omitempty"`
	Messages  []requestMessageDigest `json:"messages"`
	ToolNames []string               `json:"tool_names,omitempty"`
}

// RequestLogger 是线程安全的 JSONL 记录器（一次请求一行）。
type RequestLogger struct {
	mu     sync.Mutex
	file   *os.File
	enc    *json.Encoder
	seq    uint64
	closed bool
}

func OpenRequestLogger(path string) (*RequestLogger, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("request log path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	return &RequestLogger{file: file, enc: json.NewEncoder(file)}, nil
}

func (l *RequestLogger) record(ctx context.Context, messages []types.Message, tools []types.Tool) {
	if l == nil {
		return
	}
	entry := requestLogEntry{
		Pid:       os.Getpid(),
		At:        time.Now().Format(time.RFC3339Nano),
		SessionID: seeletelemetry.SessionIDFromContext(ctx),
		Messages:  make([]requestMessageDigest, 0, len(messages)),
		ToolNames: make([]string, 0, len(tools)),
	}
	for _, message := range messages {
		entry.Messages = append(entry.Messages, digestRequestMessage(message))
	}
	for _, tool := range tools {
		entry.ToolNames = append(entry.ToolNames, tool.Function.Name)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return
	}
	l.seq++
	entry.Seq = l.seq
	_ = l.enc.Encode(entry)
}

func (l *RequestLogger) Close() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	l.closed = true
	return l.file.Close()
}

// digestRequestMessage 生成与存储侧同源的消息指纹：role + 归一化正文 +
// tool call（id/name/arguments）。归一化把 provider 恢复注记还原为空，
// 避免“落盘为空、wire 被补恢复文本”造成伪 mismatch。
func digestRequestMessage(message types.Message) requestMessageDigest {
	digest := requestMessageDigest{Role: message.Role}
	content := ""
	if message.Content != nil {
		content = *message.Content
	}
	for _, call := range message.ToolCalls {
		digest.ToolCalls = append(digest.ToolCalls, call.ID+":"+call.Function.Name)
	}
	var builder strings.Builder
	builder.WriteString(message.Role)
	builder.WriteByte(0)
	builder.WriteString(normalizeWireContent(content))
	for _, call := range message.ToolCalls {
		builder.WriteByte(0)
		builder.WriteString(call.ID)
		builder.WriteByte(0)
		builder.WriteString(call.Function.Name)
		builder.WriteByte(0)
		builder.WriteString(call.Function.Arguments)
	}
	sum := sha256.Sum256([]byte(builder.String()))
	digest.Hash = hex.EncodeToString(sum[:])
	return digest
}

// normalizeWireContent 把 provider 恢复注记与内部控制标记还原为空内容，
// 与存储侧 transcript 的空 content 对齐。
func normalizeWireContent(content string) string {
	content = strings.TrimSpace(content)
	if content == "" || strings.HasPrefix(content, "<!-- seelex:") {
		return ""
	}
	if strings.HasPrefix(content, "[Seelex recovery note:") {
		return ""
	}
	return content
}

// RequestLogCompleter 包装同步 Completer。
type RequestLogCompleter struct {
	inner agent.Completer
	log   *RequestLogger
}

// NewRequestLogCompleter 包装一个同步 Completer：每轮先记一次请求画像，再转调原实现。
func NewRequestLogCompleter(inner agent.Completer, log *RequestLogger) *RequestLogCompleter {
	return &RequestLogCompleter{inner: inner, log: log}
}

func (c *RequestLogCompleter) Complete(ctx context.Context, messages []types.Message, tools []types.Tool) (types.Message, error) {
	c.log.record(ctx, messages, tools)
	return c.inner.Complete(ctx, messages, tools)
}

// RequestLogStreamCompleter 包装流式 StreamCompleter。
type RequestLogStreamCompleter struct {
	inner agent.StreamCompleter
	log   *RequestLogger
}

// NewRequestLogStreamCompleter 包装一个流式 Completer（同上，记录点在所有 provider 调用之前）。
func NewRequestLogStreamCompleter(inner agent.StreamCompleter, log *RequestLogger) *RequestLogStreamCompleter {
	return &RequestLogStreamCompleter{inner: inner, log: log}
}

func (c *RequestLogStreamCompleter) CompleteStream(
	ctx context.Context,
	messages []types.Message,
	tools []types.Tool,
	onChunk func(string),
) (string, string, []types.ToolCall, error) {
	c.log.record(ctx, messages, tools)
	return c.inner.CompleteStream(ctx, messages, tools, onChunk)
}

var (
	_ agent.Completer       = (*RequestLogCompleter)(nil)
	_ agent.StreamCompleter = (*RequestLogStreamCompleter)(nil)
)
