# Store DDD 当前交接：M10 起点

生成时间：2026-10-28 11:48 Asia/Shanghai

## 工作树

```text
path:   G:\seelex-worktrees\store-ddd-M8-to-M11
branch: codex/store-ddd-M8-to-M11
head:   9ac3f929 refactor(store-ddd): complete M9 role and workunit split
status: clean
```

不要在 `G:\Program\go\seelex` 主工作区继续本任务；该处存在大量无关改动。

## 当前状态

- M0–M9 已完成。
- M8 teamwork/registry 已完成；checkpoint `1a181dff`。
- M9 Role/WorkUnit 已完成；commit `9ac3f929`。
- M10 Facade 清零：未开始。
- M11 全仓验收：未开始。

进度事实源：

- [`progress.md`](progress.md)
- [`milestones.md`](milestones.md)
- [`external-change-protocol.md`](external-change-protocol.md)

## M9 已验收证据

```text
go test ./store/... ./store/role/... ./store/workunit/... ./backend/multiagent/... ./test/ -count=1
go test ./e2e -run 'TestStoreDDDM9RoleWorkUnitOwnershipGate|TestStoreDDDMTeamworkIOOwnershipGate|TestStoreDDDMTeamworkModelOwnershipGate' -count=1
go build ./...
go vet ./...
```

全部通过。`storeEngine` 172→162，`jsonRepository` 90→83；role/workunit owner
不 import 根 `store`；路径、JSON tag、head/floor 语义未变。

## M10 提示词

把以下整段交给 Qoder：

```text
只执行 SEELEX-STORE-DDD M10 Facade 清零，不进入 M11。

工作目录必须是：
G:\seelex-worktrees\store-ddd-M8-to-M11

分支必须是：
codex/store-ddd-M8-to-M11

开始前检查 git status、git log，确认 M9 里程碑 commit
9ac3f929 refactor(store-ddd): complete M9 role and workunit split 存在且无未提交改动。

先完整阅读：
- MEMORY.md
- AGENTS.md §0、§5、§8、§9、§10
- docs/2026-10-27-store-ddd-split/plan.md
- docs/2026-10-27-store-ddd-split/milestones.md §14
- docs/2026-10-27-store-ddd-split/external-change-protocol.md
- docs/2026-10-27-store-ddd-split/progress.md
- store/README.md、store/README-*.md 与各领域包 README

目标：
1. 建立并保存当前结构基线：Router、jsonRepository、storeEngine、
   SessionGranularStore 方法集合；生产代码直接 import 根 store 的文件清单；
   `*For` / `BoardsFor` / `TeamworkFor` / `ToolRecordFor` 能力发现出口清单。
2. 生产调用方逐个改为消费方定义的窄端口；具体存储实现只允许在 wiring/
   composition root 注入。不得新增万能接口、getter、facade 或 adapter 换编译。
3. 删除根 Store 承载的业务判据、领域状态机与跨域事务：
   - 根包只保留数据根、原子写、head 信封、layout、锁分片、关闭和明确 lifecycle；
   - `BoardsFor`、`TeamworkFor`、`ToolRecordFor` 等能力发现出口归零；
   - 根 `Router` / `jsonRepository` / `storeEngine` / `SessionGranularStore`
     方法数只能下降。
4. 上层直接 import 根 store 的生产文件尽量归零；暂时必须保留的兼容面要登记
   旧面、新面、首调用方、删除条件和归零测试，不得永久保留。
5. 修改领域目录之外调用点时必须走 external-change-protocol.md：
   manifest、checkpoint、可重放 patch/脚本、dry-run、apply、rollback 证据。
6. 先写会红的结构门禁：残留能力发现口、根 God 方法增长、领域反向依赖、
   wiring 承载业务顺序时失败；再逐批迁移到绿。
7. 更新 progress.md、架构图、模块 README、import ratchet、God method ratchet，
   写完整“原位置 → 新唯一位置”删除清单。

验收：
go build ./...
go test ./application/... ./middle/... ./backend/... ./store/... ./wiring/... ./e2e/... -count=1
go vet ./...
git diff --check

确认无循环依赖、无第二份业务判据、无 wiring 业务顺序。
全部通过后执行一次 M10 全仓测试，再创建只含 M10 的本地 commit；不 push。
若遇到无法用窄端口表达的合法用例，停止并报告冲突，不要靠兼容 facade 绕过。
```

## M10 完成后的交接规则

1. 更新 `progress.md`，把 M10 标成已完成并写测试、结构指标、删除清单、未决风险。
2. 记录 M10 commit SHA。
3. 再使用 `qoder-remaining-work.md` 的 M11 提示词。
4. 不 push；不清理 `.seelex`、config、dist 或用户数据。
