package gui

import "embed"

// embeddedFrontend contains the dependency-free GUI distribution.
//
//go:embed frontend/dist/*
var embeddedFrontend embed.FS
