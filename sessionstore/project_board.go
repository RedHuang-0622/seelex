package sessionstore

// project_board.go — 项目看板（project board）的**按会话派生投影**存储面。
//
// 口径（这是本文件存在的唯一理由，写在这里防误读）：
//
//  1. 它**不是领域事实源**（I1/I2）：投影是派生数据，可删可重建；读侧活体优先，
//     文件被篡改不影响任何活体/领域判定（判定照旧读 task/teamwork 事实）。
//  2. 它**不新增第二份判据**（I3）：这里只做**字段映射 + 追加 + 折叠**，不复制任何
//     领域状态机的判定逻辑。
//  3. 它**不进模型上下文**（I7）：本模块只负责落盘与读回，装配侧不得把它塞进
//     system/project 前缀或请求尾部。
//  4. 落点在 <jsonRepository.root>/project-board/... ：钉在既有 JSON 数据根之下，
//     白拿**单写者 / 原子追加 / 残尾自愈 / 目录纪律**（不新造文件系统写实现）。
//  5. 追加复用既有 `appendEventLogLine` 语义（O_APPEND 追加一行）与
//     `truncateCrashTail`（jsonl_io.go）：先截残尾、再逐行 O_APPEND。
//  6. 独立锁：投影**不新增 storageModule 枚举**（不改 module_heads.go 的
//     枚举—锁对应关系），自带按**文件路径**分片的独立锁（与 roleDraftLock 同形）。
//     跨模块共用一把锁会把"落投影"与"落消息/落团队事实"串成一个串行点。
//  7. 有界：行数越界**显式报错**（ProjectBoardMaxRows），不静默截断。
//
// 已知偏离 plan §4.3：plan 写 <baseDir>/<boardRoot>，本实现钉在
// <jsonRepository.root> 之下（理由见第 4 条）。

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// 投影契约常量（schema / 上限 / 两个白名单文件名）。
const (
	// ProjectBoardSchema 是行信封的 schema 版本；未知 schema 的行在折叠时跳过。
	ProjectBoardSchema = 1
	// ProjectBoardMaxRows 是单个投影文件的行数上限（有界，I5）。
	ProjectBoardMaxRows = 512
	// ProjectBoardWorkFile 是工作台投影文件名（只允许这两个白名单名，防路径穿越）。
	ProjectBoardWorkFile = "worktable.jsonl"
	// ProjectBoardTeamworkFile 是团队编排投影文件名。
	ProjectBoardTeamworkFile = "teamwork.jsonl"
)

// 投影行 kind（与 plan 的三种投影对象一致）。
const (
	ProjectBoardKindWork          = "work"
	ProjectBoardKindMilestone     = "milestone"
	ProjectBoardKindMilestoneItem = "milestone_item"
)

// projectBoardDirName 是数据根下投影子树的目录名。
const projectBoardDirName = "project-board"

// projectBoardRemovedField 是 tombstone 字段名：某 Key 的最新行 payload 里
// `"removed": true` 表示该 Key 从投影里消失（upsert 语义下"删除的条目永远消失不掉"
// 的解法，本契约有意补强）。
const projectBoardRemovedField = "removed"

// ProjectBoardScope 是行信封的作用域：Project 是**工作区 ID**（如 ws-…），
// Session 是**字面会话 ID**——两者都不是绝对路径（目录名一律走 hash 派生）。
type ProjectBoardScope struct {
	Project string `json:"project"`
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

// projectBoardFile 校验并归一化投影文件名（只允许两个白名单名，防路径穿越）。
func projectBoardFile(file string) (string, error) {
	name := strings.TrimSpace(file)
	switch name {
	case ProjectBoardWorkFile, ProjectBoardTeamworkFile:
		return name, nil
	default:
		return "", fmt.Errorf("session storage: project board 文件名 %q 不在白名单（%s / %s）",
			file, ProjectBoardWorkFile, ProjectBoardTeamworkFile)
	}
}

// FoldProjectBoardRows 是投影行**唯一的读法**（折叠读）：把追加日志折成当前视图。
//
// 规则：
//   - 未知 schema / 未知 kind 的行**跳过**并 logf（logf 可为 nil）；
//   - 同一 Key 取 **Seq 最大**的行（并列时后一条胜）；
//   - 取胜行的 payload[removed]==true ⇒ 该 Key 从结果里**消失**（tombstone）；
//   - 输出按 (Kind, Key) **稳定排序**（同一份输入必得同一份输出）。
func FoldProjectBoardRows(rows []ProjectBoardRow, logf func(string, ...any)) []ProjectBoardRow {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	latest := make(map[string]ProjectBoardRow, len(rows))
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
		if current, exists := latest[key]; !exists || row.Seq >= current.Seq {
			latest[key] = row
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
		if folded[i].Kind != folded[j].Kind {
			return folded[i].Kind < folded[j].Kind
		}
		return folded[i].Key < folded[j].Key
	})
	return folded
}

