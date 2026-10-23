package search

import (
	"database/sql"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/store"
)

// TestSessionNotFoundCriterionMatchesStore 钉住「会话不存在」判据的唯一性
// （AGENTS §8.3：同一判据、同一语义在第二处出现即视为缺陷）。
//
// 为什么以 store 的语义为准（证据，不是偏好）：
//
//   - `store/sessionstore.go` 只有一种存储形态（jsonl + json v8），`Open` 不做
//     后端分派，也没有 SQL/Redis 链路。也就是说今天没有任何代码路径能产生
//     `sql.ErrNoRows`。
//   - `store` 侧对「会话不存在」的判据只有一条：`errors.Is(err, fs.ErrNotExist)`
//     （`store/durable_history.go`）。store 是「未找到」错误的编码者，判据属于它。
//
// 本包此前多认一个 `sql.ErrNoRows`，同一份输入在两个判据下会得到两种答案
// （P10 判据漂移）。本用例的期望值 = store 的判决，因此修复前必须红——
// 修复前的红灯输出见 `docs/2026-10-15-architecture-reshape/p10-record.md`。
//
// 修复方式是把本包的副本删掉、改为调用 `store.IsSessionNotFound`；本用例的
// 断言表与期望值**一字未改**，只换了被测函数的落点（判据唯一化，不是"改测试
// 迁就实现"）。本包内的 `isSessionNotFound` 已不存在，因此这条用例同时钉住
// "第二份实现不许回来"。
func TestSessionNotFoundCriterionMatchesStore(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "fs.ErrNotExist", err: fs.ErrNotExist, want: true},
		{
			name: "包装后的 fs.ErrNotExist",
			err:  fmt.Errorf("load event tail: %w", fs.ErrNotExist),
			want: true,
		},
		{name: "sql.ErrNoRows（后端已退役，无此路径）", err: sql.ErrNoRows, want: false},
		{
			name: "包装后的 sql.ErrNoRows",
			err:  fmt.Errorf("load: %w", sql.ErrNoRows),
			want: false,
		},
		{name: "其他错误", err: errors.New("boom"), want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := store.IsSessionNotFound(tc.err); got != tc.want {
				t.Errorf("store.IsSessionNotFound(%v) = %v, want %v（会话不存在判据以 store 为准）",
					tc.err, got, tc.want)
			}
		})
	}
}

// TestSessionNotFoundCriterionHasNoLocalCopy 钉「判据唯一」：本包不得再定义
// 自己的"会话不存在"判据（判据落在 store，见 store/durable_history.go）。
// 用一个显然不存在的本地符号做编译期锚点会打红整包，所以这里用 AST 断言：
// 包内定义集合里不含任何"会话不存在"判据形状的函数。
//
// 机械扫描在 e2e（`e2e/session_not_found_single_criterion_test.go`）里做，
// 那条用例扫全仓，覆盖面更广；这里只保留本包的自证。
func TestSessionNotFoundCriterionHasNoLocalCopy(t *testing.T) {
	for _, name := range []string{"isSessionNotFound", "sessionNotFound", "IsSessionNotFound"} {
		if definesFuncNamed(t, ".", name) {
			t.Errorf("本包定义了 %s：判据必须只有一处（store.IsSessionNotFound），"+
				"见 AGENTS §8.3 与 P10", name)
		}
	}
}

// definesFuncNamed 报告目录下（不含测试文件自身）是否声明了名为 name 的函数。
func definesFuncNamed(t *testing.T, dir, name string) bool {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir %s: %v", dir, err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		for _, decl := range parsed.Decls {
			function, ok := decl.(*ast.FuncDecl)
			if ok && function.Recv == nil && function.Name.Name == name {
				return true
			}
		}
	}
	return false
}
