# 2026-09-16 computer use 证据链勘误：把「象征性通过」换成出站硬证据

> 日期: 2026-09-16 | 范围: `computer_use_live_smoke_test.go`（证据链重写）、
> 本 devlog（勘误）；`seelebridge/internal/config/config.go` 的假账号回退仅记录、未改

## 一、被指出来的事（先认）

有人指出：computer use 的"测试"看着跑通了，但它并没有真的接上真实模型能力——
回退配置里就是一个**示例象征作用的 `gpt-4o`，里面什么都没有**。

这个观察**部分成立，而且比字面更严重**。分三件事说清楚，各自的证据都列出来。

## 二、实证一：`gpt-4o` 假账号是真的存在的

`seelebridge/internal/config/config.go` 的 `fallbackConfig()`：当
`config/accounts.yaml` 缺失（**正常路径，不报警告**）或解析失败时，代码构造

```go
model.AccountSpec{
    Name: "fallback", Provider: "openai", Model: "gpt-4o",
    BaseURL: "https://api.openai.com/v1",
    APIKey:  os.Getenv("OPENAI_API_KEY"), // 通常为空
}
```

实测（把 headless 跑在一个**没有** `config/accounts.yaml` 的目录里，读
`Snapshot.Runtime`）：

```text
NO-CONFIG => model=gpt-4o provider=openai account=
accounts: fallback/openai/gpt-4o
```

对照（有 `config/accounts.yaml`）：

```text
model=deepseek-flash provider=openai
accounts: agent-1/openai/deepseek-flash | agent-2/... | goalplan-1/... | goalplan-2/... | subagent-1/... | subagent-2/...
```

结论：**"示例象征作用的 gpt-4o、里面啥都没有"确实存在**，它就是缺配置时的
静默回退账号（空 key 指向 `api.openai.com`）。任何在这种装配下跑的
"computer use 测试"都不接任何真实能力。

## 三、实证二：此前「回答命中标题」不是"看过图"的证据

原冒烟把"回答命中 `ForegroundWindow().Title`"当作**画面送达模型**的硬证据
（文件头原话：*"画面确实进了模型请求，模型是「看图回答」而不是猜"*）。这条
不成立：

- `computer_screenshot` 的工具结果文本里**已经**包含前台窗口标题——
  `seelebridge/tools/computer/tools_view.go`（`screenshotResult.Foreground`）→
  `seelebridge/tools/computer/tools.go:246`（`windowJSON.Title`）。
- 模型完全可以只读这段文本就"照抄标题"，从未看过像素。

对照实验（真实 provider、**不带任何图像**、只有工具结果文本，2026-09-16）：

```text
no-image answer => [Seelex]        # 与地面真值逐字相同
usage: prompt=213 completion=3     # prompt_tokens 只有文本量级
```

所以"标题命中"只说明**链路通 + 回合正常产出**，不说明画面到达。

## 四、实证三：真实 API 与视觉能力本身是通的（不是"没接真 API"）

同一件事也要说准：**computer use 冒烟的路径确实接的是真实 provider**——它加载
`config/accounts.yaml`（`api.deepseek.com`，`deepseek-flash`，有效 key）。直接
打真实接口：

```text
GET  https://api.deepseek.com/models                -> HTTP 200
     {"data":[{"id":"deepseek-flash",...},{"id":"deepseek-v4-pro",...}]}
POST /chat/completions（带一张 32x32 测试图）        -> HTTP 200
     {"content":"蓝色 红色", ...}                    # 左蓝右红，逐字答对
```

即：**模型支持图像输入，"没接真 API"这句在冒烟路径上不成立**；不成立的是
"回答命中"那条**证据**，以及缺配置时的 `gpt-4o` 回退。

## 五、修法：把出站请求变成证据（`computer_use_live_smoke_test.go`）

把账号副本的 `base_url` 改指向**本地记录代理**，代理原样转发到真实端点，并在
转发前检查请求体里有没有 `data:image/...;base64,...`：

```text
computer_use_live_smoke_test (真实 provider + 真机截屏)
  → 副本 accounts.yaml: base_url = http://127.0.0.1:<port>
  → newProviderRecordingProxy: 转发到 https://api.deepseek.com，记录随图
  → 断言：
      1) 媒体分区有 screenshot-*.png（工具真的跑了，原有）；
      2) 出站请求里至少 1 次带图像、且至少 1 次不带（记录器有区分能力）；
         随图 > 8 KiB（不是占位串）；
      3) 回答命中标题（保留，但明确降级为"回合产出正常"，不再是"看过图"证据）。
```

判定完全落在**出站流量**上，不依赖模型行为、不解析密钥、不打印任何内容。
文件头、注释与断言失败文案同步改写；`["回答命中"]` 从"证据二"降级为"证据三"。

本机实测（2026-09-16，真实 API + 真机截屏）：

```text
=== RUN   TestComputerUseLiveSmoke
[config] parsed: agent=2 subagent=2 goalplan=2
出站 provider 请求：带图 1 次 / 不带图 1 次，最大随图 279435 字节（真实转发到 https://api.deepseek.com）
真实 API + computer use 冒烟通过：session=sess_1789562025454485900
  截图引用=[media:9b25a129e3d1af3668f5730b3c184b3214915639695852c0b9faf99ffcd8a88a]
  命中标题片段="Seelex"（前台窗口 "Seelex"）
--- PASS: TestComputerUseLiveSmoke (3.18s)
```

273 KiB 的截图确实作为图像内容进了出站请求——这条才是"画面送达模型"的证据。

## 六、未做 / 待决策

- **假 `gpt-4o` 回退的产品化处置**：正确行为是显式 `AccountNotConfigured` +
  配置向导（见 `docs/devlog/2026-07-28-finish-review.md` 严重问题 #2），而不是
  构造一个能发起外网请求的空账号。这属于**首次启动行为变更**，牵动 GUI/TUI，
  未在本轮擅自改动。
- **像素进 ADVISOR**：`2026-09-15-team-work-computer-use.md` §四已记录
  "证据（句柄 + 元数据）已贯通；像素未贯通"。本轮未动。
- **team work 探针的同类弱断言**：`gui/team_work_computer_use_live_probe_test.go`
  的 `标题命中` 同属"从工具文本可读出"，但它不是该探针的主断言语义（主断言是
  `screen:`/`media:` 证据进入 ADVISOR 输入），本轮未改。