// ---------------------------------------------------------------------------
// 路径派生（一律走既有 hash()：路径与行内不得出现绝对路径/用户名）
// ---------------------------------------------------------------------------

// projectBoardRoot 返回项目级投影根：<root>/project-board/project-<hash(projectID)>。
func (repository *jsonRepository) projectBoardRoot(projectID string) string {
	return filepath.Join(repository.root, projectBoardDirName, "project-"+hash(projectID))
}

// projectBoardSessionDir 返回会话级投影目录（目录名走 hash，不含原字面量）。
func (repository *jsonRepository) projectBoardSessionDir(projectID, sessionID string) string {
	return filepath.Join(repository.projectBoardRoot(projectID), "session-"+hash(sessionID))
}

// projectBoardPath 返回某投影文件的物理路径。
func (repository *jsonRepository) projectBoardPath(projectID, sessionID, file string) string {
	return filepath.Join(repository.projectBoardSessionDir(projectID, sessionID), file)
}

// ---------------------------------------------------------------------------
// 独立锁（不新增 storageModule 枚举；与 roleDraftLock 同形，按文件路径分片）
// ---------------------------------------------------------------------------

// projectBoardLocks 是投影文件的独立锁表（path → *sync.Mutex）。
//
// 为什么不并入 sessionModuleLocks：并入就得给 storageModule 补一个枚举 + mutexFor
// 的 case（本契约明确不新增枚举），且会把"落投影"与"落消息/落团队事实"串成一个
// 串行点——那正是 media 别名成 messageMu 的教训。
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

// projectBoardMinSeq / projectBoardMaxSeq 返回一批行的 Seq 最小 / 最大值。
func projectBoardMinSeq(rows []ProjectBoardRow) uint64 {
	var min uint64
	for index, row := range rows {
		if index == 0 || row.Seq < min {
			min = row.Seq
		}
	}
	return min
}

func projectBoardMaxSeq(rows []ProjectBoardRow) uint64 {
	var max uint64
	for _, row := range rows {
		if row.Seq > max {
			max = row.Seq
		}
	}
	return max
}

