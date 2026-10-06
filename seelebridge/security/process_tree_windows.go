//go:build windows

package security

import (
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"unsafe"
)

// 进程树终止（Windows）：把执行体放进一个 Job Object，终止 = 结束整个 Job。
//
// 为什么不是 taskkill /T：taskkill 的 /T 靠 Windows 侧的"父 PID"枚举子进程，而
// MSYS2/Git Bash 的 fork 出来的子 shell 不一定挂在直接父进程下（cygwin 层自己维护
// pid 链）。实测 `bash -c "(sleep 0.5; echo GRANDCHILD) & sleep 25"` 在 150ms 被
// taskkill /T 杀掉后，GRANDCHILD 仍然落进日志——只杀到了 bash。Job Object 不看父子
// 关系，只看"谁在这个 job 里"，后来 fork 出来的孙进程也跑不掉。
//
// 只用 std syscall + kernel32 惰性绑定，不为此把 golang.org/x/sys 提成直接依赖。

const (
	jobObjectExtendedLimitInformation = 9
	jobObjectLimitKillOnJobClose      = 0x00002000
	processSetQuota                   = 0x00000100
	processTerminateAccess            = 0x00000001
	createNewProcessGroup             = 0x00000200 // CREATE_NEW_PROCESS_GROUP
	processTreeKillExitCode           = 137
)

var (
	modkernel32                  = syscall.NewLazyDLL("kernel32.dll")
	procCreateJobObjectW         = modkernel32.NewProc("CreateJobObjectW")
	procSetInformationJobObject  = modkernel32.NewProc("SetInformationJobObject")
	procAssignProcessToJobObject = modkernel32.NewProc("AssignProcessToJobObject")
	procTerminateJobObject       = modkernel32.NewProc("TerminateJobObject")

	errNoProcessTree  = errors.New("security: 进程树未绑定任何进程")
	errJobUnavailable = errors.New("security: 无法创建 Job Object")
	jobLimitsSize     = unsafe.Sizeof(jobExtendedLimitInformation{})
)

// jobExtendedLimitInformation 的内存布局必须与 Windows SDK 一致：字段顺序与类型
// 逐位对齐（amd64 上 Go 的自然对齐与 C 相同）。
type jobObjectBasicLimitInformation struct {
	perProcessUserTimeLimit int64
	perJobUserTimeLimit     int64
	limitFlags              uint32
	minimumWorkingSetSize   uintptr
	maximumWorkingSetSize   uintptr
	activeProcessLimit      uint32
	affinity                uintptr
	priorityClass           uint32
	schedulingClass         uint32
}

type jobIOCounters struct {
	readOperationCount  uint64
	writeOperationCount uint64
	otherOperationCount uint64
	readTransferCount   uint64
	writeTransferCount  uint64
	otherTransferCount  uint64
}

type jobExtendedLimitInformation struct {
	basic                 jobObjectBasicLimitInformation
	ioInfo                jobIOCounters
	processMemoryLimit    uintptr
	jobMemoryLimit        uintptr
	peakProcessMemoryUsed uintptr
	peakJobMemoryUsed     uintptr
}

// ConfigureProcessTree 让子进程自成进程组。与 Job 无关，作用是别让控制台的
// Ctrl+C 一类信号顺带打到后台命令上——它的生死只有 job_manage(op=kill) 与存活上限说了算。
//
// 只补自己那一位：SysProcAttr 有多个写者（winhide.Apply、ConfigureHiddenCommand），
// 任何一处整体赋值都会把别人的 flag 抹掉。
func ConfigureProcessTree(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= createNewProcessGroup
}

// ProcessTree 是一次执行体的可终止域。
//
// job != 0 时以 Job 为准（覆盖整棵树）；Job 建不出来、或进程**没挂进 Job** 时退化为按 PID 的
// taskkill /T，那种退化只杀得到直接子进程——调用方不得据此主张"整棵树已终止"。两种形态
// 都由 Degraded() 说得清（U5 残②：过去只认第一种，"挂不上"被瞒下）。
type ProcessTree struct {
	mu     sync.Mutex
	job    syscall.Handle
	pid    int
	closed bool
	// attachFailed 记"这个进程没进这棵树所属的 Job"（OpenProcess 打不开 / 分配进 Job 失败）。
	// 它和 job == 0 是同一件事的两种形态：终止打不到没进去的进程，被漏掉的是它的孙进程。
	attachFailed bool
}

