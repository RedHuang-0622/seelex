//go:build redprobe && windows

// H7 归因探针：栈 head 发布 rename 失败的那一刻，目标文件是否被某个句柄占住、
// 占住多久。用「以 share=0 独占打开」探测，不写不改目标，因此不破坏会话状态。
//
//	go test -tags redprobe ./sessionstore -run TestProbeStackHeadPublishHandle -count=1 -v
//
// 判读：若失败后几毫秒内独占打开即成功 → 持柄者是瞬时外部扫描（有界退避重试可
// 消除用户可见错误）；若长时间无法独占打开 → 存在长寿命句柄，需查我方读者或换
// 发布介质。注意：提交级重试不安全（history 会重复追加），重试只能落在 rename
// 本身（同一 tmp、同一字节）。
package sessionstore

import (
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// probeExclusiveOpen 以 share mode 0 打开文件；返回 nil 表示此刻无人持柄。
func probeExclusiveOpen(path string) error {
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	handle, err := syscall.CreateFile(name, syscall.GENERIC_READ, 0, nil,
		syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return err
	}
	return syscall.CloseHandle(handle)
}

func TestProbeStackHeadPublishHandle(t *testing.T) {
	harness := newJSONStackHarness(t)
	harness.seedMessages(1)
	kinds := []StackKind{StackKindPlan, StackKindTask, StackKindGoal}

	var mu sync.Mutex
	var failures, probed int
	var releaseSpent []time.Duration
	stop := make(chan struct{})
	var readers sync.WaitGroup
	for range 2 {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				for _, kind := range kinds {
					if _, err := stackReadActive(harness.journal, harness.key, kind); err != nil {
						t.Errorf("reader: %v", err)
						return
					}
				}
				time.Sleep(100 * time.Microsecond)
			}
		}()
	}
	var writers sync.WaitGroup
	for _, kind := range kinds {
		writers.Add(1)
		go func(kind StackKind) {
			defer writers.Done()
			headPath := filepath.Join(harness.store.sessionRoot(harness.key), "metadata",
				string(moduleForStackKind(kind))+".json")
			for step := 0; step < 40; step++ {
				itemID := string(kind) + "-p-" + strconv.Itoa(step)
				if _, err := harness.push(kind, itemID, StackItemInput{ItemID: itemID}); err != nil {
					t.Errorf("push: %v", err)
					return
				}
				_, err := harness.popTop(kind, itemID, "completed")
				if err == nil {
					continue
				}
				if !strings.Contains(err.Error(), "Access is denied") {
					t.Errorf("pop: %v", err)
					return
				}
				mu.Lock()
				failures++
				mu.Unlock()
				// 失败现场：谁还持着这个文件、多久松开。
				begin := time.Now()
				for wait := 0; wait < 500; wait++ {
					if probeExclusiveOpen(headPath) == nil {
						mu.Lock()
						probed++
						releaseSpent = append(releaseSpent, time.Since(begin))
						mu.Unlock()
						break
					}
					time.Sleep(time.Millisecond)
				}
				// 发布失败后 head 仍是上一个已发布版本：状态必须自洽。
				if _, err := stackReadActive(harness.journal, harness.key, kind); err != nil {
					t.Errorf("active after publish failure: %v", err)
					return
				}
			}
		}(kind)
	}
	writers.Wait()
	close(stop)
	readers.Wait()

	t.Logf("栈提交 3 kind × 40 轮：head 发布失败 %d 次，其中 %d 次探到持柄者松开", failures, probed)
	for index, spent := range releaseSpent {
		t.Logf("  第 %d 次失败后松开耗时 %v", index+1, spent)
	}
	if failures == 0 {
		t.Skip("本轮负载未触发发布失败（H7 是概率性），无持柄样本")
	}
	if probed < failures {
		t.Fatalf("%d 次失败中有 %d 次在 500ms 内始终无法独占打开：存在长寿命句柄", failures, failures-probed)
	}
}
