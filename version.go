package main

// Version is the current Seelex release version.
// Release builds override it with: -ldflags "-X main.Version=<tag>".
var Version = "v0.1.0-alpha.1"

// DefaultFrontend remains tui for normal builds. Desktop release builds
// override it with: -ldflags "-X main.DefaultFrontend=gui".
var DefaultFrontend = "tui"
