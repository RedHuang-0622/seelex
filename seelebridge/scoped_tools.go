package seelebridge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// registerProjectScopedTools overrides the Seele builtin filesystem tools.
// Holder inline providers take precedence over builtin providers with the same
// name, keeping this policy local to Seelex.
func (r *Runtime) registerProjectScopedTools() {
	r.RegisterTool("read_file", "Read a file inside the bound project.", readFileSchema(), r.scopedReadFile)
	r.RegisterTool("grep_search", "Search file contents inside the bound project.", grepSchema(), r.scopedGrep)
	r.RegisterTool("glob", "Find matching files inside the bound project.", globSchema(), r.scopedGlob)
	r.RegisterTool("write_file", "Write a file inside the bound project.", writeFileSchema(), r.scopedWriteFile)
	r.RegisterTool("edit_file", "Edit a file inside the bound project.", editFileSchema(), r.scopedEditFile)
	r.RegisterTool("bash", "Run a command with its working directory constrained to the bound project. This is not an OS sandbox.", bashSchema(), r.scopedBash)
}

type scopedReadFileInput struct {
	Path      string `json:"path"`
	StartLine int    `json:"start_line,omitempty"`
	EndLine   int    `json:"end_line,omitempty"`
}

func (r *Runtime) scopedReadFile(_ context.Context, argsJSON string) (string, error) {
	var input scopedReadFileInput
	if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
		return "", fmt.Errorf("read_file: invalid args: %w", err)
	}
	if input.Path == "" {
		return "", fmt.Errorf("read_file: path is required")
	}
	path, err := r.projectScope.ResolveRead(input.Path)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read_file: read %q: %w", input.Path, err)
	}
	if input.StartLine <= 0 {
		input.StartLine = 1
	}
	lines := strings.Split(string(data), "\n")
	start := input.StartLine - 1
	if start >= len(lines) {
		return "", fmt.Errorf("read_file: start_line %d exceeds file length %d", input.StartLine, len(lines))
	}
	end := len(lines)
	if input.EndLine > 0 && input.EndLine >= input.StartLine && input.EndLine < end {
		end = input.EndLine
	}
	return strings.Join(lines[start:end], "\n"), nil
}

type scopedGrepInput struct {
	Pattern    string `json:"pattern"`
	Path       string `json:"path,omitempty"`
	Glob       string `json:"glob,omitempty"`
	MaxResults int    `json:"max_results,omitempty"`
}
type scopedGrepResult struct {
	Path    string `json:"path"`
	LineNum int    `json:"line_num"`
	Content string `json:"content"`
}

func (r *Runtime) scopedGrep(ctx context.Context, argsJSON string) (string, error) {
	var input scopedGrepInput
	if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
		return "", fmt.Errorf("grep_search: invalid args: %w", err)
	}
	if input.Pattern == "" {
		return "[]", nil
	}
	root, err := r.projectScope.ResolveRead(input.Path)
	if err != nil {
		return "", err
	}
	if input.MaxResults <= 0 {
		input.MaxResults = 20
	}
	results := make([]scopedGrepResult, 0)
	err = filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if info.IsDir() {
			if strings.HasPrefix(info.Name(), ".") && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		if input.Glob != "" {
			matched, matchErr := filepath.Match(input.Glob, info.Name())
			if matchErr != nil || !matched {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		for index, line := range strings.Split(string(data), "\n") {
			if strings.Contains(line, input.Pattern) {
				display, _ := r.projectScope.Relative(path)
				results = append(results, scopedGrepResult{Path: display, LineNum: index + 1, Content: strings.TrimSpace(line)})
				if len(results) >= input.MaxResults {
					return filepath.SkipAll
				}
			}
		}
		return nil
	})
	if err != nil && len(results) == 0 {
		return "", fmt.Errorf("grep_search: walk: %w", err)
	}
	output, err := json.Marshal(results)
	if err != nil {
		return "", fmt.Errorf("grep_search: marshal: %w", err)
	}
	return string(output), nil
}

type scopedGlobInput struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path,omitempty"`
}

func (r *Runtime) scopedGlob(ctx context.Context, argsJSON string) (string, error) {
	var input scopedGlobInput
	if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
		return "", fmt.Errorf("glob: invalid args: %w", err)
	}
	if input.Pattern == "" {
		return "[]", nil
	}
	root, err := r.projectScope.ResolveRead(input.Path)
	if err != nil {
		return "", err
	}
	results := make([]string, 0)
	err = filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		matched, matchErr := filepath.Match(input.Pattern, path)
		if matchErr == nil && matched {
			display, _ := r.projectScope.Relative(path)
			results = append(results, display)
			return nil
		}
		matched, matchErr = filepath.Match(input.Pattern, info.Name())
		if matchErr == nil && matched {
			display, _ := r.projectScope.Relative(path)
			results = append(results, display)
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("glob: walk: %w", err)
	}
	output, err := json.Marshal(results)
	if err != nil {
		return "", fmt.Errorf("glob: marshal: %w", err)
	}
	return string(output), nil
}

