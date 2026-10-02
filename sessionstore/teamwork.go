package sessionstore

// teamwork.go — teamwork 硬编排计划的存储面（moduleTeamwork）。
//
// 口径（docs/arch/teamwork-leader-worker-architecture.md §4.6 / D3 / D4）：
//
//   - **顺序的唯一事实**是 plan.stages[].depends_on，取代旧的
//     lifecycle.order_policy / order_roles；
//   - 存储分两件：**计划**（谁、什么顺序）落 moduleTeamwork 的 head，等价于
//     plan.json——整份替换型内容，原子发布 + 校验和自愈；**审计**落同模块的
//     数据文件 teamwork/events.jsonl——只追加、不重写，是派发 / 里程碑 /
//     retire 的事实流水；
//   - **不存内容**：作业句柄只在内存（jobs I-4）、worktree 路径与差异是 git 的
//     事（D9），这里只留 worktree 的**指派名**，重派时重建。
//
// 模块锁：走 store.mu(key, moduleTeamwork)（独立锁，不与 message 共用）。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// teamworkEventFile 是审计流水文件名（追加、不重写）。
const teamworkEventFile = "events.jsonl"

// teamworkJobSubdir 是 teammate 作业**输出文件**所在子目录（产品自有，见 §4.7 输出归属）。
//
// 为什么把输出文件放在会话目录里而不是框架的临时目录：框架缺省自建
// `<outputDir>/<handle>.log` 并在**销项 / 驱逐 / Close** 三处删掉它，于是"阶段收尾读一次
// 产出"就把正文带走了——而 leader 的收口（team_close）之前，那些正文正是看板与
// team_context 的证据。把文件交给产品（jobs.Spec.OutputPath 非空时框架只按偏移读、
// 永不删），正文就活到**产品自己决定的那一刻**（整队收口），并且跟着会话一起被清理。
const teamworkJobSubdir = "jobs"

// 内置角色复用主会话（agentteam.needsRoleSession），不得作为 teammate 派发。
// 这里重复一次常量而不是 import agentteam：存储层不该依赖上层角色装配，
// 而这条约束必须在**计划可被 leader 改写**的第二道校验关口上仍然成立。
const (
	TeamworkRoleMain = "main"
	TeamworkRoleUser = "user"
)

// 里程碑状态取值（空 = pending）。
const (
	TeamworkMilestonePending = "pending"
	TeamworkMilestoneActive  = "active"
	TeamworkMilestoneDone    = "done"
)

// 审计事件的 kind 取值。
const (
	TeamworkEventPlan      = "plan"
	TeamworkEventDispatch  = "dispatch"
	TeamworkEventJoin      = "join"
	TeamworkEventMilestone = "milestone"
	TeamworkEventRetire    = "retire"
	// TeamworkEventClose 是**整队收口**（team_close）的审计行：与逐人退场（retire）
	// 分开记——收口是团队级动作，不是"最后一次退场"。
	TeamworkEventClose = "close"
)

// 计划的运行态取值（空 = 未收口；与里程碑状态的"空 = pending"同口径）。
//
// **闭板事实双写：域内权威在 plan.State**（U3 裁决）——看板存档（moduleBoardTeam）
// 只作历史/恢复，读侧不得据存档反推域内是否已收口。
const (
	// TeamworkStateClosed：整队已收口（team_close）。
	TeamworkStateClosed = "closed"
)

// TeamworkPlan 是 leader 的硬编排计划（moduleTeamwork head 的 payload）。
type TeamworkPlan struct {
	TeamID     string              `json:"team_id"`
	Version    int                 `json:"version"`
	Stages     []TeamworkStage     `json:"stages"`
	Members    []TeamworkMember    `json:"members"`
	Milestones []TeamworkMilestone `json:"milestones,omitempty"`
	State      TeamworkState       `json:"state,omitempty"`
}

// TeamworkStage 是一个编排阶段；DependsOn 是顺序/依赖的唯一事实。
type TeamworkStage struct {
	ID        string   `json:"id"`
	Roles     []string `json:"roles"`
	DependsOn []string `json:"depends_on,omitempty"`
}

