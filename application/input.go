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
		return service.applySkill(skill, parts[1:])
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

func (service *Service) submitSkill(name string, args []string) error {
	if name == "" {
		return nil
	}
	skill, ok := service.deps.Skills.Get(name)
	if !ok {
		service.addNotice("未知 Skill: " + name)
		return nil
	}
	return service.applySkill(skill, args)
}

func (service *Service) applySkill(skill SkillInfo, args []string) error {
	prompt := skill.Prompt
	if len(args) > 0 {
		prompt += "\n\n" + strings.Join(args, " ")
	}
	service.deps.Engine.SetSystemPrompt(prompt)
	service.addNotice("加载 Skill: " + skill.Name)
	return nil
}