// NewProcessTree 建 Job 并设 KILL_ON_JOB_CLOSE：句柄一旦关闭，Job 里剩下的进程全部
// 由系统带走——这是"收尾不留漏网进程"的确定点，不靠计时器猜。
//
// 建不出来（受限环境/组策略）不当失败处理：退化成按 PID 的 taskkill /T，那只能保证
// 直接子进程，由 Degraded() 说得清。调用方没有可做的补救，所以不返回 error。
func NewProcessTree() *ProcessTree {
	handle, _, _ := procCreateJobObjectW.Call(0, 0)
	if handle == 0 {
		return &ProcessTree{}
	}
	limits := jobExtendedLimitInformation{}
	limits.basic.limitFlags = jobObjectLimitKillOnJobClose
	if set, _, _ := procSetInformationJobObject.Call(
		handle, jobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&limits)), uintptr(jobLimitsSize)); set == 0 {
		_ = syscall.CloseHandle(syscall.Handle(handle))
		return &ProcessTree{}
	}
	return &ProcessTree{job: syscall.Handle(handle)}
}

// Degraded 报告这棵树是否只能按 PID 杀。true 时不得对外主张"整棵进程树已终止"。
//
// 两种形态都算退化：① Job 没建成（job == 0）；② Job 建成了但**这个进程没挂进去**
// （attachFailed）——后者过去不算，于是"挂不上"这件事对调用方不可见（U5 残②）。
func (t *ProcessTree) Degraded() bool {
	if t == nil {
		return true
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.job == 0 || t.attachFailed
}

// markAttachFailed 记下"这个进程没进 Job"。只有真的调用失败才置位：参数非法一类由
// Attach 的入参检查挡在前面，不进退化判据（那是调用方的 bug，不是环境退化）。
func (t *ProcessTree) markAttachFailed() {
	t.mu.Lock()
	t.attachFailed = true
	t.mu.Unlock()
}

// Attach 把已启动的进程纳入 Job。失败不致命：退化成按 PID 杀，但**要记进退化判据**
// （Degraded 会说 true）——"整棵树已终止"这句话从此不成立。
func (t *ProcessTree) Attach(pid int) error {
	if t == nil || pid <= 0 {
		return errNoProcessTree
	}
	t.mu.Lock()
	t.pid = pid
	job := t.job
	t.mu.Unlock()
	if job == 0 {
		return nil
	}
	proc, err := syscall.OpenProcess(processSetQuota|processTerminateAccess, false, uint32(pid))
	if err != nil {
		t.markAttachFailed()
		return fmt.Errorf("OpenProcess(%d): %w", pid, err)
	}
	defer syscall.CloseHandle(proc)
	if assigned, _, callErr := procAssignProcessToJobObject.Call(uintptr(job), uintptr(proc)); assigned == 0 {
		t.markAttachFailed()
		return fmt.Errorf("AssignProcessToJobObject(%d): %w", pid, callErr)
	}
	return nil
}

// Terminate 结束整棵树。返回错误 = 没能确认终止，调用方不得谎报已杀。
func (t *ProcessTree) Terminate() error {
	if t == nil {
		return errNoProcessTree
	}
	t.mu.Lock()
	job, pid := t.job, t.pid
	t.mu.Unlock()
	if pid <= 0 {
		return errNoProcessTree
	}
	if job != 0 {
		if done, _, callErr := procTerminateJobObject.Call(uintptr(job), processTreeKillExitCode); done != 0 {
			return nil
		} else if fallbackErr := killByPIDTree(pid); fallbackErr != nil {
			return fmt.Errorf("TerminateJobObject: %v; 退化的 taskkill 也失败: %w", callErr, fallbackErr)
		} else {
			// Job 终止没成、退化路径杀了直接子进程：仍然如实报"没保证整棵树"。
			return fmt.Errorf("TerminateJobObject: %v（已退化只杀直接子进程）", callErr)
		}
	}
	return killByPIDTree(pid)
}

// Close 释放 Job 句柄；KILL_ON_JOB_CLOSE 会顺手带走任何还在里面的进程。
// 收尾必做：不关就是每个执行体漏一个 Job。
func (t *ProcessTree) Close() {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.job == 0 || t.closed {
		return
	}
	t.closed = true
	_ = syscall.CloseHandle(t.job)
	t.job = 0
}

// killByPIDTree 是退化路径：taskkill /T 只按 Windows 父 PID 枚举（杀不到 MSYS 的
// 孙进程链）。PID 以参数数组传入，绝不拼 shell 字符串。
func killByPIDTree(pid int) error {
	killer := exec.Command("taskkill", "/PID", strconv.Itoa(pid), "/T", "/F")
	out, err := killer.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