// appendProjectBoardRows 是投影追加的**唯一实现**：
//
//   - 空批**不建文件**（I6：空则退场），只回报当前水位；
//   - 幂等：水位 >= 本批最小 Seq ⇒ 空操作并返回当前水位；
//   - 有界：本批 / 累计行数超过 ProjectBoardMaxRows ⇒ 显式报错（不静默截断）；
//   - 先截崩溃残尾（truncateCrashTail）再逐行 O_APPEND（复用 appendEventLogLine 语义）；
//   - 全程持**该文件路径的独立锁**。
func (repository *jsonRepository) appendProjectBoardRows(projectID, sessionID, file string, rows []ProjectBoardRow) (uint64, error) {
	if err := validateProjectID(projectID); err != nil {
		return 0, err
	}
	if strings.TrimSpace(sessionID) == "" {
		return 0, errors.New("session storage: session ID is required")
	}
	name, err := projectBoardFile(file)
	if err != nil {
		return 0, err
	}
	path := repository.projectBoardPath(projectID, sessionID, name)
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
	handle, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return 0, err
	}
	defer handle.Close()
	// 先截崩溃残尾，再从完整行边界续写（复用 jsonl_io.go 的唯一实现）。
	if err := truncateCrashTail(handle); err != nil {
		return 0, err
	}
	if _, err := handle.Seek(0, io.SeekStart); err != nil {
		return 0, err
	}
	data, err := io.ReadAll(handle)
	if err != nil {
		return 0, err
	}
	existing := decodeProjectBoardRows(data)
	watermark := projectBoardWatermark(existing)

	// 幂等：本批已落过（水位 >= 本批最小 Seq）⇒ 空操作。
	if watermark > 0 && watermark >= projectBoardMinSeq(rows) {
		return watermark, nil
	}
	// 累计越界：显式报错（不静默截断）。
	if len(existing)+len(rows) > ProjectBoardMaxRows {
		return watermark, fmt.Errorf("session storage: project board 行数越界（现有 %d + 本批 %d > 上限 %d）",
			len(existing), len(rows), ProjectBoardMaxRows)
	}
	if _, err := handle.Seek(0, io.SeekEnd); err != nil {
		return watermark, err
	}
	buffer := make([]byte, 0, len(rows)*256)
	for _, row := range rows {
		line, marshalErr := json.Marshal(row)
		if marshalErr != nil {
			return watermark, fmt.Errorf("session storage: 编码 project board 行: %w", marshalErr)
		}
		buffer = append(buffer, line...)
		buffer = append(buffer, '\n')
	}
	if _, err := handle.Write(buffer); err != nil {
		return watermark, err
	}
	if err := handle.Sync(); err != nil {
		return watermark, err
	}
	if next := projectBoardMaxSeq(rows); next > watermark {
		watermark = next
	}
	return watermark, nil
}

// reclaimProjectBoard 回收某会话的投影目录（删除会话时同批调用）。
//
// 幂等：目录不存在不算错（I1：投影是可删可重建的派生数据）。
//
// 注意**不校验 projectID 非空**：`Router.Delete` 允许在未设定工作区（projectID == ""）
// 时删除会话，其会话目录同样落在 `project-<hash("")>` 下——回收必须与落盘走同一套
// 派生口径，否则"删掉了会话、留下了孤儿投影"。
func (repository *jsonRepository) reclaimProjectBoard(projectID, sessionID string) error {
	if strings.TrimSpace(sessionID) == "" {
		return nil
	}
	return removeAllWithBackoff(repository.projectBoardSessionDir(projectID, sessionID))
}

// ---------------------------------------------------------------------------
// Router 五方法（写面走 withJSONRepositoryAt 的既有口径）
// ---------------------------------------------------------------------------

// AppendProjectBoardRows 追加一批投影行，返回追加后的水位（= 文件内最大 Seq）。
//
// 空批不建文件；幂等（水位 >= 本批最小 Seq ⇒ 空操作）；越界显式报错。
// 非 JSON 会话存储布局不可用（返回错误，不静默丢写）。
func (router *Router) AppendProjectBoardRows(projectID, sessionID, file string, rows []ProjectBoardRow) (uint64, error) {
	var watermark uint64
	handled, err := router.withJSONRepositoryAt(projectID, func(repository *jsonRepository, resolvedProjectID string) error {
		next, appendErr := repository.appendProjectBoardRows(resolvedProjectID, sessionID, file, rows)
		watermark = next
		return appendErr
	})
	if !handled {
		return 0, errors.New("session storage: project board 需要 JSON 会话存储布局")
	}
	return watermark, err
}

// ReadProjectBoardRows 读回某会话某投影文件的**折叠读**（唯一读法，见
// FoldProjectBoardRows）。文件不存在 ⇒ 空投影。
func (router *Router) ReadProjectBoardRows(projectID, sessionID, file string) ([]ProjectBoardRow, error) {
	name, err := projectBoardFile(file)
	if err != nil {
		return nil, err
	}
	var rows []ProjectBoardRow
	handled, err := router.withJSONRepositoryAt(projectID, func(repository *jsonRepository, resolvedProjectID string) error {
		raw, readErr := readProjectBoardFile(repository.projectBoardPath(resolvedProjectID, sessionID, name))
		if readErr != nil {
			return readErr
		}
		rows = FoldProjectBoardRows(raw, nil)
		return nil
	})
	if !handled {
		return []ProjectBoardRow{}, nil
	}
	return rows, err
}

