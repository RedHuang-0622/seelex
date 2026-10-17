package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTestTree(t *testing.T, root string, paths []string) {
	t.Helper()
	for _, path := range paths {
		full := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestListTreeSortsDirsFirstAndCountsFiles(t *testing.T) {
	root := t.TempDir()
	writeTestTree(t, root, []string{
		"a.txt", "z.md", "src/main.go", "src/util.go", "docs/guide.md",
	})
	repo := NewRepo()

	listing, err := repo.ListTree(root, "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if listing.Truncated {
		t.Fatal("unexpected truncated listing")
	}
	got := make([]string, 0, len(listing.Entries))
	for _, entry := range listing.Entries {
		got = append(got, entry.Path)
	}
	want := []string{"docs", "src", "a.txt", "z.md"}
	if len(got) != len(want) {
		t.Fatalf("paths=%v want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("paths=%v want %v", got, want)
		}
	}
	for _, entry := range listing.Entries {
		if entry.Path == "docs" && entry.Count != 1 {
			t.Fatalf("docs count=%d want 1", entry.Count)
		}
		if entry.Path == "src" && entry.Count != 2 {
			t.Fatalf("src count=%d want 2", entry.Count)
		}
	}
}

func TestListTreeFiltersIgnoredAndSensitiveEntries(t *testing.T) {
	root := t.TempDir()
	writeTestTree(t, root, []string{
		"README.md",
		".git/config",
		".seelex/session.json",
		"dist/app.js",
		"node_modules/pkg/index.js",
		"config/accounts.yaml",
		"config/accounts.local.yaml",
		"config/plain.yaml",
	})
	repo := NewRepo()

	listing, err := repo.ListTree(root, "", 1)
	if err != nil {
		t.Fatal(err)
	}
	// 可见：config（dir）、README.md；config/plain.yaml 在下一级。
	got := make([]string, 0, len(listing.Entries))
	for _, entry := range listing.Entries {
		got = append(got, entry.Path)
	}
	if len(got) != 2 || got[0] != "config" || got[1] != "README.md" {
		t.Fatalf("paths=%v want [config README.md]", got)
	}

	config, err := repo.ListTree(root, "config", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(config.Entries) != 1 || config.Entries[0].Name != "plain.yaml" {
		t.Fatalf("config entries=%+v want only plain.yaml", config.Entries)
	}
}

func TestListTreeRejectsEscapesAndAbsolutePaths(t *testing.T) {
	root := t.TempDir()
	repo := NewRepo()
	for _, rel := range []string{"..", "../other", "sub/../../outside", `C:\Windows`, "/etc"} {
		if _, err := repo.ListTree(root, rel, 1); err == nil {
			t.Fatalf("ListTree accepted escape path %q", rel)
		}
	}
}

func TestCountFilesRespectsIgnoresAndSensitiveNames(t *testing.T) {
	root := t.TempDir()
	writeTestTree(t, root, []string{
		"a.go", "b/c.go", "b/d/e.go",
		".git/hooks/pre-commit",
		"dist/bundle.js",
		"node_modules/lib/x.js",
		"config/accounts.yaml",
		"config/local.local.yaml",
	})
	repo := NewRepo()

	count, err := repo.CountFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	// 可见文件：a.go、b/c.go、b/d/e.go（3）；目录：b、b/d、config（config
	// 下文件全部敏感被排除，但目录本身仍计数）；忽略 .git/dist/node_modules。
	if count.Files != 3 || count.Dirs != 3 || count.Truncated {
		t.Fatalf("count=%+v want files=3 dirs=3", count)
	}
}

func TestCountFilesErrorsOnMissingRoot(t *testing.T) {
	repo := NewRepo()
	if _, err := repo.CountFiles(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("CountFiles accepted missing root")
	}
}

func TestListTreeNestedDepth(t *testing.T) {
	root := t.TempDir()
	writeTestTree(t, root, []string{"a/b/c/d.txt", "a/x.txt"})
	repo := NewRepo()

	listing, err := repo.ListTree(root, "a", 2)
	if err != nil {
		t.Fatal(err)
	}
	paths := make(map[string]bool)
	for _, entry := range listing.Entries {
		paths[entry.Path] = true
	}
	if !paths["a/b"] || !paths["a/x.txt"] {
		t.Fatalf("depth=2 listing=%v missing a/b or a/x.txt", paths)
	}
	if !paths["a/b/c"] {
		t.Fatalf("depth=2 listing=%v must include a/b/c", paths)
	}
	if paths["a/b/c/d.txt"] {
		t.Fatalf("depth=2 must not include a/b/c/d.txt (depth 3): %v", paths)
	}
}

// TestListTreePathsAreRootRelativeAndUnique 验证条目路径相对工作区根（跨
// 目录展开键唯一，前端惰性展开不会因同名子目录冲突）。
func TestListTreePathsAreRootRelativeAndUnique(t *testing.T) {
	root := t.TempDir()
	writeTestTree(t, root, []string{"src/main.go", "docs/src/guide.md", "a.txt"})
	repo := NewRepo()

	rootListing, err := repo.ListTree(root, "", 1)
	if err != nil {
		t.Fatal(err)
	}
	srcListing, err := repo.ListTree(root, "docs", 1)
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]bool)
	for _, entry := range append(rootListing.Entries, srcListing.Entries...) {
		if seen[entry.Path] {
			t.Fatalf("duplicate path %q", entry.Path)
		}
		seen[entry.Path] = true
	}
	if !seen["docs/src"] || !seen["a.txt"] || !seen["src"] {
		t.Fatalf("unexpected paths: %v", seen)
	}
	if len(srcListing.Entries) != 1 || srcListing.Entries[0].Path != "docs/src" {
		t.Fatalf("docs listing must be root-relative: %+v", srcListing.Entries)
	}
}
