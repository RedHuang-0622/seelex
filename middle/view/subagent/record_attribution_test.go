package subagent_view

// record_attribution_test.go — 钉住子代理工具记录的**归属与次序**（2026-10-19 方向 C）。
//
// 两条不能退让的口径：
//  1. **归属键显式**：owner 主会话由调用方（装配层，从执行上下文解析）给进来，
//     本包**绝不**用 `Snapshot.Session.ID` 当归属——切会话串写（#8/#13）就是这么来的；
//  2. **记账先发生**：无 `NodeID` 的事件（旧实现整条丢弃，#9）必须先记一笔再做 Plan
//     投影；投影的早退分支只表示"不进投影"。

import (
	"testing"

	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/kernel/state"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
)

// readingProbe 是叶子暂存入口的替身：记下每次读数带进来的归属与事件身份。
type readingProbe struct {
	owners []string
	events []dto.SubagentToolEvent
}

func (probe *readingProbe) record(ownerSessionID string, event dto.SubagentToolEvent) {
	probe.owners = append(probe.owners, ownerSessionID)
	probe.events = append(probe.events, event)
}

func newProbedCoordinator(t *testing.T, probe *readingProbe) *Coordinator {
	t.Helper()
	core := &state.Core{}
	// 视图会话故意设成"B"：任何拿视图会话当归属的实现都会被下面的断言抓住。
	core.Snapshot.Session = model.SessionState{ID: "view-session-B"}
	return NewCoordinator(Deps{
		Core: core,
		Limits: func() dto.ViewLimits {
			return dto.ViewLimits{PlanNodeEvents: 100, EvidenceChars: 800}
		},
		RecordToolOutcome: probe.record,
	})
}

func TestSubagentReadingUsesOwnerFromExecutionContext(t *testing.T) {
	probe := &readingProbe{}
	coordinator := newProbedCoordinator(t, probe)

	coordinator.HandleSubagentToolEventFor("owner-session-A", dto.SubagentToolEvent{
		ID: "subtool-1", NodeID: "node-a", Name: "bash", Status: dto.ToolEventSuccess,
	})

	if len(probe.owners) != 1 || probe.owners[0] != "owner-session-A" {
		t.Fatalf("归属必须来自执行上下文（不是视图会话）：%+v", probe.owners)
	}
	if len(probe.events) != 1 || probe.events[0].NodeID != "node-a" || probe.events[0].Name != "bash" {
		t.Fatalf("读数内容不对：%+v", probe.events)
	}
}

func TestSubagentReadingHappensBeforePlanProjectionEarlyReturn(t *testing.T) {
	probe := &readingProbe{}
	coordinator := newProbedCoordinator(t, probe)

	// 没有 NodeID（旧实现整条丢）、也没有 Plan 快照（投影必然早退）。
	coordinator.HandleSubagentToolEventFor("owner-session-A", dto.SubagentToolEvent{
		ID: "subtool-2", Name: "read_file", Status: dto.ToolEventError, Error: "permission denied",
	})

	if len(probe.events) != 1 {
		t.Fatalf("无 NodeID 的事件不许整条丢（病灶 #9）：%+v", probe.events)
	}
	if probe.events[0].ID != "subtool-2" || probe.events[0].Status != dto.ToolEventError {
		t.Fatalf("兜底身份必须保住事件 ID 与状态：%+v", probe.events[0])
	}
}

func TestSubagentReadingCompatEntryCarriesNoOwner(t *testing.T) {
	probe := &readingProbe{}
	coordinator := newProbedCoordinator(t, probe)

	// 兼容面（无归属入参）：读数交出去时 owner 是空串 —— 由读侧丢弃并计数，
	// 本包不替它编一个（尤其不拿视图会话编）。
	coordinator.HandleSubagentToolEvent(dto.SubagentToolEvent{
		ID: "subtool-3", NodeID: "node-c", Name: "bash", Status: dto.ToolEventSuccess,
	})
	if len(probe.owners) != 1 || probe.owners[0] != "" {
		t.Fatalf("兼容面不该编造归属：%+v", probe.owners)
	}
}

func TestSubagentCommitEntersByOwnerAndNode(t *testing.T) {
	var got [][2]string
	coordinator := NewCoordinator(Deps{
		Core: &state.Core{},
		Limits: func() dto.ViewLimits {
			return dto.ViewLimits{PlanNodeEvents: 100, EvidenceChars: 800}
		},
		CommitToolRecord: func(ownerSessionID, nodeID string) {
			got = append(got, [2]string{ownerSessionID, nodeID})
		},
	})
	coordinator.CloseToolRecordUnit("owner-session-A", "node-a")
	if len(got) != 1 || got[0] != [2]string{"owner-session-A", "node-a"} {
		t.Fatalf("收口入口要原样带归属与节点：%+v", got)
	}
}
