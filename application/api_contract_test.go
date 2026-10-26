package application_test

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestInboundAPIsDoNotImportConcreteImplementationPackages(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法定位 API 契约测试源码")
	}
	root := filepath.Dir(file)
	apiFiles := []string{
		filepath.Join(root, "application.go"),
		filepath.Join(root, "core", "service_api_faces.go"),
	}
	bannedPrefixes := []string{
		"github.com/RedHuang-0622/seelex/backend/",
		"github.com/RedHuang-0622/seelex/store",
		"github.com/RedHuang-0622/seelex/middle/switch/",
		"github.com/RedHuang-0622/seelex/middle/view/",
	}
	for _, apiFile := range apiFiles {
		parsed, err := parser.ParseFile(token.NewFileSet(), apiFile, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("解析 %s: %v", apiFile, err)
		}
		for _, imported := range parsed.Imports {
			path, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				t.Fatalf("解析 import %s: %v", imported.Path.Value, err)
			}
			for _, prefix := range bannedPrefixes {
				if strings.HasPrefix(path, prefix) {
					t.Errorf("%s 的入站 API 引用了具体实现包 %s", filepath.Base(apiFile), path)
				}
			}
		}
	}
}
