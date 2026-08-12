# Seelex 开发态 GUI 包

这是由 `scripts/build-gui.ps1 -BuildKind Dev` 生成的本地 smoke 测试包。

运行方式：

```powershell
.\seelex-gui.exe
```

包内的 `config/accounts.yaml` 是构建时指定的本地账号配置，勿提交或转发。会话存储会写入包目录下的 `.seelex/`。

启动阶段不会创建 framework Session。点击“新建会话”只进入未持久化 draft；首次提交真实问题时才创建 Session，恢复已有会话时只创建被恢复的 Session。

如需重新打包，请在源码仓库根目录执行：

```powershell
..\scripts\build-gui.ps1 -Version dev -BuildKind Dev -LocalConfigPath config\accounts.yaml
```
