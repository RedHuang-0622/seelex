//go:build windows

package security

import (
	"os/exec"
	"testing"

	"github.com/RedHuang-0622/seelex/internal/winhide"
)

// 生产顺序就是 scopedBash / startAsync 的顺序（winhide.Apply 后 ConfigureHiddenCommand）。
// 旧形状：后两者各自整体重写 SysProcAttr，把 CREATE_NO_WINDOW 抹掉——本用例钉这个。
func TestSysProcAttrWritersMerge(t *testing.T) {
	cmd := exec.Command("cmd.exe", "/c", "exit", "0")
	winhide.Apply(cmd)
	ConfigureHiddenCommand(cmd)

	attr := cmd.SysProcAttr
	if attr == nil || !attr.HideWindow {
		t.Fatal("HideWindow 必须保留")
	}
	if attr.CreationFlags&winhide.CreateNoWindow == 0 {
		t.Fatalf("CREATE_NO_WINDOW 被覆盖丢失: CreationFlags=%#x", attr.CreationFlags)
	}
}

func TestProcessTreeFlagsSurviveRepeatedConfigure(t *testing.T) {
	cmd := exec.Command("cmd.exe", "/c", "exit", "0")
	ConfigureProcessTree(cmd)
	ConfigureProcessTree(cmd)
	ConfigureHiddenCommand(cmd)
	winhide.Apply(cmd)
	ConfigureHiddenCommand(cmd)

	attr := cmd.SysProcAttr
	if attr == nil || !attr.HideWindow {
		t.Fatal("HideWindow 必须保留")
	}
	if attr.CreationFlags&createNewProcessGroup == 0 {
		t.Fatalf("CREATE_NEW_PROCESS_GROUP 被覆盖丢失: CreationFlags=%#x", attr.CreationFlags)
	}
	if attr.CreationFlags&winhide.CreateNoWindow == 0 {
		t.Fatalf("CREATE_NO_WINDOW 被覆盖丢失: CreationFlags=%#x", attr.CreationFlags)
	}
}

// 本机必须能建出 Job：否则 job_manage(op=kill) 只剩"杀直接子进程"的退化能力，
// 这条一旦变红就该重新审视"进程树已终止"的所有断言。
func TestNewProcessTreeNotDegraded(t *testing.T) {
	tree := NewProcessTree()
	defer tree.Close()
	if tree.Degraded() {
		t.Fatal("Job Object 建不出来：进程树终止已退化成只杀直接子进程")
	}
}
