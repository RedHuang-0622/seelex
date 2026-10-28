package boot

import (
	"context"
	"fmt"
	"os"

	toolspermission "github.com/RedHuang-0622/Seele/tools/permission"
	"gopkg.in/yaml.v3"

	"github.com/RedHuang-0622/seelex/application"
	"github.com/RedHuang-0622/seelex/backend/limit/policy"
	toolreg "github.com/RedHuang-0622/seelex/backend/tool/registry"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/internal/bootseed"
	"github.com/RedHuang-0622/seelex/internal/host"
	"github.com/RedHuang-0622/seelex/internal/startup"
)

type permissionRuntime interface {
	SetPermissionConfig(toolspermission.PermissionConfig, toolspermission.ApprovalHandler)
	SetFullAccess(bool)
	SetPermissionTierFor(sessionID, tier string) error
}

// SetupPermissionGate 根据 -permission 标志安装权限门控与起始档位。
// 起始先装 manual 权责基线（分组 + 主体 + 缺位口径 + 默认规则），config/seele.yaml
// 的 permission 段按字段覆盖它（缺失/为空的字段保持默认，不再整体替换规则集）。
// 权限档位（manual/edit/auto/full）只作为进程级默认启用；运行期由 GUI/CLI 按会话切换。
func SetupPermissionGate(runtime permissionRuntime, approval application.ApprovalBroker, mode string) error {
	tier, err := startup.ParsePermissionMode(mode)
	if err != nil {
		return err
	}
	// 免打断的读命令白名单按**装配期解析出的实际壳**分派（2026-10-15 审计 D4）：
	// 白名单是按命令文本匹配的，而同一个字符串在 POSIX 壳与 PowerShell 下是两回事——
	// 写死 POSIX 形态会让 PowerShell 宿主上的命令"过闸即死"（放行 → 执行必失败）。
	cfg := policy.PermissionConfigForShell(toolreg.ResolveShellFact().Kind)
	fileCfg, err := LoadPermissionConfig(startup.EnsureConfigFile(bootseed.PermissionConfigName, bootseed.PermissionConfigPack()))
	if err != nil {
		return err
	}
	runtime.SetPermissionConfig(MergePermissionConfig(cfg, fileCfg), NewPermissionBridge(approval))
	if tier != "" && tier != dto.PermissionTierManual {
		if err := runtime.SetPermissionTierFor("", tier); err != nil {
			return err
		}
	}
	return nil
}

// MergePermissionConfig 把 seele.yaml 的 permission 段叠加到默认权责配置上：只覆盖
// 显式给出的字段，其余保持默认——分组表与主体授权表是产品口径（谁能用哪一族工具），
// 不该因为一份只写了规则的旧配置就整块消失（那会让每个主体都变成"无授权"）。
func MergePermissionConfig(base, override toolspermission.PermissionConfig) toolspermission.PermissionConfig {
	merged := base
	if len(override.Rules) > 0 {
		merged.Rules = override.Rules
	}
	if len(override.Groups) > 0 {
		merged.Groups = override.Groups
	}
	if len(override.Subjects) > 0 {
		merged.Subjects = override.Subjects
	}
	if override.MissingBit != "" {
		merged.MissingBit = override.MissingBit
	}
	if override.Mode != "" {
		merged.Mode = override.Mode
	}
	return merged
}

// DefaultManualRules 是 manual 模式的默认白名单（seele.yaml 未配置规则时回退）。
func DefaultManualRules() []toolspermission.PermissionRule {
	return []toolspermission.PermissionRule{
		{ToolName: "grep_search", Action: toolspermission.ActionAllow},
		{ToolName: "read_file", Action: toolspermission.ActionAllow},
		{ToolName: "glob", Action: toolspermission.ActionAllow},
		{ToolName: "git_status", Action: toolspermission.ActionAllow},
		{ToolName: "git_log", Action: toolspermission.ActionAllow},
		{ToolName: "git_diff", Action: toolspermission.ActionAllow},
		{ToolName: "get_time", Action: toolspermission.ActionAllow},
		{ToolName: "mcp_load", Action: toolspermission.ActionAllow},
		{ToolName: "switch_plugin", Action: toolspermission.ActionAllow},
		{ToolName: "switch_mode", Action: toolspermission.ActionAllow},
		{ToolName: "ask_approve", Action: toolspermission.ActionAllow},
		{ToolName: "todolist_init", Action: toolspermission.ActionAllow},
		{ToolName: "todolist_add", Action: toolspermission.ActionAllow},
		{ToolName: "todolist_done", Action: toolspermission.ActionAllow},
		{ToolName: "todolist_status", Action: toolspermission.ActionAllow},
		{ToolName: "todo_init", Action: toolspermission.ActionAllow},
		{ToolName: "todo_add", Action: toolspermission.ActionAllow},
		{ToolName: "todo_done", Action: toolspermission.ActionAllow},
		{ToolName: "todo_status", Action: toolspermission.ActionAllow},
		{ToolName: "task_add", Action: toolspermission.ActionAllow},
		{ToolName: "taskadd", Action: toolspermission.ActionAllow},
		{ToolName: "task_complete", Action: toolspermission.ActionAllow},
		{ToolName: "task_failed", Action: toolspermission.ActionAllow},
		{ToolName: "task_needs_user_decision", Action: toolspermission.ActionAllow},
	}
}

// LoadPermissionConfig 读取 seele.yaml 的 permission 段（权限专用文件）：rules /
// groups / subjects / missing_bit / mode。文件缺失或 permission 段缺失 → 零值配置
// （MergePermissionConfig 保持默认）；解析失败显式报错。
func LoadPermissionConfig(path string) (toolspermission.PermissionConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return toolspermission.PermissionConfig{}, nil
		}
		return toolspermission.PermissionConfig{}, fmt.Errorf("permission: read config: %w", err)
	}
	var file struct {
		Permission toolspermission.PermissionConfig `yaml:"permission"`
	}
	if err := yaml.Unmarshal(data, &file); err != nil {
		return toolspermission.PermissionConfig{}, fmt.Errorf("permission: parse config: %w", err)
	}
	return file.Permission, nil
}

// NewPermissionBridge 创建连接 permission.ApprovalHandler → ApprovalBroker 的桥接器。
// 每次工具触发审批时，阻塞等待用户在 TUI 交互面板中作出选择。
func NewPermissionBridge(broker application.ApprovalBroker) toolspermission.ApprovalHandler {
	return func(ctx *toolspermission.ApprovalContext) (*toolspermission.ApprovalResponse, error) {
		req := ctx.Request
		appReq := application.ApprovalRequest{
			ID: req.ID,
			// 波 4 approval 会话级归属：权限审批的会话 ID 由 seelebridge
			// 权限中间件随调度 ctx 填充（registry_state.go）。
			SessionID:         req.SessionID,
			Question:          req.Preview,
			Options:           host.ConvertPermissionOptions(req.Options),
			Risk:              req.Risk,
			ToolName:          req.ToolName,
			Preview:           req.Preview,
			Timeout:           req.Timeout,
			PermissionRequest: true,
		}
		decision, err := broker.Request(context.Background(), appReq)
		if err != nil {
			return nil, err
		}
		remember := decision.OptionID == "always"
		return &toolspermission.ApprovalResponse{
			RequestID: req.ID,
			Choice:    decision.OptionID,
			Remember:  remember,
		}, nil
	}
}
