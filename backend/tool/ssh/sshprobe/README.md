# sshprobe —— SSH 连接基座的手工验收探针

一次性排障命令行，不属于产品链路，也不被任何生产代码 import。它存在的唯一理由：
在把某个远端环境交给工具族之前，先用**同一条连接基座**手工验证"连得上、认得主机、
跑得动命令"，从而把"SSH 层的问题"和"工具/权限层的问题"分开。

## 怎么用

```bash
go run ./backend/tool/ssh/sshprobe -host <host> -port 22 -user <user> [-key <path>] [-cmd "uname -a"]
```

- 不给 `-cmd` 时只做探活（dial + auth + 一次最小命令）。
- 主机指纹**未受信**时会明确拒绝并打印指纹，不会静默记下（与运行面同一纪律）。
- 口令只从凭据存储/环境读取，**不接受命令行明文**（避免进 shell history）。

## 边界

| 它做 | 它不做 |
| --- | --- |
| 验证拨号、认证、主机指纹、单条命令往返 | 不注册工具、不判权限、不改任何状态 |
| 打印失败的**分类**（不可达 / 认证失败 / 指纹未受信 / 超时） | 不做重试、不做降级 |

## 与运行面的关系

同源：两者走 `backend/tool/ssh` 的同一个 `Dial`/`Run` 实现。所以这里能跑通，
工具族在同一个环境上就不该因为"连不上"而失败；反过来，这里失败就是 SSH 层的事，
不必去翻权限门和路径门。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### main.go

- `func (s *stringList) String() string`
- `func (s *stringList) Set(v string) error`
- `func main()`
- `func run(args []string, stdout, stderr io.Writer) int` — run 是 main 的驱动函数：解析 → 建 Spec → 分派 → 渲染。它不 os.Exit，
- `func parseFlags(args []string, stderr io.Writer) (config, error)` — parseFlags 解析参数并做**互斥校验**（在连任何地方之前就把用法错误挡住）。
- `func buildSpec(cfg config) (ssh.Spec, error)` — buildSpec 把 config 变成 ssh.Spec：凭据在这一步从文件读入内存，读完即用。
- `func readSecretFile(path string) (string, error)` — readSecretFile 读一个只含凭据的文件：去掉行尾换行（编辑器普遍会补一个）。
- `func doProbe(ctx context.Context, spec ssh.Spec, cfg config, stdout, stderr io.Writer) int` — doProbe 执行探针并渲染结果。
- `func doTrust(ctx context.Context, spec ssh.Spec, cfg config, stdout, stderr io.Writer) int` — doTrust 执行首次信任（指纹必须与本次握手逐字相等，否则不写盘）。
- `func doExec(ctx context.Context, spec ssh.Spec, cfg config, stdout, stderr io.Writer) int` — doExec 执行一次命令（同步或脱钩）。
- `func exitFor(res ssh.Result) int` — exitFor 把 Result 翻成退出码：远端退出码原样透传（这才是脚本要的），
- `func renderResult(w io.Writer, spec ssh.Spec, res ssh.Result)` — renderResult 是人读渲染：先事实（状态/退出码/认证方式），再输出。
- `func isBadInput(err error) bool` — isBadInput 判断错误是否属于"调用方给了不该给的入参"（而非环境问题）。
- `func writeJSON(w io.Writer, v any)`
- `func pickOK(ok bool) string`
- `func displayID(spec ssh.Spec) string`
