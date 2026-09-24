# Qoder 皮肤 · Seelex 前端视觉换代

> 任务溯源：用户要求「先截屏 Seelex → 打开 Qoder 分析它前端漂亮的原因 → 做静态复刻查看效果 →
> 把当前前端跟进到 Qoder 同款风格（包括皮肤变换）」。
> 本目录是这次换代的**证据 + 设计说明 + 复刻页**。

## 目录

| 文件 | 是什么 |
|---|---|
| **`DESIGN-LANGUAGE.md`** | **设计语言确认稿**：环境渐变的实测配方（`#EBEBC6 → #D1DAE2`）、几何/阴影/线框纪律、反面清单 |
| **`OPTIMIZATION-PLAN.md`** | **优化方案**：按设计语言逐条改 `styles.css`（渐变缺失 / 圆角配黑下框 / 过圆角 / 不合时宜的阴影 / 多余线框），到选择器级 |
| **`MOTION-LANGUAGE.md`** | **拟物交互模式设计**：书签抽取 / 书本翻页 / 滑动 / 拉伸 —— 材质基座 + 四个动作 + 统一约束 + 落地挂点 |
| `tools/measure-skin.py` | **取色与几何量测工具**（渐变拟合 / 圆角半径 / 边界断点），上面三份文档里的数值都由它产出 |
| `tools/verify-skins.py` | **两轴渲染验证**：对 5 皮肤 × 2 深浅 各跑一次 headless Chrome，采样「壳顶 / 壳底 / 内容纸」，断言各皮肤渐变互异、深浅轴独立于皮肤 |
| `tools/make-skin-evidence.py` | 把上一步的 10 张渲染图整理成 `refs/` 里的复刻截图与渐变对照矩阵 |
| `replica.html` | **静态复刻页**：真 DOM 类名 + 真 `styles.css` + 真皮肤文件 + 一颗皮肤切换器 |
| `refs/qoder-light-clean.png` | Qoder 桌面端（浅色）实机截图，右栏/输入区/状态栏完整 |
| `refs/qoder-light-02.png`、`refs/qoder-light-03.png` | Qoder 浅色实机取色截图 |
| `refs/qoder-light-01.png` | Qoder 浅色（关闭确认弹窗期间，背景被模态模糊——这条本身就是它的模态口径） |
| `refs/replica-seelex-light.png` | 复刻页 · `qoder` 皮肤 + 浅色 |
| `refs/replica-seelex-dark.png` | 复刻页 · `qoder` 皮肤 + 深色（同一份 CSS，只换 token） |
| `refs/skins-gradient-matrix.png` | 5 皮肤 × 2 深浅 的**环境渐变对照矩阵**（headless Chrome 渲染） |
| `refs/realfrontend-qoder-skin.png` | **真前端** `gui/frontend/dist/index.html` 在换代后的渲染（无 Bridge，只看壳与皮肤） |

## Qoder 好看在哪：五条可复现的规则

对着实机截图逐块量下来，Qoder 的"漂亮"不是配色讨喜，而是五条**工程口径**。
下面每条都给出可验收的判据——复刻照这五条做，就能拿到同一观感。

### 1. 底色分层：外壳暖白 → 内容纯白（靠色温，不靠描边）

- 导航/标题栏/状态栏是**象牙暖白** `#f7f6f2`；阅读区是**纯白** `#ffffff`；
- 两者之间只有 `#e6e3da` 的 1px 发丝线，甚至常常连这条线也没有；
- 判据：把截图去色后，导航与内容仍能靠亮度差分开（亮度差 ~5%），而颜色差几乎为 0。

落地：`--bg`（外壳）/`--panel`/`--surface`（内容）三层 token + `.workspace { background: var(--surface) }`。

### 2. 颜色只承载语义（无彩色界面）

- 界面本体是**无彩色**：黑/白/灰阶；
- 颜色只在有含义时出现：琥珀=需要授权（`完全访问` 芯片）、绿=通过/增量（`+2,033`）、
  红=失败/减量（`-129`）、蓝=信息/链接（`查看`）；
- 判据：把整屏的颜色按"是否有语义"分类，找不到第三类用途的色块。

落地：`--status-running/done/failed/info` + `--tint-*`/`--border-*` 派生；主信号 `--accent` 取**墨黑**
（等价 Qoder 那颗实心发送键），蓝色退居 `--status-info`。

### 3. 几何：一档圆角 + 发丝描边 + 极轻投影

