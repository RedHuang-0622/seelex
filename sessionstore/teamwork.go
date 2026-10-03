package sessionstore

// teamwork.go — teamwork 硬编排计划的存储面（moduleTeamwork）。
//
// 口径（docs/arch/teamwork-leader-worker-architecture.md §4.6 / D3 / D4）：
//
//   - **顺序的唯一事实**是 plan.milestones[].depends_on（里程碑屏障）+ 里程碑内
//     plan.milestones[].items[].depends_on（DAG），取代旧的 lifecycle.order_policy /
//     order_roles 与阶段制时代的 plan.stages[]（2026-10-04 阶段口径整条退场：阶段与
//     里程碑的语义耦合一处也不留）；
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
	// TeamworkEventItem / Accept / Fail / Settle / Recover 是 **Work Item 生命周期**
	// 的事实流水（排活 / 验收通过 / 判失败 / 尾插 / 中断恢复）。
	TeamworkEventItem    = "item"
	TeamworkEventAccept  = "accept"
	TeamworkEventFail    = "fail"
	TeamworkEventSettle  = "settle"
	TeamworkEventRecover = "recover"
	// TeamworkEventMessage 是**尾插**进 teammate 消息队列的一行。它落在审计流里而不是
	// 另开一份存储：消息队列是"发生过的事实"的按人读法（过滤 role_session_id），
	// 与审计同一条追加型事实流——两份存储只会让"看板看到的"与"审计记的"漂移。
	TeamworkEventMessage = "message"
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
//
// **没有 stages**（2026-10-04）：阶段口径整条退场，顺序的唯一事实是里程碑屏障
// （Milestones[].DependsOn）+ 里程碑内的工作项 DAG（Items[].DependsOn）。读一份
// 阶段制时代留下的旧计划时，`stages` 这个未知字段被 JSON 解码直接忽略——旧计划
// 因此照常读得出来，只是它的"顺序"不再被任何读侧认作事实。
type TeamworkPlan struct {
	TeamID     string              `json:"team_id"`
	Version    int                 `json:"version"`
	Members    []TeamworkMember    `json:"members"`
	Milestones []TeamworkMilestone `json:"milestones,omitempty"`
	State      TeamworkState       `json:"state,omitempty"`
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
//
// 它同时是**屏障**：里程碑之间串行（DependsOn 是里程碑层的顺序唯一事实），
// 里程碑内部才按 Items 的依赖 DAG 并行。阶段制时代的 `after`（指向阶段 id）已随
// 阶段口径一起退场——留一个指向不存在的东西的字段，就是把两套语义继续焊在一起。
type TeamworkMilestone struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
	// DependsOn 是里程碑之间的依赖（屏障）：依赖未 done 的里程碑不进入"可排活"。
	DependsOn []string `json:"depends_on,omitempty"`
	// Required 是"这个里程碑需要哪些在编角色"的声明（**角色名**，不是阶段 id）；
	// 它只做校验（必须在编），不参与顺序判定——顺序归 DependsOn。
	Required []string `json:"required,omitempty"`
	Status   string   `json:"status,omitempty"`
	Content  string   `json:"content,omitempty"`
	// Items 是这个里程碑下的工作项（甘特图节点；一 Work Item 一个 teammate 一套
	// Session + worktree）。分里程碑安排工作 = 只往当前这一步的 Items 里加东西。
	Items []TeamworkWorkItem `json:"items,omitempty"`
}

// TeamworkState 是计划的运行态投影（作业句柄/里程碑结论）。
//
// 注意：jobs 里的句柄只是**投影**，不是事实来源（jobs I-4：句柄只在内存，
// 进程重启后这里的值一律视为过期，由调用方以 Observe 的返回为准）。
type TeamworkState struct {
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
	Role      string    `json:"role,omitempty"`
	Handle    string    `json:"handle,omitempty"`
	Node      string    `json:"node,omitempty"`
	Milestone string    `json:"milestone,omitempty"`
	// WorkItem 是 Work Item 口径的归属（甘特节点 id）。
	WorkItem string `json:"work_item,omitempty"`
	// RoleSessionID 是**尾插落点**的键（teammate 的消息队列按它读）。只有 message
	// 一类事件带它；其余行留空——不是所有事实都属于某个会话。
	RoleSessionID string `json:"role_session_id,omitempty"`
	Detail        string `json:"detail,omitempty"`
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
	// AppendTeamworkBinding / ReadTeamworkBindings 是「一个 Work Item 一个 Session +
	// 一个 worktree」的绑定账本（追加型 JSONL，KV 语义：按 work_item 取最后一行）。
	// 生命周期与 Session 一致：整队收口（team_close）之后一并结束。
	AppendTeamworkBinding(context.Context, Key, TeamworkBinding) error
	ReadTeamworkBindings(context.Context, Key) ([]TeamworkBinding, error)
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

	// 顺序事实只有一样：里程碑（阶段口径已退场，2026-10-04）。没有里程碑 = 这份
	// 计划没有任何可读的顺序，拒绝落盘。
	if len(plan.Milestones) == 0 {
		return errors.New("teamwork: plan.milestones 至少要有一份（顺序必须有唯一事实）")
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
	// 第二道：里程碑图（屏障 DAG）与工作项（里程碑内的甘特节点）。
	return validateTeamworkWorkItems(plan, roles)
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
