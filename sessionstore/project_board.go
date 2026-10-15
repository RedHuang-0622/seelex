package sessionstore

// project_board.go — 项目看板（project board）的**按项目派生投影**存储面。
//
// 口径（这是本文件存在的唯一理由，写在这里防误读）：
//
//  1. 它**不是领域事实源**（I1/I2）：投影是派生数据，可删可重建；读侧活体优先，
//     文件被篡改不影响任何活体/领域判定（判定照旧读 task/teamwork 事实）。
//  2. 它**不新增第二份判据**（I3）：这里只做**字段映射 + 追加 + 折叠**，不复制任何
//     领域状态机的判定逻辑。
//  3. 它**不进模型上下文**（I7）：本模块只负责落盘与读回，装配侧不得把它塞进
//     system/project 前缀或请求尾部。
//  4. 落点在**项目目录**：`<项目根>/.seelex/board/`。看板是"真相源向项目目录的公开
//     投影"——落在项目根里，其它 agent（codex / claude code）用普通文件读法就能看见；
//     不再钉在数据根的 hash 目录（那是本机私货，只有 Seelex 自己的读工具能折叠）。
//     代价是失去数据根锁的覆盖：并发只由按路径分片的独立锁（本文件 projectBoardLock）
//     兜底，跨进程一致性由使用者负责（契约 §9.3 R-3）。
//  5. 追加复用既有 `truncateCrashTail`（jsonl_io.go）：先截残尾、再逐行 O_APPEND。
//  6. 独立锁：投影**不新增 storageModule 枚举**（不改 module_heads.go 的
//     枚举—锁对应关系），自带按**文件路径**分片的独立锁（与 roleDraftLock 同形）。
//     跨模块共用一把锁会把"落投影"与"落消息/落团队事实"串成一个串行点。
//  7. 有界：行数越界先**压实**（折成每个 (会话, Key) 的最新一行再原子重写），压实后
//     仍越界才**显式报错**（ProjectBoardMaxRows），不静默截断。压实必须存在：同步器
//     每轮"整份重盖"，只报错的话触顶即"投影永久停在旧版本"（见 compactProjectBoardFile）。
//  8. **可提交件三条硬纪律**（契约 §3，现行要求）：产物不含 `lock.owner`/会话正文/
//     媒体；不含 `ws-<nano>`、绝对路径、用户名（故行内 scope 不再写工作区 ID：
//     落点本身已经回答了"哪个项目"）；不含抖动时间戳（人类件的渲染不含时刻，
//     机器件只有"内容真变了才追加"的事件时刻）。

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// 投影契约常量（schema / 上限 / 白名单文件名）。
const (
	// ProjectBoardSchema 是行信封的 schema 版本；未知 schema 的行在折叠时跳过。
	ProjectBoardSchema = 1
	// ProjectBoardMaxRows 是单个投影文件的行数上限（有界，I5）。
	ProjectBoardMaxRows = 512
	// ProjectBoardWorkFile 是工作台投影文件名（只允许白名单名，防路径穿越）。
	ProjectBoardWorkFile = "worktable.jsonl"
	// ProjectBoardTeamworkFile 是团队编排投影文件名。
	ProjectBoardTeamworkFile = "teamwork.jsonl"
	// ProjectBoardMarkdownFile 是人读看板文件名（由折叠后的行确定性渲染，
	// 不落时刻、不含本机标识 ⇒ 可提交）。
	ProjectBoardMarkdownFile = "BOARD.md"
)

// 投影行 kind（与 plan 的三种投影对象一致）。
const (
	ProjectBoardKindWork          = "work"
	ProjectBoardKindMilestone     = "milestone"
	ProjectBoardKindMilestoneItem = "milestone_item"
)

// 落点目录名：`<项目根>/.seelex/board`。
const (
	projectBoardMetaDirName = ".seelex"
	projectBoardDirName     = "board"
)

