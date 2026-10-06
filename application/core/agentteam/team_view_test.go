package agentteam

import (
	"errors"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// ② 与 ④ 的用例：
//   - floor_role 由**可选**读端口从 message head.floor 取回并填进成员表；端口
//     未实现/读取失败时保持空并只进 DesignNotice（不阻断成员表）；
//   - review-team / research-team 这类"有装配没执行者"的团队必须在成员表里说
//     出事实，不能让 UI 以为装配完就有人干活。

// floorFakePort 在基础装配桩上补出可选 floor 读面。
type floorFakePort struct {
	*fakePort
	roleName string
	err      error
}

func (port *floorFakePort) ReadFloorRole(string) (string, error) {
	return port.roleName, port.err
}

// TestRegistryViewFillsFloorFromOptionalPort 钉住 ②：数据在 message head 里，
// 出口在可选读端口——实现了就填，没实现就留空（旧宿主不因缺读面而报错）。
func TestRegistryViewFillsFloorFromOptionalPort(t *testing.T) {
	port := &floorFakePort{fakePort: newFakePort(), roleName: RoleTechlead}
	factory, err := NewFactory(port)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := NewRegistry(port)
	if err != nil {
		t.Fatal(err)
	}
	spec := testGoalSpec()
	result, err := factory.Materialize("main-1", spec, 0)
	if err != nil {
		t.Fatal(err)
	}
	// 装配回执视图同样带 floor（前端装配后立即重绘高亮）。
	if result.View.FloorRole != RoleTechlead {
		t.Fatalf("装配回执 floor_role = %q, want %q", result.View.FloorRole, RoleTechlead)
	}
	view, err := registry.View("main-1")
	if err != nil {
		t.Fatal(err)
	}
	if view.FloorRole != RoleTechlead {
		t.Fatalf("floor_role = %q, want %q", view.FloorRole, RoleTechlead)
	}
	if len(view.DesignNotice) != 0 {
		t.Fatalf("满执行者的团队不应有条目: %v", view.DesignNotice)
	}

	// 未实现 FloorPort 的宿主：留空、不报错。
	plain := newFakePort()
	plainFactory, err := NewFactory(plain)
	if err != nil {
		t.Fatal(err)
	}
	plainRegistry, err := NewRegistry(plain)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := plainFactory.Materialize("main-1", spec, 0); err != nil {
		t.Fatal(err)
	}
	plainView, err := plainRegistry.View("main-1")
	if err != nil {
		t.Fatal(err)
	}
	if plainView.FloorRole != "" {
		t.Fatalf("未实现 FloorPort 时 floor_role 应留空, 得 %q", plainView.FloorRole)
	}
}

// TestRegistryViewReportsFloorReadFailure 钉住错误语义：floor 是运行态读面，
// 读失败不阻断成员表，但必须作为事实被报出来（不静默）。
func TestRegistryViewReportsFloorReadFailure(t *testing.T) {
	port := &floorFakePort{fakePort: newFakePort(), err: errors.New("head 读取失败")}
	factory, err := NewFactory(port)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := NewRegistry(port)
	if err != nil {
		t.Fatal(err)
	}
	spec := testGoalSpec()
	if _, err := factory.Materialize("main-1", spec, 0); err != nil {
		t.Fatal(err)
	}
	view, err := registry.View("main-1")
	if err != nil {
		t.Fatalf("floor 读失败不应让成员表整体失败: %v", err)
	}
	if view.FloorRole != "" {
		t.Fatalf("读失败时不应臆造 floor: %q", view.FloorRole)
	}
	found := false
	for _, notice := range view.DesignNotice {
		if strings.Contains(notice, "floor 读取失败") {
			found = true
		}
	}
	if !found {
		t.Fatalf("floor 读失败必须进 DesignNotice: %v", view.DesignNotice)
	}
}

// TestSecondAndThirdShapesDeclareNoAutomaticTurn 钉住 ④：只带注册配置与角色会话、没有运行时
// 执行者的团队，成员表必须明说，否则前端「装配完成」会被读成「有人在工作」。
func TestSecondAndThirdShapesDeclareNoAutomaticTurn(t *testing.T) {
	cases := []struct {
		name string
		spec dto.TeamSpec
		role string
	}{
		{"review-team", testReviewSpec(), "reviewer"},
		{"research-team", testResearchSpec(), "researcher"},
	}
	for _, testCase := range cases {
		port := newFakePort()
		factory, err := NewFactory(port)
		if err != nil {
			t.Fatal(err)
		}
		result, err := factory.Materialize("main-1", testCase.spec, 0)
		if err != nil {
			t.Fatal(err)
		}
		notice := strings.Join(result.View.DesignNotice, "\n")
		if !strings.Contains(notice, "没有自动回合") || !strings.Contains(notice, testCase.role) {
			t.Fatalf("%s 的成员表必须声明没有自动回合（含 %s）: %v", testCase.name, testCase.role, result.View.DesignNotice)
		}
		// 视图读面同样口径（前端走 registry.View）。
		registry, err := NewRegistry(port)
		if err != nil {
			t.Fatal(err)
		}
		view, err := registry.View("main-1")
		if err != nil {
			t.Fatal(err)
		}
		viewNotice := strings.Join(view.DesignNotice, "\n")
		if !strings.Contains(viewNotice, "没有自动回合") {
			t.Fatalf("读视图必须同样声明没有自动回合: %v", view.DesignNotice)
		}
	}

	// tl 有执行者（goal 治理 ADVISOR 回合）：不得出现该条目。
	port := newFakePort()
	factory, err := NewFactory(port)
	if err != nil {
		t.Fatal(err)
	}
	goalResult, err := factory.Materialize("main-1", testGoalSpec(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if notice := strings.Join(goalResult.View.DesignNotice, "\n"); strings.Contains(notice, "没有自动回合") {
		t.Fatalf("tl 有自动回合，不该进无自动回合清单: %v", goalResult.View.DesignNotice)
	}
}
