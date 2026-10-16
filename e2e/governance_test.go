package e2e

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/RedHuang-0622/seelex/internal/testutil"
)

// repoRoot 返回仓库根目录。实现只有一处（`internal/testutil.RepoRoot`，向上找 go.mod），
// 这里只保留 e2e 包内的旧名字，免得改十几个调用点。
func repoRoot() string {
	return testutil.RepoRoot()
}

func TestGitHubConfigurationIsValidYAML(t *testing.T) {
	root := repoRoot()
	files := []string{
		filepath.Join(root, ".github/workflows/release.yml"),
		filepath.Join(root, ".github/dependabot.yml"),
		filepath.Join(root, ".github/ISSUE_TEMPLATE/bug.yml"),
		filepath.Join(root, ".github/ISSUE_TEMPLATE/feature.yml"),
		filepath.Join(root, ".github/ISSUE_TEMPLATE/reproduction.yml"),
		filepath.Join(root, ".github/ISSUE_TEMPLATE/config.yml"),
	}
	workflowFiles, err := filepath.Glob(filepath.Join(root, ".github/workflows/*.yml"))
	if err != nil {
		t.Fatal(err)
	}
	files = append(files, workflowFiles...)
	seen := make(map[string]struct{})
	for _, path := range files {
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var document any
		if err := yaml.Unmarshal(data, &document); err != nil {
			t.Fatalf("%s is invalid YAML: %v", path, err)
		}
	}
}
