# Skill Runtime

## 生态位

`skill` 加载目录化 Skill、解析 `SKILL.md`、限制资源路径，并根据当前 Plugin 计算可见 Skill 集合。

主要调用方：`application/assembly`（任务激活时注入 skill 栈）、`backend/engine/plugin`（激活/停用时的可见集切换）、
`backend/engine/assemble`（装配与调用 Skill 指令）。

`skill_activate` 判据经 `application/policy.SkillActivationPort` 消费；
Wiring adapter 只转发本包 `Activator`，错误文案与可激活性判断没有第二份。

## 架构图

```mermaid
flowchart TB
    subgraph SOURCES["加载位置"]
        PRIMARY["配置根目录：name/SKILL.md（标准）"]
        PLUGINDIR["plugins/<plugin>/<skill>/SKILL.md"]
        LEGACY["name.md（legacy 平铺，兼容）"]
    end

    LOADER["skill.Loader<br/>多 root 按配置顺序查找<br/>目录格式优先于 legacy"]
    SEC["ResourcePath：拒绝绝对路径与 .. 逃逸"]
    REG["skill.Registry<br/>manual + loader 合并（RWMutex）"]
    PLUGINSCOPE["plugin scope<br/>激活后按插件覆盖可见集合<br/>不污染 global registry"]
    TASK["ActivateTaskSkillsLocked<br/>task 级 skill 栈"]
    PROMPT["prompt_layer / context_runtime<br/>进入请求上下文"]

    PRIMARY --> LOADER
    PLUGINDIR --> LOADER
    LEGACY --> LOADER
    LOADER --> SEC
    SEC --> REG
    REG --> PLUGINSCOPE
    PLUGINSCOPE --> TASK
    TASK --> PROMPT
```

## 文件结构

- `loader.go`：多 root 查找、目录/legacy flat-file 兼容、create/delete 和 plugin-dir load。
- `skill.go`：`Skill` 模型、`ResourcePath` 安全解析和线程安全 Registry。
- `activate.go`：`Activator`——"模型侧激活技能"（`skill_activate` 工具后端）的**判据落点**：
  名字校验 / 可激活性（`goal` 不可激活）/ 命中查询；命中后交给宿主注入的落地动作（压 skill 层 + 会话通知）。
  2026-10-16 随 M5-R-skill **接收者转移**从 `application/assembly/skill_activate.go` 归位（plan §2.5）。
- `activate_test.go`：激活判据用例（空名 / 不可激活 / 未知技能 / 名字归一 / 未装配技能表）。

## 加载与优先级

标准格式为 `<root>/<name>/SKILL.md`，资源与脚本留在同一目录。Loader root 按配置顺序优先；目录格式优先于 legacy `<name>.md`。

Registry 合并 manual 与 loader skills；激活 Plugin 后，只暴露该 Plugin 发布的 skills。Plugin skill 同名覆盖由 plugin scope 决定，不污染 global registry。

## 安全边界

`ResourcePath` 拒绝绝对路径和 `..` 逃逸。Skill 名称必须满足安全模式。删除只作用于 Loader primary root 下解析出的目标。

## Review 指南

- reload 是否保留 manual skills，并按 root priority 确定性覆盖。
- plugin activate/deactivate 是否原子切换可见集合。
- front matter body 是否完整保留，resource path 是否 canonical 后再比较。
- 新格式必须保留已有 Skill 的向后兼容或提供迁移。

## Skill 加载位置（Mermaid）

```mermaid
flowchart LR
    subgraph Roots[加载位置]
        DIR["配置根目录：name/SKILL.md（目录格式，资源同目录）"]
        PLUGIN["plugins/插件目录下的 skill 目录"]
        LEGACY["配置根目录：name.md（legacy 平铺，兼容）"]
    end
    LOADER["skill.Loader<br/>多 root 按配置顺序查找<br/>目录格式优先于 legacy<br/>ResourcePath 防逃逸"]
    REG["skill.Registry<br/>manual + loader 合并<br/>plugin 激活后按 scope 覆盖"]
    ACT["激活：ActivateTaskSkillsLocked<br/>task 级 skill 栈"]
    PROMPT["prompt_layer skill 块<br/>进入 provider 上下文"]
    DIR --> LOADER
    PLUGIN --> LOADER
    LEGACY --> LOADER
    LOADER --> REG
    REG --> ACT
    ACT --> PROMPT
```

- 标准格式 `<root>/<name>/SKILL.md`，资源与脚本留在同一 Skill 目录（不跨根引用）；`<root>/<name>.md` 为 legacy 兼容。
- loader root 按配置顺序优先；目录格式优先于 legacy；plugin skill 只在插件激活后可见且不污染 global registry。
- 加载后进入 `skill.Registry`，任务激活时经 `ActivateTaskSkillsLocked` 注入 `prompt_layer` 的 skill 块。

## 测试

```text
go test ./backend/engine/skill/ -count=1
go test ./backend/engine/plugin/ -count=1
go test ./application/assembly/ -run ActivateSkill -count=1   # 门面级集成（压层 + 下一次提交确信它）
```

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### skill.go

- `type Skill struct` — 技能模型（名字 / 描述 / 正文 / 资源根）。
- `func (s Skill) ResourcePath(relative string) (string, error)` — 资源路径的安全解析（留在技能目录内）。
- `type Registry struct` — 线程安全的技能注册表（manual + loader，按 Plugin 作用域隔离）。
- `func NewRegistry() *Registry` / `func (r *Registry) Register(s Skill)` / `func (r *Registry) Remove(name string)` / `func (r *Registry) Get(name string) (Skill, bool)` / `func (r *Registry) All() []Skill` — 注册表的建/增/删/查/列举。
- `func (r *Registry) AddLoader(loader *Loader) error` / `func (r *Registry) Reload() error` / `func (r *Registry) SetPluginSkills(pluginName string, skills []Skill)` / `func (r *Registry) ClearPluginSkills(pluginName string)` / `func (r *Registry) ActivatePlugin(pluginName string)` / `func (r *Registry) ActivatePluginSkills(pluginName string) error` — Plugin 作用域的装载与激活。

### loader.go

- `type Loader struct` — 多 root 的技能加载器（目录格式优先于 legacy flat-file）。
- 其余函数见源码 doc 注释（多 root 查找、create/delete、plugin-dir load）。

### activate.go

- `type SkillLookup interface` — "按名查技能"的窄读口（结构满足 `contract.SkillPort` 的 `Get`）。
- `type ActivateDeps struct` — 激活落点的宿主面（技能表 + 落地动作）。
- `type Activator struct` / `func NewActivator(deps ActivateDeps) *Activator` — 落点本体（无状态）。
- `func (a *Activator) Activate(name string) (model.SkillInfo, error)` — 名字校验 → 命中查询 → 落地；被拒的名字不触发落地。
