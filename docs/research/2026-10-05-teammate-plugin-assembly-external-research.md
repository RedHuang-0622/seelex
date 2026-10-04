# teammate 的「插件装配」——外部一手调研

> 日期：2026-10-05（Playwright 实读，非二手转述）
> 目的：给「权限 × 能力插件」两轴交叉的 teammate 专业化找外部参照与**已有的坑**
> 读法：下面每条都标了 URL 与实读日期；引号内是原文字面。

---

## 1. 参照对象：Claude Code 的 subagent（per-agent 装配面）

URL：`https://code.claude.com/docs/en/sub-agents`（实读 2026-10-05；跳转自 docs.claude.com/en/docs/claude-code/sub-agents）

### 1.1 一个 agent 的装配面 = **15 个 frontmatter 字段**

> "Frontmatter fields: `description`, `tools`, `disallowedTools`, `model`, `permissionMode`, `mcpServers`, `hooks`,
> `maxTurns`, `skills`, `initialPrompt`, `memory`, `effort`, `background`, `omitClaudeMd`, and `isolation`."

它已经把我们想要的两轴**分开落成了不同字段**：

| 轴 | 字段 | 原文要点 |
|---|---|---|
| **能力** | `skills` | "Skills to **preload into the subagent's context at startup**. **The full skill content is injected, not only the description.**" |
| **能力** | `tools` / `disallowedTools` | 工具面白/黑名单 |
| **权限** | `permissionMode` | `default / acceptEdits / auto / dontAsk / bypassPermissions / plan` |
| **权限** | `hooks` / `mcpServers` | 生命周期钩子与外部工具服务 |
| **算力** | `model` / `effort` | effort "Overrides the session effort level" |
| **隔离** | `isolation` | "Set to `worktree` to run the subagent in a temporary git worktree … **branched by default from your default branch rather than the parent session's HEAD**. The worktree is automatically cleaned up if the subagent makes no changes" |
| **记忆** | `memory` | `user / project / local`，"Enables cross-session learning" |
| **有界** | `maxTurns` | 到限即停并标 partial，可 resume 续跑 |

> 对我们的直接含义：**「能力」与「权限」在外部已经是两个字段面**，而不是一个模糊的"角色"。

### 1.2 最关键的坑：**插件只能带能力，不能带权限**

> "For security reasons, **plugin subagents don't support the `hooks`, `mcpServers`, or `permissionMode` frontmatter fields.
> These fields are ignored when loading agents from a plugin.** If you need them, copy the agent file into
> `.claude/agents/` or `~/.claude/agents/`."
> "If you're the plugin's author, **ship the hooks in the plugin's `hooks/hooks.json` and the MCP servers in its `.mcp.json`
> instead**. They apply whenever the plugin is enabled rather than only inside the subagent."

**这是一条被外部产品用硬规则确立的边界**：随"别人给的包"进来的东西**不许**携带权限面与钩子；
插件带的能力只能落在**插件级**（启用即生效），不能落在**子代理级**（子代理级权限一律忽略）。

> 对我们的直接含义：用户设想的「权限 × 插件」里，**权限那一轴不能由插件供货**。
> 插件的合格内容是 `skills` / 提示词 / `tools` 收窄；`permissionMode` 类字段必须由**宿主/leader 侧**给。
> 若 Seelex 让插件带权限，等于把提权包当插件发行——这与"精选后直接可用"的目标相反。

### 1.3 「召唤 teammate 时装配调好的定义」外部已有做法

> "You can also **reuse a subagent definition as an agent team teammate**: name the subagent type when you ask Claude
> to spawn the teammate, and Claude Code **applies parts of that definition to it**."

即：**同一份能力定义同时充当 subagent 与 teammate 的装配源**——正好对应用户说的
"召唤 teammate 的时候也有调好的 plugin 可以直接装配"。

### 1.4 定义的分发层级（同名冲突时的优先级）

