# 审查：R1/R2/R3 能否加快速度、减少存储 IO 热点、减少消耗

> 审查对象：[`prompt.md`](./prompt.md)（R1 后端收口 / R2 goal techleader
> 独立存储 / R3 会话中间缓存）。证据来源：`conformance-checklist.md` §5/§6 的
> 实测记录、当前代码（提交 `45d35f6`）与同轮 A/B 纪律。

## 0. 结论速览

| 需求 | 速度 | 写 IO / 热点 | 读 IO | 资源消耗 | 判定 |
|---|---|---|---|---|---|
| R1 删旧后端实现、留接口 | 运行时中性；构建/测试变快 | 无直接减少 | 无直接减少 | 代码/维护消耗下降；配置迁移有成本 | 值得做，但**不加速业务链路** |
| R2 techleader 独立子树 | 条件性正收益（必须独立 actor；否则只是搬 IO） | 主会话分片/EVENT 变小 → 主会话热点下降 | 主会话 tail/装配扫描变小 | 子树多一份 metadata/commit 地板；总字节略增 | 能降主会话热点，**不能降单轮提交地板** |
| R3 中间缓存层 | 读路径量级改善；写路径可去掉重复读/重算 | 命中时 `shard reads=0`、`fileSHA256=0`；fsync+rename 仍在 | cache hit 消除全量扫行与重复装配 | 内存上升，必须设上限/淘汰/指标 | **主要收益在读与 CPU；写地板归 H2/H3/H4 专项** |

## 1. 现有实测基线（必须作为对照）

- **message 提交地板 63.7 ms**；**head 发布 rename ≈30 ms/次**（栈归因）；
  EVENT 域在同轮归因里 ≈150 ms/次，但规模探针证明其常数地板 ≈40 ms 来自
  “fsync 数据 + rename 发布 head”，与历史行数无关。
- **H2**：一次全量历史读让同会话提交多等 **91.7 ms**（基线 63.7 ms，预算 30 ms），
  且读与写共用 `messageMu`。
- **H3**：`appendRowsLocked` 每次提交整片重读（`message_rows.go:397`）并
  对整个分片重算 `fileSHA256`（`:421/:429/:489`）。
- **锁画像（S6/S8）**：仓库级锁延迟 12.87–12.89 s，其中
  `SaveCommitWorkspace` 8.36 s（64.9–66%）、`AssembleWireWorkspace` ≈4.2%；
  stack/guide/lifecycle 域在 S9/S16 后不再出现在画像里。
- **读快路径现状**：栈读者走内存快照 5.7–13.2 µs；但会话级 record/翻页仍走
  message 行全量扫描（见 §2 新发现）。

## 2. 审查中新发现的读放大点（R3 的直接动机）

提交 `45d35f6` 的 S20 派生读引入了两处 **O(history) 全量扫行**：

- `jsonRepository.derivedRecordPayload`（`json_layout.go:258`）→
  `readRows(key, 1, 0)`：每次 record 派生（冷读、fork、归档、快照）都全量解码；
- `jsonRepository.ReadConversationRange`（`conversation.go:96`）→
  `readRows(key, 1, 0)`：**翻页每一页都全量解码**，offset/limit 没有下推到分片。
- 对照：message tail 读已有 `readTailRowsForSelection` 的尾分片优化
  （`message_rows.go:560`），但上述两条派生读没有利用它。

**含义**：在 R3 落地前，长会话的 record/翻页路径会随历史线性变慢；R3 缓存
（或等价的“窗口下推 + 分片路由”）是修复这一点的正解。若先做 R2 再做 R3，
主会话变小会让这条放大看起来缓解，但并未根治。

## 3. R1：删旧后端实现

- **速度**：对 JSON v8 运行期无影响；收益是编译/测试面缩小（当前
  `forEachStackBackend` 只跑 json+sqlite，SQL/Redis 的语义覆盖本就接近 0）。
- **IO 热点**：不减少。删除的是**非活动代码路径**，磁盘数据仍在。
- **消耗**：二进制与维护成本下降；若连枚举一起删，配置/文档迁移是新增成本。
- **建议**：采用提示词 §2 的 **方案 A**（保留枚举 + `Open` 显式
  “backend retired/unsupported”错误），避免用户旧配置被静默改写；
  删除前按 `MEMORY.md` 中文预警 + 备份（只删代码路径，不清理 `.seelex/dist/config`）。

## 4. R2：goal techleader 独立存储

- **主会话热点下降的机制**：techleader 聊天不再写主会话 message 分片与 EVENT，
  主会话分片更小 → `appendRowsLocked` 的整片重读与 `fileSHA256` 更便宜；
  主会话 tail/装配扫描的行数也更少。这是**结构性减少主会话 IO 热点**。
