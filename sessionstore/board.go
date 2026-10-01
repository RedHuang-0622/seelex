package sessionstore

// board.go — 两块看板（goal / team）的**会话粒度元数据 + 快照**存储面。
//
// 设计依据：docs/arch/session-board-metadata-lifecycle.md（2026-10-02 已裁决）。
//
// 口径（这是本模块存在的唯一理由，写在这里防误读）：
//
//  1. 它**不是领域事实源**。goal 的事实是 goal 栈（moduleStackGoal）、teamwork 的
//     事实是计划 head（moduleTeamwork）+ 审计流水；本模块是它们的**下游存档**。
//     任何领域代码都不得读它做判定（只允许看板投影的**兜底**路径读，见 §8 I1）。
//  2. 它**不是 resume 的领域实现**。core/resume 那七步是"未完成工作续跑"，对象是
//     执行单元；看板不是执行单元，没有"重跑"一说。这里只服务**快照恢复**。
//  3. 读侧铁律：**活体投影优先**。活体给得出看板就用活体，存档只在活体给不出时兜底；
//     绝不允许"JSON 与活体不一致时以 JSON 为准"。
//
// 两个模块而不是一个（moduleBoardGoal / moduleBoardTeam）：两块看板由不同子系统写
// （goal 域 / teamwork 域），共用一个模块就是给它们造一个共享串行点——S16 把栈 head
// 按 kind 拆开的理由在这里同样成立。模块锁各一把（见 mutexFor 的 case）。
//
// 载荷形状为什么一半类型化、一半不透明：
//   - goal 的载荷是**域形状**（当前帧 = 会话存储已经在建模的 GoalFrame 语义），
//     存储层认得它，于是可以校验"同一 goal 不许同时出现在 active 与 history"这类
//     不变式（§3.1）；
//   - 团队看板的载荷是**视图快照**（形状归上层 contract/dto），存储层不复制它，
//     只当不透明字节搬运——让存储层认识视图词汇就是把两层绑死。
//
// 文件：metadata/board_goal.json / metadata/board_team.json（走既有原子 head 发布）。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// 看板 kind（与文件名一致：board_goal / board_team）。
const (
	BoardKindGoal = "goal"
	BoardKindTeam = "team"
)

// 看板生命周期状态。
const (
	// BoardStateActive：看板在册（goal 有活动帧 / 团队有计划且未收口）。
	BoardStateActive = "active"
	// BoardStateClosed：看板已关闭。读侧**不显示**它，但存档保留为历史——
	// "这块看板什么时候开的、什么时候没的、为什么"是它能回答的问题。
	BoardStateClosed = "closed"
)

// goal 看板关闭原因的取值（与 goal 域的状态迁移对齐）。
const (
	BoardCloseGoalFinish = "goal.finish"
	BoardCloseGoalAbort  = "goal.abort"
	// BoardCloseTeamReplan：team_plan 写入新版本，上一版本就此关闭（版本递进）。
	BoardCloseTeamReplan = "team.replan"
	// BoardCloseTeamClose：显式收口。
	BoardCloseTeamClose = "team.close"
)

// BoardLifecycle 是两块看板共用的生命周期头（它才是这份 JSON 存在的第一理由；
// 快照是第二理由）。
type BoardLifecycle struct {
	// Kind 必须与所在模块一致（board_goal → goal，board_team → team）。
	Kind string `json:"board_kind"`
	// State 是 active | closed。close 之后不得再 update（复活必须走一次新的 open）。
	State string `json:"state"`
	// Seq 是这块看板的元数据序号：open 置 1，此后每次更新递增；只增不回改。
	// 它**不是** goal 的 seq，也不是团队计划版本——那两者各有自己的字段。
	Seq       uint64 `json:"seq"`
	OpenedAt  int64  `json:"opened_at,omitempty"`
	UpdatedAt int64  `json:"updated_at,omitempty"`
	ClosedAt  int64  `json:"closed_at,omitempty"`
	// ClosedReason 在 state=closed 时必填（见 ValidateGoalBoardMeta）。
	ClosedReason string `json:"closed_reason,omitempty"`
	// Fingerprint 是载荷内容指纹：写入侧据此节流（内容没变不写盘）。
	Fingerprint string `json:"fingerprint,omitempty"`
}

// Closed 报告这块看板是否已关闭。
func (l BoardLifecycle) Closed() bool { return l.State == BoardStateClosed }

// GoalBoardActive 是 goal 看板载荷里"**active seq 的 goal**"——当前治理中的那一帧。
//
// 它与 GoalFrame 同形（会话存储已经在建模的形状），但**只带看板要用的字段**：
// 存档不是第五栈的第二份拷贝，它记录的是"看板上写着什么"。
type GoalBoardActive struct {
	GoalID     string   `json:"goal_id"`
	Title      string   `json:"title,omitempty"`
	Statement  string   `json:"statement,omitempty"`
	Status     string   `json:"status,omitempty"`
	Acceptance []string `json:"acceptance,omitempty"`
	OutOfScope []string `json:"out_of_scope,omitempty"`
	// Progress 是完整打点流水（有界：goal 域自身保留上限就是它的上界）。
	Progress  []GoalProgress `json:"progress,omitempty"`
	CreatedAt int64          `json:"created_at,omitempty"`
	UpdatedAt int64          `json:"updated_at,omitempty"`
}

