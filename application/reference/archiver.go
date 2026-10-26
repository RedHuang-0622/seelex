// 压缩轮次读回句柄（read_compressed_turn）：
// 窗口外轮次被压缩为 Summary 后，原文经 TurnArchiver 持久化到会话存储
// （ToolResults 通道，ref = "compressed:"+segmentID）；模型需要细节时经
// read_compressed_turn 工具读回原文——压缩丢失可逆，减少对摘要的幻觉。
// 读方法根据聊天记录的存储（store）装配：写 = SaveCommitWorkspace（显式
// 项目作用域），读 = LoadToolResultWorkspace（与 read_tool_result 同一持久化通道）。
package reference

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/RedHuang-0622/Seele/types"
	task_context "github.com/RedHuang-0622/seelex/application/execution/taskcontext"
	"github.com/RedHuang-0622/seelex/contract"
	"strings"
)

// CompressedTurnRefPrefix is the durable ToolResults namespace for compressed
// turn bodies.
const CompressedTurnRefPrefix = contract.CompressedTurnRefPrefix

// sessionCommitPort 是压缩轮次原文持久化的写通道：在**显式项目作用域**下追加
// 一个只含 ToolResults 的 commit（append-only，ref = "compressed:"+segmentID）。
//
// 为什么必须是显式作用域：`SaveCommit(sessionID, commit)` 走的是 Router 的活跃
// 写作用域，而活跃作用域是**视图**状态——切项目就变。后台会话的原文会被写进
// 另一个项目（R3 键漂移），而读面用的是显式键
// LoadToolResultWorkspace(workspaceID, sessionID, ref)：两侧必须同键。
type CompressedTurnStore interface {
	SaveCompressedTurn(projectID, sessionID, ref, content string) error
}

// CompressedTurnArchiver 实现 seelexctx.TurnArchiver：溢出轮次原文序列化
// 后经 session 管理器显式作用域落盘（ToolResults 通道，append-only，
// ref = "compressed:"+segmentID）。
type CompressedTurnArchiver struct {
	// Store is the narrow durable write port; Wiring adapts the session manager
	// to this interface without exposing store payloads to Application.
	Store CompressedTurnStore
	// SessionIDProvider 是兜底归属：仅在 ctx 未携带会话 ID 时使用（装配期
	// 预热、非回合路径）。运行中的会话归属一律以 ctx 为准 —— provider 返回
	// 的是**视图**会话，多会话并行时按它落盘会把后台会话的原文写进别的会话。
	SessionIDProvider func() string
	// ProjectIDProvider 回答"这个会话的数据落在哪个项目"（按数据实际所在解析，
	// 不按视图活跃作用域猜）；返回空串时退回 WorkspaceIDProvider。
	ProjectIDProvider func(sessionID string) string
	// WorkspaceIDProvider 是兜底项目作用域：与读面
	// session_runtime.WorkspaceID(Snapshot.CurrentWorkspace) 取的是同一个值，
	// 保证"写进去的键"就是"读回来的键"。
	WorkspaceIDProvider func() string
}

// resolveProjectID 解析落盘的项目作用域：先按会话自己的绑定（数据实际所在），
// 再退回视图当前工作区。两者都为空 = 默认项目（与读面 "" 一致）。
func (a *CompressedTurnArchiver) resolveProjectID(sessionID string) string {
	if a.ProjectIDProvider != nil {
		if projectID := strings.TrimSpace(a.ProjectIDProvider(sessionID)); projectID != "" {
			return projectID
		}
	}
	if a.WorkspaceIDProvider != nil {
		return strings.TrimSpace(a.WorkspaceIDProvider())
	}
	return ""
}

// StoreTurn 实现 seelexctx.TurnArchiver。会话归属优先取 ctx（runChat 注入
// 的会话 ID，Seele loop 原样透传给上下文控制器），provider 仅兜底。
func (a *CompressedTurnArchiver) StoreTurn(ctx context.Context, segmentID string, messages []types.Message) (string, error) {
	if a.Store == nil {
		return "", errors.New("read_compressed_turn: durable commit storage is unavailable")
	}
	sessionID := strings.TrimSpace(task_context.SessionIDFromContext(ctx))
	if sessionID == "" && a.SessionIDProvider != nil {
		sessionID = strings.TrimSpace(a.SessionIDProvider())
	}
	if sessionID == "" {
		return "", errors.New("read_compressed_turn: session ID is unavailable for this compression")
	}
	data, err := json.Marshal(messages)
	if err != nil {
		return "", fmt.Errorf("read_compressed_turn: marshal turns: %w", err)
	}
	ref := CompressedTurnRefPrefix + segmentID
	if err := a.Store.SaveCompressedTurn(a.resolveProjectID(sessionID), sessionID, ref, string(data)); err != nil {
		return "", fmt.Errorf("read_compressed_turn: persist: %w", err)
	}
	return ref, nil
}
