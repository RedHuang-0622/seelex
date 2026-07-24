# 提交 Generation

## 适用范围

为一个 session 创建 checkpoint。正常运行由 Generation Repository 自动执行；人工操作必须使用未来受支持的管理命令/API，禁止手工编辑 generation 目录或 `current`。

## 前置检查

1. session ID、当前 generation ID 和目标 store 根已确认。
2. 磁盘空间满足本次估算与安全余量。
3. 没有未处理的 store corruption；当前 generation 校验通过。
4. 运行中的 turn 已到安全 checkpoint，或 metadata 明确记录 `interrupted`。

## 流程

1. 调用 Begin，传入 expected parent/current generation。
2. 向 writer 写 snapshot、event segments、cards 和 artifact index；逐项检查大小限制。
3. Commit 生成 manifest、重新校验资源并原子发布目录。
4. repository CAS 更新 current；冲突时保留已发布 generation，但不覆盖新 current。
5. 读取 current 和 manifest，确认 generation ID、父 ID、hash、协议版本与资源数量。
6. 拉取 Session Snapshot，确认其 `generation_id` 已更新且 revision 未倒退。
7. 记录 request/audit ID。

## 成功标准

- `current` 指向完整 committed generation。
- 重新启动 reader 可恢复相同 session 摘要和 transcript 基线。
- staging 无仍被 writer 占用的目录；无资源 hash mismatch。

## 失败处理

- Commit 前失败：Abort，旧 current 不变。
- 最终目录已发布但 current CAS 冲突：不要删除目录；重新读取 current，再决定重试或回收。
- current 更新结果未知：先读 current 和审计结果，禁止盲目重复 mutation。
- 校验失败：隔离 staging/坏 generation，执行 [`disaster-recovery.md`](disaster-recovery.md)。
