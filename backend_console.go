package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/RedHuang-0622/seelex/application"
	"github.com/RedHuang-0622/seelex/seelebridge"
)

// backendApplication is the diagnostic console's narrow Application boundary.
// It keeps the console observational: all work still flows through Submit and
// the public event stream.
type backendApplication interface {
	Submit(context.Context, string) error
	WaitForIdle(context.Context) error
	Subscribe(int) application.Subscription
}

// backendWorkspaceApplication is the explicit project-selection path used by
// the diagnostic frontend. It deliberately has no implicit cwd fallback: a
// test must name the project it intends to authorize for scoped tools.
type backendWorkspaceApplication interface {
	Snapshot() application.Snapshot
	CreateWorkspace(name, rootPath, gitRemote string) error
	BindWorkspace(workspaceID string) error
}

func bindBackendProject(app backendWorkspaceApplication, rootPath string) error {
	rootPath = strings.TrimSpace(rootPath)
	if rootPath == "" {
		return nil
	}
	absPath, err := filepath.Abs(rootPath)
	if err != nil {
		return fmt.Errorf("backend: resolve project: %w", err)
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return fmt.Errorf("backend: inspect project: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("backend: project is not a directory")
	}
	for _, workspace := range app.Snapshot().Workspaces {
		if filepath.Clean(workspace.RootPath) == filepath.Clean(absPath) {
			if err := app.BindWorkspace(workspace.ID); err != nil {
				return fmt.Errorf("backend: bind project workspace: %w", err)
			}
			return nil
		}
	}
	name := filepath.Base(absPath)
	if name == "." || name == string(filepath.Separator) {
		name = "backend-diagnostic"
	}
	if err := app.CreateWorkspace(name, absPath, ""); err != nil {
		return fmt.Errorf("backend: create project workspace: %w", err)
	}
	return nil
}

func startBackendConsole(app *application.Service, prompt string, timeout time.Duration, output io.Writer) error {
	if output == nil {
		output = os.Stdout
	}
	return runBackendConsole(context.Background(), app, prompt, timeout, os.Stdin, output)
}

func openBackendOutput(logPath string) (io.Writer, func() error, error) {
	if strings.TrimSpace(logPath) == "" {
		return os.Stdout, func() error { return nil }, nil
	}
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, nil, fmt.Errorf("backend: open diagnostic log: %w", err)
	}
	return io.MultiWriter(os.Stdout, file), file.Close, nil
}

func runBackendConsole(ctx context.Context, app backendApplication, prompt string, timeout time.Duration, input io.Reader, output io.Writer) error {
	logger := newBackendEventLogger(output, time.Now)
	stopEvents := observeBackendEvents(ctx, app, logger)
	defer stopEvents()

	if prompt = strings.TrimSpace(prompt); prompt != "" {
		return submitBackendPrompt(ctx, app, logger, prompt, timeout)
	}

	fmt.Fprintln(output, "[backend] diagnostic console ready; enter one request per line, or /quit")
	scanner := bufio.NewScanner(input)
	for scanner.Scan() {
		text := strings.TrimSpace(scanner.Text())
		if text == "" {
			continue
		}
		if text == "/quit" {
			return nil
		}
		logger.LogSubmit(text)
		if err := app.Submit(ctx, text); err != nil {
			logger.LogSubmitError(err)
		}
	}
	return scanner.Err()
}

func submitBackendPrompt(ctx context.Context, app backendApplication, logger *backendEventLogger, prompt string, timeout time.Duration) error {
	logger.LogSubmit(prompt)
	if err := app.Submit(ctx, prompt); err != nil {
		logger.LogSubmitError(err)
		return err
	}
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	err := app.WaitForIdle(waitCtx)
	cancel()
	logger.LogIdle(err)
	return err
}

func observeBackendEvents(ctx context.Context, app backendApplication, logger *backendEventLogger) func() {
	subscription := app.Subscribe(256)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-subscription.Events:
				if !ok {
					return
				}
				logger.LogEvent(event)
			}
		}
	}()
	return func() {
		subscription.Close()
		<-done
	}
}

type backendEventLogger struct {
	mu             sync.Mutex
	output         io.Writer
	now            func() time.Time
	startedAt      time.Time
	lastSubmission time.Time
	requests       map[string]time.Time
}

func newBackendEventLogger(output io.Writer, now func() time.Time) *backendEventLogger {
	if now == nil {
		now = time.Now
	}
	startedAt := now()
	return &backendEventLogger{output: output, now: now, startedAt: startedAt, requests: make(map[string]time.Time)}
}

func logBackendStartup(logger *backendEventLogger, stage string) {
	if logger != nil {
		logger.LogStage(stage)
	}
}

func (logger *backendEventLogger) LogStage(stage string) {
	logger.mu.Lock()
	defer logger.mu.Unlock()
	fmt.Fprintf(logger.output, "[backend] +%s stage=%s\n", logger.now().Sub(logger.startedAt).Round(time.Millisecond), stage)
}

// LogBashEvent records the process-boundary diagnostics emitted by scopedBash.
// Like the application event logger, it intentionally excludes user command
// text, paths, arguments, and command output.
func (logger *backendEventLogger) LogBashEvent(event seelebridge.BashDiagnosticEvent) {
	logger.mu.Lock()
	defer logger.mu.Unlock()
	if event.Err != nil {
		fmt.Fprintf(logger.output, "[backend] +%s stage=%s shell=%s error_type=%T\n", logger.now().Sub(logger.startedAt).Round(time.Millisecond), event.Stage, event.Shell, event.Err)
		return
	}
	if event.ExitCode != 0 {
		fmt.Fprintf(logger.output, "[backend] +%s stage=%s shell=%s exit_code=%d\n", logger.now().Sub(logger.startedAt).Round(time.Millisecond), event.Stage, event.Shell, event.ExitCode)
		return
	}
	fmt.Fprintf(logger.output, "[backend] +%s stage=%s shell=%s\n", logger.now().Sub(logger.startedAt).Round(time.Millisecond), event.Stage, event.Shell)
}

