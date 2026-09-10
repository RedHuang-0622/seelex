// 数据根独占锁（my_design §9）。
//
// 单数据根 = 单进程写者。JSON 后端在打开数据根时抢 `<root>/lock.owner`：
//   - O_CREATE|O_EXCL 建锁 → 写 owner_process 记录（pid / 程序 / 主机 / 取得
//     与续租时间 / owner_token）；
//   - 心跳按 `lock.stale_after_seconds/3`（夹在 10s–120s）续租 renewed_at；
//   - 锁被占用时：pid 不存在 **或** 距上次续租超过 stale_after_seconds 判为
//     陈旧（两条同时成立才算活着 → pid 复用不会误判成活跃持有者）；
//   - 默认 `auto_recover=false`：陈旧锁也只报错、由用户决定，不静默接管
//     （§9「默认提示拒绝」）；
//   - 同进程内重复打开同一数据根按引用计数共享（进程本来就是同一写者）。
//
// 设计稿的「pid + 启动时间戳」在这里落成 pid + owner_token + 续租年龄：
// 进程启动时间需要平台系统调用（Linux /proc、Windows GetProcessTimes），而
// 「必须持续续租才算活着」本来就封死了 pid 复用窗口，因此不引入 x/sys 依赖。
package sessionstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const dataRootLockFile = "lock.owner"

var (
	// ErrDataRootLocked 表示数据根已被另一个存活进程独占。
	ErrDataRootLocked = errors.New("session storage: data root is locked by another process")
	// ErrDataRootStaleLock 表示数据根存在陈旧锁；默认不自动接管
	// （session_storage.lock.auto_recover=false）。
	ErrDataRootStaleLock = errors.New("session storage: stale data root lock found (set session_storage.lock.auto_recover=true to take over)")
)

// ownerRecord 是 lock.owner 的内容（§9 owner_process）。
type ownerRecord struct {
	PID        int       `json:"pid"`
	OwnerToken string    `json:"owner_token"`
	Program    string    `json:"program"`
	Hostname   string    `json:"hostname"`
	DataRoot   string    `json:"data_root"`
	AcquiredAt time.Time `json:"acquired_at"`
	RenewedAt  time.Time `json:"renewed_at"`
}

// dataRootHolder 是本进程对某个数据根的持锁状态（引用计数 + 心跳）。
type dataRootHolder struct {
	path   string
	record ownerRecord
	refs   int
	stop   chan struct{}
	done   chan struct{}
}

var dataRootLocks = struct {
	mu      sync.Mutex
	holders map[string]*dataRootHolder
}{holders: make(map[string]*dataRootHolder)}

// acquireDataRootLock 抢占数据根写者身份。settings 提供 stale_after_seconds
// 与 auto_recover 两个开关。
func acquireDataRootLock(root string, settings storageSettings) error {
	path := filepath.Join(root, dataRootLockFile)
	staleAfter := time.Duration(settings.StaleAfterSeconds) * time.Second
	if staleAfter <= 0 {
		staleAfter = 300 * time.Second
	}
	key := filepath.Clean(root)

	dataRootLocks.mu.Lock()
	if holder := dataRootLocks.holders[key]; holder != nil {
		holder.refs++
		dataRootLocks.mu.Unlock()
		return nil
	}
	dataRootLocks.mu.Unlock()

	record, acquired, err := tryWriteOwnerRecord(path, key, staleAfter, boolValue(settings.AutoRecover, false))
	if err != nil {
		return err
	}
	if !acquired {
		// existingOwner 已返回带持有者描述的拒绝错误（存活锁/陈旧锁），
		// 这里不会再出现 acquired=false 且 err==nil 的分支。
		return ErrDataRootLocked
	}

	holder := &dataRootHolder{
		path: path, record: record, refs: 1,
		stop: make(chan struct{}), done: make(chan struct{}),
	}
	dataRootLocks.mu.Lock()
	if existing := dataRootLocks.holders[key]; existing != nil {
		// 并发下同进程已抢到：共享它，丢弃刚建立的持锁记录。
		existing.refs++
		dataRootLocks.mu.Unlock()
		return nil
	}
	dataRootLocks.holders[key] = holder
	dataRootLocks.mu.Unlock()
	go holder.renewLoop(timeOrHalf(staleAfter))
	return nil
}

func timeOrHalf(value time.Duration) time.Duration {
	interval := value / 3
	if interval < 10*time.Second {
		interval = 10 * time.Second
	}
	if interval > 2*time.Minute {
		interval = 2 * time.Minute
	}
	return interval
}

// releaseDataRootLock 归还持锁计数；归零时删除锁文件并停掉心跳。
func releaseDataRootLock(root string) {
	key := filepath.Clean(root)
	dataRootLocks.mu.Lock()
	holder := dataRootLocks.holders[key]
	if holder == nil {
		dataRootLocks.mu.Unlock()
		return
	}
	holder.refs--
	if holder.refs > 0 {
		dataRootLocks.mu.Unlock()
		return
	}
	delete(dataRootLocks.holders, key)
	dataRootLocks.mu.Unlock()
	close(holder.stop)
	<-holder.done
	_ = os.Remove(holder.path)
}

