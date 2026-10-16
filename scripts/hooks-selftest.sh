#!/usr/bin/env bash
# .githooks 的**自测**（hermetic：在临时仓库里跑，不碰本仓库、不构建 30MB 二进制）。
#
# 为什么要有它：钩子是"人看不见的红线"——写坏了不会有人发现，直到某次提交把坏文件
# 放进去、或某次纯文档提交莫名其妙重建了两个二进制。这里把每条判据都跑一遍：
#   pre-commit：CRLF / BOM / 未格式化 / 禁入路径（tmp/）/ 真实配置；纯文档改动能过。
#   post-commit：**决策**本身——纯文档与纯用例提交必须跳过构建，源码/依赖改动必须构建。
#               构建命令用 SEELEX_BUILD_COMMAND 换成"写一行标记"的假脚本，因此自测
#               断言的是"钩子决定要构建"，不是"构建真的成功"（那是 pre-push 的职责）。
#
# 用法：bash scripts/hooks-selftest.sh          （退出码 0 = 全部通过）
set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
HOOKS="$REPO_ROOT/.githooks"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

pass=0
fail=0
note() { printf '%s\n' "$*"; }

# 守卫：任何写入都必须发生在临时目录里（曾经在 cd 失败的路径上把探针文件写进了仓库根，
# 那次由 pre-push 的 go build 抓出来——这里把判据前移）。
[[ -n "$WORK" && -d "$WORK" ]] || { echo "hooks-selftest: mktemp 失败" >&2; exit 1; }
cd "$WORK" || exit 1
printf 'sentinel\n' > "$WORK/.selftest-workdir" || exit 1
[[ -f "$WORK/.selftest-workdir" && "$(pwd)" == "$WORK"* ]] || { echo "hooks-selftest: 未能进入临时目录" >&2; exit 1; }
git init -q .
git config user.email "hooks-selftest@example.invalid"
git config user.name "hooks selftest"
git config core.hooksPath "$HOOKS"

marker="$WORK/build-invoked"
fake_build="$WORK/fake-build.sh"
cat > "$fake_build" <<'FAKE'
#!/usr/bin/env bash
echo "built" >> "$MARKER_FILE"
FAKE
chmod +x "$fake_build"

# ── pre-commit ────────────────────────────────────────────────────────────────
unstage_all() { git reset -q >/dev/null 2>&1 || true; }

expect_pre_commit() {  # expect_pre_commit <名称> <block|pass> [额外命令…]
  local name="$1" expect="$2"; shift 2
  local out rc
  out="$("$@" 2>&1)"; rc=$?
  if { [[ "$expect" == block && $rc -ne 0 ]] || [[ "$expect" == pass && $rc -eq 0 ]]; }; then
    note "CASE pre-commit/$name: ok ($expect)"
    pass=$((pass + 1))
  else
    note "CASE pre-commit/$name: FAIL (rc=$rc, 期望 $expect)"; note "  $out"
    fail=$((fail + 1))
  fi
}

printf 'package probe\r\n' > crlf.go
git add -f crlf.go >/dev/null 2>&1
expect_pre_commit CRLF block bash "$HOOKS/pre-commit"
unstage_all; rm -f crlf.go

printf '\xef\xbb\xbfpackage probe\n' > bom.go
git add -f bom.go >/dev/null 2>&1
expect_pre_commit BOM block bash "$HOOKS/pre-commit"
unstage_all; rm -f bom.go

# gofmt 判据用**桩**驱动，避免依赖跑自测这个 shell 里有没有 Go：
#   dirty 桩 = 报出"这个文件没格式化"，clean 桩 = 什么都不报。
printf '#!/usr/bin/env bash\nprintf "%%s\\n" "$@"\n' > fake-gofmt-dirty.sh
printf '#!/usr/bin/env bash\nexit 0\n' > fake-gofmt-clean.sh
chmod +x fake-gofmt-dirty.sh fake-gofmt-clean.sh

printf 'package probe\n\nfunc  Unformatted( ){}\n' > messy.go
git add -f messy.go >/dev/null 2>&1
expect_pre_commit gofmt-unformatted block env SEELEX_GOFMT="$WORK/fake-gofmt-dirty.sh" bash "$HOOKS/pre-commit"
expect_pre_commit gofmt-clean pass env SEELEX_GOFMT="$WORK/fake-gofmt-clean.sh" bash "$HOOKS/pre-commit"
# 没有工具链时必须**说出来**（不能静默通过）。判据要精确到"Go 不在 PATH 里"，
# 不能顺手把 git 也摘掉——那样钩子连暂存区都读不到，测的就不是这条判据了。
no_go_path=""
while IFS= read -r entry; do
  case "$entry" in
    *golang/bin*|*/go/bin*) continue ;;
  esac
  no_go_path="${no_go_path:+$no_go_path:}$entry"
