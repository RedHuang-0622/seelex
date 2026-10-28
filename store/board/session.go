package board

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/RedHuang-0622/seelex/contract/dto"
	storelayout "github.com/RedHuang-0622/seelex/store/internal/layout"
)

const (
	GoalBoardModuleID storelayout.ModuleID = "board_goal"
	TeamBoardModuleID storelayout.ModuleID = "board_team"
)

// SessionStore owns the goal/team session-board archive. Its payload types are
// shared DTOs; validation, module identity and publication semantics live here.
type SessionStore struct {
	layout *storelayout.Layout
}

func NewSessionStore(layout *storelayout.Layout) *SessionStore {
	return &SessionStore{layout: layout}
}

type BoundStore struct {
	store *SessionStore
	key   dto.SessionKey
}

type SessionBoardStore interface {
	WriteGoalBoard(context.Context, dto.GoalBoardMeta) error
	ReadGoalBoard(context.Context) (dto.GoalBoardMeta, error)
	WriteTeamBoard(context.Context, dto.TeamBoardMeta) error
	ReadTeamBoard(context.Context) (dto.TeamBoardMeta, error)
}

func (s *SessionStore) Bind(key dto.SessionKey) *BoundStore {
	return &BoundStore{store: s, key: key}
}

var _ SessionBoardStore = (*BoundStore)(nil)

func scopeOf(key dto.SessionKey) storelayout.Scope {
	return storelayout.Scope{ProjectID: key.ProjectID, SessionID: key.SessionID}
}

func (s *SessionStore) WriteGoalBoard(_ context.Context, key dto.SessionKey, meta dto.GoalBoardMeta) error {
	if err := key.Validate(); err != nil {
		return err
	}
	if err := ValidateGoalBoardMeta(meta); err != nil {
		return err
	}
	return s.layout.Commit(scopeOf(key), GoalBoardModuleID, "", meta, time.Now().UTC())
}

func (s *SessionStore) ReadGoalBoard(_ context.Context, key dto.SessionKey) (dto.GoalBoardMeta, error) {
	if err := key.Validate(); err != nil {
		return dto.GoalBoardMeta{}, err
	}
	return readOptional[dto.GoalBoardMeta](s.layout, scopeOf(key), GoalBoardModuleID)
}

func (s *SessionStore) WriteTeamBoard(_ context.Context, key dto.SessionKey, meta dto.TeamBoardMeta) error {
	if err := key.Validate(); err != nil {
		return err
	}
	if err := ValidateTeamBoardMeta(meta); err != nil {
		return err
	}
	return s.layout.Commit(scopeOf(key), TeamBoardModuleID, "", meta, time.Now().UTC())
}

func (s *SessionStore) ReadTeamBoard(_ context.Context, key dto.SessionKey) (dto.TeamBoardMeta, error) {
	if err := key.Validate(); err != nil {
		return dto.TeamBoardMeta{}, err
	}
	return readOptional[dto.TeamBoardMeta](s.layout, scopeOf(key), TeamBoardModuleID)
}

func (b *BoundStore) WriteGoalBoard(ctx context.Context, meta dto.GoalBoardMeta) error {
	return b.store.WriteGoalBoard(ctx, b.key, meta)
}

func (b *BoundStore) ReadGoalBoard(ctx context.Context) (dto.GoalBoardMeta, error) {
	return b.store.ReadGoalBoard(ctx, b.key)
}

func (b *BoundStore) WriteTeamBoard(ctx context.Context, meta dto.TeamBoardMeta) error {
	return b.store.WriteTeamBoard(ctx, b.key, meta)
}

func (b *BoundStore) ReadTeamBoard(ctx context.Context) (dto.TeamBoardMeta, error) {
	return b.store.ReadTeamBoard(ctx, b.key)
}

func ValidateGoalBoardMeta(meta dto.GoalBoardMeta) error {
	if err := validateBoardLifecycle(meta.BoardLifecycle, dto.BoardKindGoal); err != nil {
		return err
	}
	active := ""
	if meta.Active != nil {
		active = strings.TrimSpace(meta.Active.GoalID)
		if active == "" {
			return errors.New("board: active.goal_id is required（active seq 的 goal 必须有它的编号）")
		}
	}
	seen := make(map[string]struct{}, len(meta.History))
	for index, entry := range meta.History {
		id := strings.TrimSpace(entry.GoalID)
		if id == "" {
			return fmt.Errorf("board: history[%d].goal_id is required", index)
		}
		if _, duplicate := seen[id]; duplicate {
			return fmt.Errorf("board: history 里 goal_id %q 重复（history 是单向迁移的账本）", id)
		}
		seen[id] = struct{}{}
		if id == active {
			return fmt.Errorf("board: goal_id %q 同时出现在 active 与 history（存档损坏：同一个 goal 只能在一处）", id)
		}
	}
	return nil
}

func ValidateTeamBoardMeta(meta dto.TeamBoardMeta) error {
	if err := validateBoardLifecycle(meta.BoardLifecycle, dto.BoardKindTeam); err != nil {
		return err
	}
	if meta.State == dto.BoardStateActive && len(meta.Snapshot) == 0 {
		return errors.New("board: 在册的团队看板必须有快照载荷（否则重启后什么都没有）")
	}
	return nil
}

func validateBoardLifecycle(lifecycle dto.BoardLifecycle, kind string) error {
	if lifecycle.Kind != kind {
		return fmt.Errorf("board: board_kind 必须是 %q，得到 %q", kind, lifecycle.Kind)
	}
	switch lifecycle.State {
	case dto.BoardStateActive, dto.BoardStateClosed:
	default:
		return fmt.Errorf("board: state 非法 %q（active|closed）", lifecycle.State)
	}
	if lifecycle.Seq == 0 {
		return errors.New("board: seq 必须 >= 1（open 即置 1）")
	}
	if lifecycle.State == dto.BoardStateClosed {
		if lifecycle.ClosedAt <= 0 {
			return errors.New("board: state=closed 必须给出 closed_at")
		}
		if strings.TrimSpace(lifecycle.ClosedReason) == "" {
			return errors.New("board: state=closed 必须给出 closed_reason")
		}
		return nil
	}
	if lifecycle.ClosedAt != 0 {
		return errors.New("board: 未关闭的看板不得带 closed_at")
	}
	return nil
}

func readOptional[T any](layout *storelayout.Layout, scope storelayout.Scope, module storelayout.ModuleID) (T, error) {
	var zero T
	head, err := layout.ReadWithHeal(
		scope,
		module,
		false,
		func() *sync.Mutex { return layout.Lock(scope, module) },
		nil,
		nil,
	)
	if err != nil {
		if storelayout.IsNotExist(err) {
			return zero, nil
		}
		return zero, err
	}
	if len(head.Payload) == 0 {
		return zero, nil
	}
	if err := json.Unmarshal(head.Payload, &zero); err != nil {
		return zero, fmt.Errorf("session storage: decode %s head payload: %w", module, err)
	}
	return zero, nil
}
