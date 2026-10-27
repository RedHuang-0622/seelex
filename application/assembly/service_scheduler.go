package assembly

import (
	"errors"
	"fmt"
	"strings"

	applicationscheduler "github.com/RedHuang-0622/seelex/application/admin/scheduler"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/contract/protocol"
)

func withScheduledRunDefaults(spec dto.ScheduledTaskSpec) dto.ScheduledTaskSpec {
	return applicationscheduler.WithRunDefaults(spec)
}

func scheduledRunParams(effort string, loopLimit int) (string, int) {
	return applicationscheduler.RunParams(effort, loopLimit)
}

func (service *Service) validateScheduledRunParams(spec dto.ScheduledTaskSpec) error {
	return applicationscheduler.ValidateRunParams(spec)
}

func (service *Service) applyScheduledRunParams(sessionID string, task dto.ScheduledTaskSpec) error {
	effort, loops := scheduledRunParams(task.ReasoningEffort, task.LoopLimit)
	pinner, ok := service.Deps.Engine.(contract.SessionLoopLimitPort)
	if !ok {
		return errors.New("宿主引擎不支持按会话设置循环上限（SetSessionMaxLoopsFor 未实现）")
	}
	pinner.SetSessionMaxLoopsFor(sessionID, loops)
	service.knobs.BorrowSessionReasoningEffort(sessionID, effort)
	return nil
}

func (service *Service) validateScheduledWorkspace(workspaceID string) error {
	_, err := service.resolveScheduledWorkspace(workspaceID)
	return err
}

func (service *Service) resolveScheduledWorkspace(workspaceID string) (*model.WorkspaceInfo, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		var unbound *model.WorkspaceInfo
		return unbound, nil
	}
	if service.Deps.Workspace == nil {
		return nil, errors.New("指定了工作区，但工作区能力未装配")
	}
	info, err := service.Deps.Workspace.Get(workspaceID)
	if err != nil {
		return nil, fmt.Errorf("定时任务工作区 %q 不可用: %w", workspaceID, err)
	}
	return &info, nil
}

func (service *Service) validateScheduledPlugins(plugins []string) error {
	names, err := dto.NormalizePlugins(plugins, 0)
	if err != nil {
		return err
	}
	if len(names) == 0 || service.Deps.Plugins == nil {
		return nil
	}
	known := make(map[string]struct{})
	for _, info := range service.Deps.Plugins.All() {
		known[strings.TrimSpace(info.Name)] = struct{}{}
	}
	if len(known) == 0 {
		return nil
	}
	missing := make([]string, 0, len(names))
	for _, name := range names {
		if _, ok := known[name]; !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("定时任务装配的插件不存在：%s", strings.Join(missing, "、"))
	}
	return nil
}
