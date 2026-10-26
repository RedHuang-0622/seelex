package policy

import (
	"context"
	"errors"
	"fmt"
	"strings"

	view_state "github.com/RedHuang-0622/seelex/application/session/viewstate"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
)

// SwitchDeps supplies policy, effort, plugin and projection callbacks.
type SwitchDeps struct {
	ActivateSkill         func(name string) (model.SkillInfo, error)
	SetReasoningEffort    func(effort string) error
	SetLoopLimit          func(limit int) error
	SetReleaseWindow      func(window int) (int, error)
	ReleaseWindow         func() int
	SetPermissionTier     func(tier string) (string, error)
	CurrentViewSessionID  func() string
	PersistTier           func(sessionID, tier string) error
	SetUnitTier           func(sessionID, tier string)
	SetApprovalAuto       func(sessionID string, full bool)
	SetRuntimeTier        func(sessionID, tier string) error
	EffectiveTier         func(sessionID string) string
	ResolveApprovals      func(sessionID string)
	PublishTier           func(sessionID, effective string)
	EnsureViewIdle        func(label string) (string, error)
	CycleEffort           func() (string, error)
	ApplyEffort           func(level string) error
	SubagentPolicy        func() dto.SubagentPolicy
	SetSubagentPolicy     func(policy dto.SubagentPolicy)
	SyncReasoningEffort   func(level string) int
	CurrentEffort         func() string
	RenderPrompt          func() string
	SetSystemPrompt       func(sessionID, prompt string)
	CommitEffort          func(sessionID, effort, reasoning string, loop int)
	AnyChatRunning        func() bool
	ActivatePlugin        func(ctx context.Context, name string) error
	DeactivatePlugin      func(ctx context.Context) error
	ClearHistory          func()
	ResetPrompt           func(text string)
	CurrentPlugin         func() (model.PluginInfo, bool)
	ReapplyEffort         func()
	SetPluginSystemPrompt func(prompt string)
	ResetConversation     func(message string)
	CollectProjection     func(ctx context.Context) view_state.RuntimeStateProjection
	CommitProjection      func(ctx context.Context, projection view_state.RuntimeStateProjection)
}

// SwitchApplication owns low-risk policy/session switches.
type SwitchApplication struct {
	deps SwitchDeps
}

// NewSwitch constructs the policy switching application.
func NewSwitch(deps SwitchDeps) *SwitchApplication {
	return &SwitchApplication{deps: deps}
}

// ActivateSkill activates one Skill.
func (application *SwitchApplication) ActivateSkill(name string) (model.SkillInfo, error) {
	if application == nil {
		return model.SkillInfo{}, errors.New("service is not initialized")
	}
	return application.deps.ActivateSkill(name)
}

// SetReasoningEffort changes the reasoning effort.
func (application *SwitchApplication) SetReasoningEffort(effort string) error {
	return application.deps.SetReasoningEffort(effort)
}

// SetLoopLimit changes the loop limit.
func (application *SwitchApplication) SetLoopLimit(limit int) error {
	return application.deps.SetLoopLimit(limit)
}

// SetSessionReleaseWindow changes the session release window.
func (application *SwitchApplication) SetSessionReleaseWindow(window int) (int, error) {
	return application.deps.SetReleaseWindow(window)
}

// SessionReleaseWindow reads the session release window.
func (application *SwitchApplication) SessionReleaseWindow() int {
	return application.deps.ReleaseWindow()
}

// SetFullAccess maps full-access state to the permission tier.
func (application *SwitchApplication) SetFullAccess(on bool) bool {
	effective, err := application.deps.SetPermissionTier(dto.PermissionTierFromFullAccess(on))
	if err != nil {
		return false
	}
	return dto.PermissionTierIsFullAccess(effective)
}

// SetPermissionTier persists and applies one permission tier.
func (application *SwitchApplication) SetPermissionTier(tier string) (string, error) {
	if application == nil {
		return "", errors.New("permission tier: nil service")
	}
	normalized, err := dto.NormalizePermissionTier(tier)
	if err != nil {
		return "", err
	}
	sessionID := application.deps.CurrentViewSessionID()
	if err := application.deps.PersistTier(sessionID, normalized); err != nil {
		return "", err
	}
	application.deps.SetUnitTier(sessionID, normalized)
	full := dto.PermissionTierIsFullAccess(normalized)
	application.deps.SetApprovalAuto(sessionID, full)
	if err := application.deps.SetRuntimeTier(sessionID, normalized); err != nil {
		return "", err
	}
	effective := application.deps.EffectiveTier(sessionID)
	if effective == dto.PermissionTierFull {
		application.deps.ResolveApprovals(sessionID)
	}
	application.deps.PublishTier(sessionID, effective)
	return effective, nil
}

// SwitchEffort applies a selected or cycled effort level.
func (application *SwitchApplication) SwitchEffort(level string) error {
	sessionID, err := application.deps.EnsureViewIdle("切换 Effort")
	if err != nil {
		return err
	}
	if level == "" || level == "cycle" {
		level, err = application.deps.CycleEffort()
		if err != nil {
			return err
		}
	}
	if err := application.deps.ApplyEffort(level); err != nil {
		return err
	}
	application.deps.SetSubagentPolicy(application.deps.SubagentPolicy())
	currentEffort := application.deps.CurrentEffort()
	application.deps.SyncReasoningEffort(currentEffort)
	application.deps.SetSystemPrompt(sessionID, application.deps.RenderPrompt())
	application.deps.CommitEffort(sessionID, currentEffort, "", 0)
	return nil
}

// SwitchPlugin activates/deactivates one process plugin and rebuilds history.
func (application *SwitchApplication) SwitchPlugin(ctx context.Context, name string) error {
	if application.deps.AnyChatRunning() {
		return errors.New("有会话正在运行：插件切换会重建进程级提示与历史，请等待全部会话空闲后再切换")
	}
	if name == "off" || name == "none" || name == "" {
		if err := application.deps.DeactivatePlugin(ctx); err != nil {
			return fmt.Errorf("deactivate plugin: %w", err)
		}
		application.deps.ClearHistory()
		application.deps.ResetPrompt("")
		application.deps.SetPluginSystemPrompt("")
		application.deps.ReapplyEffort()
		application.deps.ResetConversation("已停用插件")
	} else {
		if err := application.deps.ActivatePlugin(ctx, name); err != nil {
			return fmt.Errorf("activate plugin: %w", err)
		}
		application.deps.ClearHistory()
		if current, ok := application.deps.CurrentPlugin(); ok {
			application.deps.ResetPrompt(strings.TrimSpace(current.Prompt))
		}
		application.deps.ReapplyEffort()
		application.deps.SetPluginSystemPrompt(application.deps.RenderPrompt())
		application.deps.ResetConversation("已切换到 " + name + " 插件")
	}
	projection := application.deps.CollectProjection(ctx)
	application.deps.CommitProjection(ctx, projection)
	return nil
}