// TeamworkMember 是一个 teammate 的在编条目（人 + 会话 + 工作区指派）。
type TeamworkMember struct {
	Role          string `json:"role"`
	RoleSessionID string `json:"role_session_id"`
	Worktree      string `json:"worktree,omitempty"`
	// ToolsPolicy 是权责档（readonly / readwrite / inherit）；与 Permission 的
	// 关系同 dto.RoleSpec：显式 Permission 非空则以格子为准，档位只用于选分支。
	ToolsPolicy string         `json:"tools_policy,omitempty"`
	Permission  map[string]int `json:"permission_groups,omitempty"`
}

// TeamworkMilestone 是 leader 声明的里程碑（内容由 leader 撰写，见 §4.5）。
type TeamworkMilestone struct {
	ID       string   `json:"id"`
	After    []string `json:"after,omitempty"`
	Required []string `json:"required,omitempty"`
	Status   string   `json:"status,omitempty"`
	Content  string   `json:"content,omitempty"`
}

// TeamworkState 是计划的运行态投影（阶段/作业句柄/里程碑结论）。
//
// 注意：jobs 里的句柄只是**投影**，不是事实来源（jobs I-4：句柄只在内存，
// 进程重启后这里的值一律视为过期，由调用方以 Observe 的返回为准）。
type TeamworkState struct {
	Stage      string            `json:"stage,omitempty"`
	Jobs       map[string]string `json:"jobs,omitempty"`
	Milestones map[string]string `json:"milestones,omitempty"`
	// 闭板三字段（closed 事实的域内权威，U3 裁决）：State 空 = 未收口。
	// ClosedReason 取 sessionstore 的收口原因词表（team.close）；看板存档里的同名字段
	// 只是这份事实的历史副本，读侧判定"还在不在册"一律读这里。
	State        string `json:"state,omitempty"`
	ClosedAt     int64  `json:"closed_at,omitempty"`
	ClosedReason string `json:"closed_reason,omitempty"`
}

// TeamworkEvent 是审计流水的一行：只追加，不重写。
type TeamworkEvent struct {
	At        time.Time `json:"at"`
	Kind      string    `json:"kind"`
	TeamID    string    `json:"team_id,omitempty"`
	Stage     string    `json:"stage,omitempty"`
	Role      string    `json:"role,omitempty"`
	Handle    string    `json:"handle,omitempty"`
	Node      string    `json:"node,omitempty"`
	Milestone string    `json:"milestone,omitempty"`
	Detail    string    `json:"detail,omitempty"`
}

// TeamworkRepository 是 teamwork 模块的读 / 写面。
//
// 它**不并进 Repository 主接口**：主接口是"每个后端都必须给出"的能力面
// （接口方法不可在包外实现，也不存在"某个后端没有栈通道"的运行期分支），而
// teamwork 只在 v8 JSON 布局上有意义。用可选接口 + 类型断言表达"有就有、
// 没有就是没装配"，比给主接口加一堆在别的后端恒返回错误的成员诚实。
type TeamworkRepository interface {
	WriteTeamworkPlan(context.Context, Key, TeamworkPlan, int) error
	ReadTeamworkPlan(context.Context, Key) (TeamworkPlan, error)
	AppendTeamworkEvent(context.Context, Key, TeamworkEvent) error
	ReadTeamworkEvents(context.Context, Key) ([]TeamworkEvent, error)
	// TeamworkJobOutputDir 返回（必要时创建）该会话 teammate 作业输出文件所在的
	// **产品自有目录**：路径交给 jobs.Spec.OutputPath 之后，框架只按偏移读、永不删，
	// 生命周期归产品（整队收口时清）。
	TeamworkJobOutputDir(context.Context, Key) (string, error)
}

