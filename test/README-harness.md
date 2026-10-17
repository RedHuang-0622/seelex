# test/harness（集成用例分卷）

## 生态位

用例基座：包说明、TestMain 的 CWD 口径与 launchFlags 摆现场句柄。

覆盖：显式名单 2 个文件（见生成器 `TEST_VOLUMES`）；未归属文件由覆盖自检拦下。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### main_test.go

- `func TestMain(m *testing.M)` — TestMain 把工作目录切到**仓库根**：本包用例按仓库相对路径读夹具
