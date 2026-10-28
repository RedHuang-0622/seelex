package assemble

// workunit_team_records.go — teammate 单元的**会话记录面**（宿主侧，不是子层实现）：
// 记录词表、落盘、清理、恢复说明、重启回灌。
//
// 它属于父那一侧：记录写着"这一份现场跑到哪"，只有生命周期（Begin / Finish / Reclaim /
// Recover）会写它，而这些动作的唯一实现在 workunit_parent.go。teammate 的**注册点**
// （workunit_team.go 的 teamUnit）里因此搜不到任何 store / worktree / jobs 写入。
//
// 记录复用既有的形状（storeworkunit.Record：NodeID = <role>-<itemID> / <role>，
// 带 Goal/Status/Summary/StagesJSON/Worktree），不另立第二份记录类型。

import (
	"context"
	"log"
	"strings"

	"github.com/RedHuang-0622/Seele/types"
	"github.com/RedHuang-0622/seelex/backend/multiagent/teamwork"
	"github.com/RedHuang-0622/seelex/backend/workunit"
	"github.com/RedHuang-0622/seelex/contract/dto"
	storeworkunit "github.com/RedHuang-0622/seelex/store/workunit"
)

// ── 会话记录：词表、落盘与清理 ──────────────────────────────────────────

// teamUnitLedger 取契约要的会话存储端口：**结构上**就是既有的
// `*storeworkunit.SessionStore`（契约里那条编译期断言钉着同一件事），未装配 = nil。
func (r *Runtime) teamUnitLedger() workunit.SessionLedger {
	if r == nil || r.nodeSessionStore == nil {
		return nil
	}
	return r.nodeSessionStore
}

// teamUnitScope 返回本会话的**存储作用域**（项目 + 会话）：与团队计划 / 绑定账本同一个
// 键（组合根注入的 KeyFor）——记录必须落在同一个项目目录下，回灌才找得到它。
//
// 为什么不读 Router 的 active workspace 或 sessionProjectIDFor：那两处是**视图与绑定**的
// 当前值（切会话会变、测试基座里可能为空），而"这支团队的记录放在哪"是派发那一刻就定下的
// 事实。两处各算一次作用域，迟早算成两个目录——子代理记录那条路上就是这么漂的。
func (r *Runtime) teamUnitScope(sessionID string) (dto.SessionKey, bool) {
	// 与团队面其它读法共用同一份解析（runtime_teamwork_scope.go）；这里只保留这个读法的
	// 名字——"单元记录落在哪个账本"与"团队计划落在哪"必须是同一个键。
	return r.teamworkKeyFor(sessionID)
}

// saveTeamUnitRecord 落一条 teammate 单元的会话记录（整条写；端口只有 Save/List，
// 记录也很小，不需要"局部更新"的第二条路）。
//
// Worktree 一栏记的是**此刻**现场在哪（从 worktree 注册表现查）：合并成功之后现场已经
// 回收，那一栏就是空的——"曾经在哪"是账本（worktrees.jsonl）与审计的事实，不在这里
// 再存一份会过期的路径。
//
// 时间戳刻意不重复一份：哪一刻开始的、哪一刻落定的，**计划里的 item.StartedAt /
// item.FinishedAt 才是那一刻的事实**；记录带 UpdatedAt（由存储写入）足够回答"这份记录
// 有多新"。
func (r *Runtime) saveTeamUnitRecord(mainSessionID string, key teamUnitRecordKey, status, summary, stage string) {
	writer := r.teamUnitRecordWriter(key)
	if writer == nil || key.NodeID == "" || key.RoleSessionID == "" {
		return
	}
	scope, ok := r.teamUnitScope(mainSessionID)
	if !ok {
		return
	}
	record := teamwork.UnitRecord(key, scope.SessionID, status, summary, stage, r.teamUnitWorktreeRecord(key.NodeID))
	// Durable transcript 与主/子代理同一策略：记录里保存当前执行面的工作历史，
	// 进程重启后 WorkItem 会话可从该记录冷恢复；只写本 WorkItem Session，不碰主会话。
	if handle := r.roleSessionHandleFor(key.RoleSessionID); handle != nil && handle.engine != nil {
		record.History = handle.engine.History()
	}
	// 身份（Unit：Kind/Role/Plugins/SystemPrompt）**不在这里拼**：它是写链上 teammate 那一环
	// 的事实（`teamUnitUnitRecordLink`），子代理那一环只填空缺。
	if err := writer.Save(scope.ProjectID, scope.SessionID, record); err != nil {
		// 落盘失败**不改执行**：记录是恢复用的证据，丢了它不该让这一轮不跑（下一次写会
		// 重来）。但它必须留痕——静默丢掉就是"重启失忆"被当成正常。
		log.Printf("seelex: 落盘 teammate 单元记录 %q（会话 %s）失败：%v", key.NodeID, key.RoleSessionID, err)
	}
}

