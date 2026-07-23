# Skill 用户上下文测试报告

> 日期：2026-07-24
> 环境：Windows amd64，Go 1.26.5，`CGO_ENABLED=0`

## 概览

| 范围 | 结果 | 关键指标 |
|------|:----:|----------|
| 格式、编译、静态分析 | ✅ | gofmt、CLI build、GUI production build、go vet 全通过 |
| Go 全量测试 | ✅ | 15 个含测试 package 全通过，74.0s |
| Application 稳定性与覆盖率 | ✅ | `count=3` 全通过，statement coverage 76.8% |
| GUI JavaScript | ✅ | 全部 JS syntax check；26/26 Node tests 通过 |
| 安全与发布静态门禁 | ✅ | 无硬编码密钥命中、无非测试 `return nil, nil`、config 白名单正确 |
| Race | CI 承接 | 本机无 C compiler 且 CGO 关闭；`.github/workflows/ci.yml` 的 Ubuntu job 执行 `-race` |

## 执行记录

| 命令 | 结果 | 耗时/摘要 |
|------|:----:|-----------|
| `gofmt -l .` | ✅ | 无输出 |
| `go build ./...` | ✅ | 18.6s |
| `go build -tags "gui,desktop,production" ./...` | ✅ | 19.7s |
| `go vet ./...` | ✅ | 15.7s |
| `go test ./... -count=1 -timeout=180s` | ✅ | 74.0s；全部 package 通过 |
| `go test ./application -count=3 -cover -timeout=60s` | ✅ | 6.589s；76.8% statements |
| `node --check gui/frontend/dist/*.js` | ✅ | 全部脚本语法通过 |
| `node --test gui/frontend/dist/*.test.mjs` | ✅ | 26 passed，0 failed |
| `git diff --check` | ✅ | 无 whitespace error |

## 本功能覆盖

| 测试维度 | 用例 | 结论 |
|----------|------|------|
| 单元 | `TestFormatSkillUserInputKeepsPlainInputUnchanged` | 无 Skill 输入不装饰 |
| 单元 | `TestFormatSkillUserInputCreatesItemizedContext` | 名称、指令、原问题完整且顺序稳定 |
| 边界 | `TestDisplayUserInputRejectsInvalidEnvelope` | 非法 marker 不丢数据 |
| History | `TestAdaptEngineMessageRestoresOriginalUserInput` | Engine input 恢复 UI 原文 |
| Queue | `TestCombineChatRequestsPreservesDisplayAndSkillBodies` | 批量只有一个外层 envelope，各项 Skill 不丢失 |
| 路由 | `TestSuggestionsAndSkillRouting`、`TestSkillLoadViaSubmit` | hash/slash 均发送需求，system prompt 隔离 |
| 状态 | `TestSkillWithoutRequirementAppliesToNextInput` | 空需求只激活、普通输入携带、`#end` 后停止携带 |
| Queue | `TestQueuedSkillRequestFreezesDisplayAndModelInput` | 排队项在提交时固化，UI 只显示原文 |
| 错误 | `TestSkillUnknown` | 未知 Skill 不启动 Chat |
| LIFO/Goal | `TestSkillEndPreservesGoalLoopLimitUntilGoalIsPopped` | 上层退栈保持 Goal 9999，Goal 退栈恢复 Effort |
| 顺序 | `TestPromptStack_RenderUsesFixedSystemOrder` | system prompt 四层顺序不受重应用影响 |

## 未执行项与承接方式

- 仓库没有 `Benchmark*` 或 `Fuzz*` 测试入口，本轮不伪造性能/模糊结果；格式化与 envelope 均为线性内存字符串处理。
- Windows 本机 `gcc` 不可用，无法本地执行 Go race。推送 `gui` 后由 Ubuntu `race-and-coverage` job 作为合并门禁；其结果将在 CI 中保留。

## 综合判断

- [x] 通过本地交付门禁
- [ ] 等待本次远端 race/coverage CI 结果
