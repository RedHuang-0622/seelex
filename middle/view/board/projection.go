package board

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
	"strings"

	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/kernel/worktable"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
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
//     key = "work:<id>"，载荷 = model.WorkItem 的字段白名单；
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
	workItems []model.WorkItem,
	teamPlan *dto.TeamworkPlan,
	teamState *dto.TeamworkState,
	evidenceChars int,
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
			store.ProjectBoardKindWork, "work:"+id, projectBoardWorkPayload(item, evidenceChars)))
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
			projectBoardMilestonePayload(milestone, state, evidenceChars)))
		for _, item := range milestone.Items {
			itemID := strings.TrimSpace(item.ID)
			if itemID == "" {
				continue
			}
			rows = append(rows, projectBoardRow(scope, seq, at,
				store.ProjectBoardKindMilestoneItem, "milestone_item:"+itemID,
				projectBoardMilestoneItemPayload(milestoneID, item, evidenceChars)))
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
func projectBoardWorkPayload(item model.WorkItem, evidenceChars int) map[string]any {
	payload := map[string]any{
		"id":    strings.TrimSpace(item.ID),
		"kind":  strings.TrimSpace(item.Kind),
		"phase": strings.TrimSpace(item.Phase),
	}
	projectBoardPutText(payload, "task", item.Task)
	projectBoardPutText(payload, "status", item.Status)
	projectBoardPutText(payload, "description", worktable.TruncateWorkEvidence(item.Description, evidenceChars))
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
func projectBoardMilestonePayload(milestone dto.TeamworkMilestone, state dto.TeamworkState, evidenceChars int) map[string]any {
	payload := map[string]any{"id": strings.TrimSpace(milestone.ID)}
	projectBoardPutText(payload, "name", milestone.Name)
	projectBoardPutText(payload, "status", projectBoardMilestoneStatus(milestone, state))
	projectBoardPutText(payload, "content", worktable.TruncateWorkEvidence(milestone.Content, evidenceChars))
	if len(milestone.DependsOn) > 0 {
		payload["depends_on"] = append([]string(nil), milestone.DependsOn...)
	}
	if len(milestone.Required) > 0 {
		payload["required"] = append([]string(nil), milestone.Required...)
	}
	payload["team_closed"] = state.State == dto.TeamworkStateClosed
	projectBoardPutText(payload, "team_state", state.State)
	return payload
}

// projectBoardMilestoneStatus 是里程碑状态的**唯一读法**：运行态优先、声明态兜底。
//
// 空串**原样返回**（不补 "pending"）：`空 = pending` 是存储层的约定
// （store/teamwork.go 的常量注释），补值就是把同一条约定写成第二份判据。
func projectBoardMilestoneStatus(milestone dto.TeamworkMilestone, state dto.TeamworkState) string {
	if runtime := strings.TrimSpace(state.Milestones[strings.TrimSpace(milestone.ID)]); runtime != "" {
		return runtime
	}
	return strings.TrimSpace(milestone.Status)
}

// projectBoardMilestoneItemPayload 把里程碑内的工作项映射成载荷（状态/依赖原样搬运）。
//
// milestone 取**父节点 id**：归属就是 plan 的结构，而声明的 item.Milestone 在
// ValidateTeamworkPlan 里必须与父节点一致（不一致整份计划被拒），读侧不再二次判定。
func projectBoardMilestoneItemPayload(milestoneID string, item dto.TeamworkWorkItem, evidenceChars int) map[string]any {
	payload := map[string]any{
		"id":        strings.TrimSpace(item.ID),
		"milestone": milestoneID,
	}
	projectBoardPutText(payload, "role", item.Role)
	projectBoardPutText(payload, "name", item.Name)
	projectBoardPutText(payload, "status", item.Status)
	projectBoardPutText(payload, "description", worktable.TruncateWorkEvidence(item.Description, evidenceChars))
	projectBoardPutText(payload, "goal", worktable.TruncateWorkEvidence(item.Goal, evidenceChars))
	projectBoardPutText(payload, "session_id", item.SessionID)
	projectBoardPutText(payload, "note", worktable.TruncateWorkEvidence(item.Note, evidenceChars))
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
func projectBoardTeamState(plan *dto.TeamworkPlan, state *dto.TeamworkState) dto.TeamworkState {
	if state != nil {
		return *state
	}
	if plan != nil {
		return plan.State
	}
	return dto.TeamworkState{}
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
	workItems []model.WorkItem,
	teamPlan *dto.TeamworkPlan,
	teamState *dto.TeamworkState,
	file string,
	evidenceChars int,
) []store.ProjectBoardRow {
	work, teamwork := projectBoardBlocks(BuildProjectBoardRows(scope, seq, at, workItems, teamPlan, teamState, evidenceChars))
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
