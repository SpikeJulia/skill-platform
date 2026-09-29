# Skill Platform 架构

## 整体

```
┌──────────────────────────────────────────────────────────┐
│ Docker 容器 (单进程)                                       │
│                                                           │
│  ┌────────────────────────────────────────────────────┐  │
│  │ PocketBase 0.25+ (Go)                              │  │
│  │                                                     │  │
│  │ ┌────────────────┐   ┌──────────────────────────┐  │  │
│  │ │ Go hooks        │   │ 静态文件服务               │  │  │
│  │ │ (main.go)       │   │ (go:embed all:pb_public) │  │  │
│  │ │                 │   │ SvelteKit 构建产物         │  │  │
│  │ │ 路由:           │   │ (HTML/JS/CSS)              │  │  │
│  │ │ /api/skills     │   └──────────────────────────┘  │  │
│  │ │ /api/install/*  │                                  │  │
│  │ │ /api/find       │   ↘ OpenAI Go SDK              │  │
│  │ │ /api/agent/*    │     ↓                           │  │
│  │ └────────────────┘   MiniMax-M3 API                  │  │
│  │                      (tool calling)                   │  │
│  │ ┌────────────────┐                                   │  │
│  │ │ PB SQLite      │  (./pb_data/data.db)             │  │
│  │ └────────────────┘                                   │  │
│  └────────────────────────────────────────────────────┘  │
│                                                           │
│ 挂载:                                                     │
│  -v <数据目录>:/app/pb_data              (PB SQLite)     │
│  -v <中央源>:/skills                     (可写)           │
│  -v <专属源>:/personal                   (可写)           │
│  -v <api-key 文件>:/config.env:ro        (API key)       │
└──────────────────────────────────────────────────────────┘
       ↑ 浏览器 (Safari/Chrome) — 127.0.0.1:8090

宿主侧（容器外）:
  launchd com.<you>.agent-skills-sync
    WatchPaths 监听 <中央源> / <专属源> 的增删 + 5 分钟兜底
      → ~/AI/agent-skills/auto-sync.sh (单实例锁 + 5s 去抖)
        → sync.sh 把 skill 软链到各 agent 的 ~/.<agent>/skills/
```

## 关键设计

1. **单二进制部署**：`go:embed all:` 把 SvelteKit 构建产物嵌入 Go 二进制，最终 `pocketbase` 一个文件搞定
2. **容器只管内容，软链由宿主建**：容器内 `/skills`、`/personal` 挂为可写，平台的「安装新 skill」直接落盘到真实目录；但 skill 软链（`~/.<agent>/skills/`）**只能在宿主建立**——容器里 `$HOME=/root`，宿主的 home 不可见。因此同步交给宿主 launchd 监听目录变化后跑 `sync.sh`
3. **API key 不入镜像**：通过文件挂载 `-v <key file>:/config.env:ro` 注入
4. **PB 既是 web 框架又是数据库**：SQLite 内嵌，零外部依赖

## 后端路由

| 路由 | 方法 | 调 LLM | 文件 |
|---|---|---|---|
| `/api/skills` | GET | ❌ | hooks/skills.go |
| `/api/skills/{name}` | GET | ❌ | hooks/skills.go |
| `/api/skills/{name}` | DELETE | ❌ | hooks/skills.go |
| `/api/agents` | GET/POST | ❌ | hooks/agents.go |
| `/api/agents/{name}` | PATCH/DELETE | ❌ | hooks/agents.go |
| `/api/install/content` | POST | ❌ | hooks/install.go |
| `/api/install/url` | POST | ✅ | hooks/install.go |
| `/api/find` | POST | ✅ | hooks/find.go |
| `/api/agent/compose` | POST | ✅ | hooks/agent.go |
| `/api/agent/orchestrate` | POST | ✅ | hooks/agent.go |
| `/api/agent/merge` | POST | ✅ | hooks/agent.go |
| `/api/agent/evolve` | POST | ✅ | hooks/agent.go |
| `/api/health` | GET | ❌ | PB 内置 |

## MiniMax-M3 agent loop

`hooks/llm.go` 的 `AgentRun` 函数实现 mini agent loop：

```
for turn 0..maxTurns:
  resp = client.ChatCompletions(MiniMax-M3, messages, tools)
  messages.append(resp.choices[0].message)  // 整体 append（含 tool_calls）

  if no tool_calls: return content  // 完成

  for each tool_call:
    result = executeTool(tool_call)  // bash / read_file / write_file / list_dir
    messages.append(tool_message(tool_call_id, result))
```

可用工具（受 SafeBash / SafePath 保护）：
- `bash(cmd)` — 白名单内命令
- `read_file(path)` — 读文件
- `write_file(skill_name, filename, content)` — 写到 `/skills/<name>/<filename>`
- `list_dir(path)` — 列目录

