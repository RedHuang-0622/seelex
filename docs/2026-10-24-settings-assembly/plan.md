# 设置与角色装配整改计划

## 1. 契约

- `AccountInfo` 增加 `role`、`api_key_set`、`source`；不增加密钥明文字段。
- 账号管理器从 `AccountSpec.Role` 派生角色，名称前缀只作为缺字段时的兼容兜底。
- 权限解析增加 `role_default` 层，顺序为
  `user_allow > agent_dispatch > session_default > role_default > global_default`。
- 角色默认存储不再写入 `global_default`；`PermissionRoleInfo` 同时给出生效向量、默认
  向量、`inherit_main_agent` 与 `ssh_user_spaces`。subagent / teammate 默认继承
  mainagent 的权限和 `ssh` 路径白名单。
- 账号档支持稳定 `name:`；界面新增或首次编辑/删除时冻结位置名，账户 CRUD 不会导致
  同角色其它账号改名。

## 2. GUI 作用域

- 应用设置：「角色与权限 / 应用」，全局不含 SSH。
- 角色与权限：四张 agent role 卡，只编辑跨会话默认权限；继承 chip 只锁角色默认。
  账户、余额与逐账户甜点上下文也在该卡内；账户新增/编辑与删除各走独立二级弹窗。
- 本会话权限：从会话/项目标题区独立打开，按 agent role 编辑当前会话覆盖，不与全局
  设置混在一起。
- 角色/会话权限中的选中项是直接放行：主代理 root 判定先读 `agent` 角色向量，选中后
  不再落到规则层 ask；未选中的能力仍按既有提权语义处理。
- 上下文窗口属于具体账户与模型，不参与角色继承，也不与账户条目分开放置。
- SSH：权限向量只有一枚 `ssh`；勾选时弹窗列出该能力允许的远端绝对路径。路径是
  能力内的白名单，不是独立的 `ssh.userspace` 权限。角色默认路径按项目保存，
  当前会话可按 agent role 写独立覆盖；未覆盖时回落角色默认。
- 项目设置：从项目标题区打开独立弹窗，直接按项目 ID 管理 1:N 命名工作环境与地址，
  不依赖 session binding；主机留空 = 本地。
  会话首次从项目环境表固定环境，固定后本会话、subagent、teammate 与后台工具都继承，
  agent 与工具入参不能切换；换环境需新建会话。
- 环境事实由适配器无感注入，不作为工具参数暴露；项目环境清单与地址放在请求尾部
  动态块，mainagent 可将同一事实用于分派，子代理实际执行仍走同一绑定。
- 子代理权限向量按 `agent role`（subagent / teammate）解析，业务成员名（如
  `impl_ui`）只用于主体与派发归属。
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
   项目工作环境、会话固定环境、角色 SSH 路径和旧会话打开。