// projectBoardRemovedField 是 tombstone 字段名：某 Key 的最新行 payload 里
// `"removed": true` 表示该 Key 从投影里消失（upsert 语义下"删除的条目永远消失不掉"
// 的解法，本契约有意补强）。
const projectBoardRemovedField = "removed"

// ProjectBoardScope 是行信封的作用域：Session 是**字面会话 ID**（不是绝对路径）。
//
// Project 槽位保留给"行内需要显式指名项目"的场合，但**生产路径不写它**：落点
// `<项目根>/.seelex/board/` 就是项目本身，工作区 ID 是 `ws-<nano>`（机器本地 ID），
// 写进产物会让它不可提交（契约 §3 硬纪律）。
type ProjectBoardScope struct {
	Project string `json:"project,omitempty"`
	Session string `json:"session"`
}

// ProjectBoardRow 是投影行的**信封**：一行 = 一条投影事实，只追加、不重写。
//
// Seq 在**文件内**单调递增，文件的水位 = 最大 Seq。Payload 是字段映射后的载荷
// （encoding/json 对 map 键按字典序编码 ⇒ 字节稳定）。
type ProjectBoardRow struct {
	Schema  int               `json:"schema"`
	Kind    string            `json:"kind"` // work | milestone | milestone_item
	Key     string            `json:"key"`
	Seq     uint64            `json:"seq"`
	At      int64             `json:"at"`
	Scope   ProjectBoardScope `json:"scope"`
	Payload map[string]any    `json:"payload,omitempty"`
}

// ProjectBoardKinds 返回本契约承认的 kind 全集（有序，便于测试与文档同步）。
func ProjectBoardKinds() []string {
	return []string{ProjectBoardKindWork, ProjectBoardKindMilestone, ProjectBoardKindMilestoneItem}
}

// isProjectBoardKind 报告 kind 是否在本契约的 kind 全集内。
func isProjectBoardKind(kind string) bool {
	switch kind {
	case ProjectBoardKindWork, ProjectBoardKindMilestone, ProjectBoardKindMilestoneItem:
		return true
	default:
		return false
	}
}

// projectBoardFile 校验并归一化投影文件名（只允许白名单名，防路径穿越）。
func projectBoardFile(file string) (string, error) {
	name := strings.TrimSpace(file)
	switch name {
	case ProjectBoardWorkFile, ProjectBoardTeamworkFile, ProjectBoardMarkdownFile:
		return name, nil
	default:
		return "", fmt.Errorf("session storage: project board 文件名 %q 不在白名单（%s / %s / %s）",
			file, ProjectBoardWorkFile, ProjectBoardTeamworkFile, ProjectBoardMarkdownFile)
	}
}

// validateProjectRoot 校验项目根（落点的唯一输入）。
//
// 空根 = 没有落点（未绑定工作区的会话不落盘，契约 §3）：**显式报错**，不让一个
// 空串被 filepath.Join 悄悄折成相对路径。
func validateProjectRoot(projectRoot string) error {
	if strings.TrimSpace(projectRoot) == "" {
		return errors.New("session storage: project root is required（未绑定工作区的会话没有看板落点）")
	}
	return nil
}

