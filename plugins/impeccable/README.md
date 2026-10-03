# impeccable — 前端设计纪律

## 生态位

`plugins/default` 是「全工具入口」，`plugins/freecad` 是 CAD 垂直验证，
`plugins/impeccable` 是**前端设计垂直面**的专业能力：**纪律 + 命令路由 + 确定性检测器**。

它解决的具体问题是「AI 生成前端的模板化」——所有模型都在同一批 SaaS 模板上训练，
于是每个项目都长出同一批 tells（Inter 打天下、紫到蓝渐变、卡片套卡片、彩色底上放灰字、
标题上一块圆角方图标）。本 plugin 把这些从"感觉"变成**写在纪律里、可机械检测**的东西。

## 文件布局

| 路径 | 是什么 |
|---|---|
| `plugin.md` | 机器契约：manifest（`name`/`description`/`include`/`exclude`）+ plugin system prompt |
| [`impeccable/SKILL.md`](impeccable/SKILL.md) | skill 主体：核心原则、四种访客模式、24 条命令路由、Seelex 取证工作流、硬纪律摘要、边界 |
| [`impeccable/reference/craft-floor.md`](impeccable/reference/craft-floor.md) | 逐字 vendored 的**工艺底线**（Verify 清单 + Refuse 清单 + Codex 三律） |

工具面不裁剪（`include: []` / `exclude: []`）：视觉工作要用 `bash` 跑检测器、
`computer_screenshot` 取渲染证据、`plan`/`goal` 承载长任务，裁剪会把手脚砍掉。
本 plugin 与 `default` 的差别在**注入的纪律与 skill**，不在工具开关。

## 出处与许可

| 项 | 值 |
|---|---|
| 上游 | [`pbakaus/impeccable`](https://github.com/pbakaus/impeccable)（作者 Paul Bakaus） |
| 许可 | **Apache-2.0**（上游仓库 `LICENSE`）；本目录下游内容受同一许可约束，保留出处 |
| 上游定位 | 「1 skill、24 commands、live browser iteration、61 deterministic detector rules」 |
| 血统 | 起自 Anthropic 的 `frontend-design` skill，再扩成纪律 + 命令 + 检测器 |

本目录**只 vendored 一份** `craft-floor.md`（逐字，带出处头）。上游还有 40+ 份
命令 playbook（`skill/reference/<command>.md`）与一个自包含引擎二进制，**故意不整包复制**：
它们量大、更新频繁，整包 vendored 会让仓库堆积无法追踪的第三方文本。
按需取用见下一节。

## 使用

```text
$impeccable          # 召回 skill（设计任务一开始就召回）
# 首次使用：先把当前 Plugin 切到本 plugin（前缀 # + plugin 名）
```

改动 UI 后的机械取证（61 条规则，零 LLM、零 API key）：

```bash
npx -y impeccable detect --json <改动路径>     # 首次会下载引擎到 ~/.impeccable/bin
npx -y impeccable detect src/ index.html        # 目录 / 文件
npx -y impeccable detect https://example.com    # 渲染后的页面（用本机 Chrome/Edge）
npx -y impeccable ignores list                  # 看豁免
```

退出码：`0` 无 primary findings / `2` 有 primary findings / `1` 有目标没扫成。
人读报告走 stderr，机器读用 `--json`（stdout）。

按需取命令 playbook：

```bash
BASE=https://raw.githubusercontent.com/pbakaus/impeccable/main/skill/reference
curl.exe -sSL "$BASE/audit.md"      # 或 polish.md / critique.md / new-work.md / live.md …
```

## 更新

- **vendored 的 `craft-floor.md` 不要手改**：重新拉上游同一路径覆盖（文件头写了出处与拉取日期）。
- 想要**整包**（含 40+ playbook 与引擎）：把上游仓库作为 submodule 或 `npx impeccable install`
  到某个宿主目录，再按需把内容并进本 plugin。
- 改了本 plugin 的**文件布局/名称**时，按 `plugins/README.md` 的维护规则同步索引与
  `e2e/layout_test.go` 的 `repositoryModules` 清单。

## 测试

```text
go test ./plugin ./skill -count=1
go test ./e2e -run 'Plugin|ModuleReadme|Repository' -count=1
```

## 边界（与 skill 一致）

- 不接后端与非 UI 任务。
- `live` 模式只对本地 checkout；线上站点只做只读检测，不动生产 CSP。
- 检测器读数只是**证据之一**：干净 ≠ 好看，渲染证据与关键路径冒烟同样要交。
