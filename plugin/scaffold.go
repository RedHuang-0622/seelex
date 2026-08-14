package plugin

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// SkillSpec 是 plugin_create / skill_create 的 skill 脚手架入参。
type SkillSpec struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Prompt      string `json:"prompt,omitempty"`
}

// CreateSpec 是 plugin_create 的入参（预定义结构，非 subagent 自拟自由文本）。
type CreateSpec struct {
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	Include     []string    `json:"include,omitempty"`
	Exclude     []string    `json:"exclude,omitempty"`
	Prompt      string      `json:"prompt,omitempty"`
	Skills      []SkillSpec `json:"skills,omitempty"`
	MCPServers  []MCPServer `json:"mcp_servers,omitempty"`
}

// Create 在首个 loader root 下脚手架一个插件目录
// （plugin.md manifest + README.md + 可选 skill 目录），**不改变运行时状态**；
// 写盘后调用 plugins_reload 生效（自迭代闭环：创建 → 加载 → 立即可用）。
func (m *Manager) Create(spec CreateSpec) ([]string, error) {
	if m == nil || m.loader == nil || len(m.loader.roots) == 0 {
		return nil, fmt.Errorf("plugin create: loader root is not configured")
	}
	if !validPluginName.MatchString(spec.Name) {
		return nil, fmt.Errorf("plugin create: invalid plugin name %q", spec.Name)
	}
	root, err := filepath.Abs(m.loader.roots[0])
	if err != nil {
		return nil, fmt.Errorf("plugin create root: %w", err)
	}
	target, err := childWithin(root, spec.Name)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(target); err == nil {
		return nil, fmt.Errorf("plugin %q already exists at %s", spec.Name, target)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("plugin create stat: %w", err)
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return nil, fmt.Errorf("plugin create directory: %w", err)
	}

	manifest := manifest{
		SchemaVersion: CurrentSchemaVersion,
		Name:          spec.Name,
		Description:   spec.Description,
		Include:       append([]string(nil), spec.Include...),
		Exclude:       append([]string(nil), spec.Exclude...),
		MCPServers:    append([]MCPServer(nil), spec.MCPServers...),
	}
	head, err := yaml.Marshal(manifest)
	if err != nil {
		_ = os.RemoveAll(target)
		return nil, fmt.Errorf("plugin create manifest: %w", err)
	}
	body := strings.TrimSpace(spec.Prompt)
	if body == "" {
		body = "# " + spec.Name + "\n\n该插件的职责说明（自迭代生成）。"
	}
	manifestContent := "---\n" + string(head) + "---\n" + body + "\n"
	manifestPath := filepath.Join(target, manifestFile)
	if err := os.WriteFile(manifestPath, []byte(manifestContent), 0o644); err != nil {
		_ = os.RemoveAll(target)
		return nil, fmt.Errorf("plugin create manifest write: %w", err)
	}
	readmePath := filepath.Join(target, "README.md")
	if err := os.WriteFile(readmePath, []byte(pluginReadmeTemplate(spec)), 0o644); err != nil {
		_ = os.RemoveAll(target)
		return nil, fmt.Errorf("plugin create readme: %w", err)
	}

	paths := []string{manifestPath, readmePath}
	for _, skill := range spec.Skills {
		path, err := m.writeSkillDir(target, skill)
		if err != nil {
			_ = os.RemoveAll(target)
			return nil, err
		}
		paths = append(paths, path)
	}
	return paths, nil
}

// CreateSkill 在已有插件的目录下脚手架一个 skill（<plugin>/<skill>/SKILL.md），
// 不改变运行时状态；随后 plugins_reload 生效。
func (m *Manager) CreateSkill(pluginName string, spec SkillSpec) (string, error) {
	if m == nil {
		return "", fmt.Errorf("skill create: manager is nil")
	}
	if !validPluginName.MatchString(pluginName) {
		return "", fmt.Errorf("skill create: invalid plugin name %q", pluginName)
	}
	m.mu.Lock()
	pluginDir := ""
	if loaded, ok := m.plugins[pluginName]; ok {
		pluginDir = loaded.RootDir
	}
	m.mu.Unlock()
	if pluginDir == "" {
		if m.loader == nil || len(m.loader.roots) == 0 {
			return "", fmt.Errorf("skill create: plugin %q not loaded and loader root is not configured", pluginName)
		}
		root, err := filepath.Abs(m.loader.roots[0])
		if err != nil {
			return "", fmt.Errorf("skill create root: %w", err)
		}
		pluginDir, err = childWithin(root, pluginName)
		if err != nil {
			return "", err
		}
		if _, err := os.Stat(pluginDir); err != nil {
			return "", fmt.Errorf("skill create: plugin %q not found at %s", pluginName, pluginDir)
		}
	}
	return m.writeSkillDir(pluginDir, spec)
}

func (m *Manager) writeSkillDir(pluginDir string, spec SkillSpec) (string, error) {
	if !validPluginName.MatchString(spec.Name) {
		return "", fmt.Errorf("skill create: invalid skill name %q", spec.Name)
	}
	target := filepath.Join(pluginDir, spec.Name)
	if _, err := os.Stat(target); err == nil {
		return "", fmt.Errorf("skill %q already exists at %s", spec.Name, target)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("skill create stat: %w", err)
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return "", fmt.Errorf("skill create directory: %w", err)
	}
	description := strings.TrimSpace(spec.Description)
	var builder strings.Builder
	builder.WriteString("---\n")
	builder.WriteString("name: ")
	builder.WriteString(spec.Name)
	builder.WriteString("\n")
	if description != "" {
		builder.WriteString("description: ")
		builder.WriteString(description)
		builder.WriteString("\n")
	}
	builder.WriteString("---\n")
	builder.WriteString(strings.TrimSpace(spec.Prompt))
	builder.WriteString("\n")
	path := filepath.Join(target, "SKILL.md")
	if err := os.WriteFile(path, []byte(builder.String()), 0o644); err != nil {
		_ = os.RemoveAll(target)
		return "", fmt.Errorf("skill create write: %w", err)
	}
	return path, nil
}

// childWithin 校验 name 落在 root 内部（防路径逃逸），返回拼接后的绝对路径。
func childWithin(root, name string) (string, error) {
	target := filepath.Join(root, name)
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes plugin root: %q", name)
	}
	return target, nil
}

func pluginReadmeTemplate(spec CreateSpec) string {
	return fmt.Sprintf(`# Plugin %s

## 模块定位

%s

## 职责与非职责

由插件作者（main agent 自迭代）维护；通过 plugins_reload 生效。

## 文件结构

- plugin.md：manifest（schema_version=%d，机器契约）
- README.md：本文件（生态位与维护方法）
%s
## Review 指南

- manifest 名称/白名单/schema 是否合法；
- include/exclude 是否覆盖预期工具面；
- skill 指令是否有清晰边界。

## 验证

运行 plugins_list 确认可见，激活后冒烟工具链。
`, spec.Name, strings.TrimSpace(spec.Description), CurrentSchemaVersion, skillStructureNote(spec.Skills))
}

func skillStructureNote(skills []SkillSpec) string {
	if len(skills) == 0 {
		return ""
	}
	var builder strings.Builder
	builder.WriteString("- skills/：各 skill 目录（SKILL.md 指令）\n")
	return builder.String()
}
