// 压缩轮次读回句柄（read_compressed_turn）：
// 窗口外轮次被压缩为 Summary 后，原文经 TurnArchiver 持久化到会话存储
// （ToolResults 通道，ref = "compressed:"+segmentID）；模型需要细节时经
// read_compressed_turn 工具读回原文——压缩丢失可逆，减少对摘要的幻觉。
// 读方法根据聊天记录的存储（sessionstore）装配：写 = SaveCommit，读 =
// LoadToolResultWorkspace（与 read_tool_result 同一持久化通道）。
package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/RedHuang-0622/Seele/types"

	"github.com/RedHuang-0622/seelex/sessionstore"
)

// compressedTurnRefPrefix 是压缩轮次原文在 ToolResults 通道中的 ref 前缀。
const compressedTurnRefPrefix = "compressed:"

// sessionCommitPort 是压缩轮次原文持久化的写通道（session.Manager 满足）。
type sessionCommitPort interface {
	SaveCommit(sessionID string, commit sessionstore.Commit) error
}

// CompressedTurnArchiver 实现 seelexctx.TurnArchiver：溢出轮次原文序列化
// 后经 session 管理器 SaveCommit 持久化（ToolResults 通道，append-only，
// ref = "compressed:"+segmentID）。
type CompressedTurnArchiver struct {
	// Sessions 提供写通道（SaveCommit），由装配方注入（session.Manager /
	// 应用服务满足；内部断言 sessionCommitPort）。
	Sessions any
	// SessionIDProvider 提供当前会话 ID（会话恢复/新建后动态变化）。
	SessionIDProvider func() string
}

// StoreTurn 实现 seelexctx.TurnArchiver。
func (a *CompressedTurnArchiver) StoreTurn(_ context.Context, segmentID string, messages []types.Message) (string, error) {
	store, ok := a.Sessions.(sessionCommitPort)
	if !ok {
		return "", errors.New("read_compressed_turn: durable commit storage is unavailable")
	}
	if a.SessionIDProvider == nil {
		return "", errors.New("read_compressed_turn: session ID provider is unavailable")
	}
	data, err := json.Marshal(messages)
	if err != nil {
		return "", fmt.Errorf("read_compressed_turn: marshal turns: %w", err)
	}
	ref := compressedTurnRefPrefix + segmentID
	commit := sessionstore.Commit{ToolResults: []sessionstore.ToolResult{{
		Ref: ref, Tool: "compact_frame", Content: string(data), Size: len(data),
	}}}
	if err := store.SaveCommit(a.SessionIDProvider(), commit); err != nil {
		return "", fmt.Errorf("read_compressed_turn: persist: %w", err)
	}
	return ref, nil
}

// ReadCompressedTurnHandler 读回一次压缩的轮次原文（分页 + 过滤）。
// 入参：segment_id（必选）、offset/limit/contains（分页，与 read_tool_result
// 同款语义）。
func (service *Service) ReadCompressedTurnHandler(_ context.Context, argsJSON string) (string, error) {
	var input struct {
		SegmentID string `json:"segment_id"`
		Offset    int    `json:"offset,omitempty"`
		Limit     int    `json:"limit,omitempty"`
		Contains  string `json:"contains,omitempty"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
		return "", fmt.Errorf("read_compressed_turn: invalid JSON: %w", err)
	}
	input.SegmentID = strings.TrimSpace(input.SegmentID)
	if input.SegmentID == "" || input.Offset < 0 {
		return "", errors.New("read_compressed_turn: segment_id is required and offset must be non-negative")
	}
	if input.Limit <= 0 {
		input.Limit = Limits().ReferencePageSize
	}
	if max := Limits().MaxReferencePageSize; max > 0 && input.Limit > max {
		input.Limit = max
	}

	service.mu.RLock()
	sessionID := service.snapshot.Session.ID
	currentWorkspaceID := workspaceID(service.snapshot.CurrentWorkspace)
	service.mu.RUnlock()

	store, ok := service.deps.Sessions.(sessionTranscriptPort)
	if !ok {
		return "", errors.New("read_compressed_turn: durable storage is unavailable")
	}
	result, err := store.LoadToolResultWorkspace(
		currentWorkspaceID,
		sessionID,
		compressedTurnRefPrefix+input.SegmentID,
	)
	if err != nil {
		return "", fmt.Errorf("read_compressed_turn: %w", err)
	}
	var messages []types.Message
	if err := json.Unmarshal([]byte(result.Content), &messages); err != nil {
		return "", fmt.Errorf("read_compressed_turn: decode stored turns: %w", err)
	}
	rendered := renderCompressedTurns(messages)
	return pageCompressedTurns(rendered, input.Offset, input.Limit, input.Contains)
}

// renderCompressedTurns 把轮次原文渲染为可读文本（按角色标记，工具链
// 与结果成对呈现）。
func renderCompressedTurns(messages []types.Message) string {
	var builder strings.Builder
	for _, message := range messages {
		content := ""
		if message.Content != nil {
			content = *message.Content
		}
		switch {
		case message.Role == "user":
			builder.WriteString("[user] ")
			builder.WriteString(content)
		case message.Role == "assistant" && len(message.ToolCalls) == 0:
			builder.WriteString("[assistant] ")
			builder.WriteString(content)
		case message.Role == "assistant":
			builder.WriteString("[assistant tools] ")
			names := make([]string, 0, len(message.ToolCalls))
			for _, call := range message.ToolCalls {
				names = append(names, call.Function.Name)
			}
			builder.WriteString(strings.Join(names, ", "))
		case message.Role == "tool":
			builder.WriteString("[tool ")
			builder.WriteString(message.Name)
			builder.WriteString("] ")
			builder.WriteString(content)
		default:
			builder.WriteString("[")
			builder.WriteString(message.Role)
			builder.WriteString("] ")
			builder.WriteString(content)
		}
		builder.WriteByte('\n')
	}
	return strings.TrimRight(builder.String(), "\n")
}

// pageCompressedTurns 按 offset/limit（字符）/contains 过滤渲染文本分页。
func pageCompressedTurns(rendered string, offset, limit int, contains string) (string, error) {
	if contains != "" {
		lines := strings.Split(rendered, "\n")
		filtered := make([]string, 0, len(lines))
		for _, line := range lines {
			if strings.Contains(line, contains) {
				filtered = append(filtered, line)
			}
		}
		rendered = strings.Join(filtered, "\n")
	}
	if offset >= utf8.RuneCountInString(rendered) {
		return "", errors.New("read_compressed_turn: offset exceeds stored content")
	}
	runes := []rune(rendered)
	if offset >= len(runes) {
		return "", errors.New("read_compressed_turn: offset exceeds stored content")
	}
	end := offset + limit
	if end > len(runes) {
		end = len(runes)
	}
	page := string(runes[offset:end])
	if end < len(runes) {
		page += "\n...[truncated; use read_compressed_turn with offset to continue]"
	}
	return page, nil
}
