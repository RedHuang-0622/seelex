//go:build windows

package security

import (
	"os/exec"
	"testing"
)

// process_tree_attach_test.go — U5 残②：「挂不上」有两种形态，退化判据必须都说真话。
//
//	① Job 建不出来（`NewProcessTree` 失败 ⇒ `job == 0`）——本机建得出来，走不到那一步；
//	② Job 建出来了、但这个进程**没挂进去**（`OpenProcess` / `AssignProcessToJobObject` 失败）。
//
// 第二种过去不进判据：树照样报 `Degraded() == false`，调用方于是可以拿"整棵树已终止"去主张一个
// **没有保证**的事实——Job 终止根本打不到那个不在 Job 里的进程，被漏掉的是它的孙进程。
// 判据的形态不对（"Job 建出来了吗" ≠ "这棵树管得住这些进程吗"），这就是要修的那一处。
//
// 这里用"打不开的 PID"构造形态 ②：失败原因不止一种（进程已退、权限不足、已经在别的 Job 里
// 不让嵌套），但它们对调用方是同一件事——那个进程没进这棵树。判据判的是**这件事**，不是原因。

// unreachablePID 是一个打不开的进程号（Windows 的 PID 是 4 的倍数且远小于这个数）。
const unreachablePID = 0x7FFFFFF0

func TestAttachFailureMarksTreeDegraded(t *testing.T) {
	tree := NewProcessTree()
	defer tree.Close()
	if tree.Degraded() {
		t.Skip("本机建不出 Job Object：形态 ② 没法在这里构造（形态 ① 由 TestNewProcessTreeNotDegraded 守）")
	}

	err := tree.Attach(unreachablePID)
	if err == nil {
		t.Skipf("本机竟然打得开 PID %d（异常环境），换一个不可达 PID 再跑", unreachablePID)
	}
	if !tree.Degraded() {
		t.Fatalf("进程没挂进 Job，树却不是退化态（err=%v）：终止只及直接子进程这件事被瞒下了", err)
	}
}

// TestAttachSuccessKeepsTreeHealthy 是阴性对照：**挂得上就不许报退化**——否则这条读数变成噪声，
// 调用方再也不会看它。
func TestAttachSuccessKeepsTreeHealthy(t *testing.T) {
	tree := NewProcessTree()
	defer tree.Close()
	if tree.Degraded() {
		t.Skip("本机建不出 Job Object")
	}

	cmd := exec.Command("cmd.exe", "/c", "ping", "-n", "6", "127.0.0.1")
	ConfigureProcessTree(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatalf("起一个活进程做对照：%v", err)
	}
	defer func() {
		// 收尾走整棵树终止：既收干净进程，也顺带证一遍"挂上了就能整树终止"。
		_ = tree.Terminate()
		_, _ = cmd.Process.Wait()
	}()

	if err := tree.Attach(cmd.Process.Pid); err != nil {
		t.Fatalf("活进程必须能挂进 Job：%v", err)
	}
	if tree.Degraded() {
		t.Fatal("挂上了却报退化：读数一旦会撒谎，调用方就不再信它")
	}
}
