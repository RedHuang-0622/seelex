# GUI 文档、Effort 与 CI 最终审查

## 1. 变更概览

本次将 Effort 从 Runtime modal 拆为 topbar 常驻四档滑杆，新增 Max 紫色动态光效与失败回滚测试；同时让 `gui` 分支触发独立 GUI CI，并建立 6 个模块详设、11 项 ADR 和带源码位置的功能追溯矩阵。

## 2. 五轴审查

| 维度 | 状态 | 评分 | 结论 |
|------|:---:|:---:|------|
| 正确性 | 通过 | A | input/change 边界、单次提交、Core 权威状态和失败回滚明确，26 个 Node tests 与 Go contracts 通过 |
| 可读性 | 通过 | A- | Effort 独立为 77 行 Controller；app.js 保持 500 行上限，命名和状态机文档一致 |
| 架构 | 通过 | A | Core 仍是唯一业务状态源；Controller 依赖注入；CI job 与平台 build 职责分离 |
| 安全性 | 通过 | A | 无新 HTML 注入路径、无密钥/外部依赖；range 值归一后才进入 Bridge |
| 性能 | 通过 | A | 拖动只预览，避免连续 Bridge/Core 调用；Max 动效只在一档运行并支持 reduced-motion |
| 测试/平台 | 通过 | A- | 本地逻辑、构建和静态视觉通过；远端 GUI tests、三平台 build、race/coverage、govulncheck 全绿 |

## 3. 功能审查结论

| 功能点 | 结论 | 实现与证据 |
|--------|------|------------|
| GUI-EFF-001 常驻且独立于弹窗 | 通过 | `index.html:14-22`；`bridge_test.go:228-241` |
| GUI-EFF-002 四档提交与回滚 | 通过 | `effort-control.js:1-77`；`effort-control.test.mjs:36-77` |
| GUI-EFF-003 Max 紫色动效 | 有条件通过 | `styles.css:72-125,358-361`；Max/Lite Edge 静态截图；真实 WebView 动画时序待 E2E |
| GUI-CI-001 gui 分支触发 | 通过 | `.github/workflows/ci.yml:5-10`；push run `30004410641` |
| GUI-CI-002 GUI tests job | 通过 | `.github/workflows/ci.yml:83-110`；本地与远端均通过 |
| 其余 GUI 功能点 | 见长期矩阵 | `docs/gui/code-review.md` 共 31 项 |

## 4. 语言与工程专项

### JavaScript/CSS

- 无模块级业务可变状态；Controller 状态封装在工厂闭包。
- 未引入 npm 依赖；所有 ES Modules 通过语法检查。
- 异步失败使用 `try/catch/finally`，disabled/pending 必然清理。
- range 原生键盘语义、ARIA text、output 与 reduced-motion 齐全。

### Go

- 仅修改 Bridge contract test，无生产 Go API/import 变化。
- `gofmt`、build、vet、全仓 tests 通过。
- 无 `return nil, nil`、硬编码密钥或包级可变连接。

### CI

- `push/pull_request` 同时覆盖 `main` 和 `gui`，提供 `workflow_dispatch`。
- GUI tests 使用固定 job name、10 分钟 timeout、Node 22 与 Go 1.25。
- Windows production build、Ubuntu race/coverage、release-safety 保持独立。

## 5. 发现的问题

### 严重

无。

### 警告

1. 真实 WebView 动画、键盘、滚动和 modal 仍缺 E2E。
2. `app.js` 已到 500 行工程上限，后续功能继续拆分独立 Controller/View。
3. 本机 `CGO_ENABLED=0`；race 和 GitHub Action 漏洞扫描已由远端 run `30004410641` 验证通过。

### 建议

1. 后续加入 fake Bridge + Playwright，并为 Max/Lite 建立视觉回归基线。
2. 将 `GUI tests` 设为 `gui` 分支必需检查。
3. 在后续发布记录中持续保存对应 CI run URL。

## 6. 最终判断

本地审查与远端 CI 全部通过，本次变更可以合并。真实 WebView E2E/视觉回归仍作为后续增强项，不影响本次交付结论。
