package core

// project_board.go — 项目看板的**投影装配面**（M2：映射 + 同步器 + 只读取数口）。
//
// 口径（与 store/project_board.go 的存储面同一份契约，写在这里防误读）：
//
//  1. 本文件**不产出任何判定**（I3）：只把活体投影的字段搬到投影行里（"零判定"）。
//     状态/依赖/收口一律**原样搬运**；域内判定照旧读 task 注册表、plan、teamwork 事实。
//  2. 投影**不是输入**（I2）：本文件里的任何函数都不进入模型上下文、不参与领域判定；
//     投影文件被篡改只影响"文件的读"，活体投影与判定一字不变。
//  3. 投影**可删可重建**（I1）：落盘失败 / 文件被删 / 进程重启都只意味着"下次触发重建"，
//     不阻塞回合、不 panic（失败只记日志）。
//  4. 触发面是一张**表**（TriggerFor）：不在调用点上散落 `name == "…"`。
//  5. 同步器**不做 IO**：OnToolComplete 只做「判触发面 → 标脏 → 唤醒唯一 flush goroutine」；
//     写盘只有 flush 一条路，且在独立 goroutine 上跑（回合路径零开销）。
//  6. 落点只有两个白名单文件：work 行 → ProjectBoardWorkFile；milestone / milestone_item
//     行 → ProjectBoardTeamworkFile（I6：空集合**不建文件**，"没有就是没有"）。
//
// 不搬的字段（显式列出，避免"漏了"与"故意"读不出来）：
//   - worktree（TeamworkWorkItem.Worktree）：绝对路径。投影文件是派生数据，落绝对路径会把
//     用户机器上的目录布局写进一份跨机不可复现的文件里（与其他行的纪律不一致）。现场归属
//     的活体口径在 teamwork 事实源（绑定账本 / 工作项本身）。
//   - handle（TeamworkWorkItem.Handle）：**进程内投影**（jobs I-4：句柄只在内存，重启即作废）。
//     落盘等于把一份注定过期的指针固化成"事实"，读侧要么误用、要么再判一次"它还算不算数"。
//   - trace / 打点（WorkItem.Trace）：高频、体量最大、对"看板"无新信息（看板看的是"活与状态"，
//     过程读数归工作表格活体读面）。
//   - attachments（WorkItem.Attachments）：可能含绝对路径，同 worktree。
//
// 未新写第二份状态判定（"交付列复用清单"，一句话：本文件里没有任何一处是在**解释**状态）：
//   buildWorkTable            application/core/work_table.go:30   活体 work 行（唯一实现）
//   taskRecordsForSession     application/core/project_board.go   按会话取 task 注册表快照（与打点块同源）
//   TeamworkPlan.Milestones   store/teamwork.go:98         里程碑与工作项的唯一事实源
//   TeamworkState.Milestones  store/teamwork.go:151        里程碑运行态（优先读法）
//   TeamworkState.State       store/teamwork.go:167        整队收口（空 = 未收口）
//   WorkItem                  middle/protocol/model/state.go:612      工作表格行（字段映射的输入）
//
// 本项**没有**第二份判定的地方（逐条对应）：无 status 归一/翻译（原样搬运，空串不补值）；
// 无依赖推导（merger/planDependencies 那套留在活体投影里）；无收口判定（只与
// store.TeamworkStateClosed 这一个常量比对）；无会话归属判定（沿用行自带的
// SessionID）；无"计划是否算有编排"这类内容判定（teamworkPlanHasOrchestration 是看板读侧的事）。

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/store"
)

// ---------------------------------------------------------------------------
// 1) 纯函数映射：活体投影 → 投影行（零判定）
// ---------------------------------------------------------------------------

// projectBoardWorkKindProjected 是工作行「投影 / 不投影」的**唯一判据表**。
//
// 这是取舍，不是判定：它只说"这一种行要不要上板"，不解释任何状态。
//
//   - todo / task：模型自己列的活（todolist）与主动登记的活（taskadd）——看板的主体 → 投影。
//   - plan：计划节点是**过程**，且每次 plan_load 整份替换（它的权威读面是计划/团队面）。
//     把它抄进看板会让"计划改了"看起来像"活变了"（同一件事在两个读面给出两种叙事）→ 不投影。
//   - subagent：子代理是**执行隔离体**（一 Work Item 一套会话 + worktree，生命周期短、
//     数量随派发爆炸）。看板看的是"这件事"，不是"跑它的容器" → 不投影。
//   - tasklist：它不是 kind（todo 行的 **phase** 才是 tasklist）。这里显式列出并判"不投影"，
//     是防上游把 phase 当 kind 写进来的防御性条目——过程类一律不搬。
//
// 表外的未知 kind 一律不投影（保守：宁可少一行，也不把不认识的东西写进派生文件）。
var projectBoardWorkKindProjected = map[string]bool{
	"todo":     true,
	"task":     true,
	"plan":     false,
	"subagent": false,
	"tasklist": false,
}