> Managed settings (1) > `--agents` CLI (2) > `.claude/agents/` 项目 (3) > `~/.claude/agents/` 用户 (4) >
> **Plugin's `agents/` directory (5，最低)** —— "Installed with plugins"

外部把「插件带来的定义」放在**最低优先级**：插件是**可被覆盖的补充**，不是权威。

### 1.5 成本口径（一个被明写的代价）

> "Those descriptions take up context … When the combined descriptions of your subagents … exceed **15,000 tokens**,
> Claude Code shows a warning at startup."（描述也要进上下文，要短）
> "**An enabled plugin is part of every session**, not only the sessions where you use it … for each skill, agent, and
> command that Claude can invoke on its own, the **name and description are in Claude's context on every turn** …
> **The full text of a skill or agent loads only when it's used.**"

---

## 2. 参照对象：插件与「精选目录」

URL：`https://code.claude.com/docs/en/plugins`（实读 2026-10-05）

- **插件 = 组件目录**："A Claude Code plugin is a directory of skills, agents, hooks, MCP servers, or other components
  that Claude Code **installs and loads as one unit**"，清单 `.claude-plugin/plugin.json`。
- **marketplace = 目录（catalog），不是托管商店**："A marketplace is a repository or directory with a
  `.claude-plugin/marketplace.json` file that lists plugins and **where to fetch each one**. **It's a catalog, not a
  hosted store.**"
- **三级来源（按名字限定）**：Official（`claude-plugins-official` 等）/ Community / **Third-party（其余全部，含你同事或你组织发布的）**。
- **安装作用域**：`user`（本机所有项目）/ `project`（经提交的 `.claude/settings.json`，人人各自装）/ `local`（仅本仓库）。
- **跨宿主可见（对应用户说的「广播到其他构建」）**：terminal、desktop app、VS Code 扩展
  > "is available in the other two on that computer, **because all three read the same settings files**."
- **安全口径**："> **Permissions: what the plugin runs, it runs as you.**" 安装前应看 context cost 与权限面。

---

## 3. 由外部证据得到的设计裁决（建议，不是事实）

1. **两轴要显式分离，且方向不同**：
   - **权限轴**（`tools_policy` / `permission_groups`）：**只能由 leader/宿主给**，不进插件、不进 marketplace。
   - **能力轴**（`plugins[]` → skills + 工具收窄 + 提示词）：**才是可分发的**，也是"精选"要管的东西。
2. **装配点应当是"会话装配期"而不是"全局开关"**：外部把 enabled plugin 做成会话全局（代价：每轮目录进上下文）；
   我们要的是**每个 teammate 各自一套**，所以必须按**会话/角色**装配，不能沿用全局单选。
3. **定义的分发层级要有优先级**：宿主内建 < 精选目录 < 团队/项目自带 < leader 显式声明。谁覆盖谁必须写死。
4. **能力要"全量注入"还是"按需激活"是个真取舍**：外部对 `skills` 前置字段是**全量注入**（贵但确定），
   对插件目录只进 name+description（便宜但靠模型自己激活）。这两个都要给用户可选。
5. **成本必须可见**：插件与 skill 的 name+description 每轮进上下文，装配界面要给"这一装配花多少上下文"的读数。
6. **精选可被覆盖**：插件带来的定义在外部是**最低优先级**——"精选"是**便利**，不是**权威**。

---

## 4. 装备来源：开源侧**已经有现成的一批**（修正「目前肯定没有」这个先验）

URL：`https://github.com/topics/claude-code-skills`、`https://github.com/VoltAgent/awesome-agent-skills`（实读 2026-10-05）

**先说结论：用户的前提「目前肯定是没有的」与观察到的事实不符。** 这个生态已经成型：

