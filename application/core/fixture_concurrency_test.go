package core

// fixture_concurrency_test.go — 夹具竞态的**回归**与**防复发**。
//
// # 这个夹具竞态是什么
//
// 对象：测试夹具 teamRecordingSessions / librarySessions（fakeSessions 的团队存储面）。
//
// 成因：**锁只上在了一侧**。夹具自己的存取方法（ReadLifecycleOrder / WriteTeamRegistry
// / setLifecycle ...）是加锁的——因为生产路径是并发的：服务与后台 goroutine 通过
// contract.SessionPort 读 lifecycle 顺序、注册表、库。但测试侧曾经绕过入口直接写字段：
//
//	sessions.policy, sessions.order = dto.OrderPolicyUserMainDecided, []string{"user", "main", "auditor"}
//	sessions.order = nil
//	sessions.registry = dto.TeamRegistry{}
//
// 按 Go 内存模型，**加锁的读**与**不加锁的写**之间没有 happens-before 关系，因此
// 这仍然是数据竞争，与"读侧加没加锁"无关：`go test -race` 会直接报
//
//	WARNING: DATA RACE
//	Read at 0x... by goroutine 18:
//	  (*teamRecordingSessions).ReadLifecycleOrder()   goal_team_wiring_test.go:33
//	Previous write at 0x... by goroutine 17:
//	  TestZZZFixtureRaceRepro()                        fixture_race_repro_test.go:32
//
// 危害不只是"偶尔崩"：写侧拆成两条字段（policy 与 order），读侧可能读到**半边更新**
// 的组合（新 policy + 旧 order）。装配/同步路径会按自相矛盾的顺序事实推进——症状
// 飘忽、只在特定调度/核数下出现，是最难查的一类 flake。
//
// # 解
//
//  1. 两侧走同一把锁：字段只能通过夹具的加锁入口读写（见 goal_team_wiring_test.go
//     的 setLifecycle/setOrder/setRegistry/lifecycleSnapshot/registrySnapshot/
//     ensuredRoles/orderSnapshot，以及 librarySessions.writeCount/globalWriteCount）；
//  2. 机械防线：TestFixturesDoNotBypassLockedAccessors 用 AST 扫本包测试源码，
//     再出现"绕过入口的字段选择子"直接失败——人可能忘，检测不会。

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// TestTeamRecordingSessionsAccessorsAreRaceFree 并发读写夹具：-race 下证明读写两侧
// 走同一把锁；同时断言**读到的组合是自洽的**（不会出现新 policy + 旧 order 的半边更新）。
func TestTeamRecordingSessionsAccessorsAreRaceFree(t *testing.T) {
	sessions := &teamRecordingSessions{}
	const rounds = 200
	var wait sync.WaitGroup
	// 写侧：两条互斥的"事实"，每条同时改解 policy 与 order（一次加锁写入）。
	wait.Add(1)
	go func() {
		defer wait.Done()
		for index := 0; index < rounds; index++ {
			if index%2 == 0 {
				sessions.setLifecycle(dto.OrderPolicyUserMainDecided, []string{"user", "main"})
				continue
			}
			sessions.setLifecycle(dto.OrderPolicyGoalLoop, []string{"user", "main", "tl"})
		}
	}()
	// 读侧：生产路径的样子（通过 port 方法读）。
	wait.Add(1)
	go func() {
		defer wait.Done()
		for index := 0; index < rounds; index++ {
			policy, order := sessions.lifecycleSnapshot()
			switch policy {
			case dto.OrderPolicyUserMainDecided:
				if len(order) != 2 {
					t.Errorf("半边更新：policy=%q 配 order=%v（写侧一次加锁写两个字段，读侧不该看到撕裂）", policy, order)
					return
				}
			case dto.OrderPolicyGoalLoop:
				if len(order) != 3 {
					t.Errorf("半边更新：policy=%q 配 order=%v", policy, order)
					return
				}
			}
		}
	}()
	wait.Wait()

	// 注册表与库写次数同样只能通过加锁入口：
	sessions.setRegistry(dto.TeamRegistry{TeamKind: dto.TeamKindGoalA2A})
	if registry := sessions.registrySnapshot(); registry.TeamKind != dto.TeamKindGoalA2A {
		t.Fatalf("注册表快照 = %+v", registry)
	}
	library := newLibrarySessions()
	library.setOrder([]string{"user", "main"})
	if got := library.orderSnapshot(); len(got) != 2 {
		t.Fatalf("库夹具顺序快照 = %v", got)
	}
	if writes := library.writeCount(); writes != 0 {
		t.Fatalf("未写入时 writeCount = %d", writes)
	}
}

// TestFixturesDoNotBypassLockedAccessors 是防复发的机械防线：AST 扫描本包测试源码，
// 出现"对夹具字段的选择子访问"（即绕过加锁入口）就失败。
//
// 允许两处：夹具自身的定义文件（存取入口就写在那里）与本文件。
func TestFixturesDoNotBypassLockedAccessors(t *testing.T) {
	// 夹具字段名（teamRecordingSessions / librarySessions 的私有状态）。
	fields := map[string]bool{
		"policy": true, "order": true, "registry": true, "ensured": true,
		"writes": true, "library": true, "employeeLibrary": true,
		"defaultOrder": true, "globalWrites": true,
	}
	// 夹具变量名（本包测试约定叫 sessions；夹具定义文件自己当然要碰字段）。
	receivers := map[string]bool{"sessions": true, "library": true}
	exempt := map[string]bool{"goal_team_wiring_test.go": true, "fixture_concurrency_test.go": true}

	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	set := token.NewFileSet()
	scanned := 0
	for _, path := range files {
		name := filepath.Base(path)
		if !strings.HasSuffix(name, "_test.go") || exempt[name] {
			continue
		}
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		parsed, err := parser.ParseFile(set, name, source, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		scanned++
		ast.Inspect(parsed, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			ident, ok := selector.X.(*ast.Ident)
			if !ok || !receivers[ident.Name] || !fields[selector.Sel.Name] {
				return true
			}
			position := set.Position(selector.Pos())
			t.Errorf("%s:%d 绕过了夹具的加锁入口（%s.%s）：夹具的读侧在生产路径上是并发的，"+
				"绕过入口的访问是数据竞争（见本文件顶部说明）。请改用 setLifecycle/setOrder/"+
				"setRegistry/lifecycleSnapshot/registrySnapshot/orderSnapshot/ensuredRoles/writeCount。",
				name, position.Line, ident.Name, selector.Sel.Name)
			return true
		})
	}
	if scanned == 0 {
		t.Fatal("没有扫描到任何测试文件：防线失效（路径变了？）")
	}
}