// BuildProjectBoardRows 把活体投影映射成投影行（**纯函数**：同样的输入必得同样的输出）。
//
// seq / at 是信封的盖章。分块落盘时**每块用自己文件的水位**盖章（水位是"每文件"的事实），
// 因此落盘方按块调用（见 projectBoardRowsForFile）；直接调用本函数时给的就是这一批的盖章。
//
// 映射规则（全部是字段搬运）：
//   - work 行：只搬 kind ∈ {todo, task}（判据表见 projectBoardWorkKindProjected），
//     key = "work:<id>"，载荷 = WorkItem 的字段白名单；
//   - milestone 行：来自 teamPlan.Milestones，key = "milestone:<id>"，状态取数 =
//     运行态 TeamworkState.Milestones[id] 优先、计划里 ms.Status 兜底（**空串原样保留**：
//     "空 = pending"是存储层的约定，投影不替它补值——补值就是第二份判据）；
//   - milestone_item 行：来自 ms.Items，key = "milestone_item:<id>"，状态原样搬运。
//
// key 带 kind 前缀不是装饰：折叠读（FoldProjectBoardRows）**只按 Key 取最大 Seq**，
// 而里程碑 id 与工作项 id 是两套命名空间（ValidateTeamworkPlan 不查它们互不相同），
// 前缀是"同一条 key 不会跨 kind 撞车"的唯一保证。
func BuildProjectBoardRows(
	scope store.ProjectBoardScope,
	seq uint64,
	at int64,
	workItems []WorkItem,
	teamPlan *store.TeamworkPlan,
	teamState *store.TeamworkState,
) []store.ProjectBoardRow {
	rows := make([]store.ProjectBoardRow, 0, len(workItems)+8)
	for _, item := range workItems {
		if !projectBoardWorkKindProjected[strings.TrimSpace(item.Kind)] {
			continue
		}
		id := strings.TrimSpace(item.ID)
		if id == "" {
			continue // 没有身份的行上不了板（折叠读也按 key 去重，空 key 无意义）
		}
		rows = append(rows, projectBoardRow(scope, seq, at,
			store.ProjectBoardKindWork, "work:"+id, projectBoardWorkPayload(item)))
	}
	if teamPlan == nil {
		return rows
	}
	state := projectBoardTeamState(teamPlan, teamState)
	for _, milestone := range teamPlan.Milestones {
		milestoneID := strings.TrimSpace(milestone.ID)
		if milestoneID == "" {
			continue
		}
		rows = append(rows, projectBoardRow(scope, seq, at,
			store.ProjectBoardKindMilestone, "milestone:"+milestoneID,
			projectBoardMilestonePayload(milestone, state)))
		for _, item := range milestone.Items {
			itemID := strings.TrimSpace(item.ID)
			if itemID == "" {
				continue
			}
			rows = append(rows, projectBoardRow(scope, seq, at,
				store.ProjectBoardKindMilestoneItem, "milestone_item:"+itemID,
				projectBoardMilestoneItemPayload(milestoneID, item)))
		}
	}
	return rows
}

// projectBoardRow 组装一行信封（信封字段只有这里一处写）。
func projectBoardRow(
	scope store.ProjectBoardScope,
	seq uint64,
	at int64,
	kind, key string,
	payload map[string]any,
) store.ProjectBoardRow {
	return store.ProjectBoardRow{
		Schema:  store.ProjectBoardSchema,
		Kind:    kind,
		Key:     key,
		Seq:     seq,
		At:      at,
		Scope:   scope,
		Payload: payload,
	}
}

// projectBoardWorkPayload 把 work 行映射成载荷（字段白名单；空字段不落盘）。
func projectBoardWorkPayload(item WorkItem) map[string]any {
	payload := map[string]any{
		"id":    strings.TrimSpace(item.ID),
		"kind":  strings.TrimSpace(item.Kind),
		"phase": strings.TrimSpace(item.Phase),
	}
	projectBoardPutText(payload, "task", item.Task)
	projectBoardPutText(payload, "status", item.Status)
	projectBoardPutText(payload, "description", truncateWorkEvidence(item.Description, Limits().EvidenceChars))
	projectBoardPutText(payload, "assignee", item.Assignee)
	projectBoardPutText(payload, "session_id", item.SessionID)
	projectBoardPutText(payload, "source_id", item.SourceID)
	projectBoardPutText(payload, "batch_id", item.BatchID)
	projectBoardPutText(payload, "batch_label", item.BatchLabel)
	if len(item.Dependencies) > 0 {
		payload["dependencies"] = append([]string(nil), item.Dependencies...)
	}
	if item.RetryCount > 0 {
		payload["retry_count"] = item.RetryCount
	}
	if !item.CreatedAt.IsZero() {
		payload["created_at"] = item.CreatedAt.UTC().Unix()
	}
	if !item.StartedAt.IsZero() {
		payload["started_at"] = item.StartedAt.UTC().Unix()
	}
	if !item.EndedAt.IsZero() {
		payload["ended_at"] = item.EndedAt.UTC().Unix()
	}
	return payload
}

