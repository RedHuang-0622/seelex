package sessionstore

// retention_advisory_lock_test.go — retention 建议读路径的**锁前提**验收
// （2026-09-29 锁面审计 §2.1：无锁调用 *Locked 入口 → 无锁发布 module head）。
//
// 这一族的判据是磁盘：module_heads.go 明文规定「重建发布必须持该模块锁」
// （否则会覆盖并发 writer 刚发布的 head，或把「append 完成但 head 未发布」的
// 行提升为已提交）。因此这里的断言是"持锁期间 metadata/message.json 一个字节
// 都没变"，不赌调度、不用 sleep。

import (
	"bytes"
	"testing"
)

// TestRetentionAdvisoryDoesNotPublishMessageHeadOutsideModuleLock 钉住：advisory
// 读路径在**零模块锁**下执行（调用方只持 router.mu），所以读 message head 必须
// 走无锁入口（TryLock + 有界失败、绝不发布）。
//
// 修前的形态：`repository.layout.readMessageHeadLocked(key)` —— 它的先决条件是
// "调用方持 messageMu"，在这里不成立；head 损坏时它走 repairModuleHeadLocked，
// 于是在零模块锁下原子替换 metadata/message.json。并发的
// messageCommitSyncLocked 与这次无锁发布交错时，head 可被回退到更早的 LastSeq，
// 下一次提交的 reapUnpublishedLocked 会按 head 截断尾分片 → **已提交行被物理删除**。
func TestRetentionAdvisoryDoesNotPublishMessageHeadOutsideModuleLock(t *testing.T) {
	const projectID, sessionID = "project-advisory", "session-advisory"
	router := newLockProfileRouter(t, t.TempDir())
	if err := router.SaveCommitWorkspace(projectID, sessionID, Commit{Events: []Event{
		{Role: "user", Content: "第一问", MessageID: "u1"},
	}}); err != nil {
		t.Fatal(err)
	}
	repository, ok := router.jsonRepository()
	if !ok {
		t.Fatal("router 不是 JSON 会话存储布局")
	}
	key := Key{ProjectID: projectID, SessionID: sessionID}
	if !repository.active(key) {
		t.Fatal("会话目录不是会话存储布局（advisory 会早退成 legacy 零值，测试失去意义）")
	}
	layout := repository.layout
	// 前置：无人持锁时 advisory 正常（否则下面的断言没有区分度）。
	if _, err := router.RetentionAdvisoryWorkspace(projectID, sessionID); err != nil {
		t.Fatalf("前置：advisory 读失败: %v", err)
	}

	corruptHeadChecksum(t, layout, key, moduleMessage)
	corrupted, err := readSharedFile(layout.modulePath(key, moduleMessage))
	if err != nil {
		t.Fatalf("读取损坏 head: %v", err)
	}

	// 另一路 writer 持 messageMu（= 正在提交）。advisory 此时不得抢锁、不得发布。
	messageLock := layout.mu(key, moduleMessage)
	messageLock.Lock()
	_, advisoryErr := router.RetentionAdvisoryWorkspace(projectID, sessionID)
	afterHold, readErr := readSharedFile(layout.modulePath(key, moduleMessage))
	messageLock.Unlock()
	if readErr != nil {
		t.Fatalf("读取 head: %v", readErr)
	}
	if !bytes.Equal(corrupted, afterHold) {
		t.Fatal("advisory 在他人持 messageMu 期间改写了 metadata/message.json —— 无锁发布逃出了模块锁")
	}
	// 无锁入口的语义是「有界失败」：拿不到锁就原样上报，不静默返回陈旧值。
	if advisoryErr == nil {
		t.Fatal("拿不到 message 模块锁时 advisory 仍成功 —— 说明它抢锁/发布了（应为有界失败）")
	}

	// 锁释放后：下一次读自愈（TryLock 成功 → 锁内重建发布），advisory 恢复。
	if _, err := router.RetentionAdvisoryWorkspace(projectID, sessionID); err != nil {
		t.Fatalf("锁释放后 advisory 仍失败: %v", err)
	}
	if _, rawErr := layout.readModuleHeadFileRaw(key, moduleMessage); rawErr != nil {
		t.Fatalf("advisory 读未自愈 message head: %v", rawErr)
	}
}

// TestRetentionAdvisoryHealsMessageHeadWhenLockIsFree 是上一条的**反向护栏**：
// 无锁入口不是"永不发布"，锁可用时必须在原地自愈（TryLock 成功 → 持锁重建），
// 否则修法就从"锁外乱发布"滑到"head 坏了永远读不回来"。
func TestRetentionAdvisoryHealsMessageHeadWhenLockIsFree(t *testing.T) {
	const projectID, sessionID = "project-advisory-heal", "session-advisory-heal"
	router := newLockProfileRouter(t, t.TempDir())
	if err := router.SaveCommitWorkspace(projectID, sessionID, Commit{Events: []Event{
		{Role: "user", Content: "第一问", MessageID: "u1"},
	}}); err != nil {
		t.Fatal(err)
	}
	repository, ok := router.jsonRepository()
	if !ok {
		t.Fatal("router 不是 JSON 会话存储布局")
	}
	key := Key{ProjectID: projectID, SessionID: sessionID}
	layout := repository.layout
	corruptHeadChecksum(t, layout, key, moduleMessage)

	if _, err := router.RetentionAdvisoryWorkspace(projectID, sessionID); err != nil {
		t.Fatalf("无人持锁时 advisory 应自愈，却失败: %v", err)
	}
	if _, rawErr := layout.readModuleHeadFileRaw(key, moduleMessage); rawErr != nil {
		t.Fatalf("无人持锁时 advisory 未自愈 message head: %v", rawErr)
	}
	// 自愈后的 head 仍能支撑提交（自愈不改写 generation 与只存在于 head 的字段）。
	if _, err := layout.messageCommit(key, "c2", []Event{
		messageRow(0, "a1", "assistant", EventKindLLM, "第一答"),
	}); err != nil {
		t.Fatalf("自愈后继续提交失败: %v", err)
	}
}