// teamUnitRecordWriter 返回 teammate 那一层的记录写入链：**子代理那条链外面再包一环**
// （add but not modify——teammate 的记录形状就是 subagent 的记录多一个身份块 + 现场四栏写全）。
// 未装配存储（fixture / 纯内存态）= nil，调用方按"不落盘"处理。
func (r *Runtime) teamUnitRecordWriter(key teamUnitRecordKey) *storeworkunit.Writer {
	if r == nil || r.unitRecordWriter == nil {
		return nil
	}
	return r.unitRecordWriter.With(teamUnitUnitRecordLink(key))
}

// teamUnitUnitRecordLink 是团队域 `TeammateUnitRecordLink` 的同身份别名：写链上 teammate
// 那一环（把这一层的身份写进记录）只有一份实现。
var teamUnitUnitRecordLink = teamwork.TeammateUnitRecordLink

// teamUnitWorktreeRecord 从 worktree 注册表取现场（**四栏全取**）。没有现场（降级共享主工作区）
// 时返回零值：记录里那一格为空 = 这件事没有独立现场。
//
// 四栏缺一不可：记录投影是恢复链上**先到**的那一份现场来源（`WorktreeManager.Restore`），
// 而"已在册不覆盖"意味着缺栏的那一份会把后来的"计划 + 账本认领"（`Adopt`，同一条判据：
// 谁先到都一样）挡回去——收尾要的正是 `MainBranch`（这次合回哪条分支）与 `BaseCommit`
// （变基与提交判定的基线）。过去这里只填 Path/Branch，于是重启后恢复出来的现场收尾
// **合不回 main**（缺栏显式发声 + 保留现场；见 worktree_weak_registration_merge_test.go）。
func (r *Runtime) teamUnitWorktreeRecord(nodeID string) storeworkunit.WorktreeRecord {
	if r == nil || r.worktreeMgr == nil || strings.TrimSpace(nodeID) == "" {
		return storeworkunit.WorktreeRecord{}
	}
	info, ok := r.worktreeMgr.Info(nodeID)
	if !ok {
		return storeworkunit.WorktreeRecord{}
	}
	return storeworkunit.WorktreeRecord{
		Path:       info.Path,
		Branch:     info.Branch,
		MainBranch: info.MainBranch,
		BaseCommit: info.BaseCommit,
	}
}

// markTeamUnitRunning 落"这一轮开始了、现场在哪"（运行期落盘：崩溃/重启后回灌的依据）。
func (r *Runtime) markTeamUnitRunning(mainSessionID string, key teamUnitRecordKey) {
	r.saveTeamUnitRecord(mainSessionID, key, teamUnitStatusRunning, key.Goal, "running")
}

// recordTeamUnitRoundOutput 把这一轮的产出预览补进记录（记录回答"跑到哪"：中断之后
// 恢复说明里"中断前结论"那一行就是它）。
func (r *Runtime) recordTeamUnitRoundOutput(mainSessionID string, key teamUnitRecordKey, output string) {
	preview := workunit.ClipPreview(output)
	if preview == "" {
		return
	}
	r.saveTeamUnitRecord(mainSessionID, key, teamUnitStatusRunning, preview, "round_output")
}

