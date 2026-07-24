# Generation 故障恢复

## 触发条件

- current 丢失、半写或指向不存在目录；
- manifest/schema/hash 不通过；
- 父链断裂或协议不兼容；
- 多个候选 generation，无法确定最新完整状态；
- store IO 错误持续出现。

## 第一阶段：隔离

1. 停止目标 session mutation；若影响 store 根，停止全部 writer。
2. 不运行清理器，不删除 staging、坏 generation 或日志。
3. 记录绝对 store 根、进程版本、协议版本、错误 code/request ID 和只读目录清单。
4. 复制或快照受影响存储到独立介质；复制时保留权限和时间信息。

## 第二阶段：诊断

1. 验证 store 根没有越界 symlink/reparse 或错误挂载。
2. 读取 current 原始字节，检查是否为单一合法 generation ID。
3. 枚举 generation，只按 manifest schema、session ID、协议、资源大小/hash 判定完整性。
4. 找出 current、最近完整父项和最近完整独立候选；不要仅按目录时间排序。
5. 检查磁盘、权限、杀毒/同步软件和 rename/fsync 平台行为。

## 第三阶段：恢复

- current 坏而目标完整：用管理 CAS/repair-pointer 操作原子重写 current。
- current generation 坏且父项完整：按回滚流程切到父项。
- snapshot 坏但事件完整：执行重建，先生成新 generation，再批准切换。
- 无完整候选：只读导出仍可验证的文本/metadata，保留原始证据，不宣称完整恢复。

## 第四阶段：验证与复盘

启动只读恢复，验证 Snapshot、事件 seq、session/workspace identity、消息/工具配对和 generation ID。再运行一次新 checkpoint，重启验证后才恢复正常 writer。记录根因、影响范围、恢复 ID、数据缺口和防复发测试。
