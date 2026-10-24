# 前置审查报告

## 需求摘要

把设置界面按全局、账户、角色、会话和项目拆成明确层级；账户新增改为二级弹窗；项目 SSH
连接表移入项目设置；角色默认权限接入唯一解析链；账户角色和凭据状态改为显式事实，并让旧
会话不再被启动配置警告弹窗阻塞。

## 影响文件清单

| 文件路径 | 修改类型 | 具体位置 | 修改原因 |
|---|---|---|---|
| `gui/frontend/dist/index.html` | 修改 | 顶部项目标题、设置弹窗 | 增加设置分类、项目设置入口、账户新增二级弹窗；按作用域拆分 SSH |
| `gui/frontend/dist/app.js` | 修改 | 设置打开/同步、账户管理、SSH、角色权限 | 增加分类切换与独立项目设置打开路径；采纳显式角色和凭据状态 |
| `gui/frontend/dist/styles.css` | 修改 | 设置与弹窗样式 | 为新层级、项目设置按钮和二级弹窗提供布局 |
| `gui/frontend/dist/*.test.mjs` | 修改/新增 | 设置契约测试 | 钉住作用域边界和二级窗口 |
| `middle/protocol/model/state.go` | 修改 | `AccountInfo` | 快照携带显式角色、凭据存在性和来源，不携带密钥明文 |
| `backend/engine/account/manager.go` | 修改 | `Account` 与 `Accounts()` | 从规格表派生角色/凭据状态 |
| `wiring/adapters/runtime_port.go` | 修改 | `RuntimePort.Accounts` | 跨层投影新增字段 |
| `internal/config/config.go` | 修改 | fallback 配置 | 明确标记内置兜底账号 |
| `gui/bridge.go` | 修改 | `PermissionRoles` | 按显式角色分组，不再依赖账号名猜测 |
| `contract/dto/role_settings.go` | 修改 | 层级解析 | 增加 `role_default` 层并保持逐标签唯一解析 |
| `wiring/adapters/role_permission_port.go` | 修改 | `RolePermissionLayers` | 角色默认不再冒充 `global_default` |
| `application/core/role_permissions.go` | 修改 | 角色默认读取 | 区分角色默认与当前会话生效值 |
| `gui/run_wails.go` | 修改 | `OnDomReady` | 启动配置问题改为非阻塞通知，旧会话窗口仍可进入 |
| 相关 README / `docs/arch` | 修改 | 设置与权限说明 | 同步当前实现和作用域边界 |

## 依赖分析

- 上游：Application Snapshot/Command API、Bridge、Runtime 账号摘要。
- 下游：GUI 设置渲染、角色权限恢复、SSH 项目环境读写。
- 数据：账号密钥只留在宿主侧；前端只接收 `api_key_set`，不接收明文。

## 循环依赖检查

- [x] DTO 只承载数据，不引用 GUI/Application/Backend 实现。
- [x] GUI 继续只依赖 Application API。
- [x] 项目 SSH 与会话 SSH 复用既有端口，不新增旁路。

## 风险预估

- 设置 DOM 重排导致既有元素契约失效：中概率、中影响；用 Node 契约测试覆盖。
- 权限层级改动改变既有角色生效结果：中概率、高影响；先写五层解析和适配层用例。
- 旧配置解析失败：中概率、高影响；保留容错加载并移除阻塞式警告。

## 建议方案

先修数据契约与权限链，再做前端作用域重排，最后删除阻塞弹窗并跑全量验证。账户新增保持
写账号档、读回、热注册的单一路径；API Key 永不回传前端。
