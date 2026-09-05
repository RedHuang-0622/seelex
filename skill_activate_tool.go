package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/RedHuang-0622/seelex/seelebridge"
	"github.com/RedHuang-0622/seelex/skill"
)

// registerSkillActivateTool 注册模型侧的技能激活工具（Codex 式 "调用 skill"
// 在 Seelex 的翻译）：
//
//   - 候选集 = skill.Registry 当前激活插件（Get/All 按 activePlugin 隔离），
//     switch_plugin 切换后本工具的候选列表自动随之切换；
//   - manual 权限模式下该工具不在白名单 → 每次调用经 permission gate 弹
//     用户审批（full_access 静默放行），批准后才执行 activate；
//   - activate 由调用方注入（生产 = application.Service.ActivateSkill，把
//     技能压进会话 skill 层，供下一轮提交经 S2 注入 Trusted Active Skill），
//     测试注入桩即可验证工具逻辑。
func registerSkillActivateTool(runtime *seelebridge.Runtime, skills *skill.Registry, activate func(name string) (skill.Skill, error)) {
	runtime.RegisterTool(
		"skill_activate",
		"激活当前插件的一个技能：把它的指令注入当前任务/会话作为 Trusted Skill。技能列表由 system 的 ## Available Skills 段给出，随 switch_plugin 切换；manual 权限下本调用需用户审批。命中后立即按返回的技能指令执行。",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name":   map[string]interface{}{"type": "string", "description": "要激活的技能名（来自 ## Available Skills）"},
				"reason": map[string]interface{}{"type": "string", "description": "为什么需要该技能（审计/审批展示用）"},
			},
			"required": []string{"name"},
		},
		func(ctx context.Context, argsJSON string) (string, error) {
			var input struct {
				Name   string `json:"name"`
				Reason string `json:"reason,omitempty"`
			}
			if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
				return "", fmt.Errorf("skill_activate: %w", err)
			}
			if input.Name == "" {
				return "", fmt.Errorf("skill_activate: name is required")
			}
			item, ok := skills.Get(input.Name)
			if !ok {
				available := make([]string, 0, 4)
				for _, s := range skills.All() {
					available = append(available, s.Name)
				}
				encoded, _ := json.Marshal(map[string]interface{}{
					"status": "unknown_skill", "skill": input.Name,
					"available": available,
					"hint":      "available skills belong to the active plugin; switch_plugin changes them",
				})
				return string(encoded), nil
			}
			activated, err := activate(input.Name)
			if err != nil {
				encoded, _ := json.Marshal(map[string]interface{}{
					"status": "error", "skill": input.Name, "reason": err.Error(),
				})
				return string(encoded), nil
			}
			encoded, err := json.Marshal(map[string]interface{}{
				"status":       "activated",
				"skill":        item.Name,
				"description":  item.Description,
				"reason":       input.Reason,
				"instructions": activated.Prompt,
				"note":         "skill activated: its contents are trusted instructions for this task; continue executing per them now. It stays active for follow-up submissions in this session until ended.",
			})
			return string(encoded), err
		},
	)
}
