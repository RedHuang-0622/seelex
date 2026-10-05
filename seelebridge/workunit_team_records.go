package seelebridge

// workunit_team_records.go — teammate 单元的**会话记录面**（宿主侧，不是子层实现）：
// 记录词表、落盘、清理、恢复说明、重启回灌。
//
// 它属于父那一侧：记录写着"这一份现场跑到哪"，只有生命周期（Begin / Finish / Reclaim /
// Recover）会写它，而这些动作的唯一实现在 workunit_parent.go。teammate 的**注册点**
// （workunit_team.go 的 teamUnit）里因此搜不到任何 store / worktree / jobs 写入。
//
// 记录复用既有的形状（sessionstore.NodeSessionRecord：NodeID = <role>-<itemID> / <role>，
// 带 Goal/Status/Summary/StagesJSON/Worktree），不另立第二份记录类型。

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"sync"

	"github.com/RedHuang-0622/seelex/seelebridge/teamwork"
	"github.com/RedHuang-0622/seelex/seelebridge/workunit"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// ── 会话记录：词表、落盘与清理 ──────────────────────────────────────────

// teamUnitLedger 取契约要的会话存储端口：**结构上**就是既有的
// `*sessionstore.NodeSessionStore`（契约里那条编译期断言钉着同一件事），未装配 = nil。
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
func (r *Runtime) teamUnitScope(sessionID string) (sessionstore.Key, bool) {
	sessionID = strings.TrimSpace(sessionID)
	if r == nil || sessionID == "" {
		return sessionstore.Key{}, false
	}
	r.teamworkMu.Lock()
	backend := r.teamworkBackend
	r.teamworkMu.Unlock()
	if backend == nil || backend.KeyFor == nil {
		return sessionstore.Key{}, false
	}
	key, ok := backend.KeyFor(sessionID)
	if !ok || strings.TrimSpace(key.ProjectID) == "" || strings.TrimSpace(key.SessionID) == "" {
		return sessionstore.Key{}, false
	}
	return key, true
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
	ledger := r.teamUnitLedger()
	if ledger == nil || key.NodeID == "" || key.RoleSessionID == "" {
		return
	}
	scope, ok := r.teamUnitScope(mainSessionID)
	if !ok {
		return
	}
	record := sessionstore.NodeSessionRecord{
		SchemaVersion: sessionstore.NodeSessionSchemaVersion,
		NodeID:        key.NodeID,
		SessionID:     key.RoleSessionID,
		MainSessionID: scope.SessionID,
		Goal:          key.Goal,
		Status:        status,
		Summary:       clipTeamPreview(summary),
		StagesJSON:    teamUnitStages(stage, summary),
		Worktree:      r.teamUnitWorktreeRecord(key.NodeID),
	}
	if err := ledger.Save(scope.ProjectID, scope.SessionID, record); err != nil {
		// 落盘失败**不改执行**：记录是恢复用的证据，丢了它不该让这一轮不跑（下一次写会
		// 重来）。但它必须留痕——静默丢掉就是"重启失忆"被当成正常。
		log.Printf("seelebridge: 落盘 teammate 单元记录 %q（会话 %s）失败：%v", key.NodeID, key.RoleSessionID, err)
	}
}

// teamUnitWorktreeRecord 从 worktree 注册表取现场（路径 + 分支）。没有现场（降级共享
// 主工作区）时返回零值：记录里那一格为空 = 这件事没有独立现场。
func (r *Runtime) teamUnitWorktreeRecord(nodeID string) sessionstore.NodeWorktreeRecord {
	if r == nil || r.worktreeMgr == nil || strings.TrimSpace(nodeID) == "" {
		return sessionstore.NodeWorktreeRecord{}
	}
	info, ok := r.worktreeMgr.Info(nodeID)
	if !ok {
		return sessionstore.NodeWorktreeRecord{}
	}
	return sessionstore.NodeWorktreeRecord{Path: info.Path, Branch: info.Branch}
}

// teamUnitStages 把一条打点（阶段 + 预览）编成记录里的 StagesJSON——形状与子代理节点
// 记录同一份（`[{"stage":…,"preview":…}]`），恢复说明据此说出"中断前到哪一步"。
func teamUnitStages(stage, preview string) []byte {
	stage = strings.TrimSpace(stage)
	if stage == "" {
		return nil
	}
	payload, err := json.Marshal([]map[string]string{{"stage": stage, "preview": clipTeamPreview(preview)}})
	if err != nil {
		return nil
	}
	return payload
}