// FoldProjectBoardRows 是投影行**唯一的读法**（折叠读）：把追加日志折成当前视图。
//
// 规则：
//   - 未知 schema / 未知 kind 的行**跳过**并 logf（logf 可为 nil）；
//   - 同一 **(会话, Key)** 取 **Seq 最大**的行（并列时后一条胜）——落点是**项目级平铺
//     文件**，多个会话共用一个文件，里程碑 id / 工作项 id 是"按会话命名"的，跨会话
//     同名不是同一条事实（键若不带会话，两条会话的 `milestone:M1` 会互相覆盖）；
//   - 取胜行的 payload[removed]==true ⇒ 该 Key 从结果里**消失**（tombstone）；
//   - 输出按 (Kind, Key, Session) **稳定排序**（同一份输入必得同一份输出）。
func FoldProjectBoardRows(rows []ProjectBoardRow, logf func(string, ...any)) []ProjectBoardRow {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	type foldKey struct{ session, key string }
	latest := make(map[foldKey]ProjectBoardRow, len(rows))
	for _, row := range rows {
		if row.Schema != ProjectBoardSchema {
			logf("project_board: 跳过未知 schema %d 的行（key=%q）", row.Schema, row.Key)
			continue
		}
		if !isProjectBoardKind(row.Kind) {
			logf("project_board: 跳过未知 kind %q 的行（key=%q）", row.Kind, row.Key)
			continue
		}
		key := strings.TrimSpace(row.Key)
		if key == "" {
			logf("project_board: 跳过没有 key 的行（kind=%q）", row.Kind)
			continue
		}
		bucket := foldKey{session: strings.TrimSpace(row.Scope.Session), key: key}
		if current, exists := latest[bucket]; !exists || row.Seq >= current.Seq {
			latest[bucket] = row
		}
	}
	folded := make([]ProjectBoardRow, 0, len(latest))
	for _, row := range latest {
		if removed, ok := row.Payload[projectBoardRemovedField].(bool); ok && removed {
			continue
		}
		folded = append(folded, row)
	}
	sort.SliceStable(folded, func(i, j int) bool {
		return projectBoardRowLess(folded[i], folded[j])
	})
	return folded
}

// projectBoardRowLess 是投影行的**唯一排序口径**：(Kind, Key, Session)。
func projectBoardRowLess(left, right ProjectBoardRow) bool {
	if left.Kind != right.Kind {
		return left.Kind < right.Kind
	}
	if left.Key != right.Key {
		return left.Key < right.Key
	}
	return left.Scope.Session < right.Scope.Session
}

// ---------------------------------------------------------------------------
// 路径派生（落点 = <项目根>/.seelex/board；不出现绝对路径与用户名到行内）
// ---------------------------------------------------------------------------

// ProjectBoardDir 返回某项目根的看板目录：`<项目根>/.seelex/board`。
//
// 导出是因为装配侧/读面都要说清"看板在哪"（工具描述、文档、诊断），而落点的
// 派生只应该有这一处实现。
func ProjectBoardDir(projectRoot string) string {
	return filepath.Join(projectRoot, projectBoardMetaDirName, projectBoardDirName)
}

// projectBoardPath 返回某项目看板目录下某白名单文件的物理路径。
func projectBoardPath(projectRoot, file string) string {
	return filepath.Join(ProjectBoardDir(projectRoot), file)
}

// ---------------------------------------------------------------------------
// 独立锁（不新增 storageModule 枚举；与 roleDraftLock 同形，按文件路径分片）
// ---------------------------------------------------------------------------

// projectBoardLocks 是投影文件的独立锁表（path → *sync.Mutex）。
//
// 为什么不并入 sessionModuleLocks：并入就得给 storageModule 补一个枚举 + mutexFor
// 的 case（本契约明确不新增枚举），且会把"落投影"与"落消息/落团队事实"串成一个
// 串行点——那正是 media 别名成 messageMu 的教训。
//
// 落点搬到项目目录后这把锁**更重要**：同一项目的多个会话共用一个平铺文件，而数据根
// 锁（`lock.owner`）已经不覆盖它了（文件在项目根、不在数据根）。
var projectBoardLocks sync.Map

// projectBoardLock 返回某投影文件路径的独立锁（按路径分片，互不阻塞）。
func projectBoardLock(path string) *sync.Mutex {
	value, _ := projectBoardLocks.LoadOrStore(path, &sync.Mutex{})
	return value.(*sync.Mutex)
}

// ---------------------------------------------------------------------------
// 落盘：追加 / 读回 / 水位 / 回收
// ---------------------------------------------------------------------------

