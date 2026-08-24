package websearch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeRegistrar 记录注册的 web_search 工具，便于单元测试触发 handler。
type fakeRegistrar struct {
	name        string
	description string
	handler     func(context.Context, string) (string, error)
}

func (f *fakeRegistrar) RegisterTool(name, description string, inputSchema map[string]interface{}, handler func(context.Context, string) (string, error)) {
	f.name = name
	f.description = description
	f.handler = handler
}

func writeAccounts(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "accounts.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRegister_NoConfigRegistersPlaceholder(t *testing.T) {
	r := &fakeRegistrar{}
	Register(r, filepath.Join(t.TempDir(), "missing.yaml"))
	if r.name != "web_search" {
		t.Fatalf("expected web_search tool, got %q", r.name)
	}
	if r.handler == nil {
		t.Fatal("expected placeholder handler")
	}
	out, err := r.handler(context.Background(), `{"query":"x"}`)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]string
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("placeholder must return JSON: %v", err)
	}
	if !strings.Contains(payload["error"], "未装配可用代理策略") {
		t.Errorf("unexpected placeholder payload: %s", out)
	}
}

func TestRegister_MinimalStrategyEndToEnd(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 4096)
		n, _ := r.Body.Read(buf)
		if !strings.Contains(string(buf[:n]), `"query":"golang"`) {
			t.Errorf("expected query golang in POST body, got %s", string(buf[:n]))
		}
		_, _ = w.Write([]byte(`{"results":[{"title":"Go","url":"https://go.dev","content":"语言"}]}`))
	}))
	defer srv.Close()

	content := `
websearch:
  max_results: 3
  strategies:
    - name: local
      endpoint: ` + srv.URL + `
      apikey: test-key
`
	r := &fakeRegistrar{}
	Register(r, writeAccounts(t, content))
	if r.name != "web_search" || r.handler == nil {
		t.Fatalf("expected registered handler, got %+v", r)
	}
	out, err := r.handler(context.Background(), `{"query":"golang","max_results":3}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "## 搜索结果") || !strings.Contains(out, "https://go.dev") {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestRegister_EmptyQueryRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"results":[]}`))
	}))
	defer srv.Close()

	content := `
websearch:
  strategies:
    - name: local
      endpoint: ` + srv.URL + `
      apikey: test-key
`
	r := &fakeRegistrar{}
	Register(r, writeAccounts(t, content))
	if _, err := r.handler(context.Background(), `{"query":"  "}`); err == nil {
		t.Fatal("expected error for empty query")
	}
}

func TestToolSchema(t *testing.T) {
	schema := toolSchema()
	props, ok := schema["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected properties, got %#v", schema)
	}
	if _, ok := props["query"]; !ok {
		t.Error("expected query property")
	}
	if _, ok := props["max_results"]; !ok {
		t.Error("expected max_results property")
	}
}
