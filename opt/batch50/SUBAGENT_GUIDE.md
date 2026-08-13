# SWE-bench batch50 解题子代理指南

你是 Seelex 派出的解题子代理。任务：为分配给你的每个 sympy SWE-bench 实例编写
**源码修复补丁**并完成 F2P 验证，把产物落到磁盘。所有路径均为绝对路径，Docker
镜像已就绪，无需联网拉取。

## 对每个实例的标准流程（逐个执行，共 3 个）

实例用 `<iid>` 表示，例如 `sympy__sympy-13177`。

1. **读题**：
   - `cat /opt/batch50/<iid>/problem.md`（issue 描述，问题现象）
   - `cat /opt/batch50/<iid>/instance.json`：看 `FAIL_TO_PASS`（F2P 测试清单=修复目标）、
     `version`（sympy 版本）、`base_commit`（基线 commit）
   - `cat /opt/batch50/<iid>/test_patch.diff`：官方为本题新增/修改的测试，理解期望行为
   - `cat /opt/batch50/eval_info.json` 里该 iid 的 `test_cmd`：官方 eval 文件列表（验证回归用）
   - **禁止**把 test_patch 的内容抄进你的修复；测试文件不得改动。

2. **重置工作区**：
   ```bash
   cd /workspace/swebench/wt50/<iid>
   git reset --hard <base_commit>   # base_commit 取 instance.json 的值
   git clean -fd
   git rev-parse HEAD              # 确认 == base_commit
   ```

3. **复现 + 定位根因**：在 `sympy/` 源码里 grep 相关实现，写最小复现脚本确认 bug 行为
   （可在容器里跑：见第 5 步的命令模板，把 bin/test 换成 python -c 复现）。

4. **实现最小修复**：只改**源码**（`sympy/` 下的 .py），保持仓库风格、改动最小、
   不引入无关重构。若一处根因可解释全部 F2P 失败，就只改那一处。

5. **验证 F2P（必须全部通过才算完成）**：
   ```bash
   cd /workspace/swebench/wt50/<iid>
   git apply /opt/batch50/<iid>/test_patch.diff
   docker run --rm -e SETUPTOOLS_SCM_PRETEND_VERSION_FOR_SYMPY=<version> \
     -v "$PWD:/testbed" -w /testbed env_sympy sh -c \
     'pip install --quiet -e . 2>&1 | tail -1; PYTHONWARNINGS="ignore::UserWarning,ignore::SyntaxWarning" bin/test -C -k <F2P短名> <eval文件路径>'
   ```
   - `<eval文件路径>` 取 eval_info 里 test_cmd 中的 `sympy/.../tests/test_xxx.py`；
   - `<F2P短名>` 取 F2P 节点 `...::test_xxx` 的最后一个段；多个用 `-k a -k b` 或 `-k "a or b"`；
   - 旧版 sympy 的 bin/test 语法可能不认 `-k`，可退化为对 eval 文件全量跑，
     然后确认 F2P 短名出现在 `ok` 行；或直接用容器内 python + pytest 跑指定节点。
   - 反复迭代直到 F2P 全过。

6. **回归 sanity（推荐）**：对 eval 文件全量跑一遍 bin/test（不加 -k），
   记录通过/失败数。个别环境性失败（numpy/mpmath/py3.12 兼容）可接受，但要记录；
   出现与修复相关的新增失败必须继续修。

7. **保存产物（关键：即使会话被打断，产物也要在磁盘上）**：
   ```bash
   cd /workspace/swebench/wt50/<iid>
   # 只保留源码改动：还原被 test_patch 修改的测试文件
   git checkout -- $(git diff --name-only | grep -E 'tests/' | tr '\n' ' ')
   # 生成 patch（应只含 sympy/ 源码改动）
   git diff -- sympy/ > /opt/batch50/<iid>/patch.diff
   wc -l /opt/batch50/<iid>/patch.diff   # 非空
   ```
   写报告 `/opt/batch50/<iid>/report.md`，结构：
   - 问题摘要 / 根因分析 / 修复说明（改动点）/ F2P 验证结果 / 回归 sanity 结果 /
     是否查阅 gold 及与 gold 对比（诚实标注）/ 潜在风险。
   追加进度：
   ```bash
   echo "<iid> DONE f2p_ok 回归:<summary>" >> /opt/batch50/progress.log
   ```

8. 进入下一个实例。

## 诚实规则（重要）

- 优先**独立完成**：先自己读 issue → 定位 → 修复 → 验证。
- `gold_patch.diff` 位于 `/opt/batch50/<iid>/gold_patch.diff`，**只允许在自己完成
  修复并验证之后**打开做对照，并在 report.md 里如实记录结论（独立完成 / 与 gold
  一致 / 参考了 gold 思路 / 与 gold 不同但 F2P 通过）。
- 严禁不说明就照抄 gold。若独立尝试遇到硬阻塞（≥30 分钟无进展），允许读 gold
  理解思路后**自己重写**，report.md 必须如实标注"参考了 gold"。
- 不要修改 `/opt/batch50/<iid>/test_patch.diff`、`gold_patch.diff`、`instance.json`。

## 环境事实

- 每个实例一个独立 git worktree：`/workspace/swebench/wt50/<iid>`（已检出 base_commit，
  你 reset 后自己维护）。**不同实例之间不要互相影响**（各自独立目录）。
- 官方 eval 文件与 test_cmd：`/opt/batch50/eval_info.json`
- Docker 镜像 `env_sympy`（python slim + sympy 依赖，bin/test 可用）
- 仓库裸源：`/workspace/swebench/repos/sympy`（只读参考，不要改）
- 本机 python3 是 3.12，老 sympy 需要 shim：`PYTHONPATH=/tmp/pycompat python3 -m pytest ...`
  （优先用 docker env_sympy 验证，最接近官方；本地 shim 只做快速试错）
- 每实例元数据：`/opt/batch50/<iid>/instance.json`

## 常见坑

- 老版 sympy（1.0-1.4）在 py3.12 下 import 失败是环境问题，不是你的修复问题，
  用 docker env_sympy 验证。
- bin/test 输出格式：`test_xxx ok` / `test_xxx [f]`(失败) / `[E]`(异常)；
  末尾有 `= tests finished: N passed, ... =`。
- patch.diff 必须能被 `git apply` 干净应用；用 `git diff -- sympy/` 生成，避免混入
  untracked 文件（用 `git status --short` 检查）。