done < <(printf '%s' "$PATH" | tr ':' '\n')
out="$(env PATH="$no_go_path" SEELEX_GOFMT= SEELEX_GO= bash "$HOOKS/pre-commit" 2>&1)"; rc=$?
if [[ $rc -eq 0 && "$out" == *"未执行格式检查"* ]]; then
  note "CASE pre-commit/no-toolchain: ok (显式警告)"; pass=$((pass + 1))
else
  note "CASE pre-commit/no-toolchain: FAIL (rc=$rc)"; note "  $out"; fail=$((fail + 1))
fi
unstage_all; rm -f messy.go

mkdir -p tmp && printf 'scratch\n' > tmp/probe.txt
git add -f tmp/probe.txt >/dev/null 2>&1
expect_pre_commit tmp-path block bash "$HOOKS/pre-commit"
unstage_all; rm -rf tmp

mkdir -p config && printf 'accounts: []\n' > config/accounts.yaml
git add -f config/accounts.yaml >/dev/null 2>&1
expect_pre_commit real-config block bash "$HOOKS/pre-commit"
unstage_all; rm -rf config

printf 'note\n' > docs.md
git add -f docs.md >/dev/null 2>&1
expect_pre_commit docs-only pass bash "$HOOKS/pre-commit"

# ── post-commit 决策 ─────────────────────────────────────────────────────────
commit_and_hook() {  # commit_and_hook <提交信息> ；跑钩子，打印是否调用构建
  # 提交本身用 SEELEX_POST_COMMIT_BUILD=0 关掉钩子（否则会真的去构建本仓库的二进制——
  # 自测要断言的是"决策"，构建用下面的假命令跑）。
  SEELEX_POST_COMMIT_BUILD=0 git commit -q -m "$1" >/dev/null 2>&1 || true
  rm -f "$marker"
  MARKER_FILE="$marker" SEELEX_BUILD_COMMAND="$fake_build" bash "$HOOKS/post-commit" >/dev/null 2>&1 || true
  [[ -f "$marker" ]]
}

expect_post_commit() {  # expect_post_commit <名称> <skip|build>
  local name="$1" expect="$2"
  if { [[ "$expect" == build && -f "$marker" ]] || [[ "$expect" == skip && ! -f "$marker" ]]; }; then
    note "CASE post-commit/$name: ok ($expect)"
    pass=$((pass + 1))
  else
    note "CASE post-commit/$name: FAIL (期望 $expect)"; fail=$((fail + 1))
  fi
  rm -f "$marker"
}

# 1) 纯文档提交 → 跳过
printf 'note\n' > docs.md
git add -f docs.md >/dev/null 2>&1
commit_and_hook "docs: self-test"
expect_post_commit docs-only skip

# 2) 纯用例提交（test/ 与 e2e/ 与根级 *_test.go）→ 跳过
mkdir -p test e2e
printf 'package test\n' > test/sample_test.go
printf 'package e2e\n' > e2e/sample_test.go
printf 'package probe\n' > root_test.go   # 会被 *_test.go 规则排除
git add -f test/sample_test.go e2e/sample_test.go root_test.go >/dev/null 2>&1
commit_and_hook "test: self-test cases"
expect_post_commit test-only skip

# 3) 源码改动 → 构建
printf 'package probe\n' > source.go
git add -f source.go >/dev/null 2>&1
commit_and_hook "feat: self-test source"
expect_post_commit source build

# 4) 依赖清单改动 → 构建
printf 'module probe\n' > go.mod
git add -f go.mod >/dev/null 2>&1
commit_and_hook "chore: self-test go.mod"
expect_post_commit go-mod build

# 5) 目录改名：从 test/ 挪到别处仍然算源码改动
git mv test/sample_test.go sample_live.go >/dev/null 2>&1
commit_and_hook "refactor: self-test rename"
expect_post_commit rename-to-source build

note "---- hooks self-test: pass=$pass fail=$fail"
[[ $fail -eq 0 ]]
