# 装配读数进团队看板 DTO 与投影（bridge-eng，milestone `m-projection`）

> 日期：2026-10-05 · 角色：bridge-eng
> 交付物：`application/contract/dto/teamwork_board.go`（`PluginAssemblyView` + 成员两格）、
> `seelebridge/runtime_role_plugins.go`（形状合一）、`seelebridge/runtime_teamwork_board.go`
> \+ `..._archive.go` / `..._close.go`（填读数）、`application/model/state.go`（快照深拷贝）
> 前置：`wi-assemble`（`r.assemblyViews` / `pluginFaceJudgement` 那一条判据）；
> 契约冻结：leader 在本轮工作正文里给的键名清单（前端按它写，见 `docs/arch/team-board-gui-tui-contract.md` §2）

---

## 1. 形状（单一来源）

```
application/contract/dto/teamwork_board.go
  PluginAssemblyView            ← 字段与 tag 的**唯一一份定义**
  TeamworkMemberView.Plugins    []string            json:"plugins,omitempty"   （声明面）
  TeamworkMemberView.Assembly   *PluginAssemblyView json:"assembly,omitempty"  （生效面读数）

seelebridge/runtime_role_plugins.go
  type rolePluginAssemblyView = dto.PluginAssemblyView   ← **别名**，不是同形的新类型
```

键集合（全部 omitempty，14 个）：`role mode plugins plugin_count skill_count
skill_catalog_runes skill_catalog_tokens_est plugin_face_tools total_tools` ＋
`plugin_face_faulted plugin_face_missing plugin_face_note` ＋ `yellow yellow_reason`。
后 3 个是**失灵读数**，沿用回执既有键名（少了它们，看板就会把"插件被撤"读成"没装配"）。

投影来源：`runtime_teamwork_board.go` 的 `teamworkMemberViews` 取计划 `members[].plugins`
（拷贝，不共享缓存里的底层数组）+ 调用方算好的 `r.assemblyViews(plan.Members)`（**同一条
`pluginFaceJudgement` 判据**，不另写收窄算法）。下发、存档、封板三条路径都走
`buildTeamworkBoardView`，读数作为参数传入 ⇒ 三处同形。

## 2. 用例清单（每条都写了"排掉了什么错"）

| 用例 | 钉什么 | 排掉的错 |
|---|---|---|
| `dto.TestPluginAssemblyViewWireKeysAreFrozen` | wire 键集合逐键等于冻结清单（多一个键也报）；全零值下发 `{}` | Go 侧改名/改 tag 完全合法而 wire 上静默变空；"0 与缺失"混为一谈 |
| `dto.TestTeamworkMemberViewCarriesAssemblyGates` | 没有装配事实 ⇒ 两个键都不出现；有声明/有读数 ⇒ 键在且形状同一份；空集 ⇒ 无 `plugins` 但 `mode=inherit-host` | 新增字段污染老载荷；空集靠字段缺失暗示（前端读不出"继承宿主"） |
| `TestTeamworkBoardCarriesMemberAssemblyReadings` | 三成员（装 docs / 空集 / 装 ops）：声明面逐条等于**落盘计划**；mode / plugin_count / skill_count / 目录 runes / token 估算 / 插件面 / 全量工具数逐格自洽；inherit-host 的插件面是**宿主真数**（不是 0）；三个面读出的数互不相同 | 读数写成常量（三个人一个数）；读数与运行面不同判据；空集被当成 0 工具面；把 inherit-host 的目录读数写成非 0 |
| `TestTeamworkBoardAssemblyFaultIsVisible` | 撤销 docs 之后：`replace` 不变、声明面不变、`plugin_face_faulted=true`、`plugin_face_tools=0`、`missing` 点名、`note` 含"失灵/工具面为空"；且**别的成员读数不受影响**（阴性对照） | 失灵被读成"没装配"；失灵被读成"满面"；整条读数一起坏掉却没人发现 |
| `TestTeamworkBoardAssemblySharesOneShapeWithReceipt` | 编译期双向别名断言 + 运行期：回执 `assemblies[i]` 与看板 `members[i].assembly` 解到**同一类型**上 `DeepEqual` | 两份手抄字段的结构体；只在一条路径上有值的 tag |
| `TestTeamBoardArchiveCarriesSameAssemblyReadings` | 存档载荷（`metadata/board_team.json` 的那一份）里的装配两格与活体下发逐格相等 | 存档路径漏填读数 ⇒ "重启恢复之后装配格消失"这种只在重启后才出现的缺陷 |
| `model.TestCloneRuntimeStateDeepCopiesMemberAssembly` / `...KeepsNilAssembly` | 声明面切片 / 读数指针 / 读数里的两处切片三层都独立；nil 读数克隆后仍是 nil | 浅拷贝让 GUI 与 TUI 共享同一条读数（一个读者改了，另一个看到别人的装配） |

## 3. 证据（命令与输出）

```
$ go test ./seelebridge/ ./application/... -count=1
ok  seelebridge 17.0s · application/contract/dto · application/model · application/core（含
application/... 全部 20 个包）—— 无 FAIL

$ gofmt -l <本轮改动的 10 个文件>
（空）

$ go vet ./seelebridge/ ./application/...
（空，exit 0）

$ go test . -run TestTeamworkPluginAssemblyHeadlessSmoke -count=1 -v
ok github.com/RedHuang-0622/seelex 3.153s
  回执原文仍带全部键：{"assemblies":[{"role":"ui-eng","mode":"replace","plugins":["impeccable"],
  "plugin_count":1,"skill_count":1,"skill_catalog_runes":585,...}]}
  A1 报告行：上限=3 · ui-eng=replace/[impeccable] 技能=1 目录=585 字节 面=57/57 ·
             plain-eng=inherit-host 面=57/57

$ go test ./gui/ ./tui/ -run "Team|Board" -count=1
ok  gui · ok  tui
```

前端未改（`app.js teamBoardInput` 原样透传 `board.members`，渲染件读 `member.plugins` /
`member.assembly` 即可）。

## 4. 已知口径与代价

- **读数按次现算，不进缓存**：看板缓存只缓存"计划 + 审计"两份**文件读**；装配读数需要
  Runtime 的插件域与工具面，缓存它就得跟着 `team_*` / `UndefinePlugin` / 宿主激活态一起
  失效——那是三个失效点，漏一处就是"面板永远显示旧装配"。代价是每次快照多做
  `len(Members)` 次"全量工具面 × 一次判据"的纯计算（无 I/O）。
- 全部字段 `omitempty`：`plugin_count=0` 之类的 0 值不再出现在 wire 上（契约要求）；语义
  由 `mode` 显式承载，不靠"哪个键缺失"。
- 前端渲染（装配格怎么画）不在本轮范围。