| 层 | 圆角 | 说明 |
|---|---|---|
| 胶囊（芯片/行/权限） | `999px` | 行高与胶囊同高，一眼是"控件" |
| 卡片（工具卡/表格/浮层） | `10px` | 与 `--r-lg` 对齐 |
| 输入区 | `16px` | 全屏唯一的大圆角，天然成为主角 |
| 描边 | `#e6e3da` 1px | 全局只有这一档，不叠加深浅两套 |
| 投影 | 只在浮层与输入区 | 其余一律无影，用底色分层 |

落地：`--r-sm/md/lg/xl/2xl = 6/8/10/14/16`；`.composer` 只留 `--shadow-sm`。

### 4. 信息密度：小灰标签 + 大行高正文

- 正文 14px / `line-height: 1.75` / 段间距 12–13px；
- 元信息（时间、token、路径、工具名）10–12px、`--faint` 灰、等宽数字；
- 分区标签**正常大小写**、12.5px、600 字重（不是"大写 + 大字距"的机器感）；
- 判据：任取一屏，正文与元信息的字号比 ≈ 1.2:1，且正文行高 ≥ 1.7。

落地：`styles.css` 第 27 节去掉 `.section-title*` 的 `text-transform/letter-spacing`。

### 5. 渐进披露：一行收口，点开才展开

- 工具调用压成一行：`执行工具 5 次 · 271s`（左侧一颗方角小箭头，右侧灰字统计）；
- 消息尾部一排安静的动作图标（复制/赞/踩/更多），不抢正文；
- 输入区是这一屏唯一的"重"元素：全宽圆角卡片 + 圆形实心发送键 + 语义权限胶囊。

落地：`styles.css` 第 27 节 j)（页签下划线）/ h)（圆形主键）/ i)（权限胶囊）。

### 反面清单（Qoder 不做的事，复刻也不做）

- 不做装饰性动画（无扫光/呼吸/连点）；
- 不给普通卡片加投影；
- 不在正文里用彩色图标（颜色留给语义）；
- 不用两种以上的强调色同时出现。

## 换代落到 Seelex 的哪几个文件

| 文件 | 改动 | 为什么 |
|---|---|---|
| `gui/frontend/dist/styles.css` | ① `:root` 基线 token 换成 Qoder 浅色（含圆角/投影）；② 末尾新增**第 27 节「Qoder 基座」**（几何/密度/层级）；③ 把 12 处硬编码色值改回 token | token 与组件几何属于**基座**，按 `themes/README.md` 的契约只能在 `styles.css` 改 |
| `gui/frontend/dist/themes/qoder.css` | 新增（默认皮肤） | 两轴换肤后，皮肤只给**品牌 token**（主信号 + 环境渐变），中性基座由深浅提供 |
| `gui/frontend/dist/themes/graphite.css`、`verdigris.css`、`paper.css`、`silver.css` | 重写（原整包 token → 只留品牌 token + 深浅两条渐变） | 每套皮肤都有自己的环境渐变色，且深浅各自取一份 |
| `gui/frontend/dist/themes/manifest.json` | schema 2：`skins[]`（5）+ `modes[]`（2），`default_skin: "qoder"` / `default_mode: "light"` | 设置弹窗的「皮肤」「深浅」两排卡片直接读 manifest，登记即出现 |
| `gui/frontend/dist/theme.js` | 改成**两轴**控制器：`applySkin`（换 `<link>`）× `applyMode`（切 `data-theme`） | 深浅与皮肤解耦，各自持久化（`seelex.skin` / `seelex.mode`） |
| `gui/frontend/dist/index.html` | 设置「外观」区加 `#mode-picker`，`data-theme="light"`、`color-scheme: light` | 深浅成为独立选择项，首帧就是浅色 |
| `gui/run_wails.go` | `BackgroundColour` → `#EBEBC6`（浅色环境渐变起点色） | Wails 窗口底与默认外观的壳色一致，消除首帧闪色 |

### 皮肤变换：拆成「深浅 × 皮肤」两轴（2026-09-24 二次迭代）

用户口径：**"皮肤和深浅色分开来做"**。于是把原来「6 套整皮肤」的耦合拆开：

- **深浅（mode）** = 中性基座（面/文本/描边/状态/半透明面/投影），写在
  `styles.css` 的 `:root`（浅色）与 `:root[data-theme="dark"]`（深色），由
  `<html data-theme>` 选择；
