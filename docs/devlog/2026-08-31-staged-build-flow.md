# 2026-08-31 分阶段构建/部署/发布流程

> 日期: 2026-08-31 | 范围: `scripts/` + 根 `Makefile` + 构建文档 | 无业务代码变更

## 背景

用户要求: 重建当前 dev GUI，并把它变成「更合理、更简便」的流程:

1. 先 build 到暂存区；
2. 检查 seelex 进程：有进程则等其退出且用户允许后再覆盖基线工作区二进制；
   无进程则直接覆盖；覆盖前保留上一可用版本（stash）便于回滚；
3. 用户允许后，再产出各系统可运行的发布版本（携带 tag，配置除 key 外全部带上，
   即仅 example 配置）。

基线工作区 = `dist/seelex-gui-dev/`（含真实 `config/accounts.yaml`、`seelex.yaml`、
`seele.yaml`、`plugins/` 与 `.seelex/` 会话数据，是当前可工作的区域）。

## 现状审查（缺口与未完成项）

- `scripts/build-dev.sh`（post-commit hook）与 `make rebuild-gui` 直接写/重建 dist，
  无进程检查、无确认门禁、无备份回滚；`make release` 的 clean 会删除
  `dist/seelex-gui-dev`（2026-08-12 事故，见 `MEMORY.md`）。
- 无 stash/回滚区，覆盖后无法一键恢复上一可用版本。
- 无构建产物冒烟测试：Makefile 与脚本都没有对二进制做无头启动验证。
- 本地无「不清空基线」的跨平台发布路径：`make release`/`scripts/build.ps1` 都清理 dist。
- 布局不一致：`scripts/build.ps1` 把 `seele.yaml`/`seelex.yaml` 复制到包根，
  而 `build-gui.ps1`/`Makefile`/CI 复制到 `config/` 下；归档格式 zip 与 tar.gz 混用。
- `dist/seelex-gui-dev/seelex-gui.exe~`（约 37 MB）为来源不明的陈旧备份，未清理。
- `internal/buildinfo/version.go` 仍是 `dev`；CHANGELOG 计划版本与已有 tag 需要用户决策。
- `scripts/README.md` 尚未完全符合 `docs/arch/readme-spec.md`（生态位标题/文件函数索引）。
- CI（`.github/workflows/release.yml`）已覆盖公开发布：跨平台 CLI + Windows GUI +
  校验和 + 私有配置审计；缺的是本地开发部署链路的同等安全网。

## 落地内容

新增 `scripts/seelex-flow.ps1`（PowerShell 5.1 兼容，UTF-8 with BOM）：

- `Stage`：构建 GUI 到 `tmp/staging-gui/`（暂存区，不触碰基线）。
- `Smoke`：无头冒烟（`-version` + `-frontend backend -backend-prompt /help`），
  报告按时间戳保留在 `tmp/smoke/`（首个报告不覆盖，供恢复参照）。
- `Deploy`：进程检测 → 确认门禁 → 旧二进制存入 `tmp/stash/seelex-gui-dev/`
  （`previous.exe` + 最近 5 份历史）→ 覆盖 `dist/seelex-gui-dev/seelex-gui.exe` →
  SHA256 校验。只替换二进制，`config/` 与 `.seelex/` 不动。
- `Rollback`：从 stash 恢复 `previous.exe` 回基线（同样有门禁与校验）。
- `Release`：要求 SemVer tag；构建 windows/amd64、linux/amd64、darwin/amd64、
  darwin/arm64 CLI + Windows GUI（Publish），运行时文件与 example 配置进入
  `config/`，zip/tar.gz + sha256；发布前审计无 `accounts.yaml`/`*.local.yaml`/`.seelex`。
- `All`：Stage → Smoke → Deploy → Smoke → Release，逐阶段确认。

根 `Makefile` 新增目标：`stage-gui` / `smoke-gui` / `deploy-gui` / `rollback-gui` /
`release-dev` / `dev-flow`（`CONFIRMED=1` 跳过交互确认，供已获授权的 Agent/CI 使用）。
`scripts/README.md` 增加「分阶段部署流程（推荐）」章节。

## 验证

- `scripts/seelex-flow.ps1` 经 PS 5.1 解析器校验通过（含 BOM 后 0 error）。
- `Stage` 构建成功：`tmp/staging-gui/seelex-gui.exe`（35.5 MB，version=dev）。
- 首次冒烟 PASS（报告 `tmp/smoke/smoke-seelex-gui-<时间戳>.log`）：
  `-version` exit=0 输出 dev；backend 启动 exit=0 且观测到 `startup.frontend.ready`。
- 部署与发布待用户确认后执行；确认后将在基线产物上做第二次冒烟并保留报告。

## 追加：聊天 / 轨迹视图分离 + 上下文轴（同日）

用户随后提出前端改造：聊天视图只显示回复正文，工具调用与 thinking 全部一行带过；
轨迹视图才有完整查看，且轨迹顶部需要一条「上下文轴」（类 DevTools Network 的
Overview，但横轴是对话顺序 + 内容体量，与时间无关）。

实现：

- 后端把 `ReasoningContent` 透传到可见消息：`application/model/state.go` 增加
  `reasoning_content,omitempty`；`adaptEngineMessage` 与冷加载路径同步；
  `runChat` 回合结束后经 `attachLatestReasoning` 从引擎历史挂最后一次 assistant
  推理，并发布携带 `reasoning_content` 的 `message.delta` 事件。
  可见 `content` 仍走 `VisibleOutputStream` 剥离 `<think>`，回复与思考不混排。
- 前端 `components.js`：assistant 消息新增「思考 · N 字符」一行 chip；工具调用
  从 IN/OUT 卡片改为「工具 name + 状态 + 耗时 + 体量」一行 chip；chip 点击跳转
  轨迹视图并定位对应行。`protocol.js` 支持 `reasoning_content` 增量事件。
- 前端 `trajectory.js`：llm 记录携带 reasoning，详情新增完整 THINK 面板；
  新增 `renderContextAxis`（按记录体量分段的横向轴 + 类型图例）。
  `trajectory-view.js` 接入轴容器，点击轴段切回全量过滤、滚动定位并高亮。
- `styles.css`：新增 chat-chip、上下文轴、THINK 面板与定位高亮样式，沿用
  既有暗色仪器风 token（黄铜主色 / 语义状态色 / mono 标签）。
- 测试同步：`components.test.mjs`、`trajectory.test.mjs`、`protocol.test.mjs`、
  `event-chain.test.mjs` 更新为新契约；`gui/bridge_test.go` 的前端契约断言
  从「聊天区 IN/OUT」改为「聊天一行 chip + 轨迹完整 IN/OUT/THINK + 上下文轴」；
  新增 `application/core/reasoning_visible_test.go` 验证推理挂载与事件。

验证：

- `node --test gui/frontend/dist/*.test.mjs`：161/161 通过。
- `go test ./application/... ./gui/...` 通过；`go test .` 通过；
  `go test ./... -p 1` 全量通过。
- 已知既有抖动（与本次改动无关）：全量并行 `go test ./...` 时
  `TestFullAccessBashToolCompletionReachesApplication` 的 8s 请求启动等待偶发超时，
  串行或单测均稳定通过。
- 第二次 Stage 构建（含前端改造）后冒烟 PASS：报告
  `tmp/smoke/smoke-seelex-gui-20260831-234644.log`，第一个冒烟报告保留作恢复参照。
