package application

import (
	"context"
	"fmt"
	"strings"
)

func (service *Service) submitCommand(ctx context.Context, input string) error {
	trimmed := strings.TrimSpace(strings.TrimPrefix(input, "/"))
	if trimmed == "" {
		return nil
	}
	parts := strings.Fields(trimmed)
	if skill, ok := service.deps.Skills.Get(parts[0]); ok {
		return service.activateSkillAndSubmit(ctx, skill, parts[1:], input)
	}
	command, ok := service.commands.Get(parts[0])
	if !ok {
		service.addNotice(fmt.Sprintf("未知命令: %s。输入 /help 查看可用命令。", parts[0]))
		return nil
	}
	result, err := command.Execute(ctx, parts[1:])
	if err != nil {
		service.addNotice(err.Error())
		return err
	}
	if result.Notice != "" {
		service.addNotice(result.Notice)
	}
	if result.Interaction != nil {
		if len(result.Interaction.Options) == 0 {
			service.addNotice("暂无可选项")
		} else {
			service.openInteraction(result.Interaction)
		}
	}
	if result.Exit {
		service.events.Publish(EventExitRequested, service.Snapshot().Revision, "", nil)
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
	skill, ok := service.deps.Skills.Get(name)
	if !ok {
		service.addNotice("未知 Skill: " + name)
		return nil
	}
	return service.activateSkillAndSubmit(ctx, skill, args, input)
}

func (service *Service) endSkill() error {
	name := service.promptStack.PopKind("skill")
	if name == "" {
		service.addNotice("当前无 Skill 可退栈")
		return nil
	}
	// goal 仍在活动栈时保持无限循环；否则只恢复循环上限，不改动 prompt 层顺序。
	maxLoops := effortLoops[service.effortManager.Current()]
	for _, layer := range service.promptStack.Layers() {
		if layer.Kind == "skill" && layer.Name == "goal" {
			maxLoops = 9999
			break
		}
	}
	service.deps.Engine.SetMaxLoops(maxLoops)
	service.addNotice("已退栈 Skill: " + name)
	return nil
}

func (service *Service) activateSkillAndSubmit(ctx context.Context, skill SkillInfo, args []string, input string) error {
	service.applySkill(skill)
	if len(args) == 0 {
		return nil
	}
	return service.submitConversation(ctx, input)
}

func (service *Service) applySkill(skill SkillInfo) {
	service.promptStack.Push("skill", skill.Name, skill.Prompt)
	// goal skill 不受 MaxLoops 限制（设大值模拟无上限）
	if skill.Name == "goal" {
		service.deps.Engine.SetMaxLoops(9999)
	}
	service.addNotice("加载 Skill: " + skill.Name)
}
