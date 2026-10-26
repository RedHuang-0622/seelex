package boardtest

import (
	"errors"
	"sync"

	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/store"
)

const (
	// Project 是 scope.project 的取值（机器本地的工作区 ID）：**只在映射层用例里**
	// 用来构造 scope；生产路径不写这一格（契约 §3 三条硬纪律）。
	Project = "ws-board"
	// ProjectRoot 是**落点**（`<项目根>/.seelex/board/`）的替身：同步器与存储面
	// 只认这一格——落点跟着项目走，与工作区 ID 无关（本轮的整条改动就是这件事）。
	ProjectRoot = `C:\board-test-project`
	At          = int64(1700000000)
)

// ---------------------------------------------------------------------------
// 夹具
// ---------------------------------------------------------------------------

// Store 是 ProjectBoardStore 的内存实现：模拟存储面的两个语义
// （水位 = 文件内最大 Seq；追加即可读回），只够同步器用。
type Store struct {
	mu      sync.Mutex
	rows    map[string][]dto.ProjectBoardRow
	appends []fakeBoardAppend
}

type fakeBoardAppend struct {
	projectRoot string
	sessionID   string
	file        string
	rows        []dto.ProjectBoardRow
}

func NewStore() *Store {
	return &Store{rows: make(map[string][]dto.ProjectBoardRow)}
}

// boardStoreKey 是内存替身的文件键：落点是**按会话分目录**的一份文件
// （`<项目根>/.seelex/board/<会话 ID>/<file>`），所以键里既有项目根也有会话——
// 这正是"两会话不共用一个文件"在替身这一层的形状（串写无处发生）。
func boardStoreKey(projectRoot, sessionID, file string) string {
	return projectRoot + "/" + sessionID + "/" + file
}

func (st *Store) AppendProjectBoardRows(projectRoot, sessionID, file string, rows []dto.ProjectBoardRow) (uint64, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.appends = append(st.appends, fakeBoardAppend{
		projectRoot: projectRoot, sessionID: sessionID, file: file,
		rows: append([]dto.ProjectBoardRow(nil), rows...),
	})
	if len(rows) > 0 {
		key := boardStoreKey(projectRoot, sessionID, file)
		st.rows[key] = append(st.rows[key], rows...)
	}
	return st.Watermark(projectRoot, sessionID, file), nil
}

func (st *Store) ProjectBoardWatermark(projectRoot, sessionID, file string) (uint64, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.Watermark(projectRoot, sessionID, file), nil
}

func (st *Store) Watermark(projectRoot, sessionID, file string) uint64 {
	var watermark uint64
	for _, row := range st.rows[boardStoreKey(projectRoot, sessionID, file)] {
		if row.Seq > watermark {
			watermark = row.Seq
		}
	}
	return watermark
}

func (st *Store) WriteCount(file string) int {
	st.mu.Lock()
	defer st.mu.Unlock()
	count := 0
	for _, call := range st.appends {
		if call.file == file && len(call.rows) > 0 {
			count++
		}
	}
	return count
}

func (st *Store) LastWrite(file string) []dto.ProjectBoardRow {
	st.mu.Lock()
	defer st.mu.Unlock()
	for index := len(st.appends) - 1; index >= 0; index-- {
		if st.appends[index].file == file && len(st.appends[index].rows) > 0 {
			return st.appends[index].rows
		}
	}
	return nil
}

// raw 是**某会话某文件**的原始行（未折叠；I6 断言"空集合不建文件"用它）。
func (st *Store) Raw(projectRoot, sessionID, file string) []dto.ProjectBoardRow {
	st.mu.Lock()
	defer st.mu.Unlock()
	return append([]dto.ProjectBoardRow(nil), st.rows[boardStoreKey(projectRoot, sessionID, file)]...)
}

// folded 复用存储面**唯一读法**（折叠读），不自己实现第二份。
func (st *Store) Folded(projectRoot, sessionID, file string) []dto.ProjectBoardRow {
	return store.FoldProjectBoardRows(st.Raw(projectRoot, sessionID, file), nil)
}

func (st *Store) DropFile(projectRoot, sessionID, file string) {
	st.mu.Lock()
	defer st.mu.Unlock()
	delete(st.rows, boardStoreKey(projectRoot, sessionID, file))
}

// newBoardTestService 造一个带 fakeRuntime 的 Service，并返回其**当前视图会话**
// （视图会话 = 实时 task 注册表，与打点表/工作表格的取数口径一致）。

type FlakyStore struct {
	*Store
	mu        sync.Mutex
	FailTimes int
}

func (st *FlakyStore) AppendProjectBoardRows(projectRoot, sessionID, file string, rows []dto.ProjectBoardRow) (uint64, error) {
	st.mu.Lock()
	fail := st.FailTimes > 0
	if fail {
		st.FailTimes--
	}
	st.mu.Unlock()
	if fail {
		return 0, errors.New("board store unavailable")
	}
	return st.Store.AppendProjectBoardRows(projectRoot, sessionID, file, rows)
}
