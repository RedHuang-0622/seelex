# FreeCAD Plugin

## 生态位

`freecad` 是 Seelex 的垂直工程能力样板，用于验证 Plugin 能否组合 MCP、批处理脚本、领域 Skill、产物导出和故障恢复，而不是限定 Seelex 只能做 CAD。

## 能力结构

- `cad-core/`：FreeCAD document、primitive、boolean、transform、array、export 和 inspect 基础函数。
- `cad-batch/`：JSON 驱动的批量建模兜底路径。
- `cad-boolean/`、`cad-fillet/`、`cad-inspect/`、`cad-repair/`、`cad-template/`：领域工作流 Skill 与脚本。
- `plugin.md`：工具过滤、FreeCAD MCP 声明和整体操作策略。

推荐顺序是 MCP 交互探索，调用过多或连续超时时切换 batch，最后才使用 headless FreeCADCmd 脚本。

## 当前风险

`plugin.md` 中 MCP command 仍包含开发机绝对路径，这是不可移植配置；跨平台发行前应改为环境变量、PATH discovery 或用户配置。README 和示例不得复制个人路径作为推荐值。

## Review

- 脚本是否 headless、确定性并显式输出产物路径。
- 循环中避免频繁 recompute；批量 primitive 后统一 fuse/recompute。
- MCP timeout 不等于建模失败，fallback 不能重复产生冲突对象。
- 输入 JSON、输出 STEP/STL 和临时文件必须限制在 project/artifact scope。

## 验证

Python/FreeCAD 集成依赖外部安装，普通 Go CI 只验证目录、manifest 和 Skill 契约：

```text
go test ./plugin ./skill . -run 'Plugin|Skill|Layout' -count=1
```
