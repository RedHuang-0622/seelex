package sessionstore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func boolPtr(value bool) *bool { return &value }

// TestJSONDataRootLockAcquiredAndReleased 验证 §9/D11 数据根独占锁随
// JSON repository 生命周期取得与归还：同进程第二次打开共享（引用计数），
// 全部 Close 后锁文件删除、可再次打开。
func TestJSONDataRootLockAcquiredAndReleased(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()

	open := func() Repository {
		t.Helper()
		repository, err := Open(ctx, Config{Backend: BackendJSON, Path: root})
		if err != nil {
			t.Fatalf("open JSON root: %v", err)
		}
		return repository
	}

	first := open()
	lockPath := filepath.Join(root, dataRootLockFile)
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("lock.owner missing after open: %v", err)
	}
	if _, held := DataRootLockedBy(root); !held {
		t.Fatal("DataRootLockedBy must report held after open")
	}

	// 同进程（同一写者）重复打开同一数据根 = 引用计数共享，不报错。
	second := open()
	if err := second.Close(); err != nil {
		t.Fatalf("close second holder: %v", err)
	}
	if _, held := DataRootLockedBy(root); !held {
		t.Fatal("lock must stay held while first holder is open")
	}

	if err := first.Close(); err != nil {
		t.Fatalf("close first holder: %v", err)
	}
	if _, err := os.Stat(lockPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("lock.owner must be removed after last Close (err=%v)", err)
	}
	if _, held := DataRootLockedBy(root); held {
		t.Fatal("DataRootLockedBy must report free after release")
	}

	// 归还后可再次打开。
	again := open()
	if err := again.Close(); err != nil {
		t.Fatalf("close reopened holder: %v", err)
	}
}

// TestJSONDataRootLockForeignProcessRejected 验证存活的其他进程持有锁时
// Open 立即报 ErrDataRootLocked（D11：不等待、不自旋、不接管）。
func TestJSONDataRootLockForeignProcessRejected(t *testing.T) {
	parentPID := os.Getppid()
	if parentPID <= 0 || !processAlive(parentPID) {
		t.Skip("no alive parent process to simulate a foreign holder")
	}
	root := t.TempDir()
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	record := newOwnerRecord(root)
	record.PID = parentPID
	record.OwnerToken = "foreign-process-token"
	record.RenewedAt = time.Now().UTC()
	if err := writeOwnerRecord(filepath.Join(root, dataRootLockFile), record); err != nil {
		t.Fatal(err)
	}

	_, err := Open(context.Background(), Config{Backend: BackendJSON, Path: root})
	if !errors.Is(err, ErrDataRootLocked) {
		t.Fatalf("open under foreign live lock err = %v, want ErrDataRootLocked", err)
	}
}

// TestJSONDataRootStaleLockRespectsAutoRecover 验证陈旧锁默认只报错
// （auto_recover=false），显式开启后才接管。
func TestJSONDataRootStaleLockRespectsAutoRecover(t *testing.T) {
	root := t.TempDir()
	record := newOwnerRecord(root)
	// 不可达 pid + 超时续租 = 陈旧。
	record.PID = 1 << 30
	record.OwnerToken = "stale-token"
	record.RenewedAt = time.Now().UTC().Add(-2 * time.Hour)
	if err := writeOwnerRecord(filepath.Join(root, dataRootLockFile), record); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	if _, err := Open(ctx, Config{Backend: BackendJSON, Path: root}); !errors.Is(err, ErrDataRootStaleLock) {
		t.Fatalf("open under stale lock (no recover) err = %v, want ErrDataRootStaleLock", err)
	}

	repository, err := Open(ctx, Config{
		Backend: BackendJSON, Path: root,
		SessionStorage: storageSettings{AutoRecover: boolPtr(true), StaleAfterSeconds: 300},
	})
	if err != nil {
		t.Fatalf("open with auto_recover=true must take over stale lock: %v", err)
	}
	if err := repository.Close(); err != nil {
		t.Fatalf("close recovered holder: %v", err)
	}
}

// TestJSONDataRootCrashedGUIResidualLockRecovered 钉住 2026-09-17 的实际事故形状：
// dev GUI 被强杀（Close 未执行）后残留 lock.owner，下一次启动必须能起来。
//
// 复现链：GUI 正常关闭会走 jsonRepository.Close → releaseDataRootLock 删除锁文件；
// 崩溃/强杀不会，于是残留一把"owner pid 已死、心跳已停"的锁。修复前
// （lock_auto_recover 缺省 false）这正是用户看到的现象——GUI 启动即退出并打印
// `stale data root lock found (set session_storage.lock.auto_recover=true to take over)`。
// 修复后 config/seelex.yaml 写死 lock_auto_recover: true，等价于本用例的显式设置。
//
// 反例（同一条链的另一半）由 TestJSONDataRootLockForeignProcessRejected 钉住：
// 持有者是**活着的**其他进程时必须继续拒绝启动，这是单写者不变量。
func TestJSONDataRootCrashedGUIResidualLockRecovered(t *testing.T) {
	root := t.TempDir()

	// 1) 模拟被强杀的 GUI：留下 owner 记录后进程消失（不调用 Close）。
	dead := newOwnerRecord(root)
	dead.PID = 1 << 30 // 不可达 pid：processAlive=false，稳过陈旧判据
	dead.OwnerToken = "crashed-gui-token"
	dead.Program = "seelex-gui.exe"
	dead.RenewedAt = time.Now().UTC().Add(-time.Hour) // 心跳早停
	lockPath := filepath.Join(root, dataRootLockFile)
	if err := writeOwnerRecord(lockPath, dead); err != nil {
		t.Fatal(err)
	}

	// 2) 修复前口径（保守）：只报错，拒绝启动。
	ctx := context.Background()
	if _, err := Open(ctx, Config{Backend: BackendJSON, Path: root}); !errors.Is(err, ErrDataRootStaleLock) {
		t.Fatalf("conservative open err = %v, want ErrDataRootStaleLock", err)
	}

	// 3) 修复后默认口径：接管残留锁并正常打开，无需用户手工删锁。
	repository, err := Open(ctx, Config{
		Backend: BackendJSON, Path: root,
		SessionStorage: storageSettings{AutoRecover: boolPtr(true), StaleAfterSeconds: 300},
	})
	if err != nil {
		t.Fatalf("open after crash (auto_recover=true) = %v, want takeover", err)
	}
	owner, err := readOwnerRecord(lockPath)
	if err != nil {
		t.Fatalf("read recovered lock record: %v", err)
	}
	if owner.OwnerToken == dead.OwnerToken {
		t.Fatal("takeover must rewrite the lock record to the new owner")
	}
	if owner.PID != os.Getpid() {
		t.Fatalf("recovered lock pid = %d, want current process %d", owner.PID, os.Getpid())
	}

	// 4) 正常关闭后锁归还，下一次启动不再走接管路径。
	if err := repository.Close(); err != nil {
		t.Fatalf("close recovered holder: %v", err)
	}
	if _, err := os.Stat(lockPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("lock.owner must be removed on clean Close (err=%v)", err)
	}
	clean, err := Open(ctx, Config{
		Backend: BackendJSON, Path: root,
		SessionStorage: storageSettings{AutoRecover: boolPtr(true), StaleAfterSeconds: 300},
	})
	if err != nil {
		t.Fatalf("open after clean close = %v", err)
	}
	if err := clean.Close(); err != nil {
		t.Fatalf("close clean holder: %v", err)
	}
}
