// Package skill — 加载和管理 Skill（策略模式的调用方）
// 类似 Claude Code 的 /<skill> 机制
package skill

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Skill 表示一个可调用的技能
type Skill struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Prompt      string `json:"prompt"`
	FilePath    string `json:"file_path"`
}

// Loader 从文件系统加载 Skill（工厂模式）
type Loader struct {
	dirs []string
	mu   sync.RWMutex
}

// NewLoader 创建 Skill 加载器
// dirs: 搜索目录列表，按优先级从高到低
func NewLoader(dirs ...string) *Loader {
	return &Loader{dirs: dirs}
}

// PrimaryDir 返回主目录（优先级最高的可写目录）
func (l *Loader) PrimaryDir() string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if len(l.dirs) > 0 {
		return l.dirs[0]
	}
	return "skills"
}

// LoadAll 从所有目录加载 Skill，按文件名排序
func (l *Loader) LoadAll() ([]Skill, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	seen := make(map[string]Skill) // name → Skill，先加载的优先级高

	for _, dir := range l.dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue // 跳过不可读目录
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			name := strings.TrimSuffix(e.Name(), ".md")
			if _, exists := seen[name]; exists {
				continue // 已有更高优先级同名 Skill
			}

			fpath := filepath.Join(dir, e.Name())
			data, err := os.ReadFile(fpath)
			if err != nil {
				continue
			}

			content := string(data)
			desc := extractDescription(content, name)

			seen[name] = Skill{
				Name:        name,
				Description: desc,
				Prompt:      content,
				FilePath:    fpath,
			}
		}
	}

	skills := make([]Skill, 0, len(seen))
	for _, s := range seen {
		skills = append(skills, s)
	}
	sort.Slice(skills, func(i, j int) bool {
		return skills[i].Name < skills[j].Name
	})
	return skills, nil
}

// Load 加载指定名称的 Skill
func (l *Loader) Load(name string) (*Skill, error) {
	skills, err := l.LoadAll()
	if err != nil {
		return nil, err
	}
	for _, s := range skills {
		if s.Name == name {
			return &s, nil
		}
	}
	return nil, fmt.Errorf("skill %q not found", name)
}

// Create 创建一个新 Skill 并写入 .md 文件到主目录
// 如果 name 已存在则返回错误
func (l *Loader) Create(name, description, prompt string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	dir := l.PrimaryDir()

	// 确保目录存在
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("skill create: mkdir %s: %w", dir, err)
	}

	fpath := filepath.Join(dir, name+".md")

	// 检查是否已存在
	if _, err := os.Stat(fpath); err == nil {
		return fmt.Errorf("skill %q already exists at %s", name, fpath)
	}

	// 构建 Markdown 内容
	content := buildMarkdown(name, description, prompt)

	if err := os.WriteFile(fpath, []byte(content), 0644); err != nil {
		return fmt.Errorf("skill create: write %s: %w", fpath, err)
	}

	return nil
}

// Delete 从主目录删除指定 Skill
func (l *Loader) Delete(name string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	dir := l.PrimaryDir()
	fpath := filepath.Join(dir, name+".md")

	if _, err := os.Stat(fpath); os.IsNotExist(err) {
		return fmt.Errorf("skill %q not found", name)
	}

	if err := os.Remove(fpath); err != nil {
		return fmt.Errorf("skill delete: remove %s: %w", fpath, err)
	}

	return nil
}

// buildMarkdown 构建 Skill 的 Markdown 内容
func buildMarkdown(name, description, prompt string) string {
	var b strings.Builder
	b.WriteString("# " + name + "\n")
	if description != "" {
		b.WriteString("\n" + description + "\n")
	}
	if prompt != "" {
		b.WriteString("\n" + prompt + "\n")
	}
	return b.String()
}

// extractDescription 从 Markdown 内容中提取第一行作为描述
func extractDescription(content string, fallback string) string {
	lines := strings.SplitN(strings.TrimSpace(content), "\n", 2)
	if len(lines) > 0 {
		line := strings.TrimSpace(lines[0])
		// 去掉 Markdown 标题标记
		line = strings.TrimLeft(line, "# ")
		if line != "" {
			return line
		}
	}
	return fallback
}

// Registry Skill 注册表（策略模式容器）
type Registry struct {
	skills  map[string]Skill
	loaders []*Loader
	mu      sync.RWMutex
}

func NewRegistry() *Registry {
	return &Registry{skills: make(map[string]Skill)}
}

func (r *Registry) Register(s Skill) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.skills[s.Name] = s
}

// Remove 从注册表中移除指定 Skill
func (r *Registry) Remove(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.skills, name)
}

func (r *Registry) Get(name string) (Skill, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.skills[name]
	return s, ok
}

func (r *Registry) All() []Skill {
	r.mu.RLock()
	defer r.mu.RUnlock()
	skills := make([]Skill, 0, len(r.skills))
	for _, s := range r.skills {
		skills = append(skills, s)
	}
	sort.Slice(skills, func(i, j int) bool {
		return skills[i].Name < skills[j].Name
	})
	return skills
}

// AddLoader 将 Loader 加载的所有 Skill 注入 Registry，并记录 loader
func (r *Registry) AddLoader(loader *Loader) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.loaders = append(r.loaders, loader)

	skills, err := loader.LoadAll()
	if err != nil {
		return err
	}
	for _, s := range skills {
		r.skills[s.Name] = s
	}
	return nil
}

// Reload 从所有 Loader 重新加载 Skill（保留内存中手动注册的 Skill）
func (r *Registry) Reload() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// 清空现有（重新从 loader 加载）
	r.skills = make(map[string]Skill)

	for _, loader := range r.loaders {
		skills, err := loader.LoadAll()
		if err != nil {
			continue
		}
		for _, s := range skills {
			r.skills[s.Name] = s
		}
	}

	return nil
}

// Count 返回已注册 Skill 数量
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.skills)
}
