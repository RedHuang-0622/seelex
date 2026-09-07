# Test Documents

本目录存放测试策略、覆盖基线、手工验证步骤、兼容矩阵和非自动化验收记录。

自动化测试的事实来源仍是测试代码和 CI workflow。测试报告应注明 commit、平台、命令、跳过项和失败项，不能把未执行的 race、外部 LLM smoke 或平台测试写成通过。

## 文档索引

| 文档 | 说明 |
|------|------|
| [REPORT-govern-live-api-2026-09-08.md](REPORT-govern-live-api-2026-09-08.md) | 治理循环真实 API 验收报告：mainAgent 发起→TL 裁决收口、headless goal_gov_* 驱动、治理快照与 TL 状态一致性（2026-09-08，deepseek 兼容端点） |
| [](2026-07-27-test-report.md) | 本地复测报告（go vet/build/test + TUI、e2e、seelebridge 全量） |
