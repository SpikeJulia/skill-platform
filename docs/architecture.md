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
│  -v ~/AI/skill-platform-data:/app/pb_data               │
│  -v ~/AI/agent-skills:/skills:ro       (只读)           │
│  -v ~/AI/asr/config.env:/config.env:ro  (API key)        │
└──────────────────────────────────────────────────────────┘
       ↑ 浏览器 (Safari/Chrome) — 127.0.0.1:8090
```

## 关键设计

1. **单二进制部署**：`go:embed` 把 SvelteKit 构建产物嵌入 Go 二进制，最终 `pocketbase` 一个文件搞定
2. **中央源只读**：容器内 `/skills` 挂为只读，agent 想"加 skill"必须通过 API（被 Go hook 校验）
3. **API key 不入镜像**：通过 `-v ~/AI/asr/config.env:/config.env:ro` 注入
4. **PB 既是 web 框架又是数据库**：SQLite 内嵌，零外部依赖

## 后端路由

| 路由 | 方法 | 调 LLM | 文件 |
|---|---|---|---|
| `/api/skills` | GET | ❌ | hooks/skills.go |
| `/api/skills/{name}` | GET | ❌ | hooks/skills.go |
| `/api/skills/{name}` | DELETE | ❌ | hooks/skills.go |
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
│   ├── api.ts          # 同源 fetch 调 Go hook
│   ├── utils.ts        # cn() helper
│   └── components/ui/  # shadcn-svelte 组件
└── routes/
    ├── +layout.svelte   # 顶栏（搜索 + 导航）
    ├── +page.svelte     # 主页：skill 卡片网格 + 拖拽排序
    ├── skills/[name]/+page.svelte  # 详情 + LLM 操作
    ├── install/+page.svelte        # 安装向导
    └── find/+page.svelte           # 语义搜结果
```

`vite.config.ts` 有个 alias：`'svelte/attachments'` → 本地 shim
（svelte-toolbelt 0.10.6 引用了但 svelte 5 不暴露）

## 关键修复记录

实施过程踩过的坑：

1. **sashabaranov/go-openai 版本不存在**（v0.20.5）→ 改 v1.42.1
2. **`openai.DefaultConfig` v1.42+ 签名变了**（只接受 apiKey）→ 用 `ClientConfig.BaseURL` 设
3. **PB 0.25 不用 echo** → handler 签名是 `*core.RequestEvent`，path param 是 `c.Request.PathValue("name")`
4. **`/api/health` 跟 PB 内置冲突** → 删自定义版
5. **Go 原始字符串不能含反引号** → system prompt 里不用 ```` ``` ````
6. **svelte-toolbelt 0.10.6 引用 svelte/attachments** → vite alias 桩掉
7. **macOS bash 3.2 不支持 `declare -A`** → sync.sh 改用 pattern + case
8. **容器路径 `/skills` 在宿主机不存在** → `SKILLS_DIR` 环境变量覆盖