- **前提条件（必须写进提示词）**：techleader 必须运行在**独立 actor/engine/
  独立会话锁**上。若它仍在主会话调用栈里同步等待，commit 的 fsync+rename 地板
  （每轮 ≈40–60 ms 量级）只是被搬到子树，主链路总时延不降。
- **不承诺**：子树自身的单轮提交地板与主会话同源（fsync+rename），不会因
  “独立存储”而变快。
- **消耗**：多一份 guide/message/event/stack head 与目录；总磁盘字节略增；
  只有当 techleader 流量占比高、且主会话随之变小时才是净收益。
- **边界**：techleader 记录默认不应回灌主会话 wire（只保留 `goal.*` 摘要/
  引用锚），否则读成本又回到主上下文装配。

## 5. R3：中间缓存层

### 能减少的

- **读 IO / CPU（量级性收益）**：cache hit 直接返回实际发出去的上下文，
  消除 `derivedRecordPayload` / `ReadConversationRange` / 装配器的全量扫行与
  重复解码；长会话冷加载不再随历史线性增长。
- **写侧重复读与重算（条件性收益）**：若缓存持有尾分片状态（行数/字节/校验），
  `appendRowsLocked` 可跳过整片重读与 `fileSHA256`（H3），实现“只写 delta 的
  jsonl append”。这是提示词里“storage jsonl 只 append / 各 stack 即时落盘”的核心价值。

### 不能减少的（需明确写进提示词，避免误判）

- **fsync + rename 发布地板**（message 63.7 ms / EVENT ≈40 ms / head ≈30 ms）：
  R3 不改变发布形态就不会消失；要降它必须在 message 读写专项里做
  group commit / 批量 fsync / 发布形态改造（H2/H3/H4）。
- **H2 的 91.7 ms 读写共用 `messageMu` 等待**：缓存若不改变 message 读写锁形态，
  也不消除这个等待；必须与专项一起改。

### 消耗与风险

- 内存新增 ≈ 每会话“wire 缓存 + 尾分片/索引状态”；必须按 §11 上限配置，
  并暴露指标（hit rate / bytes / evictions / rebuilds），否则“省 IO”会变“吃内存”。
- 缓存**不得成为第二事实源**：未命中/崩溃/跨进程必须能从存储完全派生；
  head revision、checksum、seq 空洞、LRU watermark、compact 失效要定义清楚。
- 失效成本要算进写路径：compact/LRU/删除会话时缓存必须同步截断，否则命中旧内容。

## 6. 建议加入 prompt.md 的性能判据（可直接采用）

建议在提示词末尾新增「§7 性能判据与反例」（同轮 A/B、固定机器、取 median，
遵守本机抖动 ≫ 差值的纪律）：

1. 500 轮 + 3 次压缩会话：冷加载 p50/p95 与 50 轮基线对比 **Δ ≤ 10%**；
2. 每轮加载读取的 message 分片数 **≤ 2（tail）**，读取字节与增量成正比；
3. 缓存命中时：message 提交 `shard reads = 0`、`fileSHA256 calls = 0`，
   写入字节 ≈ delta；`fsync/rename` 次数单独计量（预期不变，归专项）；
4. `mutexprofile`：`SaveCommitWorkspace` 锁等待占比不升（目标下降）；
   `go test -race` 全绿；
5. 内存 pprof：缓存占用 ≤ 配置上限；hit rate / evictions / rebuild 计数可观测；
6. R2：主会话分片行数/字节不随 techleader 轮次增长；techleader commit 只出现在子树；
7. R1：构建/测试时间下降不得以配置静默改写为代价（退役后端必须显式报错）。

同时明确两条边界写进提示词：

- R3 只承诺消除**重复读/重算/重复解码**与读放大；**写提交的 fsync+rename 地板
  不在 R3 范围**，随 message 读写热点专项。
- R2 的前提是 techleader **独立 actor**，且其记录默认不进主会话 wire。

## 7. 总体判断

- **会更快、更省**：R3 对读/装配是量级性改善，R2 在主会话 IO 热点上是结构性
  减量，R1 降维护与构建成本。
- **不会自动变快**：写路径的 fsync+rename 地板与 H2 的锁等待；这两项只有
  message 读写专项能解决，R3 若声称“降低提交 IO”必须有 H3 的重复读/重算证据，
  不能把 fsync 算进收益。
- **实施顺序建议**：R1（清干扰）→ R3 读侧最小闭环（先修 S20 派生读放大，
  再做 cache hit/失效）→ R2（techleader 子树）→ 应用层接线与 EVENT 生产者；
  H2/H3/H4 写形态专项最后做，但 R3 的接口要为它留位（尾分片状态、批量发布）。
