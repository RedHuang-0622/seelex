package seelebridge

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ── YAML 结构 ──────────────────────────────────────────────────────

type pathZoneDefault struct {
	Read   string `yaml:"read"`
	Write  string `yaml:"write"`
	Reason string `yaml:"reason"`
}

type pathGateFile struct {
	Permission struct {
		Zones       []pathZone     `yaml:"zones"`
		DefaultZone pathZoneDefault `yaml:"default_zone"`
	} `yaml:"permission"`
}

type pathZone struct {
	Prefix string       `yaml:"prefix"`
	Read   string       `yaml:"read"`   // "allow" | "deny"
	Write  string       `yaml:"write"`  // "allow" | "deny"
	Reason string       `yaml:"reason"`
	Scope  string       `yaml:"scope,omitempty"` // "workspace" etc
	rule   pathZoneRule // parsed
}

type pathZoneRule struct {
	Read  bool
	Write bool
}

// ── PathGate ────────────────────────────────────────────────────────

// PathGate enforces path-based read/write access based on prefix zones.
// Zones are matched in order (first match wins). Unmatched paths use the default zone.
type PathGate struct {
	zones       []pathZone
	defaultRule pathZoneRule
	workspace   string // injected at runtime via BindWorkspace
}

// LoadPathGate reads permission zones from a seele.yaml file.
func LoadPathGate(yamlPath string) (*PathGate, error) {
	data, err := os.ReadFile(yamlPath)
	if err != nil {
		// 配置文件不存在 = 不启用路径门控，全部放行
		if os.IsNotExist(err) {
			return &PathGate{defaultRule: pathZoneRule{Read: true, Write: true}}, nil
		}
		return nil, fmt.Errorf("pathgate: read config: %w", err)
	}

	var file pathGateFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("pathgate: parse config: %w", err)
	}

	gate := &PathGate{}
	for _, z := range file.Permission.Zones {
		z.rule = parseRule(z.Read, z.Write)
		gate.zones = append(gate.zones, z)
	}
	gate.defaultRule = parseRule(file.Permission.DefaultZone.Read, file.Permission.DefaultZone.Write)
	return gate, nil
}

func parseRule(read, write string) pathZoneRule {
	return pathZoneRule{
		Read:  strings.EqualFold(read, "allow"),
		Write: strings.EqualFold(write, "allow"),
	}
}

// BindWorkspace registers the workspace root path for zone matching.
// The workspace zone is prepended so it matches before static zones.
func (gate *PathGate) BindWorkspace(rootPath string) {
	abs, err := filepath.Abs(rootPath)
	if err != nil {
		return
	}
	gate.workspace = filepath.ToSlash(abs)
}

// UnbindWorkspace clears the workspace path.
func (gate *PathGate) UnbindWorkspace() {
	gate.workspace = ""
}

// AllowRead checks whether a read on the given path is permitted.
func (gate *PathGate) AllowRead(rawPath string) (bool, string) {
	return gate.check(rawPath, false)
}

// AllowWrite checks whether a write on the given path is permitted.
func (gate *PathGate) AllowWrite(rawPath string) (bool, string) {
	return gate.check(rawPath, true)
}

func (gate *PathGate) check(rawPath string, write bool) (bool, string) {
	normalized, err := normalizePath(rawPath)
	if err != nil {
		return false, fmt.Sprintf("非法路径: %v", err)
	}

	// 1. 检查 workspace zone（动态注入，优先级最高）
	if gate.workspace != "" && strings.HasPrefix(normalized, gate.workspace+"/") {
		return true, ""
	}

	// 2. 遍历静态 zones（按配置顺序）
	for _, z := range gate.zones {
		if strings.HasPrefix(normalized, z.Prefix) {
			if write {
				return z.rule.Write, z.Reason
			}
			return z.rule.Read, z.Reason
		}
	}

	// 3. 默认 zone
	if write {
		return gate.defaultRule.Write, "路径不在允许范围内"
	}
	return gate.defaultRule.Read, "路径不在允许范围内"
}

// ── 路径规范化 ────────────────────────────────────────────────────

func normalizePath(raw string) (string, error) {
	path := strings.TrimSpace(raw)
	if path == "" {
		return "", fmt.Errorf("empty path")
	}

	// 1. 控制字符检查
	for _, r := range path {
		if r < 0x20 && r != '\t' && r != '\n' && r != '\r' {
			return "", fmt.Errorf("path contains control character U+%04X", r)
		}
	}
	if strings.ContainsRune(path, 0x00) {
		return "", fmt.Errorf("path contains NUL byte")
	}

	// 2. 统一分隔符为 /
	path = filepath.ToSlash(path)

	// 3. 拒绝绝对路径
	if strings.HasPrefix(path, "/") || (len(path) >= 2 && path[1] == ':') {
		return "", fmt.Errorf("absolute paths are not allowed")
	}

	// 4. 拒绝路径逃逸
	parts := strings.Split(path, "/")
	var clean []string
	for _, p := range parts {
		if p == ".." {
			return "", fmt.Errorf("path traversal detected: %q", raw)
		}
		if p != "." && p != "" {
			clean = append(clean, strings.ToLower(p))
		}
	}

	return filepath.ToSlash(filepath.Join(clean...)), nil
}
