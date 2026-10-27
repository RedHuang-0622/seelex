package assembly

import (
	"errors"
	"fmt"
	"strings"

	"github.com/RedHuang-0622/seelex/contract/protocol"
)

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
