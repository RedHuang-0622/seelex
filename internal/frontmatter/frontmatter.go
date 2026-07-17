// Package frontmatter parses optional YAML frontmatter from Markdown files.
package frontmatter

import (
	"bytes"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

const delimiter = "---"

// Parse unmarshals YAML frontmatter into dst and returns the Markdown body.
// Files without frontmatter are returned unchanged.
func Parse(data []byte, dst any) (string, error) {
	normalized := bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
	text := string(normalized)
	if !strings.HasPrefix(text, delimiter+"\n") {
		return text, nil
	}

	rest := text[len(delimiter)+1:]
	end := strings.Index(rest, "\n"+delimiter+"\n")
	if end < 0 {
		return "", fmt.Errorf("frontmatter: missing closing delimiter")
	}
	if dst != nil {
		if err := yaml.Unmarshal([]byte(rest[:end]), dst); err != nil {
			return "", fmt.Errorf("frontmatter: parse yaml: %w", err)
		}
	}
	return rest[end+len("\n"+delimiter+"\n"):], nil
}