// readProjectBoardFile 读回投影文件的**原始行**（未折叠）：跳过空行与崩溃残尾
// （未换行收尾的半行），不能解析的坏行丢弃（它不是一行事实）。
func readProjectBoardFile(path string) ([]ProjectBoardRow, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return decodeProjectBoardRows(data), nil
}

// decodeProjectBoardRows 解析 JSONL 投影行（残尾 / 空行 / 坏行跳过）。
func decodeProjectBoardRows(data []byte) []ProjectBoardRow {
	segments := bytes.Split(data, []byte{'\n'})
	rows := make([]ProjectBoardRow, 0, len(segments))
	for index, segment := range segments {
		if len(bytes.TrimSpace(segment)) == 0 {
			continue
		}
		// 末段未换行收尾 = 崩溃残尾（视为未提交，直接丢弃）。
		if index == len(segments)-1 {
			continue
		}
		var row ProjectBoardRow
		if err := json.Unmarshal(segment, &row); err != nil {
			continue
		}
		rows = append(rows, row)
	}
	return rows
}

// encodeProjectBoardRows 把行集编码成 JSONL 字节（每行一条、换行收尾）。
//
// 回收（重写文件）与追加共用同一份编码口，避免"同一份行有两种字节"。
func encodeProjectBoardRows(rows []ProjectBoardRow) ([]byte, error) {
	buffer := make([]byte, 0, len(rows)*256)
	for _, row := range rows {
		line, err := json.Marshal(row)
		if err != nil {
			return nil, fmt.Errorf("session storage: 编码 project board 行: %w", err)
		}
		buffer = append(buffer, line...)
		buffer = append(buffer, '\n')
	}
	return buffer, nil
}

// projectBoardBatchAlreadyAppended 报告这一批是否**已经落过**：文件里已有同一
// (会话, Seq) 的行。
//
// 只看批首行：一批行是同一个会话的同一个 Seq 盖章（落盘方按"文件水位 + 1"整批盖章），
// 所以"批首已存在" ⇔ "这批已存在"。
func projectBoardBatchAlreadyAppended(existing, rows []ProjectBoardRow) bool {
	if len(existing) == 0 || len(rows) == 0 {
		return false
	}
	batchSession := strings.TrimSpace(rows[0].Scope.Session)
	batchSeq := rows[0].Seq
	for _, row := range existing {
		if row.Seq == batchSeq && strings.TrimSpace(row.Scope.Session) == batchSession {
			return true
		}
	}
	return false
}

// projectBoardWatermark 返回投影文件的水位（= 最大 Seq）。
func projectBoardWatermark(rows []ProjectBoardRow) uint64 {
	var watermark uint64
	for _, row := range rows {
		if row.Seq > watermark {
			watermark = row.Seq
		}
	}
	return watermark
}

// projectBoardMaxSeq 返回一批行的 Seq 最大值。
func projectBoardMaxSeq(rows []ProjectBoardRow) uint64 {
	var max uint64
	for _, row := range rows {
		if row.Seq > max {
			max = row.Seq
		}
	}
	return max
}

// readProjectBoardRowsForAppend 读回投影文件的**完整行**：先截崩溃残尾（复用
// jsonl_io.go 的 truncateCrashTail 这一份实现），再整份解码。返回时文件已停在
// 完整行边界上——追加与压实都能安全接着做。
func readProjectBoardRowsForAppend(path string) ([]ProjectBoardRow, error) {
	handle, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	defer handle.Close()
	if err := truncateCrashTail(handle); err != nil {
		return nil, err
	}
	if _, err := handle.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(handle)
	if err != nil {
		return nil, err
	}
	return decodeProjectBoardRows(data), nil
}