type scopedWriteFileInput struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func (r *Runtime) scopedWriteFile(_ context.Context, argsJSON string) (string, error) {
	var input scopedWriteFileInput
	if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
		return "", fmt.Errorf("write_file: invalid args: %w", err)
	}
	if input.Path == "" {
		return "", fmt.Errorf("write_file: path is required")
	}
	path, err := r.projectScope.ResolveWrite(input.Path)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("write_file: create parent: %w", err)
	}
	if err := os.WriteFile(path, []byte(input.Content), 0o644); err != nil {
		return "", fmt.Errorf("write_file: write %q: %w", input.Path, err)
	}
	return fmt.Sprintf(`{"status":"ok","path":%q,"size":%d}`, input.Path, len(input.Content)), nil
}

type scopedEditFileInput struct {
	Path      string `json:"path"`
	OldString string `json:"old_string"`
	NewString string `json:"new_string"`
}

func (r *Runtime) scopedEditFile(_ context.Context, argsJSON string) (string, error) {
	var input scopedEditFileInput
	if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
		return "", fmt.Errorf("edit_file: invalid args: %w", err)
	}
	if input.Path == "" || input.OldString == "" {
		return "", fmt.Errorf("edit_file: path and old_string are required")
	}
	path, err := r.projectScope.ResolveWrite(input.Path)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("edit_file: read %q: %w", input.Path, err)
	}
	content := string(data)
	if !strings.Contains(content, input.OldString) {
		return "", fmt.Errorf("edit_file: old_string not found in %q", input.Path)
	}
	count := strings.Count(content, input.OldString)
	if err := os.WriteFile(path, []byte(strings.ReplaceAll(content, input.OldString, input.NewString)), 0o644); err != nil {
		return "", fmt.Errorf("edit_file: write %q: %w", input.Path, err)
	}
	return fmt.Sprintf(`{"status":"ok","path":%q,"replacements":%d}`, input.Path, count), nil
}

type scopedBashInput struct {
	Command string `json:"command"`
	Timeout int    `json:"timeout,omitempty"`
	Workdir string `json:"workdir,omitempty"`
}
type scopedBashResult struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
}

func (r *Runtime) scopedBash(ctx context.Context, argsJSON string) (string, error) {
	var input scopedBashInput
	if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
		return "", fmt.Errorf("bash: invalid args: %w", err)
	}
	if input.Command == "" {
		return `{"stdout":"","stderr":"","exit_code":0}`, nil
	}
	workdir, err := r.projectScope.ResolveWorkdir(input.Workdir)
	if err != nil {
		return "", err
	}
	shell, flag := "sh", "-c"
	if _, err := os.Stat("/bin/bash"); err == nil {
		shell = "bash"
	} else if _, err := os.Stat(`C:\Windows\System32\cmd.exe`); err == nil {
		shell, flag = "cmd.exe", "/c"
		if _, err := os.Stat(`C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`); err == nil {
			shell, flag = "powershell", "-Command"
		}
	}
	timeout := r.scopedToolTimeout(input.Timeout)
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, shell, flag, input.Command)
	cmd.Dir = workdir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err = cmd.Run()
	if runCtx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("bash: timeout after %v", timeout)
	}
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return "", fmt.Errorf("bash: %w", err)
		}
	}
	output, _ := json.Marshal(scopedBashResult{Stdout: strings.TrimSpace(stdout.String()), Stderr: strings.TrimSpace(stderr.String()), ExitCode: exitCode})
	return string(output), nil
}

func (r *Runtime) scopedToolTimeout(requestedSeconds int) time.Duration {
	timeout := r.toolCallTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	if requestedSeconds > 0 {
		timeout = time.Duration(requestedSeconds) * time.Second
	}
	return timeout
}

func (scope *ProjectScope) Relative(path string) (string, error) {
	root, _, err := scope.roots()
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." {
		return filepath.Clean(path), err
	}
	return rel, nil
}

func readFileSchema() map[string]interface{} {
	return map[string]interface{}{"type": "object", "properties": map[string]interface{}{"path": map[string]interface{}{"type": "string"}, "start_line": map[string]interface{}{"type": "integer"}, "end_line": map[string]interface{}{"type": "integer"}}, "required": []string{"path"}}
}
func grepSchema() map[string]interface{} {
	return map[string]interface{}{"type": "object", "properties": map[string]interface{}{"pattern": map[string]interface{}{"type": "string"}, "path": map[string]interface{}{"type": "string"}, "glob": map[string]interface{}{"type": "string"}, "max_results": map[string]interface{}{"type": "integer"}}, "required": []string{"pattern"}}
}
func globSchema() map[string]interface{} {
	return map[string]interface{}{"type": "object", "properties": map[string]interface{}{"pattern": map[string]interface{}{"type": "string"}, "path": map[string]interface{}{"type": "string"}}, "required": []string{"pattern"}}
}
func writeFileSchema() map[string]interface{} {
	return map[string]interface{}{"type": "object", "properties": map[string]interface{}{"path": map[string]interface{}{"type": "string"}, "content": map[string]interface{}{"type": "string"}}, "required": []string{"path", "content"}}
}
func editFileSchema() map[string]interface{} {
	return map[string]interface{}{"type": "object", "properties": map[string]interface{}{"path": map[string]interface{}{"type": "string"}, "old_string": map[string]interface{}{"type": "string"}, "new_string": map[string]interface{}{"type": "string"}}, "required": []string{"path", "old_string", "new_string"}}
}
func bashSchema() map[string]interface{} {
	return map[string]interface{}{"type": "object", "properties": map[string]interface{}{"command": map[string]interface{}{"type": "string"}, "timeout": map[string]interface{}{"type": "integer"}, "workdir": map[string]interface{}{"type": "string"}}, "required": []string{"command"}}
}