// projectBoardMilestonePayload 把里程碑映射成载荷。
//
// 整队收口写在里程碑行上（载荷 team_state / team_closed）：它是"这一板还在不在册"的唯一读法，
// 而里程碑行是**团队块**的锚（work 行是"活"，里程碑行是"编排"）。没有里程碑时无从表达——
// 没有编排就没有"收口"这一问（不是漏读）。
func projectBoardMilestonePayload(milestone store.TeamworkMilestone, state store.TeamworkState) map[string]any {
	payload := map[string]any{"id": strings.TrimSpace(milestone.ID)}
	projectBoardPutText(payload, "name", milestone.Name)
	projectBoardPutText(payload, "status", projectBoardMilestoneStatus(milestone, state))
	projectBoardPutText(payload, "content", truncateWorkEvidence(milestone.Content, Limits().EvidenceChars))
	if len(milestone.DependsOn) > 0 {
		payload["depends_on"] = append([]string(nil), milestone.DependsOn...)
	}
	if len(milestone.Required) > 0 {
		payload["required"] = append([]string(nil), milestone.Required...)
	}
	payload["team_closed"] = state.State == store.TeamworkStateClosed
	projectBoardPutText(payload, "team_state", state.State)
	return payload
}

// projectBoardMilestoneStatus 是里程碑状态的**唯一读法**：运行态优先、声明态兜底。
//
// 空串**原样返回**（不补 "pending"）：`空 = pending` 是存储层的约定
// （store/teamwork.go 的常量注释），补值就是把同一条约定写成第二份判据。
func projectBoardMilestoneStatus(milestone store.TeamworkMilestone, state store.TeamworkState) string {
	if runtime := strings.TrimSpace(state.Milestones[strings.TrimSpace(milestone.ID)]); runtime != "" {
		return runtime
	}
	return strings.TrimSpace(milestone.Status)
}

// projectBoardMilestoneItemPayload 把里程碑内的工作项映射成载荷（状态/依赖原样搬运）。
//
// milestone 取**父节点 id**：归属就是 plan 的结构，而声明的 item.Milestone 在
// ValidateTeamworkPlan 里必须与父节点一致（不一致整份计划被拒），读侧不再二次判定。
func projectBoardMilestoneItemPayload(milestoneID string, item store.TeamworkWorkItem) map[string]any {
	payload := map[string]any{
		"id":        strings.TrimSpace(item.ID),
		"milestone": milestoneID,
	}
	projectBoardPutText(payload, "role", item.Role)
	projectBoardPutText(payload, "name", item.Name)
	projectBoardPutText(payload, "status", item.Status)
	projectBoardPutText(payload, "description", truncateWorkEvidence(item.Description, Limits().EvidenceChars))
	projectBoardPutText(payload, "goal", truncateWorkEvidence(item.Goal, Limits().EvidenceChars))
	projectBoardPutText(payload, "session_id", item.SessionID)
	projectBoardPutText(payload, "note", truncateWorkEvidence(item.Note, Limits().EvidenceChars))
	if len(item.DependsOn) > 0 {
		payload["depends_on"] = append([]string(nil), item.DependsOn...)
	}
	if item.StartedAt > 0 {
		payload["started_at"] = item.StartedAt
	}
	if item.FinishedAt > 0 {
		payload["finished_at"] = item.FinishedAt
	}
	return payload
}

// projectBoardTeamState 取整队运行态：显式传入的 TeamworkState 优先，否则用计划里那份兜底。
//
// 注意计划里的 `State` **本身就是**一个 TeamworkState（TeamworkPlan.State 字段类型，见
// store/teamwork.go:98）——所以"读整队收口"读的永远是 TeamworkState.State，
// 不存在"TeamworkPlan.State 是另一个字符串"这种事。
func projectBoardTeamState(plan *store.TeamworkPlan, state *store.TeamworkState) store.TeamworkState {
	if state != nil {
		return *state
	}
	if plan != nil {
		return plan.State
	}
	return store.TeamworkState{}
}

// projectBoardPutText 把非空文本写进载荷（空字段不落盘：读侧沿用"缺失 = 空 = 该域约定值"
// 这一条唯一约定，投影不发明第二套默认值）。
func projectBoardPutText(payload map[string]any, key, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	payload[key] = value
}

// projectBoardBlocks 把映射结果按**落点**拆成两块：work → worktable.jsonl；
// milestone / milestone_item → teamwork.jsonl。落点只由 kind 决定（一处判据）。
func projectBoardBlocks(rows []store.ProjectBoardRow) (work, teamwork []store.ProjectBoardRow) {
	work = make([]store.ProjectBoardRow, 0, len(rows))
	teamwork = make([]store.ProjectBoardRow, 0, len(rows))
	for _, row := range rows {
		if row.Kind == store.ProjectBoardKindWork {
			work = append(work, row)
			continue
		}
		teamwork = append(teamwork, row)
	}
	return work, teamwork
}

