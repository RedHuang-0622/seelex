# application/workspace

## 生态位

`application/workspace` 承载项目环境表、工作区内 Git 查询和批量项目登记后的
Application 投影编排。当前真实调用方是 `application/core.Service` 的 Workspace
兼容入口；GUI/TUI 仍经 Application 门面消费，不直接依赖本包。

## 职责与非职责

职责：

- 校验项目 ID / 环境名的输入形状并转发到项目环境表端口；
- 把客户端仓库相对路径解析为工作区根内绝对路径；
- 按可选端口能力分派仓库发现、Git 日志、改动、提交详情与提交内文件读取；
- 固定批量登记成功后的 projection、视图锁、revision 和发布顺序。

非职责：

- 项目创建、持久化、绑定或解绑；
- 工作环境凭证、默认环境与存储实现；
- Git 命令执行、目录树扫描、文件内容读取；
- Snapshot 写入、EventHub 或 GUI/TUI 行为。

## 文件结构

- `application.go`：窄端口、`Application`、Workspace 查询与批量登记编排。
- `application_test.go`：环境表、路径 containment、端口降级和发布顺序测试。

## 核心实现

`Deps` 分开接收“按项目 ID 读写的环境端口”“当前视图工作区的根端口”和
“批量登记 + projection 宿主”。工作环境不依赖当前视图会话；Git 查询则必须使用
当前视图的权威根。路径 containment 只由 `resolveRepo` / `withinRoot` 持有。

## 数据流

环境表请求按项目 ID 直接进入 `WorkspaceEnvironmentPort`。Git 请求先取得当前
workspace root，再把受控相对路径解析为仓库根，随后分派到
`WorkspaceGitReposPort`、`WorkspaceTreePort` 或 `WorkspaceFilePort`。批量登记
成功后读取 projection，锁内应用并 bump revision，解锁后发布
`snapshot.changed`。

## 依赖方向

允许依赖 `contract`、`contract/dto`、`middle/protocol/model` 与
`middle/switch/workspace.Projection` 这一只读投影形状。禁止导入
`application/core`、持有完整 `Service`、执行 Git 命令或读写真凭证。

## 并发与错误语义

查询路径不获取共享写锁。projection 采集在锁外，锁内只应用投影并 bump 一次
revision，事件发布在解锁后。未绑定工作区、缺少能力端口和非致命仓库发现失败
保持原错误/`Result.Error` 语义；绝对路径、`..`、软链逃逸和非目录属于调用方错误。

## 扩展方式

新增 Workspace 能力前先确认状态所有者与真实调用方。只允许扩展消费方实际使用的
窄端口；项目创建/绑定等仍由 `middle/switch/workspace` 的既有用例拥有，不复制进
本包。兼容 Service 入口必须保持一行转发。

## Review 指南

- 环境表是否始终按项目 ID，而不是误用当前视图会话；
- Git 相对路径是否经过词法 containment 与软链真实路径双重检查；
- 查询是否误取共享写锁；
- 批量登记失败后是否完全不触碰 projection/revision/event；
- 新包是否 import `application/core` 或持完整 `Service`。

## 测试与验证

```text
go test ./application/workspace -count=1
go vet ./application/workspace
```

关键测试文件：`application_test.go`。

## 文件与函数索引

### `application.go`

- `func New(deps Deps) *Application` — 从消费方端口构造 Workspace Application。
- `func (application *Application) WorkspaceEnvironments(projectID string) ([]model.WorkspaceEnvironmentInfo, string, error)` — 读取项目环境表。
- `func (application *Application) SetWorkspaceEnvironment(projectID, name string, change model.WorkspaceEnvironmentChange) (model.WorkspaceEnvironmentInfo, error)` — 写入项目环境。
- `func (application *Application) DeleteWorkspaceEnvironment(projectID, name string) error` — 删除项目环境。
- `func (application *Application) SetDefaultWorkspaceEnvironment(projectID, name string) error` — 设置项目默认环境。
- `func (application *Application) WorkspaceGitRepos() (dto.GitReposResult, error)` — 发现工作区内 Git 仓库。
- `func (application *Application) GitLog(repo string, limit int) (dto.GitLogResult, error)` — 读取指定仓库提交历史。
- `func (application *Application) Changes(repo string, limit int) (dto.WorkspaceChangesResult, error)` — 读取指定仓库未提交改动。
- `func (application *Application) CommitDetail(repo, hash string, limit int) (dto.GitCommitDetail, error)` — 读取提交文件清单。
- `func (application *Application) CommitFileContent(repo, hash, relPath string, limit int64) (dto.FileContent, error)` — 读取提交内文件内容。
- `func (application *Application) CreateWorkspaces(requests []dto.WorkspaceCreateRequest) (dto.WorkspaceCreateBatchResult, error)` — 批量登记并发布工作区投影。

### `application_test.go`

测试文件按仓库 README 规范不展开函数索引。