// compactProjectBoardFile 把追加流**压实**成"每个 (会话, Key) 的最新一行"并原子重写。
//
// 为什么必须有它（契约 §9.3 R-1）：同步器每轮都是"当前全量行 + Seq 从水位 +1 起"的
// **整份重盖**，行数按轮累加；触顶之后若只是报错，每次 flush 都被拒绝，投影就**永久停在
// 旧版本**——文件与人读件都还在，读的人却拿到过期内容，比"什么都没有"更坏。
//
// 复用的都是本文件既有件，不新增第二套写盘实现：FoldProjectBoardRows（**唯一**折叠读法，
// 顺带丢掉 tombstone——被删掉的 Key 本就不该留在文件里）、encodeProjectBoardRows（唯一
// 的行编码口）、writeAtomic（既有的原子重写口，回收会话时用的是同一份）。
//
// 调用方必须持有该文件路径的锁，并**先关掉自己的文件句柄**：Windows 上 rename 不肯覆盖
// 打开中的文件，留着句柄会让重写必然失败。
func compactProjectBoardFile(path string, existing []ProjectBoardRow) ([]ProjectBoardRow, error) {
	compacted := FoldProjectBoardRows(existing, func(format string, args ...any) {
		log.Printf(format, args...)
	})
	data, err := encodeProjectBoardRows(compacted)
	if err != nil {
		return nil, err
	}
	if err := writeAtomic(path, data, 0o600); err != nil {
		return nil, err
	}
	return compacted, nil
}

// appendProjectBoardRows 是投影追加的**唯一实现**：
//
//   - 空批**不建文件**（I6：空则退场），只回报当前水位；
//   - 幂等：水位 >= 本批最小 Seq ⇒ 空操作并返回当前水位；
//   - 有界：本批超过 ProjectBoardMaxRows ⇒ 显式报错；累计越界 ⇒ 先压实再判，
//     压实后仍越界才显式报错（不静默截断）；
//   - 先截崩溃残尾（truncateCrashTail）再逐行追加；
//   - 全程持**该文件路径的独立锁**；
//   - 落盘成功后刷新人读件 `BOARD.md`（派生件的渲染是同一个"落"的后半段，
//     失败只影响人读件，不影响投影行）。
func appendProjectBoardRows(projectRoot, file string, rows []ProjectBoardRow) (uint64, error) {
	if err := validateProjectRoot(projectRoot); err != nil {
		return 0, err
	}
	name, err := projectBoardFile(file)
	if err != nil {
		return 0, err
	}
	path := projectBoardPath(projectRoot, name)
	lock := projectBoardLock(path)
	lock.Lock()
	defer lock.Unlock()

	// 空批：不建文件（含首次）。水位照常回报。
	if len(rows) == 0 {
		existing, readErr := readProjectBoardFile(path)
		if readErr != nil {
			return 0, readErr
		}
		return projectBoardWatermark(existing), nil
	}
	// 本批越界：**在落盘之前**显式失败（不建文件、不静默截断）。
	if len(rows) > ProjectBoardMaxRows {
		return 0, fmt.Errorf("session storage: project board 本批行数 %d 越界（上限 %d）", len(rows), ProjectBoardMaxRows)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return 0, err
	}
	existing, err := readProjectBoardRowsForAppend(path)
	if err != nil {
		return 0, err
	}
	watermark := projectBoardWatermark(existing)

	// 幂等：本批已落过（文件里已有同一**会话·同一 Seq** 的批）⇒ 空操作。
	//
	// 判据为什么带会话：落点是**项目级平铺文件**，两个会话各自从"文件水位 + 1"起批，
	// 它们的 Seq 会落在同一段数轴上（折叠按 (会话, Key) 分桶，跨会话的 Seq 本就不必
	// 可比）。只比水位就会把"另一个会话刚好用了这个 Seq"误判成"这批落过了"——那是
	// **静默丢批**，比多写一行严重得多。
	if projectBoardBatchAlreadyAppended(existing, rows) {
		return watermark, nil
	}
	// 累计越界：先压实（折成每个 (会话, Key) 的最新一行），压实后仍越界才显式报错。
	if len(existing)+len(rows) > ProjectBoardMaxRows {
		// 重写前必须先关掉句柄（Windows 上 rename 不肯覆盖打开中的文件）。
		compacted, compactErr := compactProjectBoardFile(path, existing)
		if compactErr != nil {
			return watermark, compactErr
		}
		if len(compacted)+len(rows) > ProjectBoardMaxRows {
			return watermark, fmt.Errorf("session storage: project board 行数越界（压实后活体 %d + 本批 %d > 上限 %d）",
				len(compacted), len(rows), ProjectBoardMaxRows)
		}
		existing = compacted
	}
	// 水位**只增不减**：压实可能丢掉"文件内最大 Seq"那一行（它恰好是条 tombstone），
	// 但水位是 (会话, Seq) 这枚幂等键的分配依据，回退会让旧批被当成新批重放。
	if floor := projectBoardWatermark(existing); floor > watermark {
		watermark = floor
	}
	buffer, err := encodeProjectBoardRows(rows)
	if err != nil {
		return watermark, err
	}
	handle, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return watermark, err
	}
	defer handle.Close()
	if _, err := handle.Write(buffer); err != nil {
		return watermark, err
	}
	if err := handle.Sync(); err != nil {
		return watermark, err
	}
	if next := projectBoardMaxSeq(rows); next > watermark {
		watermark = next
	}
	refreshProjectBoardMarkdown(projectRoot)
	return watermark, nil
}

