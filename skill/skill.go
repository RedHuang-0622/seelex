// Package skill loads and manages Seelex directory-based skills.
package skill

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Skill is a prompt package rooted at a directory containing SKILL.md.
type Skill struct {
	Name           string `json:"name"`
	Description    string `json:"description"`
	Prompt         string `json:"prompt"`
	FilePath       string `json:"file_path"`
	RootDir        string `json:"root_dir"`
	LegacyFlatFile bool   `json:"legacy_flat_file,omitempty"`
}

// ResourcePath resolves a skill-relative resource without allowing root escape.
func (s Skill) ResourcePath(relative string) (string, error) {
	if s.RootDir == "" {
		return "", fmt.Errorf("skill %q has no root directory", s.Name)
	}
	if filepath.IsAbs(relative) {
		return "", fmt.Errorf("skill resource path must be relative")
	}
	root, err := filepath.Abs(s.RootDir)
	if err != nil {
		return "", fmt.Errorf("skill resource root: %w", err)
	}
	target, err := filepath.Abs(filepath.Join(root, filepath.Clean(relative)))
	if err != nil {
		return "", fmt.Errorf("skill resource path: %w", err)
	}
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("skill resource escapes root")
	}
	return target, nil
}

// Registry combines global skills with the currently active plugin skills.
type Registry struct {
	mu           sync.RWMutex
	manual       map[string]Skill
	loaded       map[string]Skill
	loaders      []*Loader
	pluginSkills map[string]map[string]Skill
	activePlugin string
}

func NewRegistry() *Registry {
	return &Registry{
		manual:       make(map[string]Skill),
		loaded:       make(map[string]Skill),
		pluginSkills: make(map[string]map[string]Skill),
	}
}

func (r *Registry) Register(s Skill) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.manual[s.Name] = s
}

func (r *Registry) Remove(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.manual, name)
	delete(r.loaded, name)
}

func (r *Registry) Get(name string) (Skill, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.activePlugin != "" {
		skills := r.pluginSkills[r.activePlugin]
		if s, ok := skills[name]; ok {
			return s, true
		}
		return Skill{}, false
	}
	if s, ok := r.manual[name]; ok {
		return s, true
	}
	s, ok := r.loaded[name]
	return s, ok
}

func (r *Registry) All() []Skill {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.activePlugin != "" {
		skills := r.pluginSkills[r.activePlugin]
		return sortedSkills(skills)
	}
	merged := make(map[string]Skill, len(r.loaded)+len(r.manual))
	for name, s := range r.loaded {
		merged[name] = s
	}
	for name, s := range r.manual {
		merged[name] = s
	}
	return sortedSkills(merged)
}

func (r *Registry) AddLoader(loader *Loader) error {
	skills, err := loader.LoadAll()
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.loaders = append(r.loaders, loader)
	for _, s := range skills {
		r.loaded[s.Name] = s
	}
	return nil
}

func (r *Registry) Reload() error {
	r.mu.RLock()
	loaders := append([]*Loader(nil), r.loaders...)
	r.mu.RUnlock()

	loaded := make(map[string]Skill)
	for _, loader := range loaders {
		skills, err := loader.LoadAll()
		if err != nil {
			return err
		}
		for _, s := range skills {
			if _, exists := loaded[s.Name]; !exists {
				loaded[s.Name] = s
			}
		}
	}
	r.mu.Lock()
	r.loaded = loaded
	r.mu.Unlock()
	return nil
}

func (r *Registry) SetPluginSkills(pluginName string, skills []Skill) {
	mapped := make(map[string]Skill, len(skills))
	for _, s := range skills {
		mapped[s.Name] = s
	}
	r.mu.Lock()
	r.pluginSkills[pluginName] = mapped
	r.mu.Unlock()
}

func (r *Registry) ClearPluginSkills(pluginName string) {
	r.mu.Lock()
	delete(r.pluginSkills, pluginName)
	if r.activePlugin == pluginName {
		r.activePlugin = ""
	}
	r.mu.Unlock()
}

func (r *Registry) ActivatePlugin(pluginName string) {
	r.mu.Lock()
	r.activePlugin = pluginName
	r.mu.Unlock()
}

func (r *Registry) ActivatePluginSkills(pluginName string) error {
	r.ActivatePlugin(pluginName)
	return nil
}

func (r *Registry) DeactivatePlugin() {
	r.mu.Lock()
	r.activePlugin = ""
	r.mu.Unlock()
}

func (r *Registry) DeactivatePluginSkills() error {
	r.DeactivatePlugin()
	return nil
}

func (r *Registry) PublishPluginSkills(pluginName string, skills []Skill) error {
	r.SetPluginSkills(pluginName, skills)
	return nil
}

func (r *Registry) Count() int { return len(r.All()) }

func sortedSkills(skills map[string]Skill) []Skill {
	result := make([]Skill, 0, len(skills))
	for _, s := range skills {
		result = append(result, s)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}
