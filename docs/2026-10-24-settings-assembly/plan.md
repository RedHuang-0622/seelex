# 设置与角色装配整改计划

## 1. 契约

- `AccountInfo` 增加 `role`、`api_key_set`、`source`；不增加密钥明文字段。
- 账号管理器从 `AccountSpec.Role` 派生角色，名称前缀只作为缺字段时的兼容兜底。
- 权限解析增加 `role_default` 层，顺序为
  `user_allow > agent_dispatch > session_default > role_default > global_default`。
- 角色默认存储不再写入 `global_default`；`PermissionRoleInfo` 同时给出生效向量、默认
  向量、`inherit_main_agent` 与 `ssh_user_spaces`。subagent / teammate 默认继承
  mainagent 的权限和远端路径范围。
- 账号档支持稳定 `name:`；界面新增或首次编辑/删除时冻结位置名，账户 CRUD 不会导致
  同角色其它账号改名。

## 2. GUI 作用域

- 应用设置：「角色与权限 / 会话权限 / 应用」，全局不含 SSH。
- 角色与权限：四张 agent role 卡；卡内折叠展示候选/确认权限栏、继承 chip、账户、
  余额与上下文窗口。账户新增/编辑与删除各走独立二级弹窗。
- 会话权限：只做当前会话按 agent role 的覆盖；全局继承开关不锁会话覆盖。
- SSH 用户空间：勾选时先弹窗列出远端绝对路径，保存后才授予权限。
- 项目设置：从项目标题区打开独立弹窗，仅管理项目 SSH 环境表。
- 所有相关弹窗的关闭按钮固定在上沿，内容滚动时位置不变。

## 3. 兼容

- 账号档继续使用现有角色分组格式；缺失显式角色字段的旧快照按账号名前缀兼容。
- 配置错误继续容错启动，但不再用原生模态框阻塞界面；通知使用既有会话可见区。
- fallback 账号在快照中明确标记来源，避免与用户配置的账号混淆。

## 4. 验证

1. 红：新增设置层级、五层权限与账号显式字段的契约用例。
2. 绿：`go test ./contract/dto ./backend/engine/account ./wiring/adapters ./application/core ./gui`。
3. 前端：`node --test gui/frontend/dist/*.test.mjs`。
4. 全仓：`go test ./... -count=1 -timeout=120s`、`go build ./...`、`go vet ./...`。
5. 提交后由 `post-commit` 重建 dev GUI，再逐项冒烟设置层级、账户新增、角色权限、
   项目 SSH、会话放行和旧会话打开。
