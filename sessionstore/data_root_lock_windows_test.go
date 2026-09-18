//go:build windows

package sessionstore

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// startTerminatedProcess 返回一个「已经退出、但 PID 仍被占用」的进程 pid。
//
// 关键在调用方握着句柄不放（不 Wait、不 Release）：Windows 在还有句柄引用时不会
// 回收进程对象与 PID，于是 OpenProcess 对死进程依然成功——这正是被强杀的 dev GUI
// 留在 lock.owner 里的 pid 形状。
func startTerminatedProcess(t *testing.T) (int, func()) {
	t.Helper()
	cmd := exec.Command("cmd", "/c", "exit", "0")
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot start helper process: %v", err)
	}
	pid := cmd.Process.Pid
	deadline := time.Now().Add(15 * time.Second)
	for {
		handle, err := syscall.OpenProcess(syscall.SYNCHRONIZE, false, uint32(pid))
		if err != nil {
			t.Fatalf("OpenProcess(%d): %v", pid, err)
		}
		status, _ := syscall.WaitForSingleObject(handle, 0)
		syscall.CloseHandle(handle)
		if status == syscall.WAIT_OBJECT_0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("helper process %d did not terminate in time", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
	return pid, func() { _ = cmd.Process.Release() }
}

// TestJSONDataRootTerminatedProcessLockIsStale 钉住 2026-09-18 事故：dev GUI 被强杀
// 留下 lock.owner 后，下一次启动必须能接管陈旧锁，而不是报「已被另一个进程占用」。
//
// 与 TestJSONDataRootCrashedGUIResidualLockRecovered 的差别在 pid 形状：那条用不可达
// pid（OpenProcess 直接失败），这条用「已退出但进程对象仍被句柄引用」的 pid——
// OpenProcess 依然成功，只有问内核「终结了没」（WaitForSingleObject）才分得清。
// 修复前 processAlive 把这种死进程当活持有者，于是 Open 报 ErrDataRootLocked，
// GUI 因为数据根初始化失败而启动即退出（-H windowsgui 下看起来就是「打不开」）。
func TestJSONDataRootTerminatedProcessLockIsStale(t *testing.T) {
	pid, cleanup := startTerminatedProcess(t)
	defer cleanup()

	if processAlive(pid) {
		t.Fatalf("processAlive(%d) = true, want false: 进程已终结、只是句柄未释放，不是活着的持有者", pid)
	}

	root := t.TempDir()
	dead := newOwnerRecord(root)
	dead.PID = pid
	dead.OwnerToken = "hard-killed-gui-token"
	dead.Program = "seelex-gui.exe"
	// 心跳必须"很新"：陈旧判定只许靠进程真的没了，不许靠超龄兜底。
	dead.RenewedAt = time.Now().UTC().Add(-5 * time.Second)
	lockPath := filepath.Join(root, dataRootLockFile)
	if err := writeOwnerRecord(lockPath, dead); err != nil {
		t.Fatal(err)
	}

	// 1) 保守口径（auto_recover=false）必须报"陈旧锁"，而不是"被别人占用"：
	//    前者给出可操作提示，后者会让用户以为另一个 seelex 还在跑。
	ctx := context.Background()
	if _, err := Open(ctx, Config{Backend: BackendJSON, Path: root}); !errors.Is(err, ErrDataRootStaleLock) {
		t.Fatalf("conservative open err = %v, want ErrDataRootStaleLock", err)
	}

	// 2) 默认口径（config 写死 lock_auto_recover=true）直接接管陈旧锁并打开。
	repository, err := Open(ctx, Config{
		Backend: BackendJSON, Path: root,
		SessionStorage: storageSettings{AutoRecover: boolPtr(true), StaleAfterSeconds: 300},
	})
	if err != nil {
		t.Fatalf("open after hard kill (auto_recover=true) = %v, want takeover", err)
	}
	owner, err := readOwnerRecord(lockPath)
	if err != nil {
		t.Fatalf("read recovered lock record: %v", err)
	}
	if owner.PID != os.Getpid() || owner.OwnerToken == dead.OwnerToken {
		t.Fatalf("takeover must rewrite the lock record to the current process, got pid=%d token=%s",
			owner.PID, owner.OwnerToken)
	}

	// 3) 干净关闭归还锁：下一次启动不再走接管路径。
	if err := repository.Close(); err != nil {
		t.Fatalf("close recovered holder: %v", err)
	}
	if _, err := os.Stat(lockPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("lock.owner must be removed on clean Close (err=%v)", err)
	}
}