// Teamwork 把 Repository 收窄到 teamwork 面。JSON 后端实现它；其他后端返回 false。
func Teamwork(repository Repository) (TeamworkRepository, bool) {
	teamwork, ok := repository.(TeamworkRepository)
	return teamwork, ok
}

// TeamworkFor 返回**当前活跃后端**的 teamwork 读/写面（JSON 后端实现它；其他后端
// 返回 false）。组合根据此把 moduleTeamwork 装配进 leader 编排面——它不需要知道
// 后端是怎么被选出来的，只问"当前后端给不给这个能力"。
func (router *Router) TeamworkFor() (TeamworkRepository, bool) {
	if router == nil {
		return nil, false
	}
	var repository TeamworkRepository
	var ok bool
	_ = router.withRepository(func(current Repository, _ string) error {
		repository, ok = Teamwork(current)
		return nil
	})
	return repository, ok
}

// teamworkDir 返回 teamwork 模块的数据目录。
func (store *storeEngine) teamworkDir(key Key) string {
	return filepath.Join(store.sessionRoot(key), "teamwork")
}

func (store *storeEngine) teamworkEventsPath(key Key) string {
	return filepath.Join(store.teamworkDir(key), teamworkEventFile)
}

// teamworkJobDir 返回 teammate 作业输出文件所在目录（产品自有；见 teamworkJobSubdir）。
func (store *storeEngine) teamworkJobDir(key Key) string {
	return filepath.Join(store.teamworkDir(key), teamworkJobSubdir)
}

// ValidateTeamworkPlan 校验一份计划（M2 的第二道关口）。
//
// 为什么存储边界也要校验一次：计划**可被 leader 改写**，装配侧的
// agentteam.Normalize 只保住了"装配那一刻"的唯一性；把校验放在真正落盘的那
// 一步，才能保证"写进会话的每一份计划都是合法的"这条不因调用方不同而漂移。
//
// maxTeammates <= 0 表示调用方不掌握人数上限（只做结构性校验）。
func ValidateTeamworkPlan(plan TeamworkPlan, maxTeammates int) error {
	if strings.TrimSpace(plan.TeamID) == "" {
		return errors.New("teamwork: plan.team_id is required")
	}
	if plan.Version < 1 {
		return fmt.Errorf("teamwork: plan.version must be >= 1, got %d", plan.Version)
	}
	if len(plan.Members) == 0 {
		return errors.New("teamwork: plan.members must not be empty")
	}
	if maxTeammates > 0 && len(plan.Members) > maxTeammates {
		return fmt.Errorf(
			"teamwork: 在编 teammate %d 人超过 limits.team.max_teammates=%d（显式拒绝，不静默排队）",
			len(plan.Members), maxTeammates)
	}

	roles := make(map[string]struct{}, len(plan.Members))
	sessions := make(map[string]struct{}, len(plan.Members))
	for index, member := range plan.Members {
		role := strings.TrimSpace(member.Role)
		if role == "" {
			return fmt.Errorf("teamwork: plan.members[%d].role is required", index)
		}
		if role == TeamworkRoleMain || role == TeamworkRoleUser {
			return fmt.Errorf("teamwork: 角色 %q 是复用主会话的内置角色，不得作为 teammate 派发", role)
		}
		if _, duplicate := roles[role]; duplicate {
			return fmt.Errorf("teamwork: 一个 teammate = 一个角色，角色 %q 重复（D8 禁止重复角色）", role)
		}
		roles[role] = struct{}{}
		sessionID := strings.TrimSpace(member.RoleSessionID)
		if sessionID == "" {
			return fmt.Errorf("teamwork: plan.members[%d].role_session_id is required（由 (team_id, role) 派生）", index)
		}
		if _, duplicate := sessions[sessionID]; duplicate {
			return fmt.Errorf("teamwork: role_session_id %q 重复（一人一会话）", sessionID)
		}
		sessions[sessionID] = struct{}{}
	}

	if len(plan.Stages) == 0 {
		return errors.New("teamwork: plan.stages must not be empty（顺序的唯一事实是 stages[].depends_on）")
	}
	stageIDs := make(map[string]struct{}, len(plan.Stages))
	for index, stage := range plan.Stages {
		id := strings.TrimSpace(stage.ID)
		if id == "" {
			return fmt.Errorf("teamwork: plan.stages[%d].id is required", index)
		}
		if _, duplicate := stageIDs[id]; duplicate {
			return fmt.Errorf("teamwork: 阶段 id %q 重复", id)
		}
		stageIDs[id] = struct{}{}
		if len(stage.Roles) == 0 {
			return fmt.Errorf("teamwork: 阶段 %q 至少要指定一个角色", id)
		}
	}
	for _, stage := range plan.Stages {
		for _, dependency := range stage.DependsOn {
			if dependency == stage.ID {
				return fmt.Errorf("teamwork: 阶段 %q 依赖自己", stage.ID)
			}
			if _, exists := stageIDs[dependency]; !exists {
				return fmt.Errorf("teamwork: 阶段 %q 依赖不存在的阶段 %q", stage.ID, dependency)
			}
		}
	}
	if err := validateStageAcyclic(plan.Stages); err != nil {
		return err
	}

	milestoneIDs := make(map[string]struct{}, len(plan.Milestones))
	for index, milestone := range plan.Milestones {
		id := strings.TrimSpace(milestone.ID)
		if id == "" {
			return fmt.Errorf("teamwork: plan.milestones[%d].id is required", index)
		}
		if _, duplicate := milestoneIDs[id]; duplicate {
			return fmt.Errorf("teamwork: 里程碑 id %q 重复", id)
		}
		milestoneIDs[id] = struct{}{}
		for _, after := range milestone.After {
			if _, exists := stageIDs[after]; !exists {
				return fmt.Errorf("teamwork: 里程碑 %q 的 after 指向不存在的阶段 %q", id, after)
			}
		}
		for _, role := range milestone.Required {
			if _, exists := roles[role]; !exists {
				return fmt.Errorf("teamwork: 里程碑 %q 的 required 指向不在编的角色 %q", id, role)
			}
		}
		switch milestone.Status {
		case "", TeamworkMilestonePending, TeamworkMilestoneActive, TeamworkMilestoneDone:
		default:
			return fmt.Errorf("teamwork: 里程碑 %q 的状态 %q 非法（pending|active|done）", id, milestone.Status)
		}
	}
	return nil
}

