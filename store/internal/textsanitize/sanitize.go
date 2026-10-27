// Package textsanitize owns the text-cleaning rules shared by commit-able derived
// views such as the project board and project-known markdown.
package textsanitize

import (
	"path/filepath"
	"regexp"
	"strings"
)

var (
	workspaceToken = regexp.MustCompile(`ws-[0-9]{6,}`)
	absoluteToken  = regexp.MustCompile(`(^|[\s（("'` + "`" + `])(?:[A-Za-z]:[\\/]|/)[^\s，。；、）)"'` + "`" + `]+`)
)

// KnownText removes machine-local workspace IDs and absolute paths from prose.
func KnownText(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = workspaceToken.ReplaceAllString(value, "ws-<workspace>")
	value = absoluteToken.ReplaceAllString(value, "$1<abs-path>")
	return strings.TrimSpace(value)
}

// RepoRelativePath validates and normalizes a repository-relative path.
func RepoRelativePath(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" || workspaceToken.MatchString(value) {
		return "", false
	}
	normalized := strings.ReplaceAll(value, "\\", "/")
	if strings.HasPrefix(normalized, "/") || filepath.IsAbs(value) {
		return "", false
	}
	if len(normalized) >= 2 && normalized[1] == ':' {
		return "", false
	}
	normalized = strings.TrimPrefix(normalized, "./")
	if normalized == "" || normalized == "." {
		return "", false
	}
	for _, segment := range strings.Split(normalized, "/") {
		if segment == ".." {
			return "", false
		}
	}
	return normalized, true
}
