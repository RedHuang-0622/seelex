package sessionstore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestJSONDataRootLockMultiProcessTwoArms 钉住 limits.runtime.allow_multi_process
// 落成数据根锁行为的两臂：
//   - 默认（nil/false）：另一个存活进程持锁时 Open 报 ErrDataRootLocked（单实例）；
//   - allow_multi_process=true：同一条链不再被拒绝，Open 成功；且本进程**不接管**
//     别人的锁记录——只放行，不夺锁、不删锁（数据根锁退化为诊断信息）。
func TestJSONDataRootLockMultiProcessTwoArms(t *testing.T) {
	parentPID := os.Getppid()
	if parentPID <= 0 || !processAlive(parentPID) {
		t.Skip("no alive parent process to simulate a foreign holder")
	}
	root := t.TempDir()
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(root, dataRootLockFile)
	record := newOwnerRecord(root)
	record.PID = parentPID
	record.OwnerToken = "foreign-process-token"
	record.RenewedAt = time.Now().UTC()
	if err := writeOwnerRecord(lockPath, record); err != nil {
		t.Fatal(err)
	}

	// 臂一：默认单实例——被拒绝。
	if _, err := Open(context.Background(), Config{Backend: BackendJSON, Path: root}); !errors.Is(err, ErrDataRootLocked) {
		t.Fatalf("default open under foreign live lock err = %v, want ErrDataRootLocked", err)
	}

	// 臂二：允许多进程——放行。
	allow := true
	repository, err := Open(context.Background(), Config{
		Backend: BackendJSON, Path: root,
		SessionStorage: storageSettings{AllowMultiProcess: &allow},
	})
	if err != nil {
		t.Fatalf("allow_multi_process=true open under foreign live lock = %v, want nil", err)
	}
	if err := repository.Close(); err != nil {
		t.Fatalf("close multi-process holder: %v", err)
	}
	// 放行不改写、不删除别人的锁记录。
	owner, err := readOwnerRecord(lockPath)
	if err != nil {
		t.Fatalf("foreign lock record must survive multi-process open: %v", err)
	}
	if owner.OwnerToken != record.OwnerToken || owner.PID != parentPID {
		t.Fatalf("multi-process open must not take over the foreign lock: got token=%q pid=%d", owner.OwnerToken, owner.PID)
	}
}

// TestJSONDataRootLockMultiProcessStaleBypass：allow_multi_process=true 时陈旧锁
// 也不再拦启动（放行语义对活跃/陈旧一视同仁），且不改写别人的陈旧记录。
func TestJSONDataRootLockMultiProcessStaleBypass(t *testing.T) {
	root := t.TempDir()
	record := newOwnerRecord(root)
	record.PID = 1 << 30
	record.OwnerToken = "stale-token"
	record.RenewedAt = time.Now().UTC().Add(-2 * time.Hour)
	lockPath := filepath.Join(root, dataRootLockFile)
	if err := writeOwnerRecord(lockPath, record); err != nil {
		t.Fatal(err)
	}

	allow := true
	repository, err := Open(context.Background(), Config{
		Backend: BackendJSON, Path: root,
		SessionStorage: storageSettings{AllowMultiProcess: &allow},
	})
	if err != nil {
		t.Fatalf("allow_multi_process=true open under stale lock = %v, want nil", err)
	}
	if err := repository.Close(); err != nil {
		t.Fatalf("close multi-process holder: %v", err)
	}
	owner, err := readOwnerRecord(lockPath)
	if err != nil {
		t.Fatalf("stale lock record must survive multi-process open: %v", err)
	}
	if owner.OwnerToken != record.OwnerToken {
		t.Fatalf("multi-process open must not take over the stale lock: got token=%q", owner.OwnerToken)
	}
}
