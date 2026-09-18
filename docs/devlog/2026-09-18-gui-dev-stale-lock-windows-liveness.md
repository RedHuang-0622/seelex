# 2026-09-18 dev GUI「又打不开」：强杀残留锁 + Windows 存活判定错误

范围：`sessionstore`（Windows 进程存活判定、数据根锁契约测试、模块 README）。
触发场景：`dist/seelex-gui-dev/seelex-gui.exe`（dev 基线包，工作目录即数据根）。

---

## 1. 症状

用户报告：dev GUI「又打不开了」——双击/启动后**窗口不出现、也没有任何提示**。

进程表里没有任何 `seelex*` 进程，`dist/seelex-gui-dev/.seelex/sessions-json/lock.owner`
却还在（本次残留记录：`pid=142248 program=seelex-gui.exe acquired=15:09:04
renewed=15:20:44`，PID 142248 已不存在）。

带 stderr 直接运行 dev 二进制得到唯一一条退出原因（`-H windowsgui` 构建没有控制台，
所以平时看不见）：

```text
[config] parsed: agent=1 subagent=1 goalplan=1
INFO shutdown complete
✖ 初始化嵌套存储失败: session storage: data root is locked by another process:
  pid=142248 program=seelex-gui.exe host=red root=.seelex\sessions-json
  acquired=2026-09-18T07:09:04Z renewed=2026-09-18T07:20:44Z
```

## 2. 定位：不是心跳超时，是「死进程被判成活的」

上一轮修复（2026-09-17，`lock_auto_recover: true`）只覆盖「持有者进程消失」这一支：
`ownerIsStale` → `!processAlive(pid)`。本次残留锁**没有**走进那一支，所以等多久都不会
自愈。

Win32 层实测（本次事故里的 PID 与一个受控子进程都复现了同一形状）：

```text
pid=142248  os.FindProcess+Release => nil（"查得到"）
            OpenProcess(SYNCHRONIZE) => 成功
            WaitForSingleObject(h,0) => WAIT_OBJECT_0  ← 内核判据：进程已终结
            GetExitCodeProcess       => 4294967295
            枚举进程（Get-Process/tasklist）=> 找不到该 PID
```

受控复现（`tmp` 探针，未进仓库）：`cmd /c exit 0` 子进程退出后**不 Wait、不 Release**
（继续握着句柄），父进程对它的 `os.FindProcess` + `Release` 依然返回 `nil`；一旦
`Release()` 放掉句柄，同一个 PID 立刻变成 `OpenProcess: The parameter is incorrect.`。

结论：Windows 的进程对象在「进程已终结、但有别的进程仍持有它的句柄」时依旧可以被
打开，PID 也仍被占用。旧实现的

```go
process, err := os.FindProcess(pid) // 内部只做 OpenProcess
if err != nil { return false }
return process.Release() == nil     // 只证明"能打开"，不证明"还活着"
```

把被强杀的 dev GUI 判成**活持有者** → `ownerIsStale=false` → `ErrDataRootLocked` →
数据根初始化失败 → 进程启动即退出。因为 `-H windowsgui` 没有控制台，用户看到的就是
「打不开」。

## 3. 修法

`sessionstore/process_alive_windows.go` 改为问内核「这个进程终结了没」：

```text
OpenProcess(SYNCHRONIZE | PROCESS_QUERY_LIMITED_INFORMATION)
  ├─ 失败：ERROR_ACCESS_DENIED → 活着（保守；对齐 unix 的 EPERM 口径）
  │        其余（含 pid 不存在）→ 死
  └─ 成功：WaitForSingleObject(handle, 0)
            ├─ WAIT_TIMEOUT   → 活着
            ├─ WAIT_OBJECT_0  → 死（句柄/PID 未回收也算死）
            └─ 其他          → 退回 GetExitCodeProcess，仅 STILL_ACTIVE 算活着
```

单写者不变量不变：**活着**的其他进程仍然立即报错拒绝启动；只有「进程真的没了」才轮到
`lock_auto_recover` 接管。`process_alive_unix.go` 未改动。

## 4. 验证证据（红 → 绿）

回归用例 `sessionstore/data_root_lock_windows_test.go`（windows-only）：
`TestJSONDataRootTerminatedProcessLockIsStale` 起一个立即退出的子进程并握着它的句柄
（不 Wait/不 Release），用这个「已终结但 PID 仍被占用」的 pid 写 `lock.owner`
（`renewed_at` 只有 5 秒前，杜绝靠心跳超龄兜底），断言：

1. `processAlive(pid) == false`（修复前 `true` → 红）；
2. 保守口径（`auto_recover=false`）报 `ErrDataRootStaleLock`，而不是
   `ErrDataRootLocked`；
3. 默认口径（`auto_recover=true`）接管成功、锁记录改写成当前进程、`Close` 后删除锁。

```text
go test ./sessionstore/ -run TestJSONDataRoot -count=1 -v
  --- PASS: TestJSONDataRootLockAcquiredAndReleased
  --- PASS: TestJSONDataRootLockForeignProcessRejected     （活持有者仍被拒）
  --- PASS: TestJSONDataRootStaleLockRespectsAutoRecover
  --- PASS: TestJSONDataRootCrashedGUIResidualLockRecovered
  --- PASS: TestJSONDataRootTerminatedProcessLockIsStale   （本次新增）
go test ./sessionstore/ -count=1 -timeout=300s   # ok 48.0s（整包）
```

## 5. 遗留

- 本修复要生效必须**重建并部署** dev 基线二进制；已存在的那份
  `dist/seelex-gui-dev/.seelex/sessions-json/lock.owner` 只是运行时租约，不是会话内容，
  删除它不会丢数据（会话数据在 `sessions/`、`sessions-json/`），但按 `MEMORY.md`
  铁律仍需先向用户预警并取得确认。
- `dist/` 下进程检测：本次事故期间没有 seelex 进程在跑，deploy 门禁可正常通过。
