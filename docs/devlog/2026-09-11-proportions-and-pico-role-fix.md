# 2026-09-11 上下文轴比例修复 + 对话列比例收口

> 日期: 2026-09-11 | 范围: `gui/frontend/dist/styles.css`、`gui/frontend/README.md`。
> 用户反馈：发送键图标跑偏（已单独修复）、**上下文轴比例不对**，并要求把中间对话列
> 也接进组件库那套比例体系。

## 上下文轴比例错乱：组件库的 role 语义冲突

现场：轨迹页上下文轴卡片里，标题与提示文字占满左半边，而**轨道挤在右侧一条窄带里**
（标签列 + 五条轨道全部压进 ~270px）。视觉上就是"左半边空着、右半边挤成一坨"。

根因：Pico 组件库把 `[role=group]` / `[role=search]` 定义成它的"输入组合"组件：

```css
[role=group],[role=search]{display:inline-flex;position:relative;width:100%;...}
[role=group]>*,[role=search]>*{position:relative;flex:1 1 auto;margin-bottom:0}
```

而 Seelex 用同一个 ARIA role 表达**语义分组**（`<div class="context-axis"
role="group">`）。于是轴卡片变成 flex 行：标题（`flex:1 1 auto`）与轨道并排，轨道里
`grid-template-columns: 56px minmax(0,1fr)` 的 `1fr` 在 auto 宽度下塌成 min-content，
整条轴被压成窄带。

同一冲突还波及另外两处：`.effort-control`（被加了 16px 下外边距，在输入区里比同排
控件高一截）、`.work-todo-status`（被加了 `width:100%`）。

修复：在"Pico 的 role 组件归位"段按元素还原布局（不使用通配 role 重置，避免误伤
自绘组件）：

```css
.app-shell .context-axis[role="group"] { display: block; width: auto; margin-bottom: 0; }
.app-shell .context-axis[role="group"] > * { flex: none; }
.app-shell .work-todo-status[role="group"] { width: auto; margin-bottom: 0; }
.app-shell .effort-control[role="group"] { margin-bottom: 0; }
```

README 同步补了"引入组件库必须注意 role 语义冲突"的说明：新增带 role 的容器前，
先确认它没有被组件库的元素/属性规则命中。

## 上下文轴自身比例

在归位基础上按可读性调比例：标签列 56→64px（放得下"前缀注入"）、轨道 14→16px、
轨道间距 3px、块最小宽 2→3px（压缩刻度 3→4px，保证可点）、块内边距与圆角对齐
控件层、轴标题与提示文字 10→10.5px。

## 对话列比例

中间对话列此前是"10px 标签 + 22px 间距"自成一套，与外壳控件层对不上。本次收口：

- 消息块间距 22→26px，消息头 10→10.5px、行高 1.72→1.75，段落间距 12→13px；
- 折叠块（思考 / 工具过程）头部统一 34px 高、内边距 8/12，思考正文
  12→12.5px、行高 1.65→1.7；
- 工具芯片最小高 24px（与图标按钮同高）、行内边距加大，工具行 9/12；
- IO 面板头 32→34px、正文 11→11.5px、展开条 28→30px。

## 验证

```text
node --test gui/frontend/dist/*.test.mjs   # 244 pass
go build ./...                             # OK
```

真机（Wails GUI + `%TEMP%` 数据副本）1:1 目视：

- 上下文轴：标题在左、轨道铺满整卡，标签列与轨道比例正常，前缀注入轨 4 段横跨全宽；
- 轨迹行列表与轴在同一容器宽度内对齐；
- 对话列：工具过程折叠头与思考块头部同高，思考正文行距舒适，消息头字号与外壳一致。

## 追加：上下文轴"线谱上看不到内容"（2026-09-11 同日）

用户追问："上下文轴里面各个线谱上的内容呢？"——轨道看起来是空的。

### 定位

先用 Node 直接跑前端纯函数复核数据侧：把真实会话的 message 行按派生规则还原成
可见消息，再 `buildTrajectory` + `renderContextAxis`，得到
`lane tool segments=292 / llm=1 / prefix=1`——**段是生成出来的**，问题在渲染。

### 根因

组件库桥接层里有一条"剥掉所有按钮边框/底色"的通配规则：

```css
.app-shell button:not(.primary-button):not(.theme-card):not(.chat-chip):not(.work-table-button) {
  border: 0; background: transparent;
}
```

上下文轴块本身就是 `<button class="axis-segment">`，特异性 (0,4,1) 压过
`.axis-segment` (0,1,0) 的底色——所有块被刷成透明，只剩 `is-wide` 的文字，
于是"轨道上有内容却看不见"。同样被误伤的还有 `.axis-detail-close`（边框被剥）。

### 修复

- 删除那条通配剥离规则，只保留"归零外边距与默认宽度"；自绘控件靠自己的类覆盖
  Pico 默认值（Pico 在前、styles.css 在后，类选择器特异性也更高）；
- 安静型文字按钮（`.text-button` / `.team-preset`）显式声明透明底、无边框；
- 顺手把轴块的对比度做实：块色从 10% 透明度 token 改成
  `color-mix(状态色, 轨底色)` 的实色（两端皮肤都读得出），每块加 1px 右分隔线
  （窄到几像素时像条码），宽块（≥6% 体量）直接显示标签，不再只有悬停才可见；
- 新增用例 `context axis marks wide blocks so the lane shows content`：
  体量相差 40 倍的两个块，只有宽块带 `is-wide`。

### 复查

真机 1:1：工具轨是一条贯穿全宽的绿色块带（内含 `bash` / `read_file` /
`search_history` 标签），输入轨与系统轨各有一块，无记录的 LLM 轨保留空轨刻度；
前缀注入轨 4 段斜纹层跨满整轴。
