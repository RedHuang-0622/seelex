package core

// 运行期 CLI/TUI 切档（P1-2）：main.go 的装配注释早就写着"运行期由 GUI/CLI 按会话
// 切换"，但命令注册表里一直没有 /permission（只有 /effort），于是 headless/TUI 用户
// 无法在运行期改档位。本文件钉住：命令存在、带描述、无参回显当前档 + 目录、带参走
// SetPermissionTier 单一路径（含写入侧校验与大小写宽容）。

import (
	"context"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

func permissionCommandFor(t *testing.T, service *Service) interface {
	Description() string
	Execute(context.Context, []string) (CommandResult, error)
} {
	t.Helper()
	command, ok := service.commands.Get("permission")
	if !ok {
		t.Fatal("/permission 未注册（CLI/TUI 运行期无法切档）")
	}
	if strings.TrimSpace(command.Description()) == "" {
		t.Fatal("/permission 缺少描述（不会出现在 /help 里）")
	}
	return command
}

// TestPermissionCommandRegisteredInHelp：命令在册，且 /help 能列出来。
func TestPermissionCommandRegisteredInHelp(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	permissionCommandFor(t, service)

	help, ok := service.commands.Get("help")
	if !ok {
		t.Fatal("/help 未注册")
	}
	result, err := help.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("/help: %v", err)
	}
	if !strings.Contains(result.Notice, "/permission") {
		t.Fatalf("/help 未列出 /permission：%q", result.Notice)
	}
}

// TestPermissionCommandWithoutArgsShowsCurrentAndCatalog：无参回显当前档位 + 档位目录
// （用户不知道有哪些档位时，不该只给一句"参数错误"）。
func TestPermissionCommandWithoutArgsShowsCurrentAndCatalog(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	command := permissionCommandFor(t, service)

	result, err := command.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("/permission（无参）: %v", err)
	}
	if !strings.Contains(result.Notice, dto.PermissionTierManual) {
		t.Fatalf("无参提示未回显当前档位：%q", result.Notice)
	}
	for _, tier := range dto.PermissionTierIDs() {
		if !strings.Contains(result.Notice, tier) {
			t.Fatalf("无参提示缺少档位 %q：%q", tier, result.Notice)
		}
	}
}

// TestPermissionCommandSwitchesTier：带参切档走 SetPermissionTier（会话槽 + 快照），
// 未识别档位显式失败且**不改**档位。
func TestPermissionCommandSwitchesTier(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	command := permissionCommandFor(t, service)
	ctx := context.Background()

	result, err := command.Execute(ctx, []string{"full"})
	if err != nil {
		t.Fatalf("/permission full: %v", err)
	}
	if !strings.Contains(result.Notice, dto.PermissionTierFull) {
		t.Fatalf("切档提示未回显生效档位：%q", result.Notice)
	}
	if got := service.Snapshot().Runtime.PermissionTier; got != dto.PermissionTierFull {
		t.Fatalf("快照档位 = %q, want full", got)
	}
	viewSessionID := service.currentViewSessionID()
	if got := service.permissionTierForSession(viewSessionID); got != dto.PermissionTierFull {
		t.Fatalf("会话槽档位 = %q, want full", got)
	}

	// 大小写/空白宽容（终端输入常常不规整）。
	if _, err := command.Execute(ctx, []string{"AUTO"}); err != nil {
		t.Fatalf("/permission AUTO: %v", err)
	}
	if got := service.permissionTierForSession(viewSessionID); got != dto.PermissionTierAuto {
		t.Fatalf("会话槽档位 = %q, want auto", got)
	}

	// 未识别档位：显式失败，档位保持不变。
	if _, err := command.Execute(ctx, []string{"sudo"}); err == nil {
		t.Fatal("/permission sudo 必须显式失败")
	}
	if got := service.permissionTierForSession(viewSessionID); got != dto.PermissionTierAuto {
		t.Fatalf("非法档位不得改变会话档位：%q", got)
	}
	if got := service.Snapshot().Runtime.PermissionTier; got != dto.PermissionTierAuto {
		t.Fatalf("非法档位不得改变快照档位：%q", got)
	}
}
