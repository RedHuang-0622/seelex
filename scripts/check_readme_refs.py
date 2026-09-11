#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""README 漂移检查：README 里引用的文件/链接是否还在。

检查两类引用：
  1. Markdown 链接目标 `](path)`；
  2. 正文反引号里的路径字面量（`foo/bar.go`、`baz.md`）。

解析顺序：README 同级相对路径 → 仓库根相对路径 → 任意同名文件；都命中不了
就报为潜在漂移。glob 占位符（`*.go`、`{a,b}`）、外部 URL 与数据文件名
（`state.json` 等示例）不计入。

`docs/YYYY-MM-DD-*/` 是按时间封存的调研/整改记录（描述当时状态，可能合法引用
后来被删除的文件），默认跳过；`--all` 纳入全量。

用法：
  python scripts/check_readme_refs.py            # 报告全部（跳过封存文档）
  python scripts/check_readme_refs.py --all      # 连封存文档一起查
  python scripts/check_readme_refs.py --strict   # 存在未解析引用时退出码 1（可用于 CI）
  python scripts/check_readme_refs.py docs/      # 只查某个目录前缀
"""

import pathlib
import re
import subprocess
import sys

REPO = pathlib.Path(__file__).resolve().parent.parent

# 示例/占位数据文件名：出现即视为叙述性举例，不判定为漂移
IGNORED_NAMES = {
    "state.json", "history.json", "context.json", "events.NNN.json",
    "metadata/event.json", "metadata/stack.json", "metadata/stack_{kind}.json",
    "session/metadata/guide.json", "session/metadata/system.json",
    "session/input/draft.json", "_test.go", "builtin_xxx.go",
    # 用户本地/运行时数据（.gitignore 内）：README 只作示例或路径说明
    "accounts.yaml", "workspace_index.json", "framework-events.json",
    "guide.json", "city_list.json",
}
# 明说「已删除/已退役」的行是在讲历史，不是在声称文件存在
REMOVAL_MARKERS = ("已删除", "已移除", "已退役", "deleted", "removed")
PATH_TOKEN = re.compile(
    r"`((?!https?:)[A-Za-z0-9_][A-Za-z0-9_./-]*\.(?:go|md|py|ps1|sh|mjs|js|ts|json|yaml|yml))`"
)
LINK_TOKEN = re.compile(
    r"\]\(((?!https?:)[^)#\s]+?\.(?:go|md|py|ps1|sh|mjs|js|ts|json|yaml|yml))\)"
)
SKIP_DIRS = {".git", ".gocache", ".venv", "node_modules"}
DATED_DOC = re.compile(r"^docs/\d{4}-\d{2}-\d{2}-")
# 第三方库名（形如 `foo.js`，非本仓库文件）
THIRD_PARTY_NAMES = {"PDF.js", "highlight.js"}


def tracked_files(pattern: str = "*") -> list:
    """已跟踪文件 + 未跟踪但未被 .gitignore 忽略的文件（相对仓库根）。"""
    out = []
    for extra in ([], ["--others", "--exclude-standard"]):
        out += subprocess.run(
            ["git", "ls-files"] + extra + [pattern],
            capture_output=True, cwd=REPO, text=True,
        ).stdout.split()
    return sorted(set(out))


def repo_files():
    """仓库内全部文件（相对仓库根）与按文件名索引。"""
    files = tracked_files()
    paths = set(files)
    by_name = {pathlib.Path(f).name for f in files}
    return paths, by_name


def readmes(prefix: str = "", include_dated: bool = False) -> list:
    out = [p for p in tracked_files() if "README" in pathlib.Path(p).name and p.endswith(".md")]
    if not include_dated:
        out = [p for p in out if not DATED_DOC.match(p)]
    return sorted(p for p in out if p.startswith(prefix))


def resolve(ref: str, readme_dir: pathlib.Path, paths: set) -> bool:
    if ref in IGNORED_NAMES or ref in THIRD_PARTY_NAMES or any(ch in ref for ch in "*{}<>"):
        return True
    if (readme_dir / ref).exists() or (REPO / ref).exists():
        return True
    # 展示文本省略前缀的写法（如 `agent-workbench/prd.json`）
    return any(p.endswith("/" + ref) for p in paths)


def main() -> int:
    args = [a for a in sys.argv[1:] if not a.startswith("--")]
    strict = "--strict" in sys.argv
    include_dated = "--all" in sys.argv
    prefix = args[0] if args else ""
    paths, by_name = repo_files()
    drift = {}
    checked = readmes(prefix, include_dated)
    for rel in checked:
        text = (REPO / rel).read_text(encoding="utf-8", errors="replace")
        readme_dir = (REPO / rel).parent
        bad = set()
        for line in text.splitlines():
            if any(marker in line for marker in REMOVAL_MARKERS):
                continue
            for ref in set(PATH_TOKEN.findall(line)) | set(LINK_TOKEN.findall(line)):
                if not resolve(ref, readme_dir, paths):
                    bad.add(ref)
        if bad:
            drift[rel] = sorted(bad)
    for rel, bad in drift.items():
        print(f"{rel} -> {bad}")
    print(f"检查 {len(checked)} 个 README，{len(drift)} 个存在未解析引用。")
    return 1 if (strict and drift) else 0


if __name__ == "__main__":
    sys.exit(main())
