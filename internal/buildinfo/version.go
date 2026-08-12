// Package buildinfo 承载构建期注入的版本信息。
package buildinfo

// Version is the current Seelex release version.
// Release builds override it with: -ldflags "-X github.com/RedHuang-0622/seelex/internal/buildinfo.Version=<tag>".
var Version = "dev"

// DefaultFrontend remains tui for normal builds. Desktop release builds
// override it with: -ldflags "-X github.com/RedHuang-0622/seelex/internal/buildinfo.DefaultFrontend=gui".
var DefaultFrontend = "tui"