// reclaimProjectBoard 回收某会话在项目看板里的行（删除会话时同批调用）。
//
// 落点是**项目级平铺文件**：一个会话退场 = 把它的行从这个项目的两份投影里摘掉
// （其余会话的行原样保留），然后重渲染人读件。幂等：没有它的行 ⇒ 空操作。
func reclaimProjectBoard(projectRoot, sessionID string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" || strings.TrimSpace(projectRoot) == "" {
		return nil
	}
	for _, name := range []string{ProjectBoardWorkFile, ProjectBoardTeamworkFile} {
		if err := pruneProjectBoardSession(projectRoot, name, sessionID); err != nil {
			return err
		}
	}
	refreshProjectBoardMarkdown(projectRoot)
	return nil
}

// pruneProjectBoardSession 把某会话的行从一份投影文件里摘掉（原子重写；空则删文件）。
func pruneProjectBoardSession(projectRoot, file, sessionID string) error {
	path := projectBoardPath(projectRoot, file)
	lock := projectBoardLock(path)
	lock.Lock()
	defer lock.Unlock()
	raw, err := readProjectBoardFile(path)
	if err != nil {
		return err
	}
	if len(raw) == 0 {
		return nil
	}
	kept := make([]ProjectBoardRow, 0, len(raw))
	for _, row := range raw {
		if strings.TrimSpace(row.Scope.Session) == sessionID {
			continue
		}
		kept = append(kept, row)
	}
	if len(kept) == len(raw) {
		return nil // 这个会话本来就没有行
	}
	if len(kept) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	}
	data, err := encodeProjectBoardRows(kept)
	if err != nil {
		return err
	}
	return writeAtomic(path, data, 0o600)
}

// ---------------------------------------------------------------------------
// 人读件（BOARD.md）：折叠后确定性渲染
// ---------------------------------------------------------------------------

// readProjectBoardFoldedRows 读回**本项目的全部折叠行**（两份白名单文件合并）。
func readProjectBoardFoldedRows(projectRoot string) ([]ProjectBoardRow, error) {
	var folded []ProjectBoardRow
	for _, name := range []string{ProjectBoardWorkFile, ProjectBoardTeamworkFile} {
		raw, err := readProjectBoardFile(projectBoardPath(projectRoot, name))
		if err != nil {
			return nil, err
		}
		folded = append(folded, FoldProjectBoardRows(raw, nil)...)
	}
	sort.SliceStable(folded, func(i, j int) bool { return projectBoardRowLess(folded[i], folded[j]) })
	return folded, nil
}

