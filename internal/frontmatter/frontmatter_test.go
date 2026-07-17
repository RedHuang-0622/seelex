package frontmatter

import "testing"

func TestParseWithAndWithoutFrontmatter(t *testing.T) {
	var meta struct {
		Name string `yaml:"name"`
	}
	body, err := Parse([]byte("---\r\nname: test\r\n---\r\n# Body\r\n"), &meta)
	if err != nil || meta.Name != "test" || body != "# Body\n" {
		t.Fatalf("meta=%#v body=%q err=%v", meta, body, err)
	}
	body, err = Parse([]byte("# Plain\n"), &meta)
	if err != nil || body != "# Plain\n" {
		t.Fatalf("plain body=%q err=%v", body, err)
	}
}

func TestParseRejectsInvalidFrontmatter(t *testing.T) {
	var meta map[string]interface{}
	if _, err := Parse([]byte("---\nname: [\n---\nbody\n"), &meta); err == nil {
		t.Fatal("invalid YAML should fail")
	}
	if _, err := Parse([]byte("---\nname: test\n"), &meta); err == nil {
		t.Fatal("missing delimiter should fail")
	}
}