- GitHub topic `claude-code-skills` 现有 **2,031 个公开仓库**（实读页面原话："Here are 2,031 public repositories matching this topic"）。
- `VoltAgent/awesome-agent-skills`（35.2k star）："A curated collection of **1000+ agent skills** from official dev teams
  and the community, compatible with Claude Code, Codex, Gemini CLI, Cursor, and more." 自述 "**Hand-picked, not AI-slop generated.**"
  条目里直接列着：
  - `anthropics/frontend-design` —— "**Frontend design and UI/UX development tools**"（＝前端 Agent 的 UI 能力）
  - `anthropics/brand-guidelines` / `theme-factory` / `web-artifacts-builder` / `canvas-design` / `skill-creator`
  - `mblode/agent-skills` —— "**Nobody ships AI slop on purpose. These skills make sure you don't. UI audits, typography,
    docs, PR review, and releases.**" + 安装通路 `npx skills add mblode/agent-skills`（＝**「去 AI 味」这一类已经有人在做了**）
- `sickn33/agentic-awesome-skills`（47.2k star）："AAS Core is the local, agent-first control plane for complete
  **catalog discovery, agent-owned selection, stack validation, and planning**, backed by **2,400+ agentic skills**.
  Includes CLI, local MCP, **catalog**, **plugins**, and Workbench." —— **这几乎就是用户设想的那套「精选 + 装配 + 校验」控制面，且已经有了。**

> 对我们设计的含义（比"后期收集"更有用）：**收集不是未来工作，是现在就有的输入**；
> 真正稀缺的不是 skill 数量，而是 ①**把它们装配到"某一个 teammate 会话"上**（外部是会话全局、我们要是 per-teammate）；
> ②**一条可核的装载门禁**（不是"看起来不错就装"，而是可装载性/不带权限面/许可证/成本四问）；
> ③**同一台机的多个构建读到同一份精选**。
> 另外 `npx skills add <repo>` 这种"一行装一个技能包"的通路，值得作为 curated 清单里的 `source` 形态参照。

**证据强度**：以上只是**仓库存在 + 官方一句话定位**层面的实读；**没有**读进任何 skill 的正文，
因此"这些 skill 好不好用/能不能直接用"仍是 **Hypothesis**。

---

### 4.1 实读可达性核验（**这一步必须做，否则候选清单会骗人**）

实读日期 2026-10-05，逐条打开：

| 清单里的字面标识 | 直接打开的结果 |
|---|---|
| `anthropics/frontend-design` | `https://github.com/anthropics/frontend-design` → **HTTP 404 · Page not found** |
| `anthropics/skills` | **存在**："Public repository for Agent Skills"；顶层 = `.claude-plugin` / `skills` / `spec` / `template` / `THIRD_PARTY_NOTICES.md` |

⇒ **清单里的 `owner/repo` 字面标识不等于"可打开的仓库"**。`anthropics/frontend-design` 极可能是
`anthropics/skills` 仓库内 `skills/` 下的一个**技能目录**（该仓库自述 "Each skill is self-contained in its own
folder with a SKILL.md"，且顶层就带 `.claude-plugin` ⇒ 它本身也能当**插件**用）；但**我没有实读那条路径**，
所以这一点仍是 Hypothesis。

**这条 404 的意义**：它正好验证了「listing-only」这个证据档是必要的——
**"在榜单上看见"与"能取到正文"之间隔着一整步**。任何候选在转正前必须先做**可达性核验**（打开一次 URL，
确认 200），再做可装载性四问。这一条已写进 `plugins/curated.yaml` 的 `promote` 字段作要求。

---

## 5. 检索边界

- 本轮只实读了上面两个官方页面（一手）；**没有**去 GitHub/开源平台实际收集前端类 skill（用户已明确"后期收集"）。
- 未验证：这两个页面的内容是否在不同版本/套餐下不同；marketplace.json 的完整 schema 未取全文。
- 未取：Codex / Cursor / Devin 等其它 harness 的同类装配面（本仓 `docs/research/` 已有二手画像，可后续并读）。
