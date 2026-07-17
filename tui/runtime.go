package tui

import (
	"context"

	"github.com/RedHuang-0622/seelex/plugin"
	"github.com/RedHuang-0622/seelex/seelebridge"
)

type RuntimeView interface {
	VisibleTools(ctx context.Context) []seelebridge.Tool
	ActivePlugin() string
	Accounts() []seelebridge.Account
	SelectAccount(name string) bool
	Provider() string
}

type PluginController interface {
	All() []plugin.Plugin
	Activate(ctx context.Context, name string) error
	Deactivate(ctx context.Context) error
	Current() (plugin.Plugin, bool)
}
