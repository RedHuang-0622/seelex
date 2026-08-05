package sessionstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"time"

	"github.com/redis/go-redis/v9"
)

// ConversationMessage 是会话语义消息的存储层 DTO（interfaces.md §Conversation
// 模块）。只表达跨后端契约，不依赖 application 包；字段与 UI 的
// application.Message 一一对应。
type ConversationMessage struct {
	ID        string                `json:"id"`
	Role      string                `json:"role"`
	Content   string                `json:"content,omitempty"`
	Tool      *ConversationToolCall `json:"tool,omitempty"`
	CreatedAt time.Time             `json:"created_at"`
}

// ConversationToolCall 是会话消息内工具调用的存储层 DTO。
type ConversationToolCall struct {
	ID        string        `json:"id"`
	Name      string        `json:"name"`
	Arguments string        `json:"arguments,omitempty"`
	Result    string        `json:"result,omitempty"`
	Error     string        `json:"error,omitempty"`
	Status    string        `json:"status"`
	Duration  time.Duration `json:"duration,omitempty"`
}

// decodeConversationRange 从 state blob 只解析 conversation 模块（不解析
// Plan/Execution/Projection 等非 conversation 子树），按 offset/limit 切页。
// 兼容 v1 SessionArchive（conversation 直接是数组）与 v2/v3 SessionRecord
// （conversation.messages）；损坏或版本不兼容显式失败，不静默成空历史。
func decodeConversationRange(payload []byte, sessionID string, offset, limit int) ([]ConversationMessage, int, error) {
	var header struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(payload, &header); err != nil {
		return nil, 0, fmt.Errorf("session storage: decode state header: %w", err)
	}
	var messages []ConversationMessage
	switch header.Version {
	case 1: // SessionArchive：conversation 直接是数组。
		var archive struct {
			Conversation []ConversationMessage `json:"conversation,omitempty"`
		}
		if err := json.Unmarshal(payload, &archive); err != nil {
			return nil, 0, fmt.Errorf("session storage: decode legacy conversation: %w", err)
		}
		messages = archive.Conversation
	case 2, 3: // SessionRecord：conversation.messages。
		var record struct {
			ID           string `json:"id"`
			Conversation struct {
				Messages []ConversationMessage `json:"messages,omitempty"`
			} `json:"conversation"`
		}
		if err := json.Unmarshal(payload, &record); err != nil {
			return nil, 0, fmt.Errorf("session storage: decode conversation module: %w", err)
		}
		if record.ID != sessionID {
			return nil, 0, fmt.Errorf("session storage: conversation session %q does not match %q", record.ID, sessionID)
		}
		messages = record.Conversation.Messages
	default:
		return nil, 0, fmt.Errorf("session storage: unsupported state version %d", header.Version)
	}
	total := len(messages)
	if offset < 0 {
		offset = 0
	}
	if offset > total {
		offset = total
	}
	end := offset + limit
	if limit <= 0 || end > total {
		end = total
	}
	return append([]ConversationMessage(nil), messages[offset:end]...), total, nil
}

func (repository *jsonRepository) ReadConversationRange(_ context.Context, key Key, offset, limit int) ([]ConversationMessage, int, error) {
	if err := key.validate(); err != nil {
		return nil, 0, err
	}
	repository.mu.RLock()
	defer repository.mu.RUnlock()
	payload, err := repository.readCurrentStateLocked(repository.sessionDir(key))
	if err != nil {
		return nil, 0, err
	}
	return decodeConversationRange(payload, key.SessionID, offset, limit)
}

func (repository *sqlRepository) ReadConversationRange(ctx context.Context, key Key, offset, limit int) ([]ConversationMessage, int, error) {
	if err := key.validate(); err != nil {
		return nil, 0, err
	}
	var payload string
	query := `SELECT state_json FROM seelex_session_state WHERE project_id=` + repository.arg(1) + ` AND session_id=` + repository.arg(2)
	if err := repository.db.QueryRowContext(ctx, query, key.ProjectID, key.SessionID).Scan(&payload); err != nil {
		return nil, 0, err
	}
	return decodeConversationRange([]byte(payload), key.SessionID, offset, limit)
}

func (repository *redisRepository) ReadConversationRange(ctx context.Context, key Key, offset, limit int) ([]ConversationMessage, int, error) {
	if err := key.validate(); err != nil {
		return nil, 0, err
	}
	payload, err := repository.client.Get(ctx, repository.stateKey(key)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, 0, fs.ErrNotExist
	}
	if err != nil {
		return nil, 0, err
	}
	return decodeConversationRange(payload, key.SessionID, offset, limit)
}