// saveTeamUnitTranscript 把当前 WorkItem Session 的工作历史写回既有单元记录，
// 不改 WorkItem 状态、不追加 stage。它用于 DialogueOnly：对话不属于做工回合，
// 但必须与普通回合一样在重启后可恢复。
func (r *Runtime) saveTeamUnitTranscript(mainSessionID string, key teamUnitRecordKey) {
	if r == nil || r.unitRecordWriter == nil || r.nodeSessionStore == nil || strings.TrimSpace(key.NodeID) == "" {
		return
	}
	scope, ok := r.teamUnitScope(mainSessionID)
	if !ok {
		return
	}
	record, err := r.nodeSessionStore.Load(scope.ProjectID, scope.SessionID, key.RoleSessionID)
	if err != nil {
		return
	}
	if handle := r.roleSessionHandleFor(key.RoleSessionID); handle != nil && handle.engine != nil {
		record.History = handle.engine.History()
	}
	if err := r.teamUnitRecordWriter(key).Save(scope.ProjectID, scope.SessionID, record); err != nil {
		log.Printf("seelex: 更新 teammate 对话记录 %q（会话 %s）失败：%v", key.NodeID, key.RoleSessionID, err)
	}
}

// teamUnitResumeHistory 返回冷恢复要注入下一轮的历史。现场角色会话仍在本进程时
// 不读盘（已有内存历史）；只有冷会话才读取该 WorkItem Session 自己的记录。
func (r *Runtime) teamUnitResumeHistory(mainSessionID, roleSessionID string) []types.Message {
	if r == nil || r.nodeSessionStore == nil || r.roleSessionHandleFor(roleSessionID) != nil {
		return nil
	}
	scope, ok := r.teamUnitScope(mainSessionID)
	if !ok {
		return nil
	}
	record, err := r.nodeSessionStore.Load(scope.ProjectID, scope.SessionID, roleSessionID)
	if err != nil {
		return nil
	}
	return append([]types.Message(nil), record.History...)
}

// settleTeamUnitRecord 把尾插的收尾分类写成记录的终态。分类本身就是写进计划的那一份
// （`workunit.ClassifyFinish` 在 settleWorkItem 步 3 里算的），不在这里二次判定。
func (r *Runtime) settleTeamUnitRecord(mainSessionID string, key teamUnitRecordKey, outcome workunit.Outcome) {
	if outcome.Kind == "" {
		return // 幂等路径：这件事已经收口过，结论已落盘在计划里
	}
	r.saveTeamUnitRecord(mainSessionID, key, teamUnitStatusFor(outcome.Kind), outcome.Notice, string(outcome.Kind))
}

// clearTeamUnitRecord 清掉一个角色会话的单元记录（这个单元被回收时随会话内容一起清：
// 记录描述的是"这一份现场跑到哪"，现场与会话都没了，它就没有事实可指）。
//
// 这是"收口之后不再算残留"的落点之一：清完再回灌，读回的就是空读数。
func (r *Runtime) clearTeamUnitRecord(mainSessionID, roleSessionID string) {
	if r == nil || r.nodeSessionStore == nil {
		return
	}
	roleSessionID = strings.TrimSpace(roleSessionID)
	scope, ok := r.teamUnitScope(mainSessionID)
	if !ok || roleSessionID == "" {
		return
	}
	if err := r.nodeSessionStore.Delete(scope.ProjectID, scope.SessionID, roleSessionID); err != nil {
		log.Printf("seelex: 清 teammate 单元记录（会话 %s）失败：%v", roleSessionID, err)
	}
}

// clearTeamUnitRecords 清掉本会话团队的**全部**单元记录（整队收口的落点：收口之后
// 这支团队不再有"跑到哪"的事实）。返回清掉的条数（诊断读数）。
//
// 会话号取调用方给的主会话号（不是 `MainSessionID()`）：收口发生在**某个会话**上，
// 而"当前活跃会话"是视图指针，不该拿它当存储键。
func (r *Runtime) clearTeamUnitRecords(sessionID string) int {
	reader := r.teamUnitReader(sessionID, r.teamSceneIndex(sessionID))
	list, err := reader.List()
	if err != nil {
		log.Printf("seelex: 读回 teammate 单元记录（会话 %s）失败：%v", sessionID, err)
		return 0
	}
	cleared := 0
	for _, unit := range list {
		r.clearTeamUnitRecord(sessionID, unit.SessionID)
		cleared++
	}
	return cleared
}

