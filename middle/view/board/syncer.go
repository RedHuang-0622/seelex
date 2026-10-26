package board

import (
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
	"github.com/RedHuang-0622/seelex/store"
)

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
	TeamworkPlanFor(sessionID string) (dto.TeamworkPlan, bool)
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
	WorkTableRows func(sessionID string) []model.WorkItem
	TeamPlanFor   func(sessionID string) *dto.TeamworkPlan
	TeamStateFor  func(sessionID string) *dto.TeamworkState
	// ProjectRootFor 是「会话 → 项目根」的解析（"" = 没有落点：未绑定工作区的会话
	// 不落盘）。落点就是 `<项目根>/.seelex/board/`；不再要工作区 ID——它是
	// `ws-<nano>`（机器本地 ID），写进产物会让看板不可提交（契约 §3 三条硬纪律）。
	ProjectRootFor func(sessionID string) string
	// EvidenceChars 是投影文本的字符上限（0 = 不限）。由装配根从生效
	// limit.Limits 投影后注入，避免 middle 反向依赖 backend/limit。
	EvidenceChars func() int
	Triggers      ProjectBoardTrigger
	FlushIdle     time.Duration
	Now           func() time.Time
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
	var items []model.WorkItem
	if s.deps.WorkTableRows != nil {
		items = s.deps.WorkTableRows(sessionID)
	}
	var plan *dto.TeamworkPlan
	if s.deps.TeamPlanFor != nil {
		plan = s.deps.TeamPlanFor(sessionID)
	}
	var state *dto.TeamworkState
	if s.deps.TeamStateFor != nil {
		state = s.deps.TeamStateFor(sessionID)
	}
	// scope 只写会话：落点本身已经回答了"哪个项目"（行内再写 `ws-<nano>` 会让产物
	// 不可提交，契约 §3 硬纪律）。行内这个 session 与落点目录名同源（存储面还会核对
	// 两者一致，不一致直接拒绝——那是"往别人的目录里塞行"）。
	scope := store.ProjectBoardScope{Session: sessionID}
	at := s.now().UTC().Unix()
	evidenceChars := 0
	if s.deps.EvidenceChars != nil {
		evidenceChars = s.deps.EvidenceChars()
	}
	s.flushFile(sessionID, projectRoot, scope, store.ProjectBoardWorkFile, at, items, plan, state, evidenceChars)
	s.flushFile(sessionID, projectRoot, scope, store.ProjectBoardTeamworkFile, at, items, plan, state, evidenceChars)
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
	items []model.WorkItem,
	plan *dto.TeamworkPlan,
	state *dto.TeamworkState,
	evidenceChars int,
) {
	watermark, err := s.deps.Store.ProjectBoardWatermark(projectRoot, sessionID, file)
	if err != nil {
		log.Printf("[project-board] 读水位失败（%s/%s），本次不落盘：%v", sessionID, file, err)
		return
	}
	rows := projectBoardRowsForFile(scope, watermark+1, at, items, plan, state, file, evidenceChars)
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
