# 循环内不再折对话 + 全局只剩一条阈值线（2026-09-30）

## 症状（用户现场）

- 重启恢复后，模型对早先对话**只剩索引**：被折轮次的内容在模型可见面上没了，要么重新
  `search_history`，要么 `read_compressed_turn` 回读。
- provider 的**前缀缓存命中前缀整段作废**：每折一次，请求开头（历史部分）就变了。
- 同一个会话里折叠发生的频率明显偏高（一轮里可能折一次）。

## 判据

折叠不是"随手做的一个动作"，而是**上下文压缩流程里的一步**：

    判定（要不要折） → 折叠（这一步产出元数据：分片、边界、证据引用） → 模型按章节写读后感

最后一步要求"手上有上一次真实请求的原件"（system / History / Tools 与 wire 同字节，前缀
重放才换得来前缀缓存）。**回合内控制器拿不到**：`ev.History` 是 Seele ReActLoop 的引擎工作
历史（装配前），system 块、项目/记忆/前缀栈块、工具面都还没进去。于是它折出来的帧只有元数据、
没有读后感——模型看不到内容；而它又改写了请求前缀，把前缀缓存一起废掉。

## 决策（用户拍定：a + ii）

1. **循环内任何阈值都不折对话**（不是"把线挪到硬线"，是根本不折）。
2. **全局只剩一条线**：装配层自动折叠的唯一阈值 = `context_hard_percent`（取消软线提前量）。
3. 循环内保留**超大工具结果兜底归档**（工具结果压缩不动，一条不删）。
4. 手动 `/compact`、模型自行 `compact_context` 照旧（显式路径不受阈值约束）。
5. 帧正文里"JSON 元数据 + 固定章节读后感"**一体**，不拆。

## 改了什么

| 文件 | 改动 |
| --- | --- |
| `seelexctx/controller.go` | `Handle` 只做超大工具结果兜底归档；整段折叠编排（窗口推导、压缩帧、ReplaceHistory、阈值判定）删除，`ControllerOptions` 从 9 个注入项收窄到 `Archive` + `MaxToolResultChars` + `Stacks`（栈顶帧 From 仍是溢出起点的去重基准） |
| `application/core/context_runtime/coordinator.go` | 自动路径折叠判据 `budget.SoftThreshold` → `budget.HardThreshold`；幂等/有效性校验同线 |
| `application/core/task_context/token_counter.go` | 预算里的 `SoftThreshold` 按**硬线**比例产出（软 == 硬 = 唯一那条线），报告面与判据同源 |
| `seelebridge/runtime_context.go` | 两个控制器构造收窄；删掉 `controllerFoldSummarizerNote`（回合内不再折帧，"该链路为何不注入摘要器"这个降级自答随之失效；结构性原因改写进构造函数的说明），`MainCompactionDAG` 的降级自答机制不动 |
| `config/seelex.yaml` + `internal/bootseed/assets/config/seelex.yaml` | 预算段注释重写：逐键列明消费者，`context_soft_percent` 标注"已不参与判据（保留键兼容）" |
| `seelexctx/limits.go` | 同一标注写进字段注释 |

## 用例口径

- **新增** `seelexctx/controller_loop_contract_test.go`：① 喂远超任何阈值线的历史，`after_tool`
  /`after_assistant` 两个入口都必须什么都不做（改动前是红的）；② 超大工具结果仍原样入库、
  未超大的不入库。
- **退役** 12 条"循环内必折"用例（`controller_test.go` 10 条、`dag_test.go` 1 条、
  `wire_protocol_safety_test.go` 1 条）与 2 条"控制器消费 limits"用例——它们钉的契约已经不在
  这一层（窗口投影保持工具配对、帧链锚、checkpoint 清理等语义属装配层）。
- **改造** 竞争门禁 B 臂：控制器已不写共享压缩栈，改按装配层同一条契约（`PushCompact`）造
  真竞争，否则探针会退化成"空集上的真命题"（该文件头注释原本就警告过这件事）。
- **回归** `task_context` 的默认档断言改为"软并入硬，两条线同值"。

## 验证

    go build ./...                                   # 全绿
    go vet ./seelexctx/                              # 全绿
    go test ./seelexctx/ -count=1                    # ok
    go test ./seelebridge/ -count=1                  # ok（15.0s）
    go test ./application/... -count=1               # ok（task_context 同步修正）

## 遗留（下一步，未纳入本次）

1. **`context_soft_percent` 彻底退场**：键 + `seelexctx.Limits.ContextSoftPercent` +
   `LoadLimits` 的 `soft < hard` 校验 + 3 处 yaml 注释。现在留键只为兼容旧配置，但"配了不生效
   的键"本身是坑（配反了还会被旧校验拒掉）。
2. **控制器链路的死代码**：`PrepareReplaceHistory` / `Repair*`（history_safety）与
   `seelexctx.ContextWindowPolicy` / `WindowPolicy` / `DefaultWindowPolicy` 若确无其他消费者，
   同批清理；`Runtime.windowPolicy()` 也已无注入点。
3. **局部折叠内容的既有改进保留**：`renderUnitLine` 的助手首段预览 + `maxUnitPreviewRunes`
   （runes 截断）继续留着——装配层摘要器不可用时 chapter2Node 落本地折叠，这条渲染路径仍在用。