// ProjectBoardWatermark 返回某会话某投影文件的水位（= 文件内最大 Seq）。
// 文件不存在 ⇒ 0。
func (router *Router) ProjectBoardWatermark(projectID, sessionID, file string) (uint64, error) {
	name, err := projectBoardFile(file)
	if err != nil {
		return 0, err
	}
	var watermark uint64
	handled, err := router.withJSONRepositoryAt(projectID, func(repository *jsonRepository, resolvedProjectID string) error {
		raw, readErr := readProjectBoardFile(repository.projectBoardPath(resolvedProjectID, sessionID, name))
		if readErr != nil {
			return readErr
		}
		watermark = projectBoardWatermark(raw)
		return nil
	})
	if !handled {
		return 0, nil
	}
	return watermark, err
}

// ProjectBoardReadOptions 是聚合读的约束：File 指定读哪个白名单文件（空 ⇒
// worktable.jsonl）；ExcludeSession 跳过某会话；MaxSessions / MaxRows 有界
// （>0 才生效）。
type ProjectBoardReadOptions struct {
	File           string
	ExcludeSession string
	MaxSessions    int
	MaxRows        int
}

// ReadProjectBoard 聚合读一个项目下的投影：复用 Router.ListWorkspace 枚举会话
// （**只读**，不自造目录枚举），逐会话「折叠读」后按 (Kind, Key) 稳定排序。
//
// 返回的 bool 表示**是否发生截断**（会话数或行数触顶），由调用方显式对外标注。
func (router *Router) ReadProjectBoard(projectID string, opts ProjectBoardReadOptions) ([]ProjectBoardRow, bool, error) {
	file := strings.TrimSpace(opts.File)
	if file == "" {
		file = ProjectBoardWorkFile
	}
	name, err := projectBoardFile(file)
	if err != nil {
		return nil, false, err
	}
	metas := router.ListWorkspace(projectID)
	truncated := false
	sessions := make([]string, 0, len(metas))
	for _, meta := range metas {
		if meta.SessionID == "" || meta.SessionID == opts.ExcludeSession {
			continue
		}
		if opts.MaxSessions > 0 && len(sessions) >= opts.MaxSessions {
			truncated = true
			break
		}
		sessions = append(sessions, meta.SessionID)
	}
	var folded []ProjectBoardRow
	handled, err := router.withJSONRepositoryAt(projectID, func(repository *jsonRepository, resolvedProjectID string) error {
		for _, sessionID := range sessions {
			raw, readErr := readProjectBoardFile(repository.projectBoardPath(resolvedProjectID, sessionID, name))
			if readErr != nil {
				return readErr
			}
			folded = append(folded, FoldProjectBoardRows(raw, nil)...)
		}
		return nil
	})
	if !handled {
		return []ProjectBoardRow{}, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	sort.SliceStable(folded, func(i, j int) bool {
		if folded[i].Kind != folded[j].Kind {
			return folded[i].Kind < folded[j].Kind
		}
		if folded[i].Key != folded[j].Key {
			return folded[i].Key < folded[j].Key
		}
		return folded[i].Scope.Session < folded[j].Scope.Session
	})
	if opts.MaxRows > 0 && len(folded) > opts.MaxRows {
		folded = folded[:opts.MaxRows]
		truncated = true
	}
	return folded, truncated, nil
}

// ReclaimProjectBoard 回收某会话的投影目录（幂等：不存在不算错）。
func (router *Router) ReclaimProjectBoard(projectID, sessionID string) error {
	handled, err := router.withJSONRepositoryAt(projectID, func(repository *jsonRepository, resolvedProjectID string) error {
		return repository.reclaimProjectBoard(resolvedProjectID, sessionID)
	})
	if !handled {
		return nil
	}
	return err
}
