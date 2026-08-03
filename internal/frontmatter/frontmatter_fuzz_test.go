package frontmatter

import "testing"

func FuzzParse(f *testing.F) {
	for _, seed := range []string{
		"plain markdown",
		"---\nname: demo\n---\n# Body\n",
		"---\r\nname: windows\r\n---\r\nBody",
		"---\nunterminated: true",
		"---\nlist: [\n---\nBody",
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			t.Skip()
		}
		var metadata map[string]any
		_, _ = Parse(data, &metadata)
	})
}
