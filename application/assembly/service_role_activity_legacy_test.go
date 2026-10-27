package assembly

import "github.com/RedHuang-0622/seelex/contract/dto"

// HandleRoleToolActivity is a test-only compatibility shim.
func (service *Service) HandleRoleToolActivity(activity dto.RoleToolActivity) {
	service.components.roleActivity.Handle(activity)
}
