# 2026-09-02 上下文轴多线谱分轨

> 日期: 2026-09-02 | 范围: `gui/frontend/dist` + 前端 README + 轨迹模块文档 | 纯前端呈现层变更

## 背景

用户提出：上下文轴应像多线谱一样把各类型的上下文块分开，类似 DevTools
Network 面板的时间轴。原实现是单条横轴，所有记录按对话顺序首尾相连分段，
类型混在同一条轨道里，跨类型的先后与体量占比难以对照。

## 实现

- `trajectory.js`：`renderContextAxis` 改为多线谱（分轨）布局：
  - 输入 / LLM / 工具 / 错误 / 通知五条固定轨道上下叠放，共用同一横轴
    （横轴语义不变：对话顺序 + 内容体量，非时间轴）；
  - 每个记录块以 `--x`（此前所有记录体量占比累计）与 `--w`（自身体量
    占比）定位到本类型轨道上；
  - 某类型无记录的区间在轨道上留空（`.is-empty` 细刻度线占位），轨道
    标签（图标 + 类型名）取代原先底部图例；
  - `data-trajectory-key` 与点击定位契约不变（切回全量 → 滚动 → 高亮）。
- `styles.css`：`.context-axis-track` 由单条 flex 改为轨道 grid；
  `.axis-segment` 改为绝对定位块；删除 `.context-axis-legend` /
  `.axis-legend-item` 样式。
- `trajectory.test.mjs`：上下文轴用例改为断言五轨固定、空轨占位、共享
  横轴 `--x`/`--w` 与稳定 key。
- 文档：`gui/frontend/README.md`、`docs/gui/modules/trajectory-view.md`
  同步描述分轨布局与面板结构。

## 验证

- `node --check gui/frontend/dist/trajectory.js`
- `node --test gui/frontend/dist/trajectory.test.mjs`：20/20 通过
- `node --test gui/frontend/dist/*.test.mjs`：178/178 通过
- Go 侧嵌入式前端契约只检查 `renderContextAxis` 名称与 THINK 面板，
  本次改动未触碰，无需变更。