// refreshProjectBoardMarkdown 按当前投影重建人读件；没有行 ⇒ 删掉它（I6：空则退场）。
//
// 失败**不上抛**：人读件是投影的渲染，渲染失败只意味着"这一版看板旧了一点"，
// 不该把一次成功的行级追加判成失败（调用方会把它当落盘失败重试，反而写出重复行）。
func refreshProjectBoardMarkdown(projectRoot string) {
	rows, err := readProjectBoardFoldedRows(projectRoot)
	if err != nil {
		log.Printf("session storage: project board 人读件折叠读失败（%s）：%v", projectRoot, err)
		return
	}
	path := projectBoardPath(projectRoot, ProjectBoardMarkdownFile)
	lock := projectBoardLock(path)
	lock.Lock()
	defer lock.Unlock()
	if len(rows) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return
		}
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		log.Printf("session storage: project board 人读件目录建不出来（%s）：%v", path, err)
		return
	}
	// 失败只记一行日志：人读件是投影的渲染，渲染失败不该把一次成功的行级追加判成失败
	// （调用方会把它当落盘失败重试，反而写出重复行）——但静默失败会让"盘上没有 BOARD.md"
	// 变成没人能查的怪事，所以留痕。
	if err := writeAtomic(path, ProjectBoardMarkdown(rows), 0o600); err != nil {
		log.Printf("session storage: project board 人读件重写失败（%s）：%v", path, err)
	}
}

// ---------------------------------------------------------------------------
// Router 五方法（写面直连项目目录：落点已与数据根解耦）
// ---------------------------------------------------------------------------

// SetProjectBoardRootFor 注入「会话 → 项目根」的解析器（装配期一次；nil = 未装配）。
//
// 只有一个消费者：会话删除时的投影回收（`ReclaimProjectBoard`）。投影的**写/读**面
// 由调用方直接带着项目根进来（application 侧的 ProjectRootFor），不经过这里——
// 会话存储层不该长出"工作区"这条外部依赖；这里收一个函数值，是为了让"删会话"这条
// 既有链路（`Router.DeleteWorkspace` → 存储回收）能顺手把项目目录里的投影摘掉。
func (router *Router) SetProjectBoardRootFor(resolve func(sessionID string) string) {
	router.mu.Lock()
	defer router.mu.Unlock()
	router.projectBoardRootFor = resolve
}

// projectBoardRootOf 读当前解析器并解析会话的项目根（未装配 / 未绑定 ⇒ ""）。
func (router *Router) projectBoardRootOf(sessionID string) string {
	router.mu.RLock()
	resolve := router.projectBoardRootFor
	router.mu.RUnlock()
	if resolve == nil {
		return ""
	}
	return strings.TrimSpace(resolve(sessionID))
}

// AppendProjectBoardRows 追加一批投影行，返回追加后的水位（= 文件内最大 Seq）。
//
// 空批不建文件；幂等（水位 >= 本批最小 Seq ⇒ 空操作）；越界显式报错。落点是
// `<项目根>/.seelex/board/<file>`（项目目录，与数据根无关）。
func (router *Router) AppendProjectBoardRows(projectRoot, file string, rows []ProjectBoardRow) (uint64, error) {
	return appendProjectBoardRows(projectRoot, file, rows)
}

// ReadProjectBoardRows 读回某项目某投影文件的**折叠读**（唯一读法）。
//
// sessionID 非空 ⇒ 只留该会话的行；空 ⇒ 本项目全部行。文件不存在 ⇒ 空投影。
func (router *Router) ReadProjectBoardRows(projectRoot, sessionID, file string) ([]ProjectBoardRow, error) {
	name, err := projectBoardFile(file)
	if err != nil {
		return nil, err
	}
	if err := validateProjectRoot(projectRoot); err != nil {
		return nil, err
	}
	raw, err := readProjectBoardFile(projectBoardPath(projectRoot, name))
	if err != nil {
		return nil, err
	}
	folded := FoldProjectBoardRows(raw, nil)
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return folded, nil
	}
	filtered := make([]ProjectBoardRow, 0, len(folded))
	for _, row := range folded {
		if strings.TrimSpace(row.Scope.Session) == sessionID {
			filtered = append(filtered, row)
		}
	}
	return filtered, nil
}

