# Front Matter Parser

## 定位

本包解析 `---` 包围的 YAML front matter 与 Markdown body，供 Plugin manifest 和 Skill loader 复用。

## 实现与边界

解析器接受 bytes，返回 header map/目标结构与正文；错误包含 YAML 或 delimiter 上下文。它不读取文件、不解释 Plugin/Skill 业务字段，也不执行模板。

## Review 指南

- CRLF、UTF-8 BOM、空 header、缺失 closing delimiter 的行为需稳定。
- body 必须原样保留，不能无意 trim prompt 内容。
- YAML 输入不可信，禁止加入动态执行或环境变量展开。

## 测试

```text
go test ./internal/frontmatter -count=1
```