// DataRootLockedBy 返回该数据根当前的存活持有者（无人持有/未锁定时 ok=false）。
// GUI/TUI 用它把「另一个进程正在写同一数据根」渲染成可操作的提示。
func DataRootLockedBy(root string) (ownerRecord, bool) {
	return DataRootLockedByWith(root, 300*time.Second)
}

// DataRootLockedByWith 按调用方 stale 口径返回持有者信息。
func DataRootLockedByWith(root string, staleAfter time.Duration) (ownerRecord, bool) {
	if staleAfter <= 0 {
		staleAfter = 300 * time.Second
	}
	record, err := readOwnerRecord(filepath.Join(filepath.Clean(root), dataRootLockFile))
	if err != nil || processAlive(record.PID) == false {
		return ownerRecord{}, false
	}
	if time.Since(record.RenewedAt) > staleAfter {
		return ownerRecord{}, false
	}
	return record, true
}

// tryWriteOwnerRecord 以 O_EXCL 建锁。返回 acquired=false 表示锁文件已存在，
// 并带回读到的持有者记录（可能为空记录 = 无法解析）。
func tryWriteOwnerRecord(path, root string, staleAfter time.Duration, autoRecover bool) (ownerRecord, bool, error) {
	record := newOwnerRecord(root)
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return ownerRecord{}, false, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if errors.Is(err, os.ErrExist) {
		return existingOwner(path, record, staleAfter, autoRecover)
	}
	if err != nil {
		return ownerRecord{}, false, fmt.Errorf("session storage: create data root lock %q: %w", path, err)
	}
	if _, err := file.Write(append(data, '\n')); err != nil {
		file.Close()
		return ownerRecord{}, false, err
	}
	if err := file.Close(); err != nil {
		return ownerRecord{}, false, err
	}
	return record, true, nil
}

// existingOwner 判定既有锁：属于本进程则接管，陈旧按 auto_recover 决定，存活
// 则显式拒绝。
func existingOwner(path string, mine ownerRecord, staleAfter time.Duration, autoRecover bool) (ownerRecord, bool, error) {
	held, err := readOwnerRecord(path)
	if err != nil {
		// 锁文件损坏/为空：无人能证明持有 → 视为陈旧（不静默改写别人数据）。
		held = ownerRecord{}
	}
	if held.PID == os.Getpid() && held.OwnerToken == mine.OwnerToken {
		return mine, true, writeOwnerRecord(path, mine)
	}
	if held.PID == os.Getpid() {
		// 同进程另一实例留下的锁（例如上一份 repository 未 Close）：直接接管。
		return mine, true, writeOwnerRecord(path, mine)
	}
	if !ownerIsStale(held, staleAfter) {
		return held, false, lockError(ErrDataRootLocked, held)
	}
	if !autoRecover {
		return held, false, fmt.Errorf("%w: %s", ErrDataRootStaleLock, describeOwner(held))
	}
	return mine, true, writeOwnerRecord(path, mine)
}

func ownerIsStale(held ownerRecord, staleAfter time.Duration) bool {
	if held.PID == 0 || held.RenewedAt.IsZero() {
		return true
	}
	if !processAlive(held.PID) {
		return true
	}
	return time.Since(held.RenewedAt) > staleAfter
}

func lockError(base error, held ownerRecord) error {
	return fmt.Errorf("%w: %s", base, describeOwner(held))
}

func describeOwner(held ownerRecord) string {
	if held.PID == 0 {
		return "锁文件无法解析"
	}
	return fmt.Sprintf("pid=%d program=%s host=%s root=%s acquired=%s renewed=%s",
		held.PID, held.Program, held.Hostname, held.DataRoot,
		held.AcquiredAt.Format(time.RFC3339), held.RenewedAt.Format(time.RFC3339))
}

func newOwnerRecord(root string) ownerRecord {
	hostName, _ := os.Hostname()
	program, _ := os.Executable()
	now := time.Now().UTC()
	return ownerRecord{
		PID: os.Getpid(), OwnerToken: randomID(), Program: filepath.Base(program),
		Hostname: hostName, DataRoot: root, AcquiredAt: now, RenewedAt: now,
	}
}

func readOwnerRecord(path string) (ownerRecord, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ownerRecord{}, err
	}
	var record ownerRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return ownerRecord{}, err
	}
	return record, nil
}

func writeOwnerRecord(path string, record ownerRecord) error {
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(path, data, 0o600)
}

// renewLoop 持续续租：写失败（例如别的进程正持柄读锁文件）只延后一次重试，
// 绝不因为续租失败就放弃写者身份。
func (holder *dataRootHolder) renewLoop(interval time.Duration) {
	defer close(holder.done)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-holder.stop:
			return
		case now := <-ticker.C:
			record := holder.record
			record.RenewedAt = now.UTC()
			if err := writeOwnerRecord(holder.path, record); err == nil {
				holder.record = record
			}
		}
	}
}