// ProjectBoardWatermark 返回某项目某投影文件的水位（= 文件内最大 Seq）。
// 文件不存在 ⇒ 0。
func (router *Router) ProjectBoardWatermark(projectRoot, file string) (uint64, error) {
	name, err := projectBoardFile(file)
	if err != nil {
		return 0, err
	}
	if err := validateProjectRoot(projectRoot); err != nil {
		return 0, err
	}
	raw, err := readProjectBoardFile(projectBoardPath(projectRoot, name))
	if err != nil {
		return 0, err
	}
	return projectBoardWatermark(raw), nil
}

// ProjectBoardReadOptions 是聚合读的约束：File 指定读哪个白名单文件（空 ⇒
// worktable.jsonl）；ExcludeSession 跳过某会话；MaxRows 有界（>0 才生效）。
//
// 落点是项目级平铺文件，所以**没有"跨会话枚举"这一步**：文件本身就是本项目的
// 全部会话（这比旧口径的"列目录再逐个折叠"少一处读法）。
type ProjectBoardReadOptions struct {
	File           string
	ExcludeSession string
	MaxRows        int
}

// ReadProjectBoard 读回一个项目的**全部折叠行**（白名单文件之一），有界、只读。
//
// 返回的 bool 表示**是否发生截断**（行数触顶），由调用方显式对外标注。
func (router *Router) ReadProjectBoard(projectRoot string, opts ProjectBoardReadOptions) ([]ProjectBoardRow, bool, error) {
	file := strings.TrimSpace(opts.File)
	if file == "" {
		file = ProjectBoardWorkFile
	}
	name, err := projectBoardFile(file)
	if err != nil {
		return nil, false, err
	}
	if err := validateProjectRoot(projectRoot); err != nil {
		return nil, false, err
	}
	raw, err := readProjectBoardFile(projectBoardPath(projectRoot, name))
	if err != nil {
		return nil, false, err
	}
	folded := FoldProjectBoardRows(raw, nil)
	exclude := strings.TrimSpace(opts.ExcludeSession)
	if exclude != "" {
		kept := make([]ProjectBoardRow, 0, len(folded))
		for _, row := range folded {
			if strings.TrimSpace(row.Scope.Session) == exclude {
				continue
			}
			kept = append(kept, row)
		}
		folded = kept
	}
	truncated := false
	if opts.MaxRows > 0 && len(folded) > opts.MaxRows {
		folded = folded[:opts.MaxRows]
		truncated = true
	}
	return folded, truncated, nil
}

// ReclaimProjectBoard 回收某会话在项目看板里的行（幂等：没有它的行不算错）。
//
// 项目根由装配期注入的解析器给出（`SetProjectBoardRootFor`）；未装配 / 该会话没有
// 工作区绑定 ⇒ 没有落点，直接返回（契约 §3：未关联会话不落盘）。
func (router *Router) ReclaimProjectBoard(_ /*projectID*/, sessionID string) error {
	projectRoot := router.projectBoardRootOf(sessionID)
	if projectRoot == "" {
		return nil
	}
	return reclaimProjectBoard(projectRoot, sessionID)
}

// ReadProjectBoardMarkdown 读回人读件（不存在 ⇒ `fs.ErrNotExist`）。
func (router *Router) ReadProjectBoardMarkdown(projectRoot string) ([]byte, error) {
	if err := validateProjectRoot(projectRoot); err != nil {
		return nil, err
	}
	return os.ReadFile(projectBoardPath(projectRoot, ProjectBoardMarkdownFile))
}