// projectBoardRowsForFile 取**某一落点**的行集：按块取数（而不是取一遍再改 Seq），
// 因为每文件的水位是独立事实，块内每行的 seq 直接由映射函数盖章。
func projectBoardRowsForFile(
	scope store.ProjectBoardScope,
	seq uint64,
	at int64,
	workItems []WorkItem,
	teamPlan *store.TeamworkPlan,
	teamState *store.TeamworkState,
	file string,
) []store.ProjectBoardRow {
	work, teamwork := projectBoardBlocks(BuildProjectBoardRows(scope, seq, at, workItems, teamPlan, teamState))
	if file == store.ProjectBoardTeamworkFile {
		return teamwork
	}
	return work
}

// projectBoardRowsFingerprint 生成"**去掉 seq/at 之后**的内容指纹"：同样的
// kind/key/payload 必得同样的指纹（seq/at 每次落盘必然不同，不参与比较）。
//
// 载荷是 map：encoding/json 按键字典序编码 ⇒ 字节稳定（同一份内容没有第二种字节）。
func projectBoardRowsFingerprint(rows []store.ProjectBoardRow) string {
	hash := sha256.New()
	for _, row := range rows {
		hash.Write([]byte(row.Kind))
		hash.Write([]byte{0})
		hash.Write([]byte(row.Key))
		hash.Write([]byte{0})
		if encoded, err := json.Marshal(row.Payload); err == nil {
			hash.Write(encoded)
		} else {
			// 载荷编码失败不可能发生（值都是字符串/数字/布尔/字符串数组）：真发生也
			// 只影响指纹（退化成"内容看起来没变"），不 panic。
			hash.Write([]byte("?"))
		}
		hash.Write([]byte{'\n'})
	}
	return hex.EncodeToString(hash.Sum(nil))
}

// ---------------------------------------------------------------------------
// 2) 触发装配表（off / todo_list / task_complete / tool_complete / all）
// ---------------------------------------------------------------------------

// 触发档名（TriggerFor 的入参词表；配置里写这些词，空/未知按默认档）。
const (
	ProjectBoardHookModeOff          = "off"
	ProjectBoardHookModeTodoList     = "todo_list"
	ProjectBoardHookModeTaskComplete = "task_complete"
	ProjectBoardHookModeToolComplete = "tool_complete"
	ProjectBoardHookModeAll          = "all"
)

// projectBoardTriggerTable 是触发装配**表**：档名 → 命中集合（nil = 一个都不命中；
// anyTool = 任意工具名都命中）。判据只有这一张表，调用点不得再写 `name == "…"`。
var projectBoardTriggerTable = map[string]struct {
	names   []string
	anyTool bool
}{
	// off：投影关掉（一行都不落）。
	ProjectBoardHookModeOff: {},
	// todo_list（**默认档**）：只看"活"变了没有——todolist 四个动作与 task 的两个端点。
	ProjectBoardHookModeTodoList: {names: []string{
		"todo_init", "todo_add", "todo_done", "todo_status", "task_add", "task_complete",
	}},
	// task_complete：只在一次活收尾时投影（省流档；代价是进行中的状态要等收尾才上板）。
	ProjectBoardHookModeTaskComplete: {names: []string{"task_complete"}},
	// tool_complete：任意工具完成都算（最实时，也最费——由 debounce 兜住频率）。
	ProjectBoardHookModeToolComplete: {anyTool: true},
	// all：与 tool_complete 同档（保留为兼容名：配置里"全都要"比"工具完成"更好读）。
	ProjectBoardHookModeAll: {anyTool: true},
}

// projectBoardHookModeOrder 是档名的**声明顺序**：装配期校验的报错文案按它列可选值。
// 它必须与上面那张表的键集**恰好相同**——两者都是同一份词表，谁多一个少一个就是
// "配置里能写的"与"运行时认的"分了家（application/core 的用例钉住这条不变量）。
var projectBoardHookModeOrder = []string{
	ProjectBoardHookModeOff,
	ProjectBoardHookModeTodoList,
	ProjectBoardHookModeTaskComplete,
	ProjectBoardHookModeToolComplete,
	ProjectBoardHookModeAll,
}

// ProjectBoardHookModes 返回触发档名的全集（装配期校验 hook 配置用：拼错的档名要
// **显式启动失败**，而不是让 TriggerFor 的默认档把它兜成"看起来生效"）。
func ProjectBoardHookModes() []string {
	return append([]string(nil), projectBoardHookModeOrder...)
}

// ProjectBoardTrigger 是触发面的编译结果（表 → 集合）。
//
// 零值 = **什么都不触发**（等价 off）：装配方一律用 TriggerFor 构造，不要手搓结构体。
type ProjectBoardTrigger struct {
	names   map[string]struct{}
	anyTool bool
}