// validateStageAcyclic 用 Kahn 拓扑消元检出依赖环。环会让 leader 的派发永远
// 等不到可跑的阶段，而配置里看不出任何异常——所以它是硬错误而不是告警。
func validateStageAcyclic(stages []TeamworkStage) error {
	remaining := make(map[string]int, len(stages))
	dependents := make(map[string][]string, len(stages))
	for _, stage := range stages {
		remaining[stage.ID] = len(stage.DependsOn)
		for _, dependency := range stage.DependsOn {
			dependents[dependency] = append(dependents[dependency], stage.ID)
		}
	}
	queue := make([]string, 0, len(stages))
	for id, count := range remaining {
		if count == 0 {
			queue = append(queue, id)
		}
	}
	resolved := 0
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		resolved++
		for _, dependent := range dependents[id] {
			remaining[dependent]--
			if remaining[dependent] == 0 {
				queue = append(queue, dependent)
			}
		}
	}
	if resolved != len(stages) {
		return errors.New("teamwork: plan.stages 的 depends_on 存在环（顺序必须是可拓扑排序的 DAG）")
	}
	return nil
}

// WriteTeamworkPlan 写入（整份替换）一份计划。maxTeammates <= 0 = 只做结构性校验。
func (repository *jsonRepository) WriteTeamworkPlan(_ context.Context, key Key, plan TeamworkPlan, maxTeammates int) error {
	if err := key.validate(); err != nil {
		return err
	}
	if err := ValidateTeamworkPlan(plan, maxTeammates); err != nil {
		return err
	}
	if repository.layout == nil {
		return errors.New("session storage: teamwork requires the v8 layout engine")
	}
	return repository.layout.commitModuleHead(key, moduleTeamwork, "", plan)
}

