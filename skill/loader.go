package skill

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/RedHuang-0622/seelex/internal/frontmatter"
)

const instructionFile = "SKILL.md"

var validName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

type metadata struct {
	Description string `yaml:"description"`
}

// Loader discovers skills from ordered root directories.
type Loader struct {
	dirs []string
	mu   sync.RWMutex
}

func NewLoader(dirs ...string) *Loader {
	cleaned := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		if path := strings.TrimSpace(dir); path != "" {
			cleaned = append(cleaned, filepath.Clean(path))
		}
	}
	return &Loader{dirs: cleaned}
}

func (l *Loader) PrimaryDir() string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.primaryDirLocked()
}

func (l *Loader) LoadAll() ([]Skill, error) {
	l.mu.RLock()
	dirs := append([]string(nil), l.dirs...)
	l.mu.RUnlock()

	seen := make(map[string]Skill)
	for _, dir := range dirs {
		if err := loadRoot(dir, seen); err != nil {
			return nil, err
		}
	}
	return sortedSkills(seen), nil
}

func (l *Loader) Load(name string) (*Skill, error) {
	if err := validateName(name); err != nil {
		return nil, err
	}
	skills, err := l.LoadAll()
	if err != nil {
		return nil, err
	}
	for i := range skills {
		if skills[i].Name == name {
			return &skills[i], nil
		}
	}
	return nil, fmt.Errorf("skill %q not found", name)
}

func (l *Loader) Create(name, description, prompt string) error {
	if err := validateName(name); err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	root := l.primaryDirLocked()
	if err := os.MkdirAll(root, 0o755); err != nil {
		return fmt.Errorf("skill create root: %w", err)
	}
	target, err := childPath(root, name)
	if err != nil {
		return err
	}
	if _, err := os.Stat(target); err == nil {
		return fmt.Errorf("skill %q already exists at %s", name, target)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("skill create stat: %w", err)
	}
	if err := os.Mkdir(target, 0o755); err != nil {
		return fmt.Errorf("skill create directory: %w", err)
	}
	content := buildMarkdown(name, description, prompt)
	if err := os.WriteFile(filepath.Join(target, instructionFile), []byte(content), 0o644); err != nil {
		_ = os.Remove(target)
		return fmt.Errorf("skill create instructions: %w", err)
	}
	return nil
}

func (l *Loader) Delete(name string) error {
	if err := validateName(name); err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	root := l.primaryDirLocked()
	target, err := childPath(root, name)
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(target, instructionFile)); err == nil {
		if err := os.RemoveAll(target); err != nil {
			return fmt.Errorf("skill delete directory: %w", err)
		}
		return nil
	}

	legacy := filepath.Join(root, name+".md")
	if _, err := os.Stat(legacy); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("skill %q not found", name)
		}
		return fmt.Errorf("skill delete stat: %w", err)
	}
	if err := os.Remove(legacy); err != nil {
		return fmt.Errorf("skill delete legacy file: %w", err)
	}
	return nil
}

func (l *Loader) primaryDirLocked() string {
	if len(l.dirs) == 0 {
		return "skills"
	}
	return l.dirs[0]
}

func loadRoot(root string, seen map[string]Skill) error {
	return loadRootFiltered(root, seen, true)
}

func loadRootFiltered(root string, seen map[string]Skill, includeLegacy bool) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("skill load root %s: %w", root, err)
	}

	for _, entry := range entries {
		if !entry.IsDir() || !validName.MatchString(entry.Name()) {
			continue
		}
		if _, exists := seen[entry.Name()]; exists {
			continue
		}
		path := filepath.Join(root, entry.Name(), instructionFile)
		s, err := readSkill(entry.Name(), path, filepath.Dir(path), false)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		seen[s.Name] = s
	}

	if !includeLegacy {
		return nil
	}
	for _, entry := range entries {
		if entry.IsDir() || strings.EqualFold(entry.Name(), instructionFile) || filepath.Ext(entry.Name()) != ".md" {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		if !validName.MatchString(name) {
			continue
		}
		if _, exists := seen[name]; exists {
			continue
		}
		path := filepath.Join(root, entry.Name())
		s, err := readSkill(name, path, root, true)
		if err != nil {
			return err
		}
		seen[s.Name] = s
	}
	return nil
}

// LoadPluginDir loads skill directories from a plugin root (no legacy flat files).
// This avoids accidentally treating plugin.md as a legacy skill.
func LoadPluginDir(dir string) ([]Skill, error) {
	seen := make(map[string]Skill)
	if err := loadRootFiltered(dir, seen, false); err != nil {
		return nil, err
	}
	return sortedSkills(seen), nil
}

func readSkill(name, path, root string, legacy bool) (Skill, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Skill{}, err
	}
	var meta metadata
	body, err := frontmatter.Parse(data, &meta)
	if err != nil {
		return Skill{}, fmt.Errorf("skill %q: %w", name, err)
	}
	description := strings.TrimSpace(meta.Description)
	if description == "" {
		description = extractDescription(body, name)
	}
	return Skill{
		Name:           name,
		Description:    description,
		Prompt:         strings.TrimSpace(body),
		FilePath:       path,
		RootDir:        root,
		LegacyFlatFile: legacy,
	}, nil
}

func validateName(name string) error {
	if !validName.MatchString(name) {
		return fmt.Errorf("invalid skill name %q", name)
	}
	return nil
}

func childPath(root, name string) (string, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("skill root: %w", err)
	}
	target := filepath.Join(rootAbs, name)
	rel, err := filepath.Rel(rootAbs, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("skill path escapes root")
	}
	return target, nil
}

func buildMarkdown(name, description, prompt string) string {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("description: ")
	b.WriteString(fmt.Sprintf("%q", description))
	b.WriteString("\n---\n\n# ")
	b.WriteString(name)
	if strings.TrimSpace(prompt) != "" {
		b.WriteString("\n\n")
		b.WriteString(strings.TrimSpace(prompt))
	}
	b.WriteString("\n")
	return b.String()
}

func extractDescription(content, fallback string) string {
	lines := strings.Split(strings.TrimSpace(content), "\n")
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		line = strings.TrimSpace(strings.TrimLeft(line, "#"))
		if line != "" {
			return line
		}
	}
	return fallback
}
