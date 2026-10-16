package teamwork

import (
	"testing"

	"github.com/RedHuang-0622/seelex/contract/dto"
)

// boardFake 实现两个窄读端口（只实现它们，**不**实现整套 contract.RuntimePort——
// 这正是 board.go 的 source 取 `any` 的理由：这两个端口是"窄可选"能力面）。
type boardFake struct {
	boardCalls int
	liveCalls  int
	pageCalls  int

	lastPageOffset int
	lastPageLimit  int
}

func (fake *boardFake) TeamworkBoardSnapshot(sessionID string) *dto.TeamworkBoardView {
	fake.boardCalls++
	return &dto.TeamworkBoardView{}
}

func (fake *boardFake) TeammateSessionLive(sessionID string) dto.TeammateSessionLiveView {
	fake.liveCalls++
	return dto.TeammateSessionLiveView{SessionID: sessionID, Running: true}
}

func (fake *boardFake) TeammateSessionLivePage(sessionID string, offset, limit int) dto.TeammateSessionLiveView {
	fake.pageCalls++
	fake.lastPageOffset = offset
	fake.lastPageLimit = limit
	return dto.TeammateSessionLiveView{SessionID: sessionID, Running: true, Offset: offset, Limit: limit}
}

// TestBoardReaderUnassembledReturnsEmptyViews 钉"未装配"口径：nil 宿主不是错误，
// 各读法返回自己的空视图（不是同一个零值）——前端据此整块退场。
func TestBoardReaderUnassembledReturnsEmptyViews(t *testing.T) {
	reader := NewBoardReader(nil)

	if got := reader.BoardViewFor("s1"); got != nil {
		t.Fatalf("未装配的看板读法必须返回 nil（整块退场），得到 %#v", got)
	}
	live := reader.SessionLiveFor("s1")
	if live.SessionID != "s1" || live.Running {
		t.Fatalf("未装配的 live 读法必须回显会话号且 Running=false，得到 %#v", live)
	}
	page := reader.SessionLivePageFor("s1", 7, 20)
	if page.SessionID != "s1" || page.Running || page.Offset != 7 || page.Limit != 20 {
		t.Fatalf("未装配的分页读法必须回显会话号与分页参数且 Running=false，得到 %#v", page)
	}
}

// TestBoardReaderUnsupportedHostReturnsEmptyViews 钉"已装配但不支持该投影"口径：
// 宿主存在但拿不到窄端口（测试桩 / 精简宿主）时，与未装配同解——这是最常见的分支。
func TestBoardReaderUnsupportedHostReturnsEmptyViews(t *testing.T) {
	reader := NewBoardReader(struct{ assembled bool }{assembled: true})

	if got := reader.BoardViewFor("s1"); got != nil {
		t.Fatalf("宿主不支持看板投影时必须返回 nil，得到 %#v", got)
	}
	if got := reader.SessionLiveFor("s1"); got.Running || got.SessionID != "s1" {
		t.Fatalf("宿主不支持 live 投影时必须回显会话号且 Running=false，得到 %#v", got)
	}
	if got := reader.SessionLivePageFor("s1", 3, 9); got.Running || got.Offset != 3 || got.Limit != 9 {
		t.Fatalf("宿主不支持分页投影时必须回显分页参数且 Running=false，得到 %#v", got)
	}
}

// TestBoardReaderForwardsToProjection 钉"装配了就走宿主"：读法不得本地判定/缓存，
// 每个读法都直达宿主端口，参数逐字传递。
func TestBoardReaderForwardsToProjection(t *testing.T) {
	fake := &boardFake{}
	reader := NewBoardReader(fake)

	if got := reader.BoardViewFor("s1"); got == nil {
		t.Fatal("装配了看板投影时必须把宿主的读数交给调用方")
	}
	if live := reader.SessionLiveFor("s1"); !live.Running {
		t.Fatalf("装配了 live 投影时必须把宿主的读数交给调用方，得到 %#v", live)
	}
	page := reader.SessionLivePageFor("s1", 5, 15)
	if !page.Running || fake.lastPageOffset != 5 || fake.lastPageLimit != 15 {
		t.Fatalf("分页参数必须逐字透传（offset=5 limit=15），得到 %#v / offset=%d limit=%d",
			page, fake.lastPageOffset, fake.lastPageLimit)
	}
	if fake.boardCalls != 1 || fake.liveCalls != 1 || fake.pageCalls != 1 {
		t.Fatalf("每个读法各转发一次，不得本地缓存：board=%d live=%d page=%d",
			fake.boardCalls, fake.liveCalls, fake.pageCalls)
	}
}
