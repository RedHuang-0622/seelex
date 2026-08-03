# 上下文内存生命周期管理 — 设计文档与现状摸底

> 状态：设计（2026-08-04）
> 目标：长上下文不再导致整体程序卡顿——内存中的上下文只存在于必要时刻（冷加载 + 按需加载），前端滑动窗口渲染，流式管道批量落库。
> 用户原则（2026-08-04）：上下文是冷加载且只加载需要的地方；存在时刻 = ① 前端 select（读）② 后端写操作 ③ 后端 select 出结果递给 LLM 的那一下；其他时候内存中不得存在。
> 工作顺序：先做泛型模板 + mock 策略验证（D），再实施 A/B/C。

## 1. 现状摸底（代码证据）

### 1.1 内存热点清单

| # | 热点 | 代码证据 | 问题 |
|---|---|---|---|
| H1 | **Session 工作历史全量常驻** | `session.Session.history []types.Message`（Seele session/chat.go:46），ChatStream 循环内逐轮 append，**结束后不释放**（rl.history 缓存，chat.go:131-132） | 长任务（数百轮 ReAct）→ history 无界增长；DurableHistory 未装配主会话（runtime.go:299 注释确认），**落库与内存释放均未接线** |
| H2 | **application 快照全量 Conversation** | `viewCoordinator.appendMessageLocked` 全量 append（service_snapshot.go:77）；Snapshot.Conversation 随每个事件经 JSON 序列化发给前端（bumpLocked + Publish） | 快照内存全量 + **每事件全量序列化传输**（O(会话长度) 每次） |
| H3 | **前端全量 DOM 重建** | conversation-view.js renderConversation 每次全量 innerHTML 重建 | 长会话 → 每轮全量 DOM 重建卡顿（用户实测卡顿主因之一） |
| H4 | **流式逐 chunk 事件** | chat.go:85 onChunk → appendDelta → 每 chunk 事件 Publish + snapshot 变更 | 高频小事件：逐 chunk 序列化/传输/渲染，无批量聚合 |
| H5 | **事件投影无界** | EventHub append-only（event/hub.go），订阅者（GUI）逐事件增量 | 会话级事件库内存增长（有 sessionstore 落库但内存事件库同步持有） |
| H6 | glob/grep 全树遍历 | scopedGlob/scopedGrep filepath.Walk 全树（已修复 2026-08-04：重目录跳过 + 超时 + ** 匹配器） | 已修复；rg 语义对齐见 §5 |

### 1.2 关键结构：当前上下文数据流

```text
LLM 流式 chunk ──► Session.history（内存全量，H1）
                     │ ChatStream 结束仍持有
                     ▼
              application snapshot.Conversation（内存全量，H2）
                     │ 每事件全量序列化
                     ▼
              前端 protocol.applyEvent → conversation-view 全量 DOM（H3/H4）
```

**结论**：上下文在内存中存在 3 份（Session history / snapshot.Conversation / 前端 DOM+JS 数组），全部无界增长且无释放点——长任务卡顿的根因。

## 2. 目标设计：三时刻生命周期

### 2.1 存在时刻原则（用户定义）

| 时刻 | 行为 | 内存状态 |
|---|---|---|
| ① 前端 select（读） | 前端拉取/滚动历史 → 后端按窗口读尾（ReadRange/ReadEventTail 已有） | 只装窗口区间，返回后释放 |
| ② 后端写操作 | ReAct 循环内 append 当前轮消息 → **批量落库**（管道） | 当前轮短暂驻留，落库后释放 |
| ③ select 递 LLM | 会话装配时把窗口历史 + 栈块组装为模型请求 → 递出后释放 | 请求构建瞬间驻留 |

**不变式**：落库是唯一持久面；内存只承载"当前轮 + 窗口"；ChatStream 结束后 Session history 引用释放（保留窗口句柄）。

### 2.2 ContextManager 泛型模板（D 先行）

```go
// 泛型上下文生命周期管理器：策略注入，mock 验证不同策略的内存控制。
type ContextManager[T any] struct {
    provider  ContextProvider[T]  // 冷存储（sessionstore）
    policy    LifecyclePolicy     // 全量常驻 / 窗口加载 / 管道批量
    window    WindowPolicy        // 窗口推导（复用 seelexctx.WindowPolicy）
    pipeline  *BatchPipeline      // 流式批量落库管道
}

// 三时刻接口（唯一访问面）
type ContextProvider[T any] interface {
    LoadWindow(ctx, offset, limit) ([]T, error)   // ① 前端 select
    Append(ctx, items []T) error                  // ② 后端写（批量）
    SelectToLLM(ctx, request AssemblyRequest) ([]T, error) // ③ 递 LLM
    Release(handle)                                // 用完即弃
}
```

mock 基准（D2）：合成 10k 轮会话 → 对比 全量常驻 / 冷加载 / 窗口加载 / 管道批量 的内存峰值、GC 压力、select 延迟、落库吞吐 → 选默认策略。

### 2.3 流式管道批量落库（C）

```text
onChunk ──► 有界缓冲管道（聚合 N chunk 或 X ms）
              ├─► 批量落库（sessionstore SaveCommit）
              └─► 节流渲染事件（rAF/聚合窗口，不与落库互斥）
```

- 缓冲上限 = 背压（管道满 → 消费侧合并窗口，不阻塞 LLM 流）；
- 渲染与落库分离：落库批量、渲染节流，互不阻塞。

### 2.4 前端滑动窗口渲染（B）

- conversation-view 改虚拟列表：只渲染可见区 DOM（上下哨兵 + IntersectionObserver）；
- 滚动向上 → invoke LoadMoreHistory（已有分页接口）；
- 流式渲染 rAF 批处理（聚合多个 chunk 一次渲染）。