- **皮肤（skin）** = 品牌（主信号 + 环境渐变），写在 `themes/<skin>.css`，
  只覆盖 8 个 `--skin-*` token，由皮肤 `<link>` 选择；
- 两者正交：**5 皮肤 × 2 深浅 = 10 种外观**。换肤链路只改
  `<html data-theme>` 与皮肤 `<link href>`；记忆拆成
  `localStorage["seelex.skin"]` 与 `localStorage["seelex.mode"]`；
  设置弹窗「外观」区因此有**两排卡片**（`#theme-picker` / `#mode-picker`）。

**每套皮肤都有自己的环境渐变**（深浅各一条，配方同第 1 节：等亮度、纯竖直、线性）：

| id | 名字 | 主信号（浅 / 深） | 环境渐变（浅 ｜ 深） |
|---|---|---|---|
| `qoder` | Qoder（默认） | 墨黑 / 象牙白 | `#EBEBC6→#D1DAE2` ｜ `#1B1A17→#171A1E` |
| `graphite` | 石墨黄铜 | 黄铜压深 / 黄铜提亮 | `#EAE6DC→#D8DDE2` ｜ `#191D22→#12171C` |
| `verdigris` | 铜绿钢青 | 铜绿压深 / 铜绿提亮 | `#DCE9DF→#CFE0E2` ｜ `#141F1C→#101B20` |
| `paper` | 暖纸白昼 | 暖铜 / 暖铜提亮 | `#F2EADB→#E4E0D6` ｜ `#201C16→#171614` |
| `silver` | 银白冷钢 | 钢青灰 / 钢青灰提亮 | `#E9EDF1→#D3D9DF` ｜ `#1A1D20→#141719` |

**渲染验证**（headless Chrome 逐组合取色，脚本 `tools/verify-skins.py`）：
浅色下 5 套皮肤壳顶色**互异**；同一深浅下所有皮肤的内容纸色**一致**（＝深浅轴独立于皮肤），
且浅/深纸色不同（`#FBFAF6` vs `#1F1F1F`）——两轴确实解耦。

### 三个拍板（用户：1/2/3 都按推荐）——已落地/已定案

| # | 决策点（出处） | 推荐选项 | 用户裁决 | 状态 |
|---|---|---|---|---|
| 1 | 书签抽取的手势（`MOTION-LANGUAGE.md` §7.1） | 先做右键/长按**菜单版**（M2a），观察频次再决定是否升级拖拽 | 按推荐 | **已定案**（动效阶段 M2a 执行，本次不含动效实现） |
| 2 | 抽屉收起时内容区行为（`MOTION-LANGUAGE.md` §7.2） | 内容区**随 flex 扩张**（壳不动），不做整屏平移 | 按推荐 | **已定案**（与现有 flex 行为一致，无需改动） |
| 3 | 描边色温（`OPTIMIZATION-PLAN.md` P2-1） | **方案 A**：`--border` 改中性 `#E7E7E4`，新增 `--border-warm: #e6e3da` 兜暖色场合 | 按推荐 | **已落地**（见 `styles.css` 的 `:root`） |

## 怎么看复刻页 / 怎么验证

```bash
# 1) 直接看（file:// 也能跑，皮肤文件走同源相对路径）
#    浏览器打开 docs/design/qoder-skin/replica.html
#    右上角「皮肤」切换器可现场换肤；也可用 URL hash：
#    replica.html#silver-dark   replica.html#graphite-light

# 2) 起个静态服务（也给虚拟机用）
python -m http.server 8123 --directory .
#    http://<host-ip>:8123/docs/design/qoder-skin/replica.html

# 3) 皮肤契约自检（token 清单在 themes/README.md）
node --test gui/frontend/dist/theme.test.mjs
```

复刻页的三条自查（本次已逐条截图验证）：

1. **外壳暖白 / 内容纯白**：`refs/replica-seelex-light.png` 左栏取色 `#f7f6f2`、中栏 `#ffffff`；
2. **换肤只换色、不换几何**：`refs/replica-seelex-dark.png` 与浅色图逐像素比对，圆角/间距/字号一致，
   只有 token 变化（左栏落到 `#181818`、中栏 `#262626`）；
3. **浅色下无深色残留**：复刻页在 `qoder` + 浅色下没有任何深色底块——因为 12 处硬编码色值已改为 token。

## 还没做的（诚实清单）

- **用户自带皮肤**：仍只支持随包皮肤（`themes/README.md` 里那条"之后要做"没动）；
- **皮肤文件里的选择器规则**：本次所有几何改动都进了 `styles.css`，皮肤包依旧"只覆盖 token"，
  契约没被破坏；
