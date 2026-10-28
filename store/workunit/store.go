package workunit

// store.go — node/subagent 会话记录的**落盘 owner**（M9 从根 `jsonRepository` 迁入）
// 与写侧责任链。
//
// 物理事实（与 M9 之前逐字相同，路径零迁移）：
//
//   - 记录文件 = `<mainSessionRoot>/subagents/<mainSessionID>-<nodeSessionID>.json`，
//     一条记录一份 JSON、整份原子替换；
//   - 目录权限 0700、文件权限 0600，与旧实现逐字一致；
//   - 缺失 = `ErrNotFound`（按显式 id 点名要的东西不存在），不是"空记录"。
//
// 锁身份：本包的 `Store.mu` 就是旧 `jsonRepository.mu` 在这一点上的串行化语义——同一条
// 记录文件的整份替换不与其他写者交错。跨实例不共享锁，因此读走 `storeroot.ReadSharedFile`
// （Windows 上 rename 发布窗口里读者不会自己失败，也不会挡住删除），写走 `WriteAtomic`。

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	storeroot "github.com/RedHuang-0622/seelex/store/root"
)

// DirName 是记录目录名：挂在主会话存储目录下，改一个字节就是数据迁移。
const DirName = "subagents"

// Store 拥有 `<sessionRoot>/subagents/` 下的 node/subagent 记录文件。
//
// 零值不可用：目录由 New 固定，`mu` 串行化同实例的整份替换与读取。
type Store struct {
	dir string
	mu  sync.RWMutex
}

// New 构造记录存储。sessionRoot 是**主会话**的存储目录（记录按主会话索引）。
func New(sessionRoot string) *Store {
	return &Store{dir: filepath.Join(sessionRoot, DirName)}
}

// Write 写入（覆盖）一条记录：补默认值 → 建目录 → 编码 → 原子替换。
//
// 主/子会话 ID 都必须非空：文件名由两者拼成，缺一个就写不出可被主会话索引找到的
// 记录（静默写进一个没人能枚举到的名字 = 重启失忆）。
func (s *Store) Write(record Record) error {
	if strings.TrimSpace(record.MainSessionID) == "" || strings.TrimSpace(record.SessionID) == "" {
		return errors.New("session storage: node session requires main and sub session IDs")
	}
	if record.SchemaVersion == 0 {
		record.SchemaVersion = SchemaVersion
	}
	if record.UpdatedAt.IsZero() {
		record.UpdatedAt = time.Now().UTC()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return fmt.Errorf("session storage: create node session dir: %w", err)
	}
	payload, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("session storage: encode node session %q: %w", record.SessionID, err)
	}
	if err := storeroot.WriteAtomic(filepath.Join(s.dir, FileName(record.MainSessionID, record.SessionID)), payload, 0o600); err != nil {
		return fmt.Errorf("session storage: write node session %q: %w", record.SessionID, err)
	}
	return nil
}

// Read 读取一条记录。文件不存在返回 `ErrNotFound` 包装（带点名的那种子会话 ID）；
// 其它 IO 错误与解码错误显式报错，不返回半份记录。
func (s *Store) Read(mainSessionID, nodeSessionID string) (Record, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	path := filepath.Join(s.dir, FileName(mainSessionID, nodeSessionID))
	payload, err := storeroot.ReadSharedFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Record{}, fmt.Errorf("%w: %q", ErrNotFound, nodeSessionID)
		}
		return Record{}, fmt.Errorf("session storage: read node session %q: %w", nodeSessionID, err)
	}
	var record Record
	if err := json.Unmarshal(payload, &record); err != nil {
		return Record{}, fmt.Errorf("session storage: decode node session %q: %w", nodeSessionID, err)
	}
	return record, nil
}

// List 从主会话索引列举全部记录（按 NodeID 排序）。
//
// 目录不存在 = 这个主会话还没有任何子会话，返回**非 nil 空切片**（"还没有"是状态，
// 不是错误）。单条损坏/读不动只跳过该条：一条坏记录不许让整个主会话索引不可枚举。
func (s *Store) List() ([]Record, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entries, err := os.ReadDir(s.dir)
	if os.IsNotExist(err) {
		return []Record{}, nil
	}
	if err != nil {
		return nil, err
	}
	records := make([]Record, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		payload, readErr := storeroot.ReadSharedFile(filepath.Join(s.dir, entry.Name()))
		if readErr != nil {
			continue // 单条损坏不阻断主索引列举
		}
		var record Record
		if json.Unmarshal(payload, &record) != nil {
			continue
		}
		records = append(records, record)
	}
	sort.Slice(records, func(i, j int) bool { return records[i].NodeID < records[j].NodeID })
	return records, nil
}

// Delete 删除一条记录。文件或目录不存在 = 已经离场（幂等，不是错误）。
func (s *Store) Delete(mainSessionID, nodeSessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := filepath.Join(s.dir, FileName(mainSessionID, nodeSessionID))
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("session storage: delete node session %q: %w", nodeSessionID, err)
	}
	return nil
}

