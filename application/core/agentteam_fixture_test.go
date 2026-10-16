package core

import (
	"testing"

	"github.com/RedHuang-0622/seelex/contract/dto"
)

// agentteam_fixture_test.go — application 层的团队夹具。
//
// 这里的团队曾经来自产品里的内置形态目录（`agentteam/presets.go` 的 goal-a2a /
// review-team / research-team）。形态目录已于 2026-10-01 删除：一支团队有谁、什么
// 顺序由数据说（TeamSpec / 团队库条目），不由代码里的模板说。测试需要一份具体
// 团队时，就地写一份夹具即可。
//
// 状态（A06，2026-10-15）：**装配面（`Factory` / `MaterializeAgentTeam`）已随 J-1a
// 退场**，因此这里的 `dto.TeamSpec` 夹具（原 `goalTeamFixture`）已无被测对象，随之删除——
// 留着它会让"本包还有一条活的装配链"看起来成立。下面留下的是**仍在被使用的夹具**：
// 全局母本端口桩（`librarySessions`）与视图会话桩（`summonService`）。

// ── librarySessions（自 agentteam_library_service_test.go 迁入，2026-10-12 J-1a）──
//
// 为什么迁进来：它在 `teamRecordingSessions` 之上补出**全局母本**（团队库 / 员工库 /
// 默认顺序）的读写桩。原本的定义文件随 Agent Team 母本 CRUD 的用例一起删除（那些用例
// 测的面已经不在），但 `fixture_concurrency_test.go` 仍要用它做**夹具锁纪律**的守卫
// （直接读夹具字段 = 数据竞争）。夹具不是被测能力，收敛到同一份夹具文件即可。
//
// 母本读写方法（ReadTeamLibrary / WriteTeamLibrary / …）保留：它们与
// fixture_concurrency_test.go 的字段名清单（`writes`/`library`/`employeeLibrary`/
// `defaultOrder`/`globalWrites`）是同一份事实——删字段就会让那条机械防线失去对象。
//
// 状态（A06）：这些"母本"端口（`dto.TeamLibrary` / `dto.EmployeeLibrary` /
// `dto.DefaultOrder`）属于**已退场的 Agent Team 用户面**，保留为 legacy 夹具桩：
// 只用于锁纪律守卫与旧 wire 形状的回归，不代表产品还有母本 CRUD。
type librarySessions struct {
	*teamRecordingSessions
	library         dto.TeamLibrary
	writes          int
	employeeLibrary dto.EmployeeLibrary
	defaultOrder    dto.DefaultOrder
	globalWrites    int
}

func newLibrarySessions() *librarySessions {
	return &librarySessions{teamRecordingSessions: &teamRecordingSessions{}}
}

func (s *librarySessions) ReadTeamLibrary(string) (dto.TeamLibrary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.library, nil
}

func (s *librarySessions) WriteTeamLibrary(_ string, library dto.TeamLibrary) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writes++
	library.Configured = true
	s.library = library
	return nil
}

func (s *librarySessions) ReadEmployeeLibrary(string) (dto.EmployeeLibrary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	library := s.employeeLibrary
	library.Employees = append([]dto.RoleSpec(nil), s.employeeLibrary.Employees...)
	return library, nil
}

func (s *librarySessions) WriteEmployeeLibrary(_ string, library dto.EmployeeLibrary) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.globalWrites++
	library.Configured = true
	s.employeeLibrary = library
	return nil
}

func (s *librarySessions) ReadDefaultOrder(string) (dto.DefaultOrder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	order := s.defaultOrder
	order.OrderRoles = append([]string(nil), s.defaultOrder.OrderRoles...)
	return order, nil
}

func (s *librarySessions) WriteDefaultOrder(_ string, order dto.DefaultOrder) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.globalWrites++
	order.Configured = true
	s.defaultOrder = order
	return nil
}

// globalWriteCount 读当前全局母本写次数（断言"读不写盘"用）。
func (s *librarySessions) globalWriteCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.globalWrites
}

// writeCount 读团队库写次数（加锁：直接读 sessions.writes 与写侧没有 happens-before
// 关系，那是数据竞争，见 fixture_concurrency_test.go）。
func (s *librarySessions) writeCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writes
}

// setLibrary 覆盖夹具的团队库（加锁）。
func (s *librarySessions) setLibrary(library dto.TeamLibrary) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.library = library
}

// summonService 造一个带会话端口的服务，并把视图会话钉成固定 ID
// （"落到当前会话"的用例，断言必须指向确定的那个会话）。
//
// 2026-10-12（J-1a）：它原先住在 input_team_test.go（召唤用例的夹具），那个文件随
// `@` 手动召唤团队退场而删除；函数本身与会话团队面无关（只钉视图会话号），因此迁到
// 本夹具文件继续供 goal 权限面等用例使用。
func summonService(t *testing.T, sessions SessionPort) *Service {
	t.Helper()
	service := newTestService(t, &fakeEngine{}, withTestSessions(sessions))
	service.ViewMu.Lock()
	service.Core.Snapshot.Session.ID = "sess-summon"
	service.ViewMu.Unlock()
	return service
}
