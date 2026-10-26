// Package window exposes the Application-facing context-window policy types
// while keeping the one implementation in Engine input/contextcontrol.
package window

import (
	contextcontrol "github.com/RedHuang-0622/seelex/backend/engine/input/contextcontrol"
)

type WindowConfig = contextcontrol.WindowConfig
type DefaultWindowPolicy = contextcontrol.DefaultWindowPolicy

func NewDefaultWindowPolicy(config WindowConfig) DefaultWindowPolicy {
	return contextcontrol.NewDefaultWindowPolicy(config)
}

func DefaultWindowConfig() WindowConfig {
	return contextcontrol.DefaultWindowConfig()
}

func LoadWindowConfig(path string) (WindowConfig, error) {
	return contextcontrol.LoadWindowConfig(path)
}

func Apply(config WindowConfig) {
	contextcontrol.Apply(config)
}

func Current() WindowConfig {
	return contextcontrol.Current()
}
