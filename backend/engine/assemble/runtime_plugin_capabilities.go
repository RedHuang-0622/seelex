package assemble

import "github.com/RedHuang-0622/seelex/contract/dto"

// SetPluginCapabilitiesProvider installs the safe plugin-detail projection.
func (r *Runtime) SetPluginCapabilitiesProvider(provider func(names []string) []dto.PluginCapabilityView) {
	if r == nil {
		return
	}
	r.pluginCapabilitiesMu.Lock()
	r.pluginCapabilities = provider
	r.pluginCapabilitiesMu.Unlock()
}

func (r *Runtime) pluginCapabilityViews(names []string) []dto.PluginCapabilityView {
	if r == nil {
		return nil
	}
	r.pluginCapabilitiesMu.RLock()
	provider := r.pluginCapabilities
	r.pluginCapabilitiesMu.RUnlock()
	if provider == nil {
		return nil
	}
	return provider(names)
}