// TriggerFor 由档名编译出触发面：off / todo_list（= 空串，默认档）/ task_complete /
// tool_complete / all；**未知档按默认档**（todo_list）处理——"配置写错了"退化成
// "用了默认"，而不是"投影静默全关"或"投影全开"。
func TriggerFor(hookMode string) ProjectBoardTrigger {
	mode := strings.ToLower(strings.TrimSpace(hookMode))
	if mode == "" {
		mode = ProjectBoardHookModeTodoList
	}
	entry, ok := projectBoardTriggerTable[mode]
	if !ok {
		entry = projectBoardTriggerTable[ProjectBoardHookModeTodoList]
	}
	if entry.anyTool {
		return ProjectBoardTrigger{anyTool: true}
	}
	if len(entry.names) == 0 {
		return ProjectBoardTrigger{}
	}
	names := make(map[string]struct{}, len(entry.names))
	for _, name := range entry.names {
		names[name] = struct{}{}
	}
	return ProjectBoardTrigger{names: names}
}

// Matches 报告这次工具完成是否落在触发面上。空工具名恒不命中（没有名字就不是一次工具完成）。
func (t ProjectBoardTrigger) Matches(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	if t.anyTool {
		return true
	}
	_, ok := t.names[name]
	return ok
}

// Off 报告这个触发面是否"什么都不触发"（诊断 / 装配自检用）。
func (t ProjectBoardTrigger) Off() bool { return !t.anyTool && len(t.names) == 0 }

// ---------------------------------------------------------------------------
// 3) 写侧端口与同步器
// ---------------------------------------------------------------------------

// ProjectBoardStore 是投影的**写侧端口**（签名与 store.Router 同形：
// *store.Router 直接就实现它，装配方不必写适配器）。
//
// 键是**项目根 + 会话**（`<项目根>/.seelex/board/<会话 ID>/`）：看板跟着项目走，且
// **按会话分目录**——同一个投影文件只有一个写者（防串写），水位/批号以"一会话一文件"
// 为单位（幂等可判），回收就是摘掉该会话自己的目录。
type ProjectBoardStore interface {
	AppendProjectBoardRows(projectRoot, sessionID, file string, rows []store.ProjectBoardRow) (uint64, error)
	ProjectBoardWatermark(projectRoot, sessionID, file string) (uint64, error)
}

// ProjectBoardSink 是**工具完成**的订阅口（M3 把钩子接到它上：唯一一个方法，
// 名字与钩子的语义一一对应；同步器是"Sink"而不是"hook"——它不改变工具行为）。
type ProjectBoardSink interface {
	OnToolComplete(name string, err error, sessionID string)
}

// 编译期证明：存储面写口就是 Router 的那两个方法，订阅口的实现就是同步器本身。
var (
	_ ProjectBoardStore = (*store.Router)(nil)
	_ ProjectBoardSink  = (*ProjectBoardSyncer)(nil)
)

// TeamworkPlanSource 是**窄可选端口**：Runtime 实现了它才算"这个宿主有 teamwork 计划可读"。
//
// 为什么不塞进 contract.RuntimePort：计划只在 v8 JSON 布局上有意义，且有读它的只有
// 看板一条路；让 RuntimePort 长出一个"没装配就恒返回 false"的成员，会让每个 fake /
// harness 都要为一条没有消费者的能力改接口（与 contract.TeamworkBoardProjection
// 同一口径：**有就有、没有就是没有**）。
type TeamworkPlanSource interface {
	TeamworkPlanFor(sessionID string) (store.TeamworkPlan, bool)
}

// projectBoardDefaultFlushIdle 是 flush 静默窗缺省值（毫秒级抖动的合并窗口：同批
// 多次触发只落一次盘）。
const projectBoardDefaultFlushIdle = 2 * time.Second

// ProjectBoardDeps 是同步器的装配入参。
//
// 取数一律是**函数**（而不是数据）：flush 在独立 goroutine 上"取活体"，装配方因此
// 不必关心视图锁与调用时机——函数内部按既有活体路径取数（见 Service.WorkTableRowsFor）。
type ProjectBoardDeps struct {
	Store         ProjectBoardStore
	WorkTableRows func(sessionID string) []WorkItem
	TeamPlanFor   func(sessionID string) *store.TeamworkPlan
	TeamStateFor  func(sessionID string) *store.TeamworkState
	// ProjectRootFor 是「会话 → 项目根」的解析（"" = 没有落点：未绑定工作区的会话
	// 不落盘）。落点就是 `<项目根>/.seelex/board/`；不再要工作区 ID——它是
	// `ws-<nano>`（机器本地 ID），写进产物会让看板不可提交（契约 §3 三条硬纪律）。
	ProjectRootFor func(sessionID string) string
	Triggers       ProjectBoardTrigger
	FlushIdle      time.Duration
	Now            func() time.Time
}

