package application_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/contract/dto"
	storeteamwork "github.com/RedHuang-0622/seelex/store/teamwork"
)

func TestTeamworkPlanUsesContractIdentity(t *testing.T) {
	if reflect.TypeOf(storeteamwork.Plan{}) != reflect.TypeOf(dto.TeamworkPlan{}) {
		t.Fatal("store TeamworkPlan 必须保留 contract/dto 同身份 alias")
	}
	if reflect.TypeOf(storeteamwork.State{}) != reflect.TypeOf(dto.TeamworkState{}) {
		t.Fatal("store TeamworkState 必须保留 contract/dto 同身份 alias")
	}
}

func TestApplicationExecutionHasNoTeamworkPlanAlias(t *testing.T) {
	applicationDir := sourceApplicationDir(t)
	executionDir := filepath.Join(applicationDir, "execution")
	err := filepath.WalkDir(executionDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(filepath.Base(path), "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		for _, declaration := range file.Decls {
			general, ok := declaration.(*ast.GenDecl)
			if !ok || general.Tok != token.TYPE {
				continue
			}
			for _, spec := range general.Specs {
				typeSpec := spec.(*ast.TypeSpec)
				if typeSpec.Name.Name == "TeamworkPlan" || typeSpec.Name.Name == "TeamworkState" {
					t.Errorf("%s 仍声明 TeamworkPlan/State 迁移 alias", path)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("扫描 application/execution: %v", err)
	}
}
