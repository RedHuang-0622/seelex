# Plugin / Skill 文件架构重构方案

> 日期：2026-07-19
> 状态：方案草案

---

## 一、当前问题

### 1.1 技能与脚本分离

```
skills/cad-batch/SKILL.md          ← 只有提示词，没有脚本
plugins/freecad/scripts/xxx.py     ← 脚本散落在这里
_fc_batch5.py                      ← 还有脚本在项目根目录
```

一个 skill 的提示词和相关脚本割裂在三个地方，LLM 拿到提示词后找不到脚本。

### 1.2 技能目录不归插件管

```
skills/               ← 全局技能，所有插件都可见
plugins/freecad/      ← 插件自己的技能放哪？
```

`switch_plugin freecad` 只切换了系统提示词和 MCP 工具，但 **技能列表不变**。`#cad-batch` 在 default plugin 下也能调，不合理。

### 1.3 脚本散落无归属

```
_fc_batch5.py         ← 根目录
_fc_flange.py
plugins/freecad/
  scripts/            ← 又一个脚本目录
  skills/             ← 之前错误创建的 skill 目录
```

脚本没有和具体技能绑定，不知道哪个脚本属于哪个 skill。

---

## 二、目标架构

### 2.1 目录结构

```
plugins/
  <plugin_name>/
    plugin.md                     ← Plugin 配置 + 系统提示词
    <skill_name>/
      SKILL.md                    ← Skill 提示词
      script1.py                  ← 脚本（可选，0-N 个）
      script2.py
      config.json                 ← 配置文件（可选）
    <another_skill>/
      SKILL.md
      tool.sh
    ...
```

**例子：**

```
plugins/
  freecad/
    plugin.md                     ← FreeCAD 系统提示词
    
    cad-flange/                   ← Skill: 法兰设计
      SKILL.md                    ← 提示词：怎么用法兰 skill
      gen_flange.py               ← 脚本：法兰参数化生成
      
    cad-boolean/                  ← Skill: 批量布尔
      SKILL.md
      batch_boolean_cut.py        ← 脚���：多工具融合后一次切割
      batch_placement.py          ← 脚本：圆周/直线阵列
      
    cad-fillet/                   ← Skill: 圆角处理
      SKILL.md
      batch_fillet.py             ← 脚本：分批倒角
      
    cad-inspect/                  ← Skill: 诊断测量
      SKILL.md
      inspect_objects.py          ← 脚本：对象诊断
```

### 2.2 行为定义

| 操作 | 效果 |
|------|------|
| `switch_plugin freecad` | ① 加载 `plugin.md` 正文为系统提示词 ② 加载 `plugins/freecad/` 下所有子目录为 skill ③ 只有这些 skill 对 LLM 可见 |
| `switch_plugin default` | ① 加载 `plugins/default/plugin.md` ② 只有 `plugins/default/` 下的 skill 可见 |
| `#cad-flange` | 在可见 skill 中查找 `cad-flange`，注入 `SKILL.md` 提示词 + 相关脚本路径到上下文 |

### 2.3 核心改变

| 维度 | 改之前 | 改之后 |
|------|--------|--------|
| Skill 归属 | 全局 `skills/` | 按插件分区 `plugins/<name>/<skill>/` |
| Skill 可见性 | 所有插件共享 | 只有当前插件的 skill 可见 |
| 脚本位置 | 根目录、`scripts/`、散落各处 | `SKILL.md` 同目录下 |
| 插件切换 | 只切系统提示词 | 切系统提示词 + skill 列表 |
| 全局 skill | 需要清理 | 不再有全局 skill |

---

## 三、涉及改动

### 3.1 Plugin Loader

当前 `plugin/loader.go`：

```go
type Plugin struct {
    Prompt    string      // plugin.md 正文 → 系统提示词
    Skills    []Skill     // 从 <plugin>/skills/ 加载
    MCPServers []MCPServer
    ...
}
```

需要的改动：

```go
type Plugin struct {
    Prompt    string      // plugin.md 正文 → 系统提示词
    Skills    []Skill     // 从 <plugin>/*/SKILL.md 加载（直接子目录）
    MCPServers []MCPServer
    RootDir   string      // plugin 根目录（用于解析脚本路径）
}

type Skill struct {
    Name        string    // 目录名
    Prompt      string    // SKILL.md 正文
    Scripts     []string  // 同目录下的脚本文件路径
    RootDir     string    // skill 目录路径
}
```

### 3.2 Skill Registry

当前有全局 `skills/` 目录的加载逻辑。改为：

- `Registry.AddLoader()` 不再加载全局 `skills/`
- `PublishPluginSkills()` 注册插件 skill 到 registry
- `ActivatePluginSkills(name)` 激活时只暴露该插件的 skill
- `DeactivatePluginSkills()` 停用时清除所有 skill

### 3.3 Suggestions 可见性

改 `application/completion.go` 中 `#` 的补全逻辑：

- 当前：`#` 显示所有 skill（全局）
- 改为：`#` 只显示**当前激活插件**下的 skill

### 3.4 脚本路径解析

Skill 的 `Scripts` 字段存相对于 skill 目录的文件路径。LLM 上下文中注入：

```
可用脚本：
  gen_flange.py        — 法兰参数化生成（plugins/freecad/cad-flange/gen_flange.py）
  batch_boolean_cut.py — 批量布尔切割
```

LLM 需要调用时，通过 `execute_python` 或 `read_file` 传入完整路径。

---

## 四、迁移步骤

| 步骤 | 内容 | 涉及文件 |
|------|------|----------|
| 1 | 删除全局 `skills/` 目录 | `skills/` |
| 2 | 清理 `plugins/freecad/scripts/` | `plugins/freecad/scripts/` |
| 3 | 清理 `_fc_*.py` 根目录脚本 | `_fc_*.py` |
| 4 | 创建 `plugins/freecad/<skill>/SKILL.md + 脚本` | 新目录 |
| 5 | 修改 `plugin/loader.go` 支持子目录 skill | `plugin/loader.go` |
| 6 | 修改 `plugin/manager.go` 技能可见性 | `plugin/manager.go` |
| 7 | 修改 `application/completion.go` skill 补全范围 | `application/completion.go` |
| 8 | 修改 `skill/` 包的注册/激活逻辑 | `skill/skill.go` |
| 9 | 迁移已有 skill | `skills/cad-*` → `plugins/*/` |
| 10 | 全量测试 | `go test ./...` |

---

## 五、目标效果

```
改之前：
  #cad-batch 在 default plugin 下也能调        ← 不合理
  SKILL.md 在 skills/cad-batch/                ← 和脚本分离
  脚本在 plugins/freecad/scripts/               ← 不跟 skill 走
  根目录还有 _fc_flange.py                      ← 无归属

改之后：
  switch_plugin freecad → 只看到 freecad 的 skill  ← 合理
  #cad-flange → SKILL.md + gen_flange.py 在同一目录  ← 自包含
  _fc_*.py → 全部归入对应 skill 目录                 ← 有归属
  全局 skills/ → 删除                                ← 不污染
```

## 六、不兼容说明

| 变化 | 影响 |
|------|------|
| 全局 `skills/` 删除 | 所有 `#xxx` 调用必须在对应 plugin 激活后才能用 |
| Skill 路径变更 | `#xxx` 的 `SKILL.md` 读取路径自动适配 |
| 脚本路径变更 | `read_file` 调脚本时路径需切换到 `plugins/<name>/<skill>/` |
