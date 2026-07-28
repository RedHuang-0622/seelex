# GitHub Automation

## 模块定位

`.github/workflows` 定义 Seelex 的持续集成、前端契约、race/coverage、安全扫描和发布流程，是本地验证要求的自动化事实来源。

## Workflows

- `ci.yml`：格式、普通/GUI 构建、vet、全量 Go 测试、源码安全 grep、前端 Node tests、Linux race+coverage、release allowlist 和 govulncheck。
- `release.yml`：按 tag 构建和打包发布产物，注入版本号并复制公开运行资源。

## 设计原则

- 无业务依赖的 job 分开并行，缩短反馈时间。
- Linux 承担 race runner；Windows 承担真实 GUI build-tag 编译。
- 安全 grep 规则必须精确，不能因多返回值或 JSON tag 产生系统性误报。
- release 只能使用白名单文件，禁止携带本机账号、local config、session data 或开发缓存。

## Review 指南

- Action 固定到可信版本或 commit，权限采用最小值。
- 新 job 是否真的独立，是否错误依赖 build/test 顺序。
- shell 命令是否跨平台，bash 与 PowerShell 语义是否混用。
- 自动创建 Issue 时必须去重、标记来源，并避免秘密进入日志/body。

本地等价命令以根 [`AGENTS.md`](../AGENTS.md) 为准。
