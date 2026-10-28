package store

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// session_ssh_state.go 是「会话级 SSH 状态」的持久化：**当前限定的远端环境** +
// **会话里被放权的环境集合**。
//
// 口径（2026-10-23 用户定调，取代早前"会话自带一条 SSH 连接"的设想）：
//   - SSH 环境表挂在**项目**上（dev / test / prod …），会话不持有连接信息；
//   - 会话只持有两件事：现在限定用哪个环境、以及用户在这个会话里放权了哪些环境；
//   - **缺省什么都不给**：没有当前环境、没有被放权的环境 = 这个会话用不了 SSH。
//     SSH 是用户在会话里推动的放权，不是默认能力（失败方向必须是收紧）。
//
// 存法：项目目录下 `session-ssh.jsonl`，**追加写、按行回放、同一会话后写的行覆盖
// 先写的**。为什么不是 JSON 全量重写：这是权限状态，需要留下"谁在什么时候放权 /
// 收回"的行迹；全量重写会把历史抹掉，事后无法复原。
//
// 并发：进程内一把按项目分配的写锁（同 RolePermissionStore 的 fileMu 口径），
// 追加用 O_APPEND 单次写入（小于 PIPE_BUF 量级的一行），不会与其它写者交错成半行。

// sessionSSHFileName 是会话 SSH 状态的文件名（项目目录下）。
const sessionSSHFileName = "session-ssh.jsonl"

const (
	sessionSSHDirMode  = 0o700
	sessionSSHFileMode = 0o600
)

// SessionSSHState 是**一个会话**的 SSH 状态快照（回放后的最新一条）。
//
// 字段语义：
//   - CurrentEnv：会话当前限定的环境名；空 = 没有限定（用不了 SSH）。
//   - AllowedEnvs：本会话被放权的环境名集合；空 = 一个都没放权。
//   - UpdatedAt：最后一次变更时间（回放时用它判"后写的行"）。
type SessionSSHState struct {
	SessionID   string    `json:"session_id"`
	CurrentEnv  string    `json:"current_env,omitempty"`
	AllowedEnvs []string  `json:"allowed_envs,omitempty"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// SessionSSHStateStore 读写会话级 SSH 状态（一个项目一份 JSONL 文件）。
type SessionSSHStateStore struct {
	router *Router
	mu     sync.Mutex
}

// NewSessionSSHStateStore 构造会话 SSH 状态存储（router 为 nil 时读返回空状态、
// 写显式失败——没装配存储不许假装写成功）。
func NewSessionSSHStateStore(router *Router) *SessionSSHStateStore {
	return &SessionSSHStateStore{router: router}
}

// State 读一个会话的 SSH 状态（没有记录 → 零值状态 + 空环境，即"用不了 SSH"）。
func (store *SessionSSHStateStore) State(projectID, sessionID string) (SessionSSHState, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return SessionSSHState{}, errors.New("session ssh: session id is required")
	}
	states, err := store.replay(projectID)
	if err != nil {
		return SessionSSHState{}, err
	}
	state := states[sessionID]
	if state.SessionID == "" {
		return SessionSSHState{SessionID: sessionID, AllowedEnvs: []string{}}, nil
	}
	if state.AllowedEnvs == nil {
		state.AllowedEnvs = []string{}
	}
	return state, nil
}

// Put 追加一条状态变更（整份状态写一行：调用方给出"改完之后是什么样"）。
//
// 整份而不是增量（+env / -env）的原因：增量行要回放两条才知道结果，而权限状态的
// 读者只需要"现在是什么"；整份写下让回放变成"取最后一行"，也没有"减一个不存在的
// 环境"这类边界。
func (store *SessionSSHStateStore) Put(projectID string, state SessionSSHState) error {
	sessionID := strings.TrimSpace(state.SessionID)
	if sessionID == "" {
		return errors.New("session ssh: session id is required")
	}
	if store == nil || store.router == nil {
		return errors.New("session ssh: state storage is not assembled")
	}
	record := SessionSSHState{
		SessionID:   sessionID,
		CurrentEnv:  strings.TrimSpace(state.CurrentEnv),
		AllowedEnvs: normalizeEnvNames(state.AllowedEnvs),
		UpdatedAt:   time.Now().UTC(),
	}
	payload, err := json.Marshal(record)
	if err != nil {
		return err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.router.withRepositoryAt(projectID, func(repository Repository, resolvedProjectID string) error {
		layout, ok := repository.(*jsonRepository)
		if !ok {
			return errors.New("session ssh: state storage requires session layout")
		}
		path := filepath.Join(layout.projectDir(resolvedProjectID), sessionSSHFileName)
		if err := os.MkdirAll(filepath.Dir(path), sessionSSHDirMode); err != nil {
			return err
		}
		file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, sessionSSHFileMode)
		if err != nil {
			return err
		}
		defer file.Close()
		_, err = file.Write(append(payload, '\n'))
		return err
	})
}

// replay 回放整份文件，按会话取**最后一行**（后写覆盖先写）。
//
// 坏行一律跳过（半行 / 手改坏 / 旧版本）：一份坏行不该让整个会话的 SSH 状态读不出来。
// 读不出来时状态为零值——方向是收紧（会话用不了 SSH），不是放开。
func (store *SessionSSHStateStore) replay(projectID string) (map[string]SessionSSHState, error) {
	states := map[string]SessionSSHState{}
	if store == nil || store.router == nil {
		return states, nil
	}
	var payload []byte
	err := store.router.withRepositoryAt(projectID, func(repository Repository, resolvedProjectID string) error {
		layout, ok := repository.(*jsonRepository)
		if !ok {
			return errors.New("session ssh: state storage requires session layout")
		}
		raw, err := os.ReadFile(filepath.Join(layout.projectDir(resolvedProjectID), sessionSSHFileName))
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		payload = raw
		return nil
	})
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(string(payload), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var record SessionSSHState
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			continue
		}
		if strings.TrimSpace(record.SessionID) == "" {
			continue
		}
		record.AllowedEnvs = normalizeEnvNames(record.AllowedEnvs)
		states[record.SessionID] = record
	}
	return states, nil
}

// normalizeEnvNames 归一环境名集合：去空白、丢空项、保序去重，永不返回 nil。
func normalizeEnvNames(names []string) []string {
	normalized := make([]string, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		if _, dup := seen[trimmed]; dup {
			continue
		}
		seen[trimmed] = struct{}{}
		normalized = append(normalized, trimmed)
	}
	return normalized
}
