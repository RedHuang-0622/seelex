package config

import (
	"fmt"
	"strings"

	toolspermission "github.com/RedHuang-0622/Seele/tools/permission"

	"github.com/RedHuang-0622/seelex/contract/dto"
)

func ParseFrontendMode(value string) (string, error) {
	mode := strings.ToLower(strings.TrimSpace(value))
	switch mode {
	case "tui", "gui", "headless", "backend":
		return mode, nil
	default:
		return "", fmt.Errorf("%q，允许值为 tui、gui、headless 或 backend", value)
	}
}

// ParsePermissionMode 解析 -permission 标志为**权限档位 id**（manual/edit/auto/
// full；兼容旧别名 full_access → full）。空/缺省 → manual。
//
// 档位取代旧的二元 manual/full_access：manual/edit/auto 是对 root 规则表的声明式
// 覆盖，full 是执行门短路放行（= 旧 full_access）。
func ParsePermissionMode(value string) (string, error) {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	if trimmed == "" {
		return dto.PermissionTierManual, nil
	}
	if trimmed == string(toolspermission.ModeFullAccess) {
		return dto.PermissionTierFull, nil
	}
	tier, err := dto.NormalizePermissionTier(trimmed)
	if err != nil {
		return "", fmt.Errorf("%q，允许值为 manual、edit、auto、full（旧别名 full_access）", value)
	}
	return tier, nil
}
