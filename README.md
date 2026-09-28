# Skill Platform v0.1

自托管的 Skill 管理平台，运行在 Mac mini 上。常驻一个 docker 容器，UI 仿 Claude 风格。

## 是什么

- 单一 docker 容器：PocketBase 0.25+ (Go 单二进制) + SvelteKit 静态前端（嵌入 Go 二进制）
- 中央源：`~/AI/agent-skills/`（已通过 `sync.sh` 软链到 5 个 agent）
- 数据：`~/AI/skill-platform-data/`（PB SQLite）
- API：MiniMax-M3 直调（v0.1 不装 pi）

## 跑起来

```bash
# 1. 启动容器
cd ~/AI/skill-platform
docker compose up -d --build

# 2. 浏览器打开
open http://127.0.0.1:8090
```

## 功能（v0.1）

| 功能 | 路径 | 调 LLM？ |
|---|---|---|
| 列出 skill | `GET /api/skills` | ❌ |
| 查看 SKILL.md | `GET /api/skills/{name}` | ❌ |
| 卸载 skill | `DELETE /api/skills/{name}` | ❌ |
| 粘贴内容安装 | `POST /api/install/content` | ❌ |
| 粘贴 URL 安装 | `POST /api/install/url` | ✅ |
| 语义搜 | `POST /api/find` | ✅ |
| 组合 skill | `POST /api/agent/compose` | ✅ |
| 编排工作流 | `POST /api/agent/orchestrate` | ✅ |
| 合并 skill | `POST /api/agent/merge` | ✅ |
| 进化 skill | `POST /api/agent/evolve` | ✅ |

## 升级

```bash
git pull && docker compose up -d --build
```

## 备份

```bash
cp -r ~/AI/skill-platform-data ~/AI/skill-platform-data.bak.$(date +%F)
```

## 资源

- 镜像：~50-60MB
- 内存：~20-40MB idle
- CPU：<1% idle

## 开发模式

不用 docker，直接在宿主机跑：

```bash
cd backend
go build -o /tmp/skill-platform .

# 启动
MINIMAX_API_KEY=$(grep '^MINIMAX_API_KEY=' ~/AI/asr/config.env | cut -d= -f2- | tr -d '"\n ') \
SKILLS_DIR=/Users/tangxuan/AI/agent-skills \
  /tmp/skill-platform serve --http=127.0.0.1:8090
```

## 已知限制（v0.1）

- 不支持 Tailscale 暴露（仅本机）
- 不接 pi（v0.2）
- 节点画布推迟到 v0.2
- 拖拽排序只存 localStorage（v0.2 加 PB SQLite 持久化）
- Auth 关闭（localhost 默认安全；v0.2 加多用户）
