package workspace

import switchworkspace "github.com/RedHuang-0622/seelex/middle/switch/workspace"

type SwitchUseCase = switchworkspace.UseCase
type SwitchDeps = switchworkspace.Deps
type SwitchProjection = switchworkspace.Projection
type SwitchViewState = switchworkspace.ViewState

func NewSwitch(deps SwitchDeps) *SwitchUseCase {
	return switchworkspace.New(deps)
}
