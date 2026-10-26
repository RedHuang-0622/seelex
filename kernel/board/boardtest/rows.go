package boardtest

import (
	"encoding/json"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
)

func WaitForWrites(t *testing.T, st *Store, file string, want int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if st.WriteCount(file) >= want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("project board %s 落盘次数 = %d，希望至少 %d", file, st.WriteCount(file), want)
}

// settleBoard 等过若干倍静默窗：用来证明"没有第 N 次落盘"（消极断言需要时间）。
func Settle(idle time.Duration) {
	time.Sleep(6 * idle)
}

func MarshalRows(t *testing.T, rows []dto.ProjectBoardRow) string {
	t.Helper()
	encoded, err := json.Marshal(rows)
	if err != nil {
		t.Fatalf("编码投影行: %v", err)
	}
	return string(encoded)
}

func RowByKey(rows []dto.ProjectBoardRow, key string) (dto.ProjectBoardRow, bool) {
	for _, row := range rows {
		if row.Key == key {
			return row, true
		}
	}
	return dto.ProjectBoardRow{}, false
}

func PayloadText(row dto.ProjectBoardRow, key string) string {
	value, ok := row.Payload[key].(string)
	if !ok {
		return ""
	}
	return value
}

// boardKeys 取行集的 key 集（排序后），用于"投影/不投影"的集合断言。
func Keys(rows []dto.ProjectBoardRow) []string {
	keys := make([]string, 0, len(rows))
	for _, row := range rows {
		keys = append(keys, row.Key)
	}
	sort.Strings(keys)
	return keys
}

func TodoItem(id, text, status string) model.WorkItem {
	return model.WorkItem{ID: id, Kind: "todo", Phase: "tasklist", Task: text, Status: status}
}

func TaskItem(id, text, status string) model.WorkItem {
	return model.WorkItem{ID: id, Kind: "task", Phase: "task", Task: text, Status: status}
}

// LiveRows 是"活体取数"的测试替身：flush 在独立 goroutine 上读它，测试在另一个
// goroutine 上改它——必须自己加锁（真装配里那一侧是 Service 的视图锁）。
type LiveRows struct {
	mu    sync.Mutex
	items []model.WorkItem
}

func NewLiveRows(items ...model.WorkItem) *LiveRows {
	return &LiveRows{items: items}
}

func (live *LiveRows) Set(items []model.WorkItem) {
	live.mu.Lock()
	defer live.mu.Unlock()
	live.items = items
}

func (live *LiveRows) Add(item model.WorkItem) {
	live.mu.Lock()
	defer live.mu.Unlock()
	live.items = append(live.items, item)
}

func (live *LiveRows) Drop(index int) {
	live.mu.Lock()
	defer live.mu.Unlock()
	if index < 0 || index >= len(live.items) {
		return
	}
	live.items = append(live.items[:index:index], live.items[index+1:]...)
}

func (live *LiveRows) Rows() []model.WorkItem {
	live.mu.Lock()
	defer live.mu.Unlock()
	return append([]model.WorkItem(nil), live.items...)
}

// ---------------------------------------------------------------------------
// T2.1 核心一致性：同一输入跑两遍，折叠读结果与活体投影逐行相等
// ---------------------------------------------------------------------------

func SessionRows(t *testing.T, st *Store, session, file string) int {
	t.Helper()
	rows := st.Folded(ProjectRoot, session, file)
	for _, row := range rows {
		if row.Scope.Session != session {
			t.Fatalf("会话 %q 的文件里混进了 %q 的行（串写）：%#v", session, row.Scope.Session, row)
		}
	}
	return len(rows)
}