// ── 恢复说明：一次性注入（与 SubagentResumeNote 同形）──────────────────

// teammate 单元的恢复说明按**角色会话号**存：角色会话是回灌的落点，说明只进它自己的下一次
// 装配。容器只有一份（`resumeNotes`，见 resume_notes.go）——**键语义留在这里**：subagent 的键
// 是节点 id、在节点装配 PromptBlocks 时读；teammate 的键是角色会话号、在 worker 回合装配系统
// 提示时读。正文也只有一份（`workunit.RecoveryNote`）。

// setTeamResumeNote 记下该角色会话下一次装配要带的恢复说明。
func (r *Runtime) setTeamResumeNote(roleSessionID, note string) {
	if r == nil {
		return
	}
	r.teamResume.Set(roleSessionID, note)
}

// consumeTeamResumeNote 取走该角色会话的恢复说明并**清掉**（一次装配读完即消：恢复
// 说明是"这件事是中断后续跑的"这一刻的事实，不是长期人格设定）。
func (r *Runtime) consumeTeamResumeNote(roleSessionID string) string {
	if r == nil {
		return ""
	}
	return r.teamResume.Take(roleSessionID)
}

// clearTeamResumeNote 丢掉该角色会话还没被读走的恢复说明（收口清会话内容时用：会话内容
// 都不在了，说明也没有落点）。
func (r *Runtime) clearTeamResumeNote(roleSessionID string) {
	if r == nil {
		return
	}
	r.teamResume.Clear(roleSessionID)
}

// ── 重启回灌 ───────────────────────────────────────────────────────────

// TeamRecovery 是一次重启回灌的宿主读数：团队账本侧（可重派清单 / 活绑定）+
// 契约侧（认领现场 / 回灌会话 / 中断清单）。
type TeamRecovery struct {
	Report teamwork.RecoveryReport
	Resume workunit.Resume
}

