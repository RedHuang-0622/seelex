# 2026-09-25 · 后台命令轮询转常驻 + 终止口 + 打点表跟随 worktable（含实时探针）

工作包：[`docs/2026-09-24-async-tool-deferred-ack/README.md`](../2026-09-24-async-tool-deferred-ack/README.md) §10（逐片台账与判据）。
本篇只记三件"读代码看不出来"的事：打架是怎么发现的、覆盖丢失那条、为什么不走注册表；
外加本轮 owner 追加的探针要求与其代价。

## 1. 起点：轮询切片"硬判据过、成本判据不过"

上一轮（2026-09-24）落地的轮询切片默认关。§8.3 五指标 A/B 的实测结论是：派发后台命令这一事
让 T3 墙钟从 23.13s 降到 3.95s（−82.9%），但另外两臂只省 5.5% / 1.3%，而总 prompt 反而
+127% / +133%。也就是说：**轮询能救的是"等长命令"，救不了"每一轮都要多付一份信封"**。
本轮据此把能力转常驻（是否后台由模型逐次自选），同时给打点表加实时探针，把"还在跑吗"这类
确认型轮询从一次往返降成零次。

## 2. D1：轮询会被"无进展预算"掐死——静态发现，A/B 没撞上

`application/core/task_context/task_execution.go` 的进展指纹是 `name + "\x00" + result`，
同一个指纹连续出现就不推进 ProgressEpoch；`coordinator.go` 的三轴预算里
`MaxNoProgressRounds`（lite 6 / medium 10）一到就以 `no observable progress` 终止回合。
而异步载荷**刻意不含时间戳**（载荷会永久留在可缓存前缀里，含了就每轮白烧一次）——
于是"一条安静但确实还在跑"的命令，第二次起的取回结果逐字节相同 ⇒ 计数不推进 ⇒ 回合被判死，
而进程活得好好的。

A/B 那 18 个回合没撞上，纯粹因为样本是 `sleep 20; echo SENT-*`：一直在吐增量，字节口径的进展
是真的在推进。**这是"测试样本恰好回避了缺陷形状"**，不是缺陷不存在。

修法定在判据语义而不是载荷：epoch 未变，但这一轮**有在途后台执行被查询** ⇒ 不算无进展
（`AsyncPendingFor` 从执行登记表读，只数本会话、只数在跑）。不碰载荷、不调阈值、
不新增"轮询次数"阈值——拿墙钟猜状态是本仓库明令禁止的方向。空转仍由
`MaxToolCalls / MaxToolRounds` 封顶。

红→绿对照今天补齐了（`coordinator_async_budget_test.go`：同一 epoch 冻结，只改 `pending`
的回答值，`0` 必须被停、`1` 十轮不被停），原计划的 `asyncproof` 反证 tag 因此不再需要。

## 3. D2：子进程属性有三个写者，会互相覆盖（不修就等于 kill 是假的）

`internal/winhide.Apply` 先设 `HideWindow + CREATE_NO_WINDOW`，紧接着
`security.ConfigureHiddenCommand` **整体重写** `SysProcAttr{HideWindow:true}` —— 后者把前者
的 `CreationFlags` 抹掉。修它不是为了洁癖：`async_kill` 与硬超时都要往同一处加位
（`CREATE_NEW_PROCESS_GROUP` / Job 句柄），不收口就是第三个互相覆盖的写者。
`ConfigureHiddenCommand` 改成"合并而非赋值"，并用
`TestSysProcAttrWritersMerge` 钉住回归。

**这条是读码得出的静态结论，窗口闪烁是否真的存在过没有实地观察到**，台账里按"未证"记。

## 4. 一处计划外修正：`taskkill /T` 杀不掉 MSYS2 的孙进程

方案里写的是 Windows 用 `taskkill /PID n /T /F`。实测派
`bash -c "(sleep 0.5; echo GRANDCHILD) & sleep 25"`，150ms 杀掉 bash 之后 GRANDCHILD 照样
落进日志。原因：MSYS2/Git Bash 的 fork 子 shell 不一定挂在被记录的直接父 PID 下，`/T` 靠父
PID 枚举就漏。改成 **Job Object + `KILL_ON_JOB_CLOSE`**——Job 不看父子关系，且"执行体收尾时
关句柄"天然成为唯一确定的回收点；`taskkill` 只留作建不出 Job 时的降级路径，并由
`Degraded()` 如实报出（降级时不得主张"整棵进程树已终止"）。

探测过程中我自己踩了两次假信号：① 用 PATH 上的 `bash` 而不是 `scopedBashCommand` 解析出的
那颗 shell，行为不同；② **本机 Windows build 26200 没有 `wmic`**，进程列表"空"是命令不存在，
不是进程不存在。结论：跨平台"进程还在不在"不能当判据，改用行为判据（孙进程写出的那行
GRANDCHILD 是否出现）+ 终端到达时刻是否远早于 `WaitDelay`。

## 5. 为什么后台行不进 task 注册表（我自己给的选项措辞不准，先认）

规划阶段我给 owner 的选项里写过"进注册表则工作表格/打点块/事件全部零改动生效"。核实后这句
不成立：`application/core/session_history.go` 会把 `record.Tasks` 随会话落盘并在恢复时回灌，
而句柄表是内存的——真进去，重启后台账里就留下一条永远 `running` 的假行。要避开就得给驱逐与
关停补"终态回执"，那正好把本轮明确否掉的"句柄持久化"从后门买回来。

