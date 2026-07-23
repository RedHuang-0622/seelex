package gui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/RedHuang-0622/seelex/application"
)

const eventName = "seelex:event"

// Application is the narrow application-core contract consumed by the GUI.
// The interface belongs to the caller so the desktop bridge can be tested
// without constructing the Seele runtime.
type Application interface {
	Snapshot() application.Snapshot
	Subscribe(buffer int) application.Subscription
	Submit(context.Context, string) error
	CancelChat(string) bool
	ResolveInteraction(context.Context, string, string) error
	SelectAccount(context.Context, string) error
	SwitchEffort(context.Context, string) error
	SwitchPlugin(context.Context, string) error
	LoadMoreHistory(int) error
	Suggestions(string) []application.Suggestion
}

type emitter func(context.Context, string, any)

type Options struct {
	Title       string
	Version     string
	ProjectRoot string
	Width       int
	Height      int
}

type AppInfo struct {
	Title   string      `json:"title"`
	Version string      `json:"version"`
	Project ProjectInfo `json:"project"`
}

type ProjectInfo struct {
	Name    string          `json:"name"`
	Root    string          `json:"root"`
	Sources []ProjectSource `json:"sources"`
}

type ProjectSource struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
	Path string `json:"path"`
}

// Bridge adapts the headless application service to desktop-safe methods.
type Bridge struct {
	app     Application
	info    AppInfo
	mu      sync.Mutex
	ctx     context.Context
	cancel  context.CancelFunc
	sub     application.Subscription
	wg      sync.WaitGroup
	running bool
}

func NewBridge(app Application, options Options) (*Bridge, error) {
	if app == nil {
		return nil, errors.New("gui: application is required")
	}
	title := strings.TrimSpace(options.Title)
	if title == "" {
		title = "Seelex"
	}
	return &Bridge{app: app, info: AppInfo{Title: title, Version: options.Version, Project: discoverProject(options.ProjectRoot)}}, nil
}

func discoverProject(root string) ProjectInfo {
	if strings.TrimSpace(root) == "" {
		root, _ = os.Getwd()
	}
	absRoot, err := filepath.Abs(root)
	if err == nil {
		root = absRoot
	}
	project := ProjectInfo{Name: filepath.Base(filepath.Clean(root)), Root: filepath.Clean(root)}
	candidates := []ProjectSource{
		{Name: "README", Kind: "documentation", Path: "README.md"},
		{Name: "Changelog", Kind: "documentation", Path: "CHANGELOG.md"},
		{Name: "Agent configuration", Kind: "configuration", Path: "seele.yaml"},
		{Name: "Account template", Kind: "configuration", Path: filepath.Join("config", "accounts.example.yaml")},
		{Name: "Plugins", Kind: "capability", Path: "plugins"},
		{Name: "Project docs", Kind: "documentation", Path: "docs"},
	}
	for _, source := range candidates {
		if _, statErr := os.Stat(filepath.Join(root, source.Path)); statErr == nil {
			source.Path = filepath.ToSlash(source.Path)
			project.Sources = append(project.Sources, source)
		}
	}
	return project
}

func (bridge *Bridge) start(ctx context.Context, emit emitter) {
	bridge.mu.Lock()
	if bridge.running {
		bridge.mu.Unlock()
		return
	}
	bridge.ctx, bridge.cancel = context.WithCancel(ctx)
	bridge.sub = bridge.app.Subscribe(256)
	bridge.running = true
	loopContext := bridge.ctx
	subscription := bridge.sub
	bridge.wg.Add(1)
	bridge.mu.Unlock()

	go func() {
		defer bridge.wg.Done()
		if emit != nil {
			emit(loopContext, "seelex:ready", bridge.app.Snapshot())
		}
		for {
			select {
			case <-loopContext.Done():
				return
			case event, ok := <-subscription.Events:
				if !ok {
					return
				}
				if emit != nil {
					emit(loopContext, eventName, event)
				}
			}
		}
	}()
}

func (bridge *Bridge) stop() {
	bridge.mu.Lock()
	if !bridge.running {
		bridge.mu.Unlock()
		return
	}
	cancel := bridge.cancel
	subscription := bridge.sub
	bridge.running = false
	bridge.cancel = nil
	bridge.ctx = nil
	bridge.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	subscription.Close()
	bridge.wg.Wait()
}

func (bridge *Bridge) requestContext() context.Context {
	bridge.mu.Lock()
	defer bridge.mu.Unlock()
	if bridge.ctx != nil {
		return bridge.ctx
	}
	return context.Background()
}

func (bridge *Bridge) Info() AppInfo { return bridge.info }

func (bridge *Bridge) Snapshot() application.Snapshot { return bridge.app.Snapshot() }

func (bridge *Bridge) Submit(text string) error {
	return bridge.app.Submit(bridge.requestContext(), text)
}

func (bridge *Bridge) CancelChat(requestID string) bool {
	return bridge.app.CancelChat(requestID)
}

func (bridge *Bridge) ResolveInteraction(id, optionID string) error {
	return bridge.app.ResolveInteraction(bridge.requestContext(), id, optionID)
}

func (bridge *Bridge) SelectAccount(name string) error {
	return bridge.app.SelectAccount(bridge.requestContext(), name)
}

func (bridge *Bridge) SwitchEffort(level string) error {
	return bridge.app.SwitchEffort(bridge.requestContext(), level)
}

func (bridge *Bridge) SwitchPlugin(name string) error {
	return bridge.app.SwitchPlugin(bridge.requestContext(), name)
}

func (bridge *Bridge) LoadMoreHistory(limit int) error {
	return bridge.app.LoadMoreHistory(limit)
}

func (bridge *Bridge) Suggestions(input string) []application.Suggestion {
	return bridge.app.Suggestions(input)
}
