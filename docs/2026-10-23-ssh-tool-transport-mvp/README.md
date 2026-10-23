# SSH 工具族双传输 —— 孤立可行性 MVP

一次性工作包，**不属于产品链路**：不注册工具、不改派发、不碰权限。设计见 [`../../docs/arch/ssh-tool-transport.md`](../../docs/arch/ssh-tool-transport.md)。

## 跑法

```bash
go run ./docs/2026-10-23-ssh-tool-transport-mvp
```

全部 9 组场景 PASS 才退出 0。程序自带进程内 sshd 夹具（`fixture.go`），**不需要真机、不联网**。

## 它证明了什么

| 场景 | 结论 |
| --- | --- |
| S1 | 同一份 handler 骨架跑在本地/远端两面，`read_file` 结果逐字节一致（A1/A2） |
| S2 | 远端写读往返，中文与制表符/换行不掉字节（路径含中文也成立） |
| S3 | `edit` 的"唯一串"判据两面共用：第二次替换同一片段，两面报同一个错误码（A2） |
| S4 | 远端退出码 3 ⇒ `completed` + `exit_code=3`，**不是失败** |
| S5 | 远端路径不存在 ⇒ `SSH_PATH_NOT_FOUND`，与本地 IO 错分开（A6） |
| S6 | 选择矩阵六格全部可判定，显式意图优先（A3） |
| S7 | `channel_timeout` + `still_running`，不返回 error（A7） |
| S8 | 责任链顺序 = 审计 → 权限 → 选择 → 终端；权限拒绝时**短路**，终端零调用 |
| S9 | 显式指名 `ssh` 但未启用 ⇒ 报错，**不静默回落本机**（A4） |

## 结构

| 文件 | 作用 |
| --- | --- |
| `transport.go` | `Transport` 策略接口（5 个方法）+ 结果形状 + 工具错误码 |
| `local.go` | 本地执行面（os 文件系统 + 本机进程）+ **两面共用的判据**（`applyEdit`/`capContent`） |
| `ssh.go` | 远端执行面（复用 `backend/tool/ssh`）+ `Class → 错误码` 唯一映射表 |
| `chain.go` | 责任链（与框架 `MetaMiddleware` 同形状）+ 三个中间件 + 终端分发 |
| `select.go` | 传输选择的**纯函数**（六格矩阵 + 显式意图优先） |
| `fixture.go` | 进程内 sshd + 极小 POSIX 解释器（只认本设计真正会发出的命令形态） |
| `main.go` | 九组场景 + 断言汇总 |

## MVP 暴露的两个真问题（落地前必须处理）

### F1 非 ASCII 路径不能内联（已在本 MVP 修正）

首轮 S2 直接失败：远端路径 `out/新文件.txt` 使内联命令含非 ASCII，基座按纪律**拒收**（`SSH_NON_ASCII_INLINE_COMMAND`）——它做得对，是设计有洞。

修法：路径与内容**都**走 base64，命令串保持纯 ASCII：

```sh
cat -- "$(printf %s '<base64 路径>' | base64 -d)"
printf %s '<base64 内容>' | base64 -d > "$(printf %s '<base64 路径>' | base64 -d)"
```

这条应写进落地实现，不能只在 MVP 里有：**凡是要进命令串的字符串（路径、参数），一律先 base64**。

### F2 基座无法同时表达"命令 + stdin 数据"（未修，属 M2）

`backend/tool/ssh` 的 `Request` 把 `Inline` / `CommandFile` / `Stdin` 三者做成**互斥**：`Stdin` 被解释成"脚本文本"。而写文件天然需要"命令 `base64 -d > path`" + "数据（base64 载荷）"同时给出，于是 MVP 只能把载荷内联进命令串。

内联的代价：命令长度随文件线性增长（argv/行长上限）、大文件不可行。落地时应给 `Request` 增加一个明确的 `Data` 字段（语义：喂给命令的 stdin 数据），与"脚本来源"分开。这是一行语义的事，不动既有纪律。

## 刻意简化（不要当成已完成）

- 夹具的"shell"只认 5 种命令形态；真机远端命令面（`cat` / `base64 -d` 的可用性）属 M2，失败应回 `SSH_REMOTE_UNSUPPORTED`。
- `bg_manage` 远端面只演示**句柄命名空间带 host 前缀**，没有实现日志增量取回。
- pathGate 未按 transport 换根（M3），因此远端路径越界在 MVP 里由远端语义兜底而非闸门。
- 没有接权限主体/路由组：`PermissionMiddleware` 是桩。
