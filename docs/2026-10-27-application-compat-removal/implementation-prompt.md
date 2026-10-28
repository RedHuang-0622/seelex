# 后续实施提示词

继续在 B 工作树实施 Application 兼容层退场。

工作树：

```text
C:\Users\redre\.codex\worktrees\application-compat-removal-b\seelex
```

基线只读工作树：

```text
C:\Users\redre\.codex\worktrees\application-compat-removal\seelex
```

开始前必须阅读：

1. 根目录 `AGENTS.md`、`MEMORY.md`。
2. `docs/2026-10-27-application-compat-removal/front-review.md`
3. `docs/2026-10-27-application-compat-removal/plan.md`
4. `docs/2026-10-27-application-compat-removal/deletion-list.md`

目标：彻底删除 `application/compat`、`application.Frontend`、`assembly.Frontend`、`FrontendPorts`、旧宽入站接口、test-only forwarding 和测试 alias 补丁。GUI/TUI/headless/console/wiring 改为直接消费各自声明的窄能力接口；assembly 继续拥有共享状态、锁和生命周期顺序。

硬性约束：

- 不把 compat/frontend.go 整体搬到任何新包。
- 不创建第二个万能 Frontend/Application/Bridge 接口。
- consumer interface 声明在 consumer 包；单接口不超过 20 方法。
- assembly 组件快照只含 concrete owner 字段，不含方法、业务判据、锁或全局状态。
- 不改变产品行为、wire、持久化、session scope、revision/seq 或关闭顺序。
- 不删除真实行为测试；纯“facade 只转发/旧名字存在”测试可以删除，行为测试迁到 owner。
- 不同时重构无关存储/会话代码，不把 god 类型换个位置。
- 不 commit/push，不修改 A 工作树，不触碰用户 `.seelex`、真实配置或 `dist`。

执行顺序：

1. 建立 A/B 脚本与结构红测；证明当前 compat/test shim 存在时门禁失败。
2. 建立 assembly 分组组件快照，替换 tool registry 与 bootstrap。
3. 迁 headless/console，再迁 TUI，最后按领域分批迁 GUI。
4. 所有调用方归零后，物理删除 compat、旧接口、assembly Frontend 与 test-only shim。
5. 逐项核对 deletion-list；每删一项给出旧调用方和新 owner 的结构证据。
6. 在 A/B 分别运行同一测试，按包比较结果；B 全绿后再跑 build/vet/README/frontend gates。

每次汇报必须包含：

- 已完成里程碑和未完成项；
- 旧转发入口 → 新唯一 owner 的删除清单；
- 新增/修改的 consumer interfaces 与 assembly component fields；
- 文件/行数、方法数、compat 引用数前后值；
- 首次红测失败证据；
- A/B 测试命令与实际结果；
- 是否出现未决语义、跨领域契约变化或文件冲突。

验收终态：

```text
test ! -d application/compat
rg 'application/compat|applicationcompat|compat\.Frontend' --glob '*.go' --glob '*.md' .   # 无结果
rg 'FrontendPorts|frontendOfTestService' application                                      # 无结果
rg '^func \(frontend \*Frontend\)' application                                            # 无结果
```

只有当前树证明以上规则、A/B 对比无行为回归、并完成全量验证时，才可判定目标完成。
