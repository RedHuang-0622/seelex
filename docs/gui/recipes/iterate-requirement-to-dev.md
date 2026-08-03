# 运行证据门禁的需求到 Dev 自迭代

## 启动条件

1. 固定 source generation、项目/平台适用范围、retrieval profile 和 gate policy version。
2. 配置最大迭代次数、允许自动执行的 work item 类型和人工审批责任人。
3. SourceCatalog 完成版本、chunk、hash、ACL 和 authority metadata 校验。
4. 当前 run 无未恢复的 baseline publish 或实现提交。

## 流程

1. 生成候选需求并拆分 mandatory/optional atomic claims。
2. 对每个 claim 执行 exact、BM25、vector、graph retrieval；保存 query digest 和完整回执。
3. 独立判定每条 evidence 的 supports/contradicts/related/insufficient 关系。
4. 用版本化 policy 计算 evidence readiness 和四级 gate decision。
5. 将非 eligible 条目加入人工队列；不得从范围清单删除。
6. 人工处理补证、冲突、项目特有、限制接受、澄清、驳回或延期。
7. 发布需求 baseline generation；范围报告列出 eligible 与 pending/rejected/deferred。
8. 对 eligible requirements 执行架构分配和架构证据门禁，发布架构 generation。
9. 冻结接口/Schema 后执行详设门禁，生成设计项和 verification points，发布详设 generation。
10. 生成带 trace 和验收标准的 Dev work items；经批准后实现并固定 commit/revision。
11. 执行 schema、unit、integration、race、security 和 E2E；保存 test evidence。
12. 对失败分类并路由到正确层。重开上游时失效受影响下游资格，发布后继 generation。
13. 全部门禁通过则完成；达到循环上限、关键冲突或连续重复失败则停止并转人工。

## 人工检查点

- 安全相关 requirement 自动通过后的强制签署；
- `mark_project_specific` 与 capability gap 创建；
- 关键 evidence conflict 处置；
- 架构接口冻结；
- Dev 对仓库写入和任何破坏性操作；
- release gate。

## 故障恢复

恢复时读取 [`dev-iteration.schema.json`](../schemas/dev-iteration.schema.json) run、固定 baseline generation 和实现 revision。只重放已提交状态；in-progress LLM/RAG/Dev 调用视为中断，通过幂等键查询结果或重新执行。不得推断一次外部副作用“应该已经成功”。

## 完成记录

归档 run、assessment、retrieval receipts、人工决策、四层 baseline generation、implementation revision、测试报告、feedback closure 和最终 trace matrix。敏感 source 内容仍按 ACL 保存，不复制进通用日志。