// teamUnitPreviewLimit 是记录里预览的字符上限：记录要能进恢复说明（system 注入），
// 必须有界。
const teamUnitPreviewLimit = 240

// clipTeamPreview 把一段正文压成有界的一行。
func clipTeamPreview(text string) string {
	flat := strings.Join(strings.Fields(text), " ")
	if runes := []rune(flat); len(runes) > teamUnitPreviewLimit {
		return string(runes[:teamUnitPreviewLimit]) + "…"
	}
	return flat
}

// markTeamUnitRunning 落"这一轮开始了、现场在哪"（运行期落盘：崩溃/重启后回灌的依据）。
func (r *Runtime) markTeamUnitRunning(mainSessionID string, key teamUnitRecordKey) {
	r.saveTeamUnitRecord(mainSessionID, key, teamUnitStatusRunning, key.Goal, "running")
}

// recordTeamUnitRoundOutput 把这一轮的产出预览补进记录（记录回答"跑到哪"：中断之后
// 恢复说明里"中断前结论"那一行就是它）。
func (r *Runtime) recordTeamUnitRoundOutput(mainSessionID string, key teamUnitRecordKey, output string) {
	preview := clipTeamPreview(output)
	if preview == "" {
		return
	}
	r.saveTeamUnitRecord(mainSessionID, key, teamUnitStatusRunning, preview, "round_output")
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
		log.Printf("seelebridge: 清 teammate 单元记录（会话 %s）失败：%v", roleSessionID, err)
	}
}

// clearTeamUnitRecords 清掉本会话团队的**全部**单元记录（整队收口的落点：收口之后
// 这支团队不再有"跑到哪"的事实）。返回清掉的条数（诊断读数）。
//
// 会话号取调用方给的主会话号（不是 `MainSessionID()`）：收口发生在**某个会话**上，
// 而"当前活跃会话"是视图指针，不该拿它当存储键。
func (r *Runtime) clearTeamUnitRecords(sessionID string) int {
	cleared := 0
	for _, record := range r.teamUnitRecords(sessionID, r.teamSceneIndex(sessionID)) {
		r.clearTeamUnitRecord(sessionID, record.SessionID)
		cleared++
	}
	return cleared
}

// ── 恢复说明：一次性注入（与 SubagentResumeNote 同形）──────────────────

// teamResumeState 保存 teammate 单元的恢复说明（按**角色会话号**存：角色会话是回灌的
// 落点，说明只进它自己的下一次装配）。
//
// 与 `subagentResumeState.notes` 同一形状、同一语义（system 注入、读完即消），只是键与
// 落点不同：subagent 的键是节点 id、在节点装配 PromptBlocks 时读；teammate 的键是角色
// 会话号、在 worker 回合装配系统提示时读。
type teamResumeState struct {
	mu    sync.Mutex
	notes map[string]string
}

// setTeamResumeNote 记下该角色会话下一次装配要带的恢复说明。
func (r *Runtime) setTeamResumeNote(roleSessionID, note string) {
	if r == nil || strings.TrimSpace(roleSessionID) == "" || strings.TrimSpace(note) == "" {
		return
	}
	r.teamResume.mu.Lock()
	if r.teamResume.notes == nil {
		r.teamResume.notes = map[string]string{}
	}
	r.teamResume.notes[strings.TrimSpace(roleSessionID)] = note
	r.teamResume.mu.Unlock()
}

// consumeTeamResumeNote 取走该角色会话的恢复说明并**清掉**（一次装配读完即消：恢复
// 说明是"这件事是中断后续跑的"这一刻的事实，不是长期人格设定）。
func (r *Runtime) consumeTeamResumeNote(roleSessionID string) string {
	if r == nil {
		return ""
	}
	roleSessionID = strings.TrimSpace(roleSessionID)
	if roleSessionID == "" {
		return ""
	}
	r.teamResume.mu.Lock()
	defer r.teamResume.mu.Unlock()
	note := r.teamResume.notes[roleSessionID]
	delete(r.teamResume.notes, roleSessionID)
	return note
}

// clearTeamResumeNote 丢掉该角色会话还没被读走的恢复说明（收口清会话内容时用：会话内容
// 都不在了，说明也没有落点）。
func (r *Runtime) clearTeamResumeNote(roleSessionID string) {
	if r == nil {
		return
	}
	r.teamResume.mu.Lock()
	delete(r.teamResume.notes, strings.TrimSpace(roleSessionID))
	r.teamResume.mu.Unlock()
}