// ProjectBoardSyncer 是投影同步器：唯一写盘路径 + 唯一 flush goroutine。
//
// 语义（三步，一步都不多做）：
//   - OnToolComplete：判触发面（err != nil 或未命中 ⇒ 返回）→ 标脏 → 唤醒（容量 1、latest-wins）；
//   - flush：取活体 → BuildProjectBoardRows 拆两块 → 指纹去重 → 差集补 tombstone → 落盘；
//   - 失败只记日志（I1：投影可重建，不 panic、不阻塞回合）。
type ProjectBoardSyncer struct {
	deps ProjectBoardDeps
	idle time.Duration
	now  func() time.Time

	wake      chan struct{}
	done      chan struct{}
	closeOnce sync.Once
	wg        sync.WaitGroup

	mu    sync.Mutex
	dirty map[string]struct{}
	// last 是每 (会话, 文件) 的上次落盘记账：指纹（去 seq/at）、水位、key→kind。
	// 它只是**落盘方的内存记账**（不是事实源）：进程重启即空，下次触发重建一遍即可（I1）。
	last map[boardFileKey]boardFileState
}

// boardFileKey 是落盘记账的索引（会话 × 白名单文件）。
type boardFileKey struct {
	session string
	file    string
}

// boardFileState 是一次成功落盘之后的记账值。
type boardFileState struct {
	fingerprint string
	watermark   uint64
	kinds       map[string]string
}

// NewProjectBoardSyncer 装配同步器并启动唯一 flush goroutine。
//
// Store 为 nil（宿主没装配存储面）⇒ 不启动 goroutine，OnToolComplete 恒为空操作——
// "没装配就没有投影"，而不是让宿主为一条没有落点的路径付一个常驻 goroutine。
func NewProjectBoardSyncer(deps ProjectBoardDeps) *ProjectBoardSyncer {
	syncer := &ProjectBoardSyncer{
		deps:  deps,
		idle:  deps.FlushIdle,
		now:   deps.Now,
		wake:  make(chan struct{}, 1), // 容量 1：突发触发合并成一次唤醒（latest-wins）
		done:  make(chan struct{}),
		dirty: make(map[string]struct{}),
		last:  make(map[boardFileKey]boardFileState),
	}
	if syncer.idle <= 0 {
		syncer.idle = projectBoardDefaultFlushIdle
	}
	if syncer.now == nil {
		syncer.now = time.Now
	}
	if deps.Store == nil {
		return syncer
	}
	syncer.wg.Add(1)
	go syncer.run()
	return syncer
}

// OnToolComplete 是 ProjectBoardSink 的实现：**不做任何 IO**。
//
// err != nil（工具失败）⇒ 不标脏：失败的调用不改动"活"，投影没必要跟着抖一次。
func (s *ProjectBoardSyncer) OnToolComplete(name string, err error, sessionID string) {
	if s == nil || s.deps.Store == nil || err != nil {
		return
	}
	if !s.deps.Triggers.Matches(name) {
		return
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return // 没有归属会话就没有落点（投影文件是按会话派生的一层）
	}
	s.mu.Lock()
	s.dirty[sessionID] = struct{}{}
	s.mu.Unlock()
	s.signal()
}

// signal 非阻塞唤醒 flush goroutine（信号口容量 1）。
//
// 丢唤醒是安全的：脏集合是**集合**，且 flush 落盘前重新取活体——多丢一次只意味着
// "下一次唤醒时一起落"，不会丢数据（latest-wins）。
func (s *ProjectBoardSyncer) signal() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// run 是唯一的 flush goroutine：唤醒重设静默窗（debounce），窗口到期落盘，Close 时补一次。
func (s *ProjectBoardSyncer) run() {
	defer s.wg.Done()
	timer := time.NewTimer(s.idle)
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	defer timer.Stop()
	for {
		select {
		case <-s.done:
			s.flushDirty() // 收口补一次：关闭前把脏数据落掉（失败只记日志）
			return
		case <-s.wake:
			// 静默窗从**这一次**触发重新计时：同批多次触发只落一次盘。
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(s.idle)
		case <-timer.C:
			s.flushDirty()
		}
	}
}

// flushDirty 把脏集合取走（先取走再落盘：落盘期间新来的触发会攒进下一批，不会丢）。
func (s *ProjectBoardSyncer) flushDirty() {
	s.mu.Lock()
	dirty := s.dirty
	s.dirty = make(map[string]struct{})
	s.mu.Unlock()
	if len(dirty) == 0 {
		return
	}
	sessions := make([]string, 0, len(dirty))
	for sessionID := range dirty {
		sessions = append(sessions, sessionID)
	}
	sort.Strings(sessions)
	for _, sessionID := range sessions {
		s.flushSession(sessionID)
	}
}