因此保留 owner 选的**行身份口径**（`kind=task` + `source_id=async:<handle>`），但改由登记表
**只读投影**合成，不写注册表：行随状态出现、随终态/驱逐消失，天然不落盘（记为不变量 I-21）。
代价是 `buildWorkTable` 多一路输入、生命周期消费者由三个变四个。

## 6. owner 追加的探针要求，以及它的边界

要求原话：打点表要有 任务描述、运行的指令、后台情况的实时查看，"这里需要做个探针"。落法与边界：

- **描述有归宿才敢要**：`bash background=true` 新增必填 `description`（handler 当场拒绝缺它，
  不给占位符）——后台命令会活过这一轮，行标题没有别的诚实来源。
- **指令原文进 GUI 描述列**，这与我上一版方案里"三不（不含命令原文/绝对路径/时间戳）"冲突，
  按裁定改写判据：**探针到 GUI 可以带路径与时间；进模型上下文的打点块与载荷不行**。
  用例 `TestAsyncTraceLinesCarryNoPathsOrLogContent` 同时钉这两头。
- **实时性靠事件而不是心跳**：信号源三类（派发、终态、去抖后的"有新字节"，1s 窗口）。
  去抖只限制发信号的频率，不参与任何死活判断。安静没输出的命令不刷信号——那一刻界面上
  "不动"就是真没有新东西，而不是我们把信号吞了。
- **前端零改动**：描述列、附件列（日志路径）、可展开的打点表（时间/操作/状态/证据/耗时）
  在 `gui/frontend/dist/work-table.js` 里本来就存在；`实发` 角标的判据（归属本会话 + 未终态）
  恰好与"这行在不在尾部打点块里"等价，所以后台行自动获得了正确的角标语义。这是先查字段归宿
  再提方案的直接收益。

## 7. 常驻开的代价，明写不藏

A/B 实测两套 schema 差 **+213 token/轮**。默认开 = 所有会话每轮固定付这笔，无论有没有后台命令。
另两处口径没变：单次工具调用钉住会话仍 ≤ 60s（I-22，所以 `until_done` 不做）；跨回合"无人取回"
由打点块缓解、不消除（推送/补记链路回闯会话锁，已废）。

## 8. 同仓并发与文档生成的一次踩雷

另一会话在同一批文件上飞（`context_compact_gate.go`、`config/seelex.yaml` 的 `context_*` 块、
`task_context_state.go`）。我只动 async 相关行；但 `scripts/gen_core_readme_index.py` 会重写
core 的**全部五个分卷**且以 CRLF 落盘，跑完 `README-context.md` 多出 69 行——那是**他们的**
新文件进了索引。处置：`git checkout` 还原该卷、把保留的四卷行尾改回 LF，并在台账里写明
"索引刷新要等那颗雷拆掉后在干净树上重做"。顺带两条文档漂移修正：`AGENTS.md` 的 Seele 版本
v0.1.1 → go.mod 实解析 v0.3.0；本包 §1 标题写"编号接存储架构 §4"是误述（存储侧是 `I1…I19`
无连字符，与本包 `I-16…I-22` 不同一条链）。

## 9. 门禁与未做项

`go build ./...` / `go vet ./...` / `gofmt -l`（触及目录）全 0；
`go test ./seelebridge/... ./application/... ./seelexctx/... ./internal/... -race` = 54 包 ok / 0 FAIL；
`-tags raceproof ... -count=20 -race` 仍报红（旧指针形状的反证没被签名改动弄丢）。
日志在 `_logs/async_s3_*.log`。

**未做**：GUI 实机走查（要 `make rebuild-gui`，其 dist 配置复制按铁律须先经确认）、
`ArchiveSession` 调用点的直接用例、`Degraded()` 的真实触发路径。逐项记在台账 §10.6。

## 10. 真 API A/B 复跑（2026-09-26，24 回合）

两臂只差 `limits.async_exec.enabled`；四条任务把每一步（用哪个工具、调几次、`wait_ms` 多少）
写进提示词，为的是不让模型手气决定回合形状。T4 是新增的"安静长命令"：20 秒里零输出，
B 臂被要求轮询 6 次 —— 6 次取回载荷逐字节相同，正是 S2 会被"无进展预算"误杀的形状。

结果三条：

1. **轮询不破前缀，这次是算出来的**。探针取相邻两条请求的 `messages` 区做字节前缀比较，
   76 个可比请求 tail-growth **0 破坏**；provider 报的命中 ÷ 上一请求 prompt = 兑现率
   A 0.981 / B 1.000。合起来才说明"多出来的那些轮询一条也没造成额外 miss"。
   只看命中率（A 0.947 / B 0.956）区分不了"没破"与"破了但历史本来就长"。
2. **成本判据仍不过，但要说清贵在哪**：名义 prompt B 是 A 的 1.1–3.9 倍，而按全价计费的
   未命中边际只从 7906 涨到 15046 token（+90%）。差额几乎全是"每轮重放历史"的已缓存前缀。
   收益端：合计墙钟 −26.2%，其中"派发后不取回就收尾"那类（T3）从 73.3s 掉到 18.1s。
3. **T4 三条重复全部 `polls=6`、无一被判死** ⇒ S2 的修法在真实链路上成立。

两个采集器 bug 值得单独记：LCP 一开始算在整条请求体上（`tools` 排在 `messages` 之后，
公共前缀必然在 messages/tools 边界停下，7 轮全报 tail 破坏，而 cached 同时是 0.97 ——
两者背离就是采集器错）；修完后还剩外层 `]` 与 `,` 的差一字节。诊断靠 `SEELEX_ASYNC_AB_DUMPDIR`
把每条请求体落盘做字节比对。**结论性数字在采集器修对之前一条都不采信**，这条纪律又救了一次。