// RecoverTeamworkUnits 重启回灌本会话的 teammate 单元（契约 `Lifecycle.Recover` 的落点）：
//
//	① 认领现场：`adoptTeamworkScenes`（**必须在 `Prune` 之前**——顺序是判据的一部分，
//	   否则"干净但还没合并"的现场会被当孤儿连分支一起删，F4）；
//	② 团队账本：`Coordinator.Recover`（状态说在跑、句柄已作废的清单 + 活绑定）；
//	③ 会话回灌：读回记录 → 对"记录说在跑、本进程已无它的执行面"的单元判**中断**；
//	④ 注入恢复说明：把 `workunit.RecoveryNote` 记到该角色会话名下，等它下一次装配
//	   （一次性读完即消）。
//
// 它**不动**任何现场与会话：中断之后要接着干，记忆与现场必须还在（这正是"恢复之后
// 上下文记忆必须建在"的落点）。未装配存储/编排面时给出空读数，不是错误。
func (r *Runtime) RecoverTeamworkUnits(ctx context.Context, sessionID string) (TeamRecovery, error) {
	recovery := TeamRecovery{}
	sessionID = strings.TrimSpace(sessionID)
	if r == nil || sessionID == "" {
		return recovery, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	// ① 现场：认领（不重建、不清理）。无现场 = 0（幂等 no-op）。
	recovery.Resume.Scenes = r.adoptTeamworkScenes(sessionID)
	index := r.teamSceneIndex(sessionID)
	// 读法只有一份（workunit.UnitReader）：List → 按现场名单过滤 → 折算成读数
	// （workunit.ProgressOf）；此前这里是"自己 List + 自己按名单过滤 + 自己判 InFlight"的
	// 第三份实现，与子代理侧、与生命周期的记录读面各写一遍。
	reader := r.teamUnitReader(sessionID, index)
	records, err := reader.Records()
	if err != nil {
		log.Printf("seelex: 读回 teammate 单元记录（会话 %s）失败：%v", sessionID, err)
		records = nil
	}
	// 一次读、两份视图：读数（判在跑）与原始记录（恢复说明的正文）。**只读一次账本**——
	// 读面返回原始清单，折算仍只有 `ProgressOf` 那一处。
	progress := make(map[string]workunit.Progress, len(records))
	stored := make(map[string]storeworkunit.Record, len(records))
	for _, record := range records {
		unit := workunit.ProgressOf(workunit.KindTeammate, record)
		progress[unit.NodeID] = unit
		stored[unit.NodeID] = record
	}
	recovery.Resume.Sessions = len(progress)
	// ② 团队账本读数（可重派清单）。未装配编排面 = 只按记录回灌。
	coordinator, coordErr := r.coordinatorForSession(sessionID)
	if coordErr != nil {
		return recovery, nil
	}
	report, err := coordinator.Recover(ctx)
	if err != nil {
		return recovery, err
	}
	recovery.Report = report
	alive, err := coordinator.AliveSceneNodes(ctx)
	if err != nil {
		return recovery, err
	}
	// ③ 中断判定 + ④ 恢复说明：判据（记录说在跑且执行面不在 / 账本侧同一条事实 / 去重保序）
	// 与说明正文都在团队域（`InterruptedUnits`）；本层只给"执行面还在不在"那一格读数。
	units := teamwork.InterruptedUnits(index, progress, stored, report.Interrupted,
		func(entry teamwork.SceneEntry, roleSessionID string) bool {
			return r.teamUnitSurfaceAlive(entry, roleSessionID, alive)
		})
	for roleSessionID, note := range units.Notes {
		r.setTeamResumeNote(roleSessionID, note)
	}
	recovery.Resume.Interrupted = units.Interrupted
	return recovery, nil
}

// teamUnitReader 组装 teammate 侧的读面（`workunit.UnitReader`）：作用域解析仍只有
// `teamUnitScope` 一处，**归属过滤**在这里给出。
//
// 记录文件与子代理节点记录**共用**同一个 `subagents/` 目录，所以必须按现场名单过滤：
// 只有 nodeID 落在本会话团队现场名单（计划 + 账本）里的记录才属于 teammate 单元——
// 不猜、不扫目录（过滤本身由读面的 `owned` 回调表达，读法不在这里复制一遍）。
func (r *Runtime) teamUnitReader(sessionID string, index []teamSceneEntry) *workunit.UnitReader {
	if r == nil || len(index) == 0 {
		return workunit.NewUnitReader(nil, "", "", workunit.KindTeammate, nil)
	}
	scope, ok := r.teamUnitScope(sessionID)
	if !ok {
		return workunit.NewUnitReader(nil, "", "", workunit.KindTeammate, nil)
	}
	// 归属过滤的判据在团队域（名单 = `teamwork.SceneIndex` 的产物）：这里只把"这条记录
	// 的 nodeID 在不在名单里"交给域包，不在装配层另建一份集合与比较口径。
	return workunit.NewUnitReader(r.teamUnitLedger(), scope.ProjectID, scope.SessionID, workunit.KindTeammate,
		func(record storeworkunit.Record) bool {
			return teamwork.SceneOwnsNode(index, record.NodeID)
		})
}

// teamUnitSurfaceAlive 报告这个单元在本进程里还有没有**执行面**：
//   - 它自己的角色会话句柄还在册（角色会话是进程内的执行面，见 roleTurnState），或
//   - 计划里这一件事（或这个角色）的作业句柄还在册。
//
// 两者都没有 = 记录说的"在跑"其实已经随进程一起没了 ⇒ 中断（交上层重派或人工处置）。
func (r *Runtime) teamUnitSurfaceAlive(entry teamSceneEntry, roleSessionID string, alive map[string]bool) bool {
	if r.roleSessionLive(roleSessionID) {
		return true
	}
	if alive[entry.NodeID] {
		return true
	}
	return entry.Role != "" && alive[entry.Role]
}

// roleSessionLive 报告一个角色会话在本进程里是否还活着。
//
// 角色会话**刻意不接 DurableHistory**（它是进程内执行面），因此重启即空——这正是
// "记录说在跑、而进程里已经没有它"这条判据的依据。
func (r *Runtime) roleSessionLive(roleSessionID string) bool {
	roleSessionID = strings.TrimSpace(roleSessionID)
	if r == nil || roleSessionID == "" {
		return false
	}
	state := r.roleTurnState()
	state.mu.Lock()
	defer state.mu.Unlock()
	return state.sessions[roleSessionID] != nil
}
