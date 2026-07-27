# 测试报告

## 结论

最小黄金旅程实现通过构建、静态检查、全仓测试、Schema 契约测试和 100 次稳定性复跑。

## 覆盖范围

- E2E-J01：提交、user/assistant item、流式 delta、Chat running 收敛。
- E2E-J02 allow 主链路：Tool running、审批打开、允许、Tool success、审批关闭。
- Scenario loader：未知字段和未实现 expectation 明确失败。
- 事件断言：严格有序前缀与无序收敛事件分离，兼容 ApprovalBroker 的合法并发时序。

## 未覆盖

- E2E-J02 reject/cancel/timeout 失败路径。
- E2E-J03 至 J10：Card、Workspace、Artifact、Session resume、Resync、多会话和 Dev loop。
- L2 Playwright、L3 Wails smoke、L4 live Agent。
- `go test -race` 未在本轮 Windows 环境执行；CI 的 Linux race job仍是并发门禁。

## 2026-07-27 审查后验证

- `TestGoldenJourneyManualPlan` 通过：真实 `WorkPlanTool` 的 `plan_load → plan_run → manual approval → completed PlanState`。
- `TestGoldenJourneyChatToolApproval` 与 `TestGoldenJourneyManualPlan` 各连续运行 100 次，均通过。
- `go vet ./...`、`go build ./...`、`go test ./... -count=1 -timeout=180s` 通过。
- `node --test gui/frontend/dist/*.test.mjs`：26/26 通过。
- `go test -race ./...` 仍未执行：本机缺少 GCC。
