package core

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/application/core/input_router"
)

func (service *Service) registerBuiltinCommands() error {
	var registrationErr error
	register := func(name, description string, execute func(context.Context, []string) (CommandResult, error)) {
		if registrationErr != nil {
			return
		}
		if err := service.commands.Register(input_router.NewCommandFunc(name, description, execute)); err != nil {
			registrationErr = fmt.Errorf("register command %q: %w", name, err)
		}
	}
	register("help", "显示帮助信息", func(context.Context, []string) (CommandResult, error) {
		var builder strings.Builder
		builder.WriteString("可用命令:\n")
		for _, command := range service.commands.All() {
			fmt.Fprintf(&builder, "  /%-12s  %s\n", command.Name(), command.Description())
		}
		builder.WriteString(fmt.Sprintf("\n提示: %s=命令  %s=切换插件  %s=召回 Skill  %s=手动召唤团队",
			SigilCommand, SigilPlugin, SigilSkill, SigilTeam))
		// 面板口径与提交口径必须一致：`/` 只列"能从输入框直接提交"的入口
		// （命令 + Skill）。工具由模型调用、经权限门，不在这里出现——一个能力
		// 要让用户打 /名字 显式调用，前提是它已注册成命令。
		builder.WriteString(fmt.Sprintf(
			"\n说明: %s 面板只列可直接提交的入口（命令与 Skill；Skill 也可用 %s<name>）。"+
				"工具由模型调用、不在此列出：需要 %s名字 显式调用时，先把该能力注册成命令。",
			SigilCommand, SigilSkill, SigilCommand))
		return CommandResult{Notice: builder.String()}, nil
	})
	register("clear", "清空对话历史", func(context.Context, []string) (CommandResult, error) {
		service.Deps.Engine.ClearHistory()
		service.resetConversation("已清空")
		return CommandResult{}, nil
	})
	register("model", "显示当前模型和 Provider", func(context.Context, []string) (CommandResult, error) {
		return CommandResult{Notice: fmt.Sprintf("Model: %s  Provider: %s", service.Deps.Runtime.Model(), service.Deps.Runtime.Provider())}, nil
	})
	register("history", "显示历史消息统计", func(context.Context, []string) (CommandResult, error) {
		history := service.Deps.Engine.History()
		if len(history) == 0 {
			return CommandResult{Notice: "历史为空"}, nil
		}
		roles := make(map[string]int)
		for _, message := range history {
			roles[message.Role]++
		}
		parts := make([]string, 0, len(roles))
		for role, count := range roles {
			parts = append(parts, fmt.Sprintf("%s: %d", role, count))
		}
		sort.Strings(parts)
		return CommandResult{Notice: fmt.Sprintf("共 %d 条 (%s)", len(history), strings.Join(parts, ", "))}, nil
	})
	register("trace", "显示调用追踪树", func(context.Context, []string) (CommandResult, error) {
		trace := service.Deps.Engine.TraceText()
		if trace == "" {
			trace = "暂无追踪数据"
		}
		return CommandResult{Notice: trace}, nil
	})
	register("compact", "压缩当前上下文（折叠为有界 checkpoint，原始轮次仍可回读）", func(ctx context.Context, _ []string) (CommandResult, error) {
		result, err := service.CompactContextNow(ctx)
		if err != nil {
			return CommandResult{}, err
		}
		if !result.Compacted || !result.Recorded {
			// 只有真落了压缩记录才按「记录」成句。折叠已发生但记录不产生
			// （回合已收尾的自动压缩）时，结果面没有 reason 与区间（零值），
			// 套记录句式就会对用户说出「已压缩上下文：v3（），…」——空原因，
			// 且把真正解释（Note：折叠已发生、为何无记录）丢掉。此时回带 Note，
			// 与 compact_context 工具同一口径。
			return CommandResult{Notice: result.Note}, nil
		}
		// 记录成句的唯一样板在 compactionRecordNote（与 compact_context 工具
		// 同一句）：只说记录里**已有**的区间字段（message_from/to、event_from/to），
		// 不说 messages_before——那是装配前的引擎历史条数（含 system 行、
		// 冷加载/路由会话合法为 0），拿它当"压缩前 N 条消息"会印出与事实相反
		// 的句子（实测同一支既说过"压缩前 0 条消息"、也说过"压缩前 2 条消息"，
		// 而真实区间是 message-1..message-103 / 事件 1..6）。
		return CommandResult{Notice: compactionRecordNote(result)}, nil
	})
	register("new", "准备新会话（首次发送时创建）", func(context.Context, []string) (CommandResult, error) {
		return CommandResult{}, service.BeginNewSession()
	})
	register("resume", "恢复历史会话：/resume <session_id>", func(ctx context.Context, args []string) (CommandResult, error) {
		service.ViewMu.RLock()
		capabilities := service.Core.Snapshot.Capabilities
		service.ViewMu.RUnlock()
		if !capabilities.SessionResume {
			reason := strings.TrimSpace(capabilities.SessionResumeReason)
			if reason == "" {
				reason = "当前 Engine 不支持历史替换"
			}
			return CommandResult{Notice: "会话恢复暂不可用: " + reason}, nil
		}
		if len(args) == 0 {
			return CommandResult{Interaction: service.sessionInteraction()}, nil
		}
		return CommandResult{}, service.resumeSession(strings.TrimSpace(args[0]))
	})
	register("fork", "从会话分支出新会话：/fork [session_id]（默认当前会话，切到最新完整轮次）", func(ctx context.Context, args []string) (CommandResult, error) {
		service.ViewMu.RLock()
		currentID := service.Core.Snapshot.Session.ID
		draft := service.Core.Snapshot.Session.Draft
		service.ViewMu.RUnlock()
		parentID := currentID
		if len(args) > 0 {
			parentID = strings.TrimSpace(args[0])
		}
		if parentID == "" || (parentID == currentID && draft) {
			return CommandResult{Notice: "当前会话尚未持久化，无法 fork（请先发送一条消息）"}, nil
		}
		childID, err := service.ForkSessionLatest(parentID)
		if err != nil {
			return CommandResult{}, err
		}
		return CommandResult{Notice: "已从 " + parentID + " 分支出新会话: " + childID}, nil
	})
	register("sessions", "列出所有持久化会话", func(context.Context, []string) (CommandResult, error) {
		sessions := service.Snapshot().Sessions
		if len(sessions) == 0 {
			return CommandResult{Notice: "暂无持久化会话"}, nil
		}
		var builder strings.Builder
		builder.WriteString("持久化会话:\n")
		for _, session := range sessions {
			fmt.Fprintf(&builder, "  %s  %s  %s  tok:%d\n", session.ID, session.Name, session.UpdatedAt.Format("01-02 15:04"), session.TokenCount)
		}
		return CommandResult{Notice: builder.String()}, nil
	})
	register("pool", "显示并切换账号池", func(context.Context, []string) (CommandResult, error) {
		return CommandResult{Interaction: service.accountInteraction()}, nil
	})
	register("plugins", "列出可用插件", func(context.Context, []string) (CommandResult, error) {
		plugins := service.Deps.Plugins.All()
		if len(plugins) == 0 {
			return CommandResult{Notice: "暂无可用插件"}, nil
		}
		current, _ := service.Deps.Plugins.Current()
		var builder strings.Builder
		builder.WriteString("可用插件:\n")
		for _, plugin := range plugins {
			marker := "  "
			if plugin.Name == current.Name {
				marker = "* "
			}
			fmt.Fprintf(&builder, "%s%-16s %s\n", marker, plugin.Name, plugin.Description)
		}
		return CommandResult{Notice: builder.String()}, nil
	})
	register("plugin", "切换插件：/plugin <name|off>", func(ctx context.Context, args []string) (CommandResult, error) {
		if len(args) == 0 {
			current, ok := service.Deps.Plugins.Current()
			if !ok {
				return CommandResult{Notice: "当前未激活插件"}, nil
			}
			return CommandResult{Notice: "当前插件: " + current.Name}, nil
		}
		name := strings.ToLower(strings.TrimSpace(args[0]))
		if err := service.SwitchPlugin(ctx, name); err != nil {
			return CommandResult{}, err
		}
		if name == "off" || name == "none" {
			return CommandResult{Notice: "已停用插件"}, nil
		}
		return CommandResult{Notice: "已切换插件: " + name}, nil
	})
	register("diag", "系统诊断信息", func(context.Context, []string) (CommandResult, error) {
		service.ViewMu.RLock()
		snap := service.Core.Snapshot
		service.ViewMu.RUnlock()
		return CommandResult{Notice: RenderDiag(snap)}, nil
	})
	register("exit", "退出程序", func(context.Context, []string) (CommandResult, error) { return CommandResult{Exit: true}, nil })
	register("effort", "切换 Effort 等级: /effort <lite|medium|high|max>", func(ctx context.Context, args []string) (CommandResult, error) {
		if len(args) == 0 {
			return CommandResult{Notice: "当前 Effort: " + service.effortManager.Current() + "（可用: lite, medium, high, max）"}, nil
		}
		level := strings.ToLower(strings.TrimSpace(args[0]))
		// 与 GUI 共用 SwitchEffort 单一路径：视图会话 running 时拒绝，且
		// system prompt 只写目标（视图）会话引擎（G0b）。
		if err := service.SwitchEffort(ctx, level); err != nil {
			return CommandResult{}, err
		}
		return CommandResult{Notice: "Effort 已切换为: " + level}, nil
	})
	// 运行期切档（main.go 装配注释承诺的 CLI 面）：与 GUI chip / headless 共用
	// SetPermissionTier 单一路径——写入侧校验、会话粒度落地（含跨重启落盘）、执行门
	// 与审批面按本会话同步。刻意**不加 running 守卫**：运行中切档正是用来放行/收紧
	// 当前审批的（与 /effort 的守卫语义不同）。
	register("permission", "查看/切换本会话权限档位: /permission <manual|edit|auto|full>", func(_ context.Context, args []string) (CommandResult, error) {
		if len(args) == 0 {
			current := service.permissionTierForSession(service.currentViewSessionID())
			var builder strings.Builder
			builder.WriteString("当前档位（本会话）: " + current + "\n")
			for _, tier := range dto.PermissionTiers() {
				marker := "  "
				if tier.ID == current {
					marker = "* "
				}
				fmt.Fprintf(&builder, "%s%-8s %s\n", marker, tier.ID, tier.Description)
			}
			return CommandResult{Notice: builder.String()}, nil
		}
		effective, err := service.SetPermissionTier(args[0])
		if err != nil {
			return CommandResult{}, err
		}
		return CommandResult{Notice: "本会话权限档位已切换为: " + effective}, nil
	})
	return registrationErr
}