## 3. 波及文件预估

| 改动 | 波及文件 | 预估量 |
|---|---|---|
| A: Session history 释放 + DurableHistory 装配 | `seelebridge/runtime.go`（newMainSession 已装配 History）、`sessionstore/durable_history.go`（窗口读已有）、`seelebridge/context_components.go` | 小-中 |
| A: snapshot.Conversation 有界化 | `application/core/service_snapshot.go`、`service_interaction.go`、`application/model/state.go`（DTO 增窗口字段）、`application/core/service_input.go` | 中 |
| A: 事件投影有界化 | `application/event/hub.go`（订阅端窗口化）、`application/core/chat.go`（Publish 频控） | 中 |
| B: 前端虚拟列表 | `gui/frontend/dist/conversation-view.js`（重写渲染核心）、`app.js`（滚动哨兵接线）、`styles.css`（虚拟行样式） | 中-大 |
| C: 流式管道 | `application/core/chat.go`（appendDelta 改管道）、新增 `application/core/stream_pipeline.go`、`sessionstore/event_store.go`（批量写入） | 中 |
| D: 泛型模板 + mock | 新增 `seelexctx/lifecycle/`（ContextManager + 策略）、`seelexctx/lifecycle/mock_bench_test.go` | 中（独立，先做） |
| 摸底后续：rg 语义 | `seelebridge/scoped_tools.go`（glob/grep 接 gitignore 语义）、新增 `seelebridge/ignore.go` | 小-中 |

## 4. 主流实现审查：rg（ripgrep）的 glob/grep 做法

审查结论（2026-08-04）：ripgrep 的搜索遍历实现是当前主流基准，与我们刚修复的 glob/grep 对比如下：

| 维度 | ripgrep | Seelex 现状（修复后） | 差距 |
|---|---|---|---|
| **忽略语义** | ignore crate：`.gitignore`/`.ignore`/`.rgignore` 精确 glob + **negation（! 反向规则）** + 目录名启发式 | heavyDirNames 硬编码目录名 + 隐藏目录跳过 | **中-大**：无 gitignore 语义、无 negation、不可配置 |
| **遍历并发** | 并行遍历（rayon 线程池按目录并行） | filepath.Walk 串行 | 中：大仓库并发收益明显 |
| **二进制检测** | 内容嗅探（NUL 字节）跳过二进制 | 无（全文本读） | 中：大二进制文件拖慢 grep |
| **类型过滤** | `-t`/扩展名预过滤（walk 阶段跳过） | 无 | 小 |
| **输出** | 行缓冲 + 截断策略 | MaxResults 上限（已有） | 小 |
| **glob 匹配** | ** 递归 + 段语义（globset crate） | matchGlobPattern（2026-08-04 自研，** 递归 + 正斜杠归一） | 小：语义对齐，缺 negation/花括号展开 |

**对齐计划**（进 P2 实施清单）：
1. 忽略规则可配置：`.gitignore`/`.ignore` 文件读取（项目根 + 向上查找）+ heavyDirNames 作为兜底目录启发式；
2. negation 支持（!pattern 重新包含）；
3. 并行遍历（目录级 goroutine 池，注意结果排序确定性）；
4. 二进制嗅探（读前 8KB 检测 NUL）。

## 5. 已知问题登记（2026-08-04）

| # | 问题 | 状态 | 处置 |
|---|---|---|---|
| B1 | **glob/grep 卡顿**：`**/*` 全树遍历（Windows 反斜杠导致 filepath.Match 恒不匹配 → 遍历但结果空；慢盘上分钟级卡顿） | ✅ 已修复（提交中） | 重目录跳过 + walk 超时（limits.walk_timeout=30s）+ ** 递归匹配器（matchGlobPattern）；测试覆盖 |
| B2 | **前端审批弹窗不显示**（权限 ask 卡住工具执行） | 🔧 已加固待验证 | z-index 100 + 聚焦首个选项 + [interaction] 诊断日志；代码链路（broker→EventHub→bridge→前端）验证全通，若仍不弹需运行时日志 |
| B3 | **沙箱接入疑似导致工具挂起** | ↩️ 已回滚 | scopedBash 恢复 v1 直连 exec；CommandSandbox 接口保留，根因定位后再接入（fail-fast） |
| B4 | **前端 ES module 解析失败（closeNodeDetail 重复声明）** | ✅ 已修复 | `681dfe9`；验证方法升级：module 加载（node --input-type=module）而非 node --check |
| B5 | **GUI 构建缺 desktop tag** | ✅ 已修复 | 构建必须 `-tags "gui,desktop,production"`（scripts/build-gui.ps1 为准） |
| B6 | 沙箱回滚后工具仍卡？ | ⏳ 待用户冒烟确认 | 若仍卡需提供：具体工具 + 卡顿时长 + 阶段（主代理/子代理） |

## 6. 实施顺序

1. **D（先行）**：泛型 ContextManager + 策略 mock 基准（独立包，不依赖 A/B/C）
2. **A**：Session history 释放 + DurableHistory 装配（H1）→ snapshot 有界化（H2）→ 事件投影有界化（H5）
3. **C**：流式管道批量落库（H4）
4. **B**：前端虚拟列表（H3）
5. **P2**：rg 语义对齐（ignore/negation/并行/二进制嗅探）

## 7. 验收标准

- mock 基准：冷加载策略下 10k 轮会话内存峰值 < 全量常驻的 30%，select 延迟 < 50ms；
- 长会话（500+ 轮）GUI 冒烟：无卡顿（帧率稳定），滚动历史可用；
- 流式：10k chunks 落库批量 ≤ 100 次写入，渲染帧率 ≥ 30fps；
- `go build/vet/test ./...` 全绿。