// ReadTeamworkPlan 读回计划（缺失返回 fs.ErrNotExist）。
func (repository *jsonRepository) ReadTeamworkPlan(_ context.Context, key Key) (TeamworkPlan, error) {
	if err := key.validate(); err != nil {
		return TeamworkPlan{}, err
	}
	if repository.layout == nil {
		return TeamworkPlan{}, errors.New("session storage: teamwork requires the v8 layout engine")
	}
	return readModuleHeadPayload[TeamworkPlan](repository.layout, key, moduleTeamwork)
}

// AppendTeamworkEvent 追加一条审计事件（只追加、不重写）。
//
// 追加是**有界且原子**的：一行 JSON + '\n'，单次 write 落在 O_APPEND 上；
// 崩溃只会留下半行残尾，读侧跳过（与 compact.jsonl / message 分片同一口径）。
func (repository *jsonRepository) AppendTeamworkEvent(_ context.Context, key Key, event TeamworkEvent) error {
	if err := key.validate(); err != nil {
		return err
	}
	if repository.layout == nil {
		return errors.New("session storage: teamwork requires the v8 layout engine")
	}
	if strings.TrimSpace(event.Kind) == "" {
		return errors.New("teamwork: event.kind is required")
	}
	if event.At.IsZero() {
		event.At = time.Now().UTC()
	}
	line, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("teamwork: encode event: %w", err)
	}
	store := repository.layout
	if _, err := store.ensureLayoutGuide(key); err != nil {
		return err
	}
	dir := store.teamworkDir(key)
	lock := store.mu(key, moduleTeamwork)
	lock.Lock()
	defer lock.Unlock()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(filepath.Join(dir, teamworkEventFile), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("teamwork: open audit log: %w", err)
	}
	defer file.Close()
	if _, err := file.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("teamwork: append audit log: %w", err)
	}
	return nil
}

// ReadTeamworkEvents 读回全部审计事件（按文件顺序 = 发生顺序）。
//
// 崩溃残尾（最后一行不是完整 JSON）跳过而不是报错：审计面永远比"能读出一行
// 半成品的 JSON"更值得。
func (repository *jsonRepository) ReadTeamworkEvents(_ context.Context, key Key) ([]TeamworkEvent, error) {
	if err := key.validate(); err != nil {
		return nil, err
	}
	if repository.layout == nil {
		return nil, errors.New("session storage: teamwork requires the v8 layout engine")
	}
	file, err := readSharedFile(repository.layout.teamworkEventsPath(key))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []TeamworkEvent{}, nil
		}
		return nil, err
	}
	lines := strings.Split(string(file), "\n")
	events := make([]TeamworkEvent, 0, len(lines))
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		var event TeamworkEvent
		if err := json.Unmarshal([]byte(trimmed), &event); err != nil {
			if index == len(lines)-1 {
				continue // 崩溃残尾
			}
			return nil, fmt.Errorf("teamwork: decode audit row %d: %w", index, err)
		}
		events = append(events, event)
	}
	return events, nil
}

// TeamworkJobOutputDir 返回（并确保存在）teammate 作业输出文件所在的产品自有目录。
//
// 目录在**会话目录内**（`<sessionRoot>/teamwork/jobs`）：它跟着会话走，会话被清理时一并
// 消失，不需要第二套回收机制。这里只负责"把目录交出去"，文件名与生命周期归调用方
// （派发时分配、整队收口时清）——存储层不替产品决定"哪一轮的输出该留着"。
func (repository *jsonRepository) TeamworkJobOutputDir(_ context.Context, key Key) (string, error) {
	if err := key.validate(); err != nil {
		return "", err
	}
	if repository.layout == nil {
		return "", errors.New("session storage: teamwork requires the v8 layout engine")
	}
	dir := repository.layout.teamworkJobDir(key)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("teamwork: create job output dir: %w", err)
	}
	return dir, nil
}
