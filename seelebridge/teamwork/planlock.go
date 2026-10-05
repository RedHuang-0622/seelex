package teamwork

// planlock.go — 「计划头读-改-写」的**进程内**互斥锁。
//
// 背景（2026-10-05，docs/devlog/2026-10-05-teamwork-self-survey.md §2）：计划头
// （sessionstore 的 moduleTeamwork head）的每个变更方法都是 ReadPlan → 改 → WritePlan，
// 而 store 的模块锁只覆盖单次 commitModuleHead——**跨这三步没有锁**。实测 5 个工作项
// 并发 settle 时，2 个被后写覆盖回 running（丢失更新）。这个文件补上那把跨步的锁。
//
// 取舍（为什么不是 store 层 CAS）：
//
//   - **不在 store 层做 CAS 的原因**：CAS 要求每次写入携带"我读到的是哪一版"，即计划头
//     要从结构化文档退化成单调版本号 + 整个 RMW 在 store 内重放（compare → 调用方重试）。
//     交付契约第 5 条明确"不改 sessionstore 的单次写接口"，且 Coordinator 的变更**不是**
//     单点赋值（recomputeMilestones、幂等闸门、审计行都在临界区里做），把这些搬进 store
//     会把"编排语义"漏进存储层——而 store.go 的分工注释正是不许这两层互相认识。
//   - **锁粒度选在"会话作用域键"**：计划头天然按 Key 寻址（一个 session 一份计划），
//     变更之间彼此冲突的判据也是"同一份计划"。放到更细（work item）会让同一计划的多个
//     工作项各写各的，仍会互相覆盖；放到更粗（进程/项目）会误伤无关会话。
//   - **顺序铁律**：计划锁永远最外层——它在任何 store 调用（WritePlan / AppendEvent /
//     AppendBinding，内部都取 sessionstore 的模块锁）**之前**取得，释放之后才返回。
//     因此"计划锁 → 模块锁"是一条全序，不存在反向获取，也就没有环。

import (
	"sync"

	"github.com/RedHuang-0622/seelex/sessionstore"
)

// planLocks 是「计划头读-改-写」的互斥锁表，按会话作用域键（sessionstore.Key）分片。
//
// 为什么是**包级表**而不是 Coordinator 的字段：契约要求"同一进程内、同一 Key 的所有
// Coordinator 实例共享同一把计划锁"。Runtime.coordinatorForKey 确实按 Key 复用实例，但
// 那是**装配层**的承诺、且只覆盖单个 Runtime——测试（newItemFixtureOver）与"重启"复现里，
// 同一个 Key 上会同时存在多个 Coordinator（共享同一份持久面）。锁放在实例字段上时，那种
// 情形就各锁各的，等于没有锁。包级表把"同一 Key = 同一把锁"变成**结构性**事实。
//
// 表的生命周期与进程同寿：Key 数量有界（会话数），条目单调增长不构成实质泄漏；不设 GC
// 是为了让"并发安全"不依赖任何后台清理（清理与取锁之间的一点竞态就足以让两方各拿一把）。
var (
	planLocksMu sync.Mutex
	planLocks   = map[sessionstore.Key]*sync.Mutex{}
)

// planLockFor 返回（必要时建）某个会话作用域键的计划锁。
func planLockFor(key sessionstore.Key) *sync.Mutex {
	planLocksMu.Lock()
	defer planLocksMu.Unlock()
	if lock, ok := planLocks[key]; ok {
		return lock
	}
	lock := &sync.Mutex{}
	planLocks[key] = lock
	return lock
}

// lockPlan / unlockPlan 成对使用，包住一段「读计划 → 改 → 写计划」的临界区。
//
// **不可重入**：临界区里只许调用不取同一把锁的东西。Coordinator 内部需要嵌套的场合
// （Close → closeStepsLocked）一律走 `...Locked` 变体，由外层持锁、内层不取锁。
func (c *Coordinator) lockPlan()   { planLockFor(c.key).Lock() }
func (c *Coordinator) unlockPlan() { planLockFor(c.key).Unlock() }