// LogToolHookEvent records the framework-to-application projection boundary.
// It does not include tool arguments, output, or error text.
func (logger *backendEventLogger) LogToolHookEvent(event application.ToolHookDiagnosticEvent) {
	logger.mu.Lock()
	defer logger.mu.Unlock()
	if event.Err != nil {
		fmt.Fprintf(logger.output, "[backend] +%s stage=%s tool=%s error_type=%T\n", logger.now().Sub(logger.startedAt).Round(time.Millisecond), event.Stage, event.Name, event.Err)
		return
	}
	fmt.Fprintf(logger.output, "[backend] +%s stage=%s tool=%s\n", logger.now().Sub(logger.startedAt).Round(time.Millisecond), event.Stage, event.Name)
}

func (logger *backendEventLogger) LogSubmit(text string) {
	logger.mu.Lock()
	defer logger.mu.Unlock()
	logger.lastSubmission = logger.now()
	fmt.Fprintf(logger.output, "[backend] +0s stage=submit input_chars=%d\n", len([]rune(text)))
}

func (logger *backendEventLogger) LogSubmitError(err error) {
	logger.mu.Lock()
	defer logger.mu.Unlock()
	fmt.Fprintf(logger.output, "[backend] +0s stage=submit.error error=%q\n", err.Error())
}

func (logger *backendEventLogger) LogIdle(err error) {
	logger.mu.Lock()
	defer logger.mu.Unlock()
	if err != nil {
		fmt.Fprintf(logger.output, "[backend] +%s stage=chat.idle.error error=%q\n", logger.elapsedLocked(""), err.Error())
		return
	}
	fmt.Fprintf(logger.output, "[backend] +%s stage=chat.idle\n", logger.elapsedLocked(""))
}

func (logger *backendEventLogger) LogEvent(event application.Event) {
	logger.mu.Lock()
	defer logger.mu.Unlock()
	if event.RequestID != "" {
		if _, ok := logger.requests[event.RequestID]; !ok && event.Kind == application.EventMessageAdded {
			logger.requests[event.RequestID] = logger.requestStartLocked(event.RequestID)
			fmt.Fprintf(logger.output, "[backend] +%s request=%s stage=chat.started\n", logger.elapsedLocked(event.RequestID), event.RequestID)
		}
	}

	switch event.Kind {
	case application.EventToolStarted, application.EventToolCompleted:
		logger.logToolEventLocked(event)
	case application.EventInteractionOpened:
		fmt.Fprintf(logger.output, "[backend] +%s request=%s stage=interaction.opened\n", logger.elapsedLocked(event.RequestID), event.RequestID)
	case application.EventInteractionClosed:
		fmt.Fprintf(logger.output, "[backend] +%s request=%s stage=interaction.closed\n", logger.elapsedLocked(event.RequestID), event.RequestID)
	case application.EventError:
		fmt.Fprintf(logger.output, "[backend] +%s request=%s stage=chat.error payload_bytes=%d\n", logger.elapsedLocked(event.RequestID), event.RequestID, len(event.Payload))
	case application.EventSnapshotChanged:
		fmt.Fprintf(logger.output, "[backend] +%s request=%s stage=snapshot.changed\n", logger.elapsedLocked(event.RequestID), event.RequestID)
	case application.EventSubagentToolStarted, application.EventSubagentToolCompleted:
		fmt.Fprintf(logger.output, "[backend] +%s request=%s stage=%s payload_bytes=%d\n", logger.elapsedLocked(event.RequestID), event.RequestID, event.Kind, len(event.Payload))
	}
}

func (logger *backendEventLogger) logToolEventLocked(event application.Event) {
	var message application.Message
	if err := json.Unmarshal(event.Payload, &message); err != nil || message.Tool == nil {
		fmt.Fprintf(logger.output, "[backend] +%s request=%s stage=%s payload_bytes=%d\n", logger.elapsedLocked(event.RequestID), event.RequestID, event.Kind, len(event.Payload))
		return
	}
	tool := message.Tool
	if event.Kind == application.EventToolStarted {
		fmt.Fprintf(logger.output, "[backend] +%s request=%s stage=tool.started tool=%s\n", logger.elapsedLocked(event.RequestID), event.RequestID, tool.Name)
		return
	}
	fmt.Fprintf(logger.output, "[backend] +%s request=%s stage=tool.completed tool=%s status=%s tool_duration=%s result_bytes=%d error_bytes=%d\n", logger.elapsedLocked(event.RequestID), event.RequestID, tool.Name, tool.Status, tool.Duration, len(tool.Result), len(tool.Error))
}

func (logger *backendEventLogger) requestStartLocked(requestID string) time.Time {
	if started, ok := logger.requests[requestID]; ok {
		return started
	}
	if !logger.lastSubmission.IsZero() {
		return logger.lastSubmission
	}
	return logger.now()
}

func (logger *backendEventLogger) elapsedLocked(requestID string) time.Duration {
	return logger.now().Sub(logger.requestStartLocked(requestID)).Round(time.Millisecond)
}
