# auto_get_jobs（定时任务白名单命令的脚本目录）

这个目录由 Seelex 在启动时**按需初始化**：当候选链（相对当前工作目录 →
相对二进制所在目录）上都找不到 `local/tools/auto_get_jobs/main.py` 时，
`internal/bootseed` 会把这份骨架写进来，让人一眼看到"要放什么、放哪儿"。

## 这里应该有什么

| 文件 | 谁提供 | 说明 |
|---|---|---|
| `main.py` | **你自己** | 被调度的入口，见下 |
| `.env` | **你自己** | 脚本自己的密钥/配置（`API_URL` / `API_KEY` / `MODEL` 一类），**不要提交** |
| `requirements.txt` / `.venv` | **你自己** | 依赖与虚拟环境 |

脚本本体在 Seelex 里**不分发**：白名单命令指向的是本机这份目录，默认那条
`auto_get_jobs` 来自上游第三方项目 <https://github.com/SanThousand/auto_get_jobs>
（按它自己的 README 安装依赖、准备 Chromedriver 与 `.env`）。

## 白名单命令的登记条件

`main.go` 的 `resolveAutoGetJobsDir` 只有在**本目录里存在 `main.py`**、并且
`resolvePythonCommand` 找到了 `python`/`py` 时，才登记白名单命令。缺任何一条都
会跳过登记并打日志：

```text
scheduled tasks: auto_get_jobs 脚本目录未找到（期望 local\tools\auto_get_jobs）...
scheduled tasks: 未找到 python 解释器，跳过 auto_get_jobs 白名单命令登记
```

这是既有安全口径的一部分（登记即信任、argv 固定直传），**不要**为了让下拉框
有值而放宽它：命令类任务只跑这里登记过的脚本。

## 补齐之后

1. 把上游项目放到本目录（或把已有的 `main.py` 放进本目录）；
2. 配好 `.env` 与 `user_requirements.txt`；
3. 重启 Seelex —— `新建定时任务` 弹窗的"命令"下拉才会出现
   `BOSS直聘自动投简历`。
