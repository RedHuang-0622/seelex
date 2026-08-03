# 从事件与资源重建 Generation

## 使用场景

current generation 的 snapshot 缺失或不可信，但存在完整、可验证的父 generation 与后续 event/resource 记录。重建永远创建新 generation，不原地修补旧目录。

## 流程

1. 将目标 session 置为 maintenance/read-only，固定源 generation 和 event segment。
2. 校验源 manifest、父链、所有输入 hash、event schema、scope、seq 连续性和 revision 单调性。
3. 从最近完整 snapshot 开始，用与生产一致的纯 reducer 重放事件。
4. 遇到未知事件、seq gap、外部副作用或不确定 tool 状态时停止；不得猜测。
5. 对重建状态执行领域不变量检查：session ID、message/tool 配对、interaction 唯一性、Workspace binding 和 limits。
6. 通过正常 Begin/Put/Commit 发布一个新 generation，metadata 标记 `reason=rebuild` 和 source IDs。
7. 不自动切 current。先以只读方式打开新 generation，对比摘要、计数和预期 hash。
8. 经批准后按 rollback/CAS 流程切换 current。

## 验收

重建必须可重复：相同已验证输入得到相同规范化 content hash。任何依赖当前时间、随机数或外部网络的字段都从记录输入读取或明确归一化。