// flushSession 落一个会话的两个投影文件（work 行与 milestone 行各落各的）。
func (s *ProjectBoardSyncer) flushSession(sessionID string) {
	projectRoot := ""
	if s.deps.ProjectRootFor != nil {
		projectRoot = strings.TrimSpace(s.deps.ProjectRootFor(sessionID))
	}
	if projectRoot == "" {
		// 没有项目根 = 没有落点（未绑定工作区的会话不落盘）。
		log.Printf("[project-board] 会话 %s 没有工作区绑定，本次不落投影", sessionID)
		return
	}
	var items []WorkItem
	if s.deps.WorkTableRows != nil {
		items = s.deps.WorkTableRows(sessionID)
	}
	var plan *store.TeamworkPlan
	if s.deps.TeamPlanFor != nil {
		plan = s.deps.TeamPlanFor(sessionID)
	}
	var state *store.TeamworkState
	if s.deps.TeamStateFor != nil {
		state = s.deps.TeamStateFor(sessionID)
	}
	// scope 只写会话：落点本身已经回答了"哪个项目"（行内再写 `ws-<nano>` 会让产物
	// 不可提交，契约 §3 硬纪律）。行内这个 session 与落点目录名同源（存储面还会核对
	// 两者一致，不一致直接拒绝——那是"往别人的目录里塞行"）。
	scope := store.ProjectBoardScope{Session: sessionID}
	at := s.now().UTC().Unix()
	s.flushFile(sessionID, projectRoot, scope, store.ProjectBoardWorkFile, at, items, plan, state)
	s.flushFile(sessionID, projectRoot, scope, store.ProjectBoardTeamworkFile, at, items, plan, state)
}

// flushFile 落一个文件：取水位 → 指纹去重 → 差集补 tombstone → 追加。
//
// 三处判据都在这里（**只此一处**）：
//   - 指纹去重：去掉 seq/at 后内容没变 ⇒ 不写盘（"没变"不该产生一行追加）；
//   - I1 自愈：文件不在了（水位退到记账值之下）⇒ 即使内容没变也重建（否则一次误删
//     会被指纹去重固化成"投影永远缺失"）；
//   - I6 空则退场：没有行、也没有要吊销的 key ⇒ 不建文件（只记账）。
func (s *ProjectBoardSyncer) flushFile(
	sessionID, projectRoot string,
	scope store.ProjectBoardScope,
	file string,
	at int64,
	items []WorkItem,
	plan *store.TeamworkPlan,
	state *store.TeamworkState,
) {
	watermark, err := s.deps.Store.ProjectBoardWatermark(projectRoot, sessionID, file)
	if err != nil {
		log.Printf("[project-board] 读水位失败（%s/%s），本次不落盘：%v", sessionID, file, err)
		return
	}
	rows := projectBoardRowsForFile(scope, watermark+1, at, items, plan, state, file)
	fingerprint := projectBoardRowsFingerprint(rows)
	kinds := make(map[string]string, len(rows))
	for _, row := range rows {
		kinds[row.Key] = row.Kind
	}

	key := boardFileKey{session: sessionID, file: file}
	s.mu.Lock()
	previous, known := s.last[key]
	s.mu.Unlock()

	if known && previous.fingerprint == fingerprint && watermark >= previous.watermark {
		return // 内容没变且文件还在：不写盘
	}
	batch := rows
	if known {
		// 与上次的 key 集合做差：消失的 key 补一行 tombstone（payload{"removed":true}），
		// 否则 upsert 语义下"删掉的条目永远消失不掉"。
		removed := make([]string, 0, len(previous.kinds))
		for previousKey := range previous.kinds {
			if _, still := kinds[previousKey]; !still {
				removed = append(removed, previousKey)
			}
		}
		sort.Strings(removed) // 稳定顺序（同一份内容没有第二种字节序）
		for _, previousKey := range removed {
			batch = append(batch, projectBoardRemovedRow(scope, watermark+1, at, previous.kinds[previousKey], previousKey))
		}
	}
	if len(batch) == 0 {
		s.remember(key, boardFileState{fingerprint: fingerprint, watermark: watermark})
		return
	}
	next, err := s.deps.Store.AppendProjectBoardRows(projectRoot, sessionID, file, batch)
	if err != nil {
		// 不更新记账 ⇒ 下次触发重试（投影是派生数据，失败不 panic、不阻塞回合）。
		log.Printf("[project-board] 追加投影失败（%s/%s），本次不落盘：%v", sessionID, file, err)
		return
	}
	s.remember(key, boardFileState{fingerprint: fingerprint, watermark: next, kinds: kinds})
}

// projectBoardRemovedRow 组装一行 tombstone（折叠读遇到 payload["removed"]==true 即让该 key 消失）。
func projectBoardRemovedRow(scope store.ProjectBoardScope, seq uint64, at int64, kind, key string) store.ProjectBoardRow {
	return projectBoardRow(scope, seq, at, kind, key, map[string]any{"removed": true})
}

// remember 更新落盘记账。
func (s *ProjectBoardSyncer) remember(key boardFileKey, state boardFileState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.last == nil {
		s.last = make(map[boardFileKey]boardFileState)
	}
	s.last[key] = state
}

// Close 幂等：关掉唯一 goroutine 并等它退出（退出前补一次落盘）。
//
// 不泄漏 goroutine：第二次 Close 只等已退出的 goroutine（wg.Wait 立即返回）。
// **不得**在 flush goroutine 自己身上调用（会在 wg.Wait 上自等）。
func (s *ProjectBoardSyncer) Close() {
	if s == nil {
		return
	}
	s.closeOnce.Do(func() { close(s.done) })
	s.wg.Wait()
}

