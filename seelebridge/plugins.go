package seelebridge

import (
	"fmt"

	"github.com/RedHuang-0622/Seele/agent/core/tool/holder"
)

func (r *Runtime) DefinePlugin(name, description string, include, exclude []string) error {
	if name == "" {
		return fmt.Errorf("seelebridge: plugin name is empty")
	}
	manager := r.agent.Tools().Plugin()
	if manager == nil {
		return fmt.Errorf("seelebridge: plugin manager is unavailable")
	}
	manager.Define(holder.NewPlugin(name, description, include, exclude))
	return nil
}

func (r *Runtime) UndefinePlugin(name string) {
	if manager := r.agent.Tools().Plugin(); manager != nil {
		manager.Undefine(name)
	}
}

func (r *Runtime) ActivatePlugin(name string) error {
	return r.agent.Tools().ActivatePlugin(name)
}

func (r *Runtime) DeactivatePlugin() { r.agent.Tools().DeactivatePlugin() }

func (r *Runtime) ActivePlugin() string { return r.agent.Tools().ActivePlugin() }