// GoalBoardHistory 是 goal 看板载荷里的一条 **history goal**（已结束的目标）。
//
// 只追加、不重写：Status / ClosedAt / ClosedReason 在**弹栈那一刻**写死——那三样
// 之后就再也查不到了（活动栈弹栈即消失、Controller.History 是进程内态）。
type GoalBoardHistory struct {
	GoalID       string `json:"goal_id"`
	Title        string `json:"title,omitempty"`
	Status       string `json:"status,omitempty"`
	ClosedAt     int64  `json:"closed_at,omitempty"`
	ClosedReason string `json:"closed_reason,omitempty"`
	// ProgressCount 是收口时的打点数（记录"做了多少"而不复制整份流水）。
	ProgressCount int `json:"progress_count,omitempty"`
}

// GoalBoardMeta 是 goal 看板的存档（moduleBoardGoal head 的 payload）。
//
// 不变式（ValidateGoalBoardMeta 逐条查）：Active 非空 ⟺ state=active；
// 同一个 goal_id 不得同时出现在 Active 与 History（出现即存档损坏）。
type GoalBoardMeta struct {
	BoardLifecycle
	Active  *GoalBoardActive   `json:"active,omitempty"`
	History []GoalBoardHistory `json:"history,omitempty"`
}

// TeamBoardMeta 是团队看板的存档（moduleBoardTeam head 的 payload）。
//
// Snapshot 是**视图快照**（与下发前端的 dto.TeamworkBoardView 同形），存储层不认识它：
// 它存在的理由只有一个——重启后作业行（jobs I-4：句柄只在内存）能照原样"看一眼"，
// 而不是空白。读侧必须把它标成 recovered 且作业行标 stale。
type TeamBoardMeta struct {
	BoardLifecycle
	TeamID   string          `json:"team_id,omitempty"`
	Version  int             `json:"version,omitempty"`
	Snapshot json.RawMessage `json:"snapshot,omitempty"`
}

// BoardRepository 是两块看板存档的读 / 写面。
//
// 与 TeamworkRepository 同一形态：**不并进 Repository 主接口**——主接口是"每个后端
// 都必须给出"的能力面，而看板存档只在 v8 JSON 布局上有意义。用可选接口 + 类型断言
// 表达"有就有、没有就是没装配"。
type BoardRepository interface {
	WriteGoalBoard(context.Context, Key, GoalBoardMeta) error
	ReadGoalBoard(context.Context, Key) (GoalBoardMeta, error)
	WriteTeamBoard(context.Context, Key, TeamBoardMeta) error
	ReadTeamBoard(context.Context, Key) (TeamBoardMeta, error)
}

// Boards 把 Repository 收窄到看板存档面（JSON 后端实现它；其他后端返回 false）。
func Boards(repository Repository) (BoardRepository, bool) {
	boards, ok := repository.(BoardRepository)
	return boards, ok
}

// BoardsFor 返回**当前活跃后端**的看板存档读/写面。
//
// 它把 Key 交给调用方（与 TeamworkFor 同形）：适合"调用方已经握有 Key"的装配。
// 只知道 sessionID 的领域侧用 BoardsForSession。
func (router *Router) BoardsFor() (BoardRepository, bool) {
	if router == nil {
		return nil, false
	}
	var repository BoardRepository
	var ok bool
	_ = router.withRepository(func(current Repository, _ string) error {
		repository, ok = Boards(current)
		return nil
	})
	return repository, ok
}

// SessionBoards 是**绑定会话**的看板存档面：Key 已烘焙进实现，调用方不再重复传。
//
// 为什么两套面并存：BoardsFor（机械面）把 Key 交给调用方；BoardsForSession 适合
// "只知道 sessionID"的领域侧——goal 域的 ContextStateStore 就是这种：它只握有
// SessionContextStore，项目与工作区由它给出。两套面的读写面完全同构，不存在
// "某一套能做的事另一套做不到"。
type SessionBoards interface {
	WriteGoalBoard(context.Context, GoalBoardMeta) error
	ReadGoalBoard(context.Context) (GoalBoardMeta, error)
	WriteTeamBoard(context.Context, TeamBoardMeta) error
	ReadTeamBoard(context.Context) (TeamBoardMeta, error)
}

// BoardsForSession 返回绑定到某会话的看板存档面（Key = Router 当前写作用域 +
// sessionID）。未实现存档面的后端 / sessionID 为空 → false。
func (router *Router) BoardsForSession(sessionID string) (SessionBoards, bool) {
	repository, ok := router.BoardsFor()
	if !ok {
		return nil, false
	}
	key := Key{ProjectID: router.Workspace(), SessionID: strings.TrimSpace(sessionID)}
	if err := key.validate(); err != nil {
		return nil, false
	}
	return sessionBoards{repository: repository, key: key}, true
}