- **`.file-pdf-canvas` / `.docx-wrapper > .docx` 保持纯白**：那是"文档纸张"，不是 UI 面，
  不该跟着皮肤变深——这是有意的例外，不是漏改；
- 复刻页是**静态样例**（示例数据写死在 HTML 里），不是运行时的另一种渲染路径。

## 虚拟机（Ubuntu）投放清单

目标机：VMware 里的 Ubuntu，NAT 网段 `192.168.227.0/24`
（宿主在 `192.168.227.1`，虚拟机历史租约 `192.168.227.128`，主机名 `red-virtual-machine`）。

已在本机起好静态服务（宿主 → 虚拟机可直接访问）：

```powershell
# 宿主上已执行（如需重启）：
C:\users\redre\anaconda3\python.exe -m http.server 8123 --bind 0.0.0.0 --directory G:\Program\go\seelex
# 自检：HTTP 200，339828 bytes
```

虚拟机里打开（Firefox 即可，无需装任何东西）：

```
http://192.168.227.1:8123/docs/design/qoder-skin/build/seelex-qoder-preview.html
```

- 单文件产物 332 KB，**断网也能看**（CSS 全内联）；
- 换肤在页面右下角「皮肤 / 深浅」两排胶囊里，或用 URL hash：`...#silver-dark`、`...#graphite-light`；
- 想换成"跑真前端"，把上面的路径换成本仓库 `gui/frontend/dist/` 的静态目录即可
  （真前端要 Wails Bridge 才有数据，纯静态打开只能看壳与皮肤——这也是本目录要做复刻页的原因）。

> 阻塞点：虚拟机当前停在 GDM 登录页（且已有一次密码失败），宿主侧没有可用凭据；
> 22/3389/80 均未开放，也没有配置共享文件夹，因此**投放需要虚拟机的登录密码**。

---

## 勘误与更新（二次实机复核，量化取色）

对 `refs/qoder-light-clean.png` 做逐像素量测（工具：`tools/measure-skin.py`）后，
上面"五条规则"里有三处需要更正/补强：

| # | 原文 | 实测 | 处置 |
|---|---|---|---|
| 1 | 第 1 条只说了"外壳暖白 `#f7f6f2` / 内容纯白"——**是平色分层** | 外壳是**一条竖向渐变**：`linear-gradient(180deg, #EBEBC6 0%, #D1DAE2 100%)`，等亮度（L≈85% 全程波动 <2%）、纯竖直（横向通道差 **0.0**）、线性 sRGB（R² **0.999**） | **这正是用户点名欣赏的"通透"**，且**当前一行都没落地**（`.left-panel/.right-panel/.topbar` 仍是平色） → `OPTIMIZATION-PLAN.md` P0-1 |
| 2 | 第 3 条写"输入区 16px"，且第 27 节 m) 把发送键改成了**圆形**（`border-radius: 999px`） | composer 实机半径 **14px**（不是 16px）；发送键 **35×35、半径 6–8px 的圆角方**（角剖面 dy 0→8，8→0；圆的话该是 17.5） | **`styles.css` 第 27 节 m) 改错了** → `OPTIMIZATION-PLAN.md` P1-1 |
| 3 | "浅色下无深色残留：12 处硬编码色值已改为 token" | 仍有 **2 处黑影 + 1 处假高光**没被覆盖：`.right-tab.is-active::after`（深色三角 + `0 2px 4px rgba(0,0,0,.25)`）、`::before`、以及 `:root` 外的 `#ffffff` 混色 | 第 27 节 j) 只覆盖了 `box-shadow`，**伪元素与 `border-color` 是残留**——用户看到的"圆角配黑色下外框"就是它 → `OPTIMIZATION-PLAN.md` P0-2 |

另外第 3 条还漏了一条：**列表行在实机上是"无底、无框、无圆角"的**
（左栏 y=872..920 逐像素扫过，整段只有文字像素，没有胶囊填充），
行步距实测 **37px**。

### 接下来（已排好序，见三份新文档）

```
DESIGN-LANGUAGE.md  →  OPTIMIZATION-PLAN.md  →  MOTION-LANGUAGE.md
   （定语言）              （拆丑的）                （再谈拟物动效）
```

顺序不能反：动效会**放大**黑影——带着 `rgba(0,0,0,.30)` 与 `999px` 去做拖拽，
"丑爆了"会在运动中最显眼。