// ---------------------------------------------------------------------------
// 4) Service 侧的只读取数口（M3 装配用；本项**不接钩子**）
// ---------------------------------------------------------------------------

// WorkTableRowsFor 返回指定会话的**活体工作表格投影**（只读；不重算任何状态）。
//
// 取数路径与实时重投影**同一份实现**（buildWorkTable + 按会话取 task 快照 + 既有
// 子代理树/后台执行投影）：投影行因此天然等于"此刻工作表格里那几行"，投影侧不新增
// 状态判定（I3）。后台执行（async）行也在此列——它就是既有活体投影的一部分（工作
// 表格是跨会话台账），行自带 session_id 供读侧归属。
func (service *Service) WorkTableRowsFor(sessionID string) []WorkItem {
	if service == nil || service.Deps.Runtime == nil {
		return nil
	}
	sessionID = strings.TrimSpace(sessionID)
	service.ViewMu.RLock()
	viewSessionID := service.Core.Snapshot.Session.ID
	plan := service.sessionActivePlanLocked(sessionID)
	tree := cloneSubAgentTreeForSync(service.Core.Snapshot.Runtime.SubAgentTree)
	service.ViewMu.RUnlock()
	return buildWorkTable(
		plan,
		service.taskRecordsForSession(sessionID, viewSessionID),
		tree,
		service.asyncRunsForTable(),
	)
}

// ProjectRootFor 返回会话所属工作区的**项目根**（绝对路径；"" = 未绑定/无落点）。
//
// 项目根的权威读法是 WorkspacePort.SessionWorkspace（与 bindSessionProjectRoot /
// restoreInitialWorkspace 同源）——看板的落点就是 `<项目根>/.seelex/board/`，
// 所以"项目根"这一格必须来自**绑定事实**，不能由视图/活跃工作区猜。
//
// 未绑定工作区的会话返回 ""（**没有落点**：不落盘，契约 §3）。这里**不**回退到
// Sessions.Workspace()：那是"视图当前工作区"，拿它当落点等于把别的项目的看板写进去。
func (service *Service) ProjectRootFor(sessionID string) string {
	if service == nil || service.Deps.Workspace == nil {
		return ""
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return ""
	}
	if workspace, ok := service.Deps.Workspace.SessionWorkspace(sessionID); ok {
		return strings.TrimSpace(workspace.RootPath)
	}
	return ""
}

// ProjectBoardTeamPlanFor 按**窄可选端口**取 teamwork 计划（未装配 / 该会话没有计划 ⇒ nil）。
//
// 这一步（端口探测 + 收窄）只写在这里：M3 装配时把它作为 ProjectBoardDeps.TeamPlanFor
// 注入即可，同步器不必知道 Runtime 的接口形状。
func (service *Service) ProjectBoardTeamPlanFor(sessionID string) *store.TeamworkPlan {
	if service == nil || service.Deps.Runtime == nil {
		return nil
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil
	}
	source, ok := service.Deps.Runtime.(TeamworkPlanSource)
	if !ok {
		return nil
	}
	plan, ok := source.TeamworkPlanFor(sessionID)
	if !ok {
		return nil
	}
	return &plan
}

// ProjectBoardTeamStateFor 取该会话的 teamwork **运行态**（里程碑结论 / 整队收口）。
//
// 运行态就在计划载荷的那一格里（TeamworkPlan.State 是 TeamworkState，见
// store/teamwork.go:98）：读它而不是另找一份，是"整队收口只有一个权威格"这条
// 裁决的落地。
func (service *Service) ProjectBoardTeamStateFor(sessionID string) *store.TeamworkState {
	plan := service.ProjectBoardTeamPlanFor(sessionID)
	if plan == nil {
		return nil
	}
	state := plan.State
	return &state
}

// taskRecordsForSession 按会话取 task 注册表快照：视图/活跃会话 = 实时注册表（"" 分区），
// 后台会话 = 自身 scope 分区；Runtime 未实现 TaskSnapshotFor 时退化为全局快照。
//
// **唯一实现**：打点表（workTableTraceBlockFor）与看板取数（WorkTableRowsFor）走同一份，
// 避免"同一条会话作用域判据写两遍"（两遍迟早不一致：一份改了、另一份没改）。
func (state *serviceState) taskRecordsForSession(sessionID, viewSessionID string) []dto.TaskRecord {
	if state == nil || state.Deps.Runtime == nil {
		return nil
	}
	forTasks, ok := state.Deps.Runtime.(interface{ TaskSnapshotFor(string) []dto.TaskRecord })
	if !ok {
		return state.Deps.Runtime.TaskSnapshot()
	}
	scope := sessionID
	if scope == "" || scope == viewSessionID {
		// 视图/活跃会话的 task scope 恒为实时注册表（"" 分区）。
		scope = ""
	}
	return forTasks.TaskSnapshotFor(scope)
}