// sessionBoards 是 SessionBoards 的实现：把烘焙的 Key 注入每一次读写。
type sessionBoards struct {
	repository BoardRepository
	key        Key
}

func (b sessionBoards) WriteGoalBoard(ctx context.Context, meta GoalBoardMeta) error {
	return b.repository.WriteGoalBoard(ctx, b.key, meta)
}

func (b sessionBoards) ReadGoalBoard(ctx context.Context) (GoalBoardMeta, error) {
	return b.repository.ReadGoalBoard(ctx, b.key)
}

func (b sessionBoards) WriteTeamBoard(ctx context.Context, meta TeamBoardMeta) error {
	return b.repository.WriteTeamBoard(ctx, b.key, meta)
}

func (b sessionBoards) ReadTeamBoard(ctx context.Context) (TeamBoardMeta, error) {
	return b.repository.ReadTeamBoard(ctx, b.key)
}

// ValidateGoalBoardMeta 校验 goal 看板存档。
//
// 校验落在**真正落盘的那一步**（与 ValidateTeamworkPlan 同一理由）：存档可被上层
// 反复重写，"写进会话的每一份存档都是合法的"这条不能随调用方漂移。
func ValidateGoalBoardMeta(meta GoalBoardMeta) error {
	if err := validateBoardLifecycle(meta.BoardLifecycle, BoardKindGoal); err != nil {
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

// ValidateTeamBoardMeta 校验团队看板存档。
func ValidateTeamBoardMeta(meta TeamBoardMeta) error {
	if err := validateBoardLifecycle(meta.BoardLifecycle, BoardKindTeam); err != nil {
		return err
	}
	if meta.State == BoardStateActive && len(meta.Snapshot) == 0 {
		return errors.New("board: 在册的团队看板必须有快照载荷（否则重启后什么都没有）")
	}
	return nil
}

// validateBoardLifecycle 校验两块看板共用的生命周期头。
func validateBoardLifecycle(lifecycle BoardLifecycle, kind string) error {
	if lifecycle.Kind != kind {
		return fmt.Errorf("board: board_kind 必须是 %q，得到 %q", kind, lifecycle.Kind)
	}
	switch lifecycle.State {
	case BoardStateActive, BoardStateClosed:
	default:
		return fmt.Errorf("board: state 非法 %q（active|closed）", lifecycle.State)
	}
	if lifecycle.Seq == 0 {
		return errors.New("board: seq 必须 >= 1（open 即置 1）")
	}
	if lifecycle.State == BoardStateClosed {
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

// WriteGoalBoard 写入（整份替换）goal 看板存档。
//
// 写失败**必须**上抛：close 写不进去就不能把看板当关闭（否则重启后它会作为 active
// 复活）。这是本设计里唯一一处"写失败要影响行为"的地方。
func (repository *jsonRepository) WriteGoalBoard(_ context.Context, key Key, meta GoalBoardMeta) error {
	if err := key.validate(); err != nil {
		return err
	}
	if err := ValidateGoalBoardMeta(meta); err != nil {
		return err
	}
	if repository.layout == nil {
		return errors.New("session storage: board archive requires the v8 layout engine")
	}
	return repository.layout.commitModuleHead(key, moduleBoardGoal, "", meta)
}

// ReadGoalBoard 读回 goal 看板存档（缺失返回 fs.ErrNotExist）。
func (repository *jsonRepository) ReadGoalBoard(_ context.Context, key Key) (GoalBoardMeta, error) {
	if err := key.validate(); err != nil {
		return GoalBoardMeta{}, err
	}
	if repository.layout == nil {
		return GoalBoardMeta{}, errors.New("session storage: board archive requires the v8 layout engine")
	}
	return readModuleHeadPayload[GoalBoardMeta](repository.layout, key, moduleBoardGoal)
}

// WriteTeamBoard 写入（整份替换）团队看板存档。
func (repository *jsonRepository) WriteTeamBoard(_ context.Context, key Key, meta TeamBoardMeta) error {
	if err := key.validate(); err != nil {
		return err
	}
	if err := ValidateTeamBoardMeta(meta); err != nil {
		return err
	}
	if repository.layout == nil {
		return errors.New("session storage: board archive requires the v8 layout engine")
	}
	return repository.layout.commitModuleHead(key, moduleBoardTeam, "", meta)
}

// ReadTeamBoard 读回团队看板存档（缺失返回 fs.ErrNotExist）。
func (repository *jsonRepository) ReadTeamBoard(_ context.Context, key Key) (TeamBoardMeta, error) {
	if err := key.validate(); err != nil {
		return TeamBoardMeta{}, err
	}
	if repository.layout == nil {
		return TeamBoardMeta{}, errors.New("session storage: board archive requires the v8 layout engine")
	}
	return readModuleHeadPayload[TeamBoardMeta](repository.layout, key, moduleBoardTeam)
}
