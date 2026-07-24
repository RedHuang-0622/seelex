# 回滚到历史 Generation

## 原则

Rollback 只切换 session 的 current 指针并重新恢复状态，不修改历史 generation。它是破坏当前可见状态的管理操作，必须显式授权、expected current 和审计。

## 流程

1. 暂停目标 session 接受新 turn；等待或取消当前执行并 checkpoint 为 interrupted。
2. 读取 current，记录 `from_generation_id`。
3. 校验目标 manifest、全部资源 hash、协议兼容性和 session ID。
4. 导出/固定当前 generation，确保可反向恢复。
5. 调用 rollback，携带 `expected_generation_id=from_generation_id`。
6. repository CAS 更新 current；Application 从目标恢复，并分配新的内存 revision。
7. 拉 Snapshot 验证 session、generation、conversation 摘要、Workspace binding 和 pending interaction。
8. 运行只读 smoke；确认后恢复 mutation。

## 禁止项

- 不复制目标文件覆盖 current generation。
- 不在校验失败时使用 `force` 跳过 hash。
- 不回滚到其他 session 的 generation。
- 不把恢复前的 `running/awaiting_approval` 原样继续执行。

## 回滚失败

CAS 冲突时停止并重新评估，不重复使用旧 expected ID。恢复失败但指针已更新时，立即尝试切回已固定的 from generation；仍失败则保持只读并进入故障恢复。
