package core

import (
	"context"
	"fmt"
	applicationinput "github.com/RedHuang-0622/seelex/application/input"
	applicationpolicy "github.com/RedHuang-0622/seelex/application/policy"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	"strings"
)

func (service *Service) submitCommand(ctx context.Context, input string) error {
	trimmed := strings.TrimSpace(strings.TrimPrefix(input, "/"))
	if trimmed == "" {
		return nil
	}
	parts := strings.Fields(trimmed)
	if skill, ok := service.Deps.Skills.Get(parts[0]); ok {
		return service.activateSkillAndSubmit(ctx, skill, parts[1:], input)
	}
	command, ok := service.commands.Get(parts[0])
	if !ok {
		service.components.view.AddNotice(service.unknownCommandNotice(parts[0]))
		return nil
	}
	result, err := command.Execute(ctx, parts[1:])
	if err != nil {
		service.components.view.AddNotice(err.Error())
		return err
	}
	if result.Notice != "" {
		service.components.view.AddNotice(result.Notice)
	}
	if result.Interaction != nil {
		if len(result.Interaction.Options) == 0 {
			service.components.view.AddNotice("暂无可选项")
		} else {
			service.openInteraction(result.Interaction)
		}
	}
	if result.Exit {
		service.Events.Publish(EventExitRequested, service.components.frontendSnapshot.Snapshot().Revision, "", nil)
	}
	return nil
}

func (service *Service) submitSkill(ctx context.Context, name string, args []string, input string) error {
	if name == "" {
		return nil
	}
	if name == "end" {
		return service.endSkill()
	}
	skill, ok := service.Deps.Skills.Get(name)
	if !ok {
		notice := "未知 Skill: " + name
		if hint := service.sigilMigrationHint(applicationinput.SigilSkill, name); hint != "" {
			notice += hint
		}
		service.components.view.AddNotice(notice)
		return nil
	}
	return service.activateSkillAndSubmit(ctx, skill, args, input)
}

// submitPluginSwitch 是 `#` 前缀的落点：切换/停用插件。
//
// 只在"名字其实属于别的域"时补一句 notice——那正是旧前缀肌肉记忆的症状
// （`#` 曾经是"召回 Skill"）；其它失败原样返回错误，由调用方按既有口径呈现，
// 不在这里重复一份错误文案。
func (service *Service) submitPluginSwitch(ctx context.Context, name string) error {
	if err := service.components.policyApplication.SwitchPlugin(ctx, name); err != nil {
		if hint := service.sigilMigrationHint(applicationinput.SigilPlugin, name); hint != "" {
			service.components.view.AddNotice(fmt.Sprintf("未知 Plugin: %s%s", name, hint))
		}
		return err
	}
	return nil
}

func (service *Service) endSkill() error {
	name := service.promptStack.PopKind("skill")
	if name == "" {
		service.components.view.AddNotice("当前无 Skill 可退栈")
		return nil
	}
	// goal 仍在活动栈时保持无限循环；否则只恢复循环上限，不改动 prompt 层顺序。
	// 恢复的是**当前生效的循环上限**（用户用拉条改过就是改过的值），不是档位默认值
	// ——旧实现直接取 maxLoopsFor(档位)，于是"把循环次数拧到 600"会在一次退栈后
	// 无声回到 384。
	loopLimit := service.knobs.CurrentLoopLimit()
	for _, layer := range service.promptStack.Layers() {
		if layer.Kind == "skill" && layer.Name == "goal" {
			loopLimit = applicationpolicy.Get().InputLoopLimit // limits.input_loop_limit（默认 9999 ≈ 无上限）
			break
		}
	}
	service.knobs.ApplyLoopLimit(loopLimit)
	service.components.view.AddNotice("已退栈 Skill: " + name)
	return nil
}

func (service *Service) activateSkillAndSubmit(ctx context.Context, skill model.SkillInfo, args []string, input string) error {
	service.prepareCompletedTaskBoundary()
	service.applySkill(skill)
	if len(args) == 0 {
		return nil
	}
	return service.submitConversation(ctx, input)
}

func (service *Service) prepareCompletedTaskBoundary() {
	service.ViewMu.RLock()
	terminal := false
	if !service.Core.Snapshot.Chat.Running {
		terminal = service.Core.Snapshot.Task != nil &&
			(service.Core.Snapshot.Task.Status == TurnCompleted || service.Core.Snapshot.Task.Status == TurnFailed)
		if state := service.components.tasks.CurrentTaskExecution(); state != nil {
			terminal = terminal || state.Status == TurnCompleted || state.Status == TurnFailed
		}
	}
	service.ViewMu.RUnlock()
	if terminal {
		service.promptStack.ClearKind("skill")
		service.knobs.ApplyLoopLimit(service.knobs.CurrentLoopLimit())
	}
}

func (service *Service) applySkill(skill model.SkillInfo) {
	service.promptStack.Push("skill", skill.Name, skill.Prompt)
	// goal skill 不受 MaxLoops 限制（设大值模拟无上限）
	if skill.Name == "goal" {
		service.knobs.ApplyLoopLimit(applicationpolicy.Get().InputLoopLimit)
	}
	service.components.view.AddNotice("加载 Skill: " + skill.Name)
}
