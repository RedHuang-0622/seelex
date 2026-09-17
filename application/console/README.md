# Console

## 生态位

`application/console` 提供 headless 后端诊断控制台：绑定项目、提交提示、观察事件流并以脱敏格式输出阶段日志。
它是 `-frontend backend` 的入口，只通过 `application.Service` 的窄接口工作，不依赖具体 UI。

主要调用方：组合根 `main.go`（`-frontend backend` 分支）与诊断/冒烟脚本。

## 架构图

```mermaid
flowchart LR
    CLI["main.go（-frontend backend）"] --> START["console.Start"]
    START --> BIND["BindProject"]
    START --> RUN["Run：提交提示 → 观察事件"]
    BIND --> SVC["application.Service"]
    RUN --> SVC
    SVC --> HUB["application/event.Hub"]
    HUB --> LOG["NewEventLogger：脱敏事件日志"]
    LOG --> OUT["OpenOutput：stdout / 文件"]
```

## 时序图

```mermaid
sequenceDiagram
    autonumber
    participant CLI as main.go
    participant C as console
    participant S as application.Service
    participant E as EventHub
    participant O as 输出 / 日志

    CLI->>C: Start（含 -backend-* 参数）
    C->>S: BindProject（可选）
    C->>S: Subscribe
    C->>S: Submit(prompt)
    loop 直到终态或超时
        E-->>C: Event 增量
        C->>O: 脱敏阶段日志
    end
    C-->>CLI: 退出码
```

## 导出

- `Start`：启动诊断控制台主循环。
- `OpenOutput` / `NewEventLogger`：输出与脱敏事件日志器。
- `BindProject`：将后端会话绑定到项目工作区。
- `Run`：无 UI 的单轮提交-观察循环。

## 验证

```text
go test ./application/console -count=1
```