// ── 重启回灌 ───────────────────────────────────────────────────────────

// TeamRecovery 是一次重启回灌的宿主读数：团队账本侧（可重派清单 / 活绑定）+
// 契约侧（认领现场 / 回灌会话 / 中断清单）。
type TeamRecovery struct {
	Report teamwork.RecoveryReport
	Resume workunit.Resume
}

// RecoverTeamworkUnits 重启回灌本会话的 teammate 单元（契约 `Unit.Recover` 的落点）：
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
	records := r.teamUnitRecords(sessionID, index)
	recovery.Resume.Sessions = len(records)
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
	// ③ 中断判定：记录说在跑（running/queued），而本进程里既没有它的角色会话句柄、
	// 也没有它的作业句柄。
	interrupted := make([]string, 0)
	seen := make(map[string]bool, len(index))
	addInterrupted := func(nodeID string) {
		if nodeID == "" || seen[nodeID] {
			return
		}
		seen[nodeID] = true
		interrupted = append(interrupted, nodeID)
	}
	surface := func(entry teamSceneEntry, record sessionstore.NodeSessionRecord) bool {
		return r.teamUnitSurfaceAlive(entry, record, alive)
	}
	for _, entry := range index {
		record, ok := records[entry.NodeID]
		if !ok || !workunit.InFlight(record.Status) {
			continue
		}
		if surface(entry, record) {
			continue
		}
		addInterrupted(entry.NodeID)
		// ④ 恢复说明：事实来自记录本身（不编造），注入该角色会话的下一次装配。
		r.setTeamResumeNote(record.SessionID, workunit.RecoveryNote(workunit.KindTeammate, record))
	}
	// 账本侧的同一条事实（状态说在跑、句柄却不在册）也一并列出：**记录缺席**时不至于漏掉
	// 这一件可重派的工作。有执行面的（角色会话还在跑）不算中断——它现在就在本进程里。
	byItem := make(map[string]teamSceneEntry, len(index))
	for _, entry := range index {
		if entry.WorkItem != "" {
			byItem[entry.WorkItem] = entry
		}
	}
	for _, itemID := range report.Interrupted {
		entry, ok := byItem[itemID]
		if !ok {
			continue
		}
		if record, has := records[entry.NodeID]; has && surface(entry, record) {
			continue
		}
		addInterrupted(entry.NodeID)
	}
	recovery.Resume.Interrupted = interrupted
	return recovery, nil
}

// teamUnitRecords 读回这支团队在本会话里的单元记录（按 nodeID 索引）。
//
// 记录文件与子代理节点记录**共用**同一个 `subagents/` 目录，所以这里必须按现场名单过滤：
// 只有 nodeID 落在本会话团队现场名单（计划 + 账本）里的记录才属于 teammate 单元——
// 不猜、不扫目录。
func (r *Runtime) teamUnitRecords(sessionID string, index []teamSceneEntry) map[string]sessionstore.NodeSessionRecord {
	records := map[string]sessionstore.NodeSessionRecord{}
	if r == nil || r.nodeSessionStore == nil || len(index) == 0 {
		return records
	}
	scope, ok := r.teamUnitScope(sessionID)
	if !ok {
		return records
	}
	stored, err := r.nodeSessionStore.List(scope.ProjectID, scope.SessionID)
	if err != nil {
		log.Printf("seelebridge: 读回 teammate 单元记录（会话 %s）失败：%v", sessionID, err)
		return records
	}
	wanted := make(map[string]bool, len(index))
	for _, entry := range index {
		wanted[entry.NodeID] = true
	}
	for _, record := range stored {
		if wanted[strings.TrimSpace(record.NodeID)] {
			records[record.NodeID] = record
		}
	}
	return records
}

// teamUnitSurfaceAlive 报告这个单元在本进程里还有没有**执行面**：
//   - 它自己的角色会话句柄还在册（角色会话是进程内的执行面，见 roleTurnState），或
//   - 计划里这一件事（或这个角色）的作业句柄还在册。
//
// 两者都没有 = 记录说的"在跑"其实已经随进程一起没了 ⇒ 中断（交上层重派或人工处置）。
func (r *Runtime) teamUnitSurfaceAlive(entry teamSceneEntry, record sessionstore.NodeSessionRecord, alive map[string]bool) bool {
	if r.roleSessionLive(record.SessionID) {
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