**安全边界**：
- `SafePath` 拒绝 `..`、路径分隔符、隐藏目录 → 防 path traversal
- `SafeBash` 白名单 + 危险模式检查（`rm -rf /` / `sudo`） → 防误删系统
- `WriteFile` filename 禁止路径分隔符 → 写文件限在 skill 顶层

## 前端结构

SvelteKit 2 + Svelte 5 + adapter-static（嵌入 Go 二进制）：

```
src/
├── app.css              # Claude 风格主题（stone + 暖色 + 12px 圆角）
├── lib/
│   ├── api.ts                    # 同源 fetch 调 Go hook
│   ├── agents-state.svelte.ts    # agent 列表的全局共享状态（rune 模块）
│   ├── utils.ts                  # cn() helper
│   └── components/
│       ├── AgentManagerDialog.svelte  # 管理 agent tabs（整行拖拽排序）
│       └── ui/                        # shadcn-svelte 组件
└── routes/
    ├── +layout.svelte   # 顶栏（搜索 + 导航 + 齿轮入口）
    ├── +page.svelte     # 主页：skill 卡片网格 + 拖拽排序
    ├── skills/[name]/+page.svelte  # 详情 + LLM 操作
    ├── install/+page.svelte        # 安装向导
    └── find/+page.svelte           # 语义搜结果
```

**排序实现**：主页卡片和管理 Dialog 都用**自研 pointer 拖拽**，不用 `svelte-dnd-action`。
原因见下方修复记录第 11 条。要点：

- 只重排数组、`{#each}` 用业务字段（`name`）作 key，**节点身份从不改变**
- 位移动画交给 `animate:flip`；落点计算用 `offsetLeft/offsetTop` 而非 `getBoundingClientRect()`
  （后者含 transform，会把正在飞行的卡片当成已就位，导致落点反复横跳）
- 卡片常驻 `select-none`，否则拖动途中会把沿途文字整片选中
- 拖完松手浏览器会补一个 `click`，用**时间戳**抑制而非布尔标记（重排后 pointerup 与 click 的
  target 常常不是同一张卡，布尔标记会一直没被消费，反而吃掉后面一次真实点击）

## 关键修复记录

实施过程踩过的坑：

1. **sashabaranov/go-openai 版本不存在**（v0.20.5）→ 改 v1.42.1
2. **`openai.DefaultConfig` v1.42+ 签名变了**（只接受 apiKey）→ 用 `ClientConfig.BaseURL` 设
3. **PB 0.25 不用 echo** → handler 签名是 `*core.RequestEvent`，path param 是 `c.Request.PathValue("name")`
4. **`/api/health` 跟 PB 内置冲突** → 删自定义版
5. **Go 原始字符串不能含反引号** → system prompt 里不用 ```` ``` ````
6. **`svelte/attachments` 的本地 shim 是错的** → 曾用 vite alias 桩掉，但 shim 返回了错误的 symbol
   描述，导致 bits-ui 的 `attachRef` **从不执行**、`ref` 恒为 null、Dialog 永远卸载不掉
   （遮罩继续拦截点击）。正解是升级 Svelte（5.33.19 已含所需修复），shim 已删除
7. **macOS bash 3.2 不支持 `declare -A`** → sync.sh 改用 pattern + case
8. **容器路径 `/skills` 在宿主机不存在** → `SKILLS_DIR` 环境变量覆盖
9. **`//go:embed pb_public/*` 会漏掉 `_` 开头的文件** → Vite 生成 chunk 时同名去重会加 `_` 前缀
   （如 `_LjICcBR.js`），而 go:embed 默认排除 `.`/`_` 开头的文件。缺失时服务端对未知路径回退
   `index.html` 并返回 `text/html`，浏览器对 module 脚本做严格 MIME 校验直接拒绝执行 → **整页白屏**。
   改用 `//go:embed all:pb_public/*`。这个坑是间歇性的，只在该次构建恰好产出 `_` 前缀文件时发作
10. **Go map 迭代顺序随机** → `ListSkills()` 把 map 转 slice 时没排序，同一份数据每次返回顺序都不同，
    未在前端 localStorage 固定过顺序的 skill 每次刷新位置随机 → 加 `sort.Slice` 按 name 排序
11. **`svelte-dnd-action` 0.9.79 在 Svelte 5 下不可用** → 它的 `watchDraggedElement()` 只在原 DOM
    节点被摘出时才调用，于是要么拖不动（用稳定 key）、要么「拖一个少一个」（用库要求的 `id` 作 key，
    原节点被 `hideElement` 后挂到容器外）。改为自研 pointer 拖拽
12. **bits-ui 的 Presence 依赖 rAF** → 页面 `visibilityState=hidden` 时浏览器停掉 rAF，
    关闭动画的 Promise 永不落定，Dialog 卡在 `data-state="closed"` 却不卸载。
    真实使用碰不到（要点击就必须可见），但 headless 自动化会随机中招 →
    测试里加 `Emulation.setFocusEmulationEnabled`