// MarkTerminal 就地记录一条 node 记录的终端状态。
//
// 状态词表：queued|running|done|failed（与落盘 wire 一致）；错误只写 Error 栏，
// 不新增字段（记录形状零变化）。身份不可改名：NodeID / SessionID / MainSessionID / Goal
// 一律取**已落盘**的那份，调用方只能改终态那一组事实（收尾不许顺手改掉"这是谁"）。
//
// 记录不存在时返回与 Read 同一个 `ErrNotFound` 包装：终态是证据，写不到明确的人身上
// 就必须显式失败，不许凭空造一条没有 History/Worktree 的空记录。
func (s *Store) MarkTerminal(mainSessionID string, record Record) error {
	stored, err := s.Read(mainSessionID, record.SessionID)
	if err != nil {
		return err
	}
	stored.Status = record.Status
	stored.Summary = record.Summary
	stored.Error = record.Error
	stored.EndedAt = record.EndedAt
	if stored.EndedAt.IsZero() {
		stored.EndedAt = time.Now().UTC()
	}
	stored.UpdatedAt = record.UpdatedAt
	if stored.UpdatedAt.IsZero() {
		stored.UpdatedAt = stored.EndedAt
	}
	return s.Write(stored)
}

// FileName 生成 `<mainSessionID>-<nodeSessionID>.json` 文件名
// （用户约定：从主会话索引直接定位子会话）。ID 中的路径分隔符被替换，
// 防止构造逃逸路径。
func FileName(mainSessionID, nodeSessionID string) string {
	sanitize := func(id string) string {
		return strings.NewReplacer("/", "_", "\\", "_", ":", "_").Replace(id)
	}
	return sanitize(mainSessionID) + "-" + sanitize(nodeSessionID) + ".json"
}

// ── 写侧责任链 ────────────────────────────────────────────────────────────

// 写侧为什么是责任链：两条链**写的是同一张记录**——teammate 的记录形状是 subagent 记录的
// **纯增幅**（多一个身份块 `UnitRecord`、现场四栏写全），所以 teammate 那一层 = 子代理链
// **再加一环**，而不是第二条写路径（"add but not modify"）。链上每一环只负责"我这一层知道的
// 事实"，且**只填空、不覆盖**别人已经写下的事实：teammate 那一环写下 `Kind=teammate` 之后，
// 子代理那一环看到身份已经有了就不再动它。
//
// 恢复侧反过来是**策略模式**（按记录快照里的身份分派）：要读的东西、要恢复的内容确实不同
// ——子代理恢复的是详情数据面 + 子代理树节点 + 现场登记，teammate 恢复的是团队现场
// （见 `backend/engine/assemble/runtime_unit_recovery.go`）。

// Link 是写链上的一环：拿到记录、**增补**自己这一层知道的事实、交下一环。
type Link func(Record) Record

// Saver 是链末端的**写面**端口：链只管"写怎么走链"，落地由末端负责。
type Saver interface {
	Save(projectID, mainSessionID string, record Record) error
}

// Recorder 是链末端存储的**完整面**：写面 + 读面（列举/读取/删除/项目作用域）。
// `Store()` 返回它，因为读面仍走末端存储。
type Recorder interface {
	Saver
	ProjectID() string
	Load(projectID, mainSessionID, nodeSessionID string) (Record, error)
	List(projectID, mainSessionID string) ([]Record, error)
	Delete(projectID, mainSessionID, nodeSessionID string) error
}

// Writer 是会话记录写入的唯一出口：逐环增补 → 末端一次落盘。
//
// 零值不可用：`Save` 在链或末端缺失时**显式报错**（记录是恢复用的证据，静默丢它就是
// "重启失忆"被当成正常）。
type Writer struct {
	recorder Recorder
	links    []Link
}

// NewWriter 以既有存储为末端起一条链。
func NewWriter(recorder Recorder) *Writer {
	return &Writer{recorder: recorder}
}

// With 派生一条**在外面再包一环**的链（本链不动）：新的一环先跑，之后才走本链原有的环。
// 于是 teammate 那一层 = 子代理链外面再包一环（**add but not modify**：teammate 的记录就是
// subagent 的记录多一个身份块），不必另立第二条写路径。
//
// 顺序的意义：**越外层的环越先写自己知道的事实，内层的环只填空缺**（子代理那一环见
// `SubagentUnitRecordLink`：身份已经有了就不动它）。所以外层包进来的环可以放心先写。
func (w *Writer) With(links ...Link) *Writer {
	if w == nil {
		return nil
	}
	derived := &Writer{recorder: w.recorder}
	derived.links = append(derived.links, links...)
	derived.links = append(derived.links, w.links...)
	return derived
}

// Save 走完整条链再落盘（链上每一环只看得到自己这一层的事实）。
func (w *Writer) Save(projectID, mainSessionID string, record Record) error {
	if w == nil || w.recorder == nil {
		return errors.New("session storage: node session record writer is unavailable")
	}
	for _, link := range w.links {
		if link == nil {
			continue
		}
		record = link(record)
	}
	return w.recorder.Save(projectID, mainSessionID, record)
}

// Store 返回链的末端存储：**读面**（列举/读取/删除/项目作用域）仍走它，本类型只管
// "写怎么走链"。链没装末端时返回 nil（调用方按未装配处理）。
func (w *Writer) Store() Recorder {
	if w == nil {
		return nil
	}
	return w.recorder
}
