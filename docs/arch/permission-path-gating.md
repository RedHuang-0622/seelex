# Permission Gate 路径归属判定

> 状态：详细设计  
> 日期：2026-07-26  
> 依赖：`seele.yaml` 配置扩展

## 1. 问题

当前 `read_file`、`write_file`、`grep_search`、`glob` 等文件操作工具对所有路径一视同仁。但实际上：

- **Plugin 文件**（`plugins/**/plugin.md`、`plugins/**/SKILL.md`）是系统定义，Agent 只应读取，不应修改。修改需通过 `skill.Create`/`skill.Delete` API。
- **Workspace 文件**（用户项目的源码、文档、配置）是 Agent 的工作对象，读写均可能，取决于权限模式。
- **系统配置文件**（`seele.yaml`、`config/accounts.yaml`）Agent 完全不应访问。

没有路径归属判定，Agent 可能误改 Plugin 定义、覆盖系统配置、甚至在 Workspace 外读写。

## 2. 设计

### 2.1 路径分区

| 分区 | 路径前缀 | 读权限 | 写权限 | 审批 |
|------|---------|--------|--------|------|
| **plugin** | `plugins/` | 允许（只读） | 拒绝 | 不需要 |
| **workspace** | Workspace RootPath | 允许 | 允许 | manual 模式下高风险操作需审批 |
| **config** | `seele.yaml`、`config/` | 拒绝 | 拒绝 | — |
| **default** | 其他 | 拒绝 | 拒绝 | — |

### 2.2 YAML 配置

```yaml
# seele.yaml
permission:
  mode: manual  # manual | full_access

  # 路径分区：按前缀匹配，先匹配先生效
  zones:
    - prefix: "plugins/"
      read: allow
      write: deny
      reason: "Plugin 定义文件只读，修改请用 skill API"

    - prefix: "config/"
      read: deny
      write: deny
      reason: "系统配置文件不可访问"

    - prefix: ""          # workspace 路径——在运行时动态注入
      read: allow
      write: allow
      scope: workspace     # 标记为 workspace 区

  # default zone（未匹配任何 prefix 的路径）
  default_zone:
    read: deny
    write: deny
    reason: "路径不在允许范围内"
```

### 2.3 路径规范化

在判定归属前，所有路径必须规范化：

```
1. 去除 \x00 等控制字符
2. 统一分隔符为 /
3. 解析 .. 和 .
4. 拒绝绝对路径（仅允许相对路径）
5. 拒绝包含符号链接的路径（P0）
6. 转为绝对路径后，截取相对工作目录的部分
```

### 2.4 判定流程

```
工具调用 (read_file, write_file, ...)
  │
  ├─ 1. 规范化路径
  │     ├─ 非法字符 → 拒绝
  │     ├─ 绝对路径 → 拒绝
  │     └─ .. 逃逸 → 拒绝
  │
  ├─ 2. 匹配 zone（按 prefix 顺序）
  │     ├─ plugins/xxx → plugin zone: read=allow, write=deny
  │     ├─ src/xxx     → 匹配到 workspace zone（动态注入的 prefix=""）
  │     ├─ config/xxx  → config zone: read=deny, write=deny
  │     └─ /etc/xxx    → default zone: deny
  │
  ├─ 3. 权限模式判定
  │     ├─ full_access: write 放行（仅 workspace zone）
  │     └─ manual: 高风险 write 操作弹审批框
  │
  └─ 4. 执行或拒绝
```

## 3. 运行时 Workspace 注入

Workspace zone 的 `prefix` 在 `seele.yaml` 里是空字符串。运行时由 `PermissionGate` 在收到 Workspace 绑定事件后，将 Workspace 的 `RootPath` 注册到 zone 列表最前面：

```go
func (gate *PermissionGate) BindWorkspace(rootPath string) {
    gate.zones = append([]Zone{{
        Prefix: filepath.ToSlash(rootPath) + "/",
        Read:   Allow,
        Write:  Allow,
        Scope:  "workspace",
    }}, gate.zones...)
}
```

这样 Workspace 路径优先于 default zone，且只在会话绑定 Workspace 后生效。未绑定 Workspace 的会话无法进行文件读写。

## 4. 与现有架构的关系

| 现有组件 | 改动 |
|---------|------|
| `seele.yaml` | 新增 `permission.zones` 字段 |
| `main.go` `setupPermissionGate` | 改为读取 `seele.yaml` 中的 zones 配置 |
| `seelebridge.Runtime` | 加 `BindWorkspacePaths(rootPath)` 方法 |
| Permission 中间件 | 文件工具调用前加路径规范化 + zone 匹配 |

## 5. 不做什么

- **不拦截 MCP 工具**：MCP Server 的文件操作由 Server 自身管理，Permission Gate 只拦截 Seelex 直接注册的文件工具
- **不做文件内容扫描**：只根据路径前缀判定，不读取文件内容做安全分析（那是 AV 的事）
- **不做符号链接实时追踪**：P0 默认拒绝符号链接路径。后续可在 PathGuard 模块实现

## 6. 测试要点

- `read_file("plugins/default/plugin.md")` → allow
- `write_file("plugins/default/SKILL.md")` → deny（提示用 skill API）
- `read_file("../etc/passwd")` → deny（路径逃逸）
- `read_file("/absolute/path")` → deny（绝对路径）
- `read_file("src/main.go")` → allow（workspace zone，已绑定）
- `read_file("src/main.go")` → deny（workspace zone，未绑定 Workspace）
