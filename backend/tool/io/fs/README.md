# FS

## 生态位

`seelebridge/fs` 提供文件系统 actor：`FileSystem` 按路径分片串行化写操作，
避免并行子代理互相覆盖 `write_file`/`edit_file`（P0 修复，2026-08）。

被 `seelebridge` 根包装配消费；不反向依赖根包。

## 时序图

```mermaid
sequenceDiagram
    autonumber
    participant A as 子代理 A
    participant B as 子代理 B
    participant FS as fs.FileSystem
    participant S as 分片（按路径）
    participant D as 磁盘

    A->>FS: write_file(p)
    B->>FS: write_file(p)
    FS->>S: 解析路径分片
    Note over S: 同一路径的写操作串行化
    S->>D: 先提交 A 的写入
    S->>D: 再提交 B 的写入
    Note over D: 后写覆盖先写，但不会出现交叉写入的半成品
```

## 架构图

```mermaid
flowchart LR
    ROOT["seelebridge 根包装配"] --> FSA["fs.FileSystem actor"]
    TOOLS["scoped 文件工具<br/>read/write/edit"] --> FSA
    SUBS["并行子代理 / Plan 节点"] --> FSA
    FSA --> SHARD["按路径分片<br/>同一路径串行"]
    SHARD --> DISK["workspace 磁盘"]
```

## 验证

```text
go test ./seelebridge/fs -count=1
```
