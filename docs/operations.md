# Skill Platform 运维手册

## 日常

### 启动 / 停止

```bash
cd ~/AI/skill-platform
docker compose up -d          # 启动
docker compose down           # 停止
docker compose restart        # 重启
docker compose logs -f        # 看日志
```

### 健康检查

```bash
curl http://127.0.0.1:8090/api/health
# → {"message":"API is healthy.","code":200,"data":{}}
```

### 资源监控

```bash
docker stats skill-platform --no-stream
```

期望：MEM USAGE 20-50MB / CPU <1%。

## 数据

### 备份

```bash
# 手动备份
cp -r ~/AI/skill-platform-data ~/AI/skill-platform-data.bak.$(date +%F)

# 恢复
rm -rf ~/AI/skill-platform-data
cp -r ~/AI/skill-platform-data.bak.2026-09-12 ~/AI/skill-platform-data
docker compose restart
```

### 备份什么

```
~/AI/skill-platform-data/        ← PB SQLite（含 llm_config 里的 API key）+ 上传文件
~/AI/agent-skills/               ← 中央源（实际就是 skill 本身，建议另外 backup 到 NAS）
```

## 升级

### 升级 Skill Platform

```bash
cd ~/AI/skill-platform
git pull
docker compose up -d --build
```

### 升级模型

不用改代码。打开顶栏的「Agent 配置」，改「模型名称」（和提供商、接口地址、API 格式、
API Key）→ 保存并测试。配置存在 PB 的 `llm_config` 表里。

想确认当前配的是什么：
```bash
curl -s http://127.0.0.1:8090/api/config
```

## 故障排查

### 容器起不来

```bash
docker compose logs skill-platform
# 看 panic 信息
```

常见：
- `/api/health conflicts` → 检查 hooks/register.go 不要注册内置端点
- `尚未配置 API Key` → 到顶栏「Agent 配置」里填 key（key 存在 PB 里，不走环境变量）

### LLM 调用超时

推理模型慢（30s-2min 都有可能）。前端 "调 LLM 中..." 转圈耐心等。

如果持续超时：先看「Agent 配置」里配的是什么，然后用同样的协议和地址直连测。
下面以默认的 Anthropic Messages 协议为例（key 从 PB 里读，不落命令行历史）：

```bash
KEY=$(curl -s http://127.0.0.1:8090/api/config | grep -o '"has_api_key":[a-z]*' || true)
echo "$KEY"   # 只看有没有配；真要直连请自己从「Agent 配置」复制 key

curl -X POST https://api.minimaxi.com/anthropic/v1/messages \
  -H "x-api-key: <你的 key>" \
  -H "anthropic-version: 2023-06-01" \
  -H "Content-Type: application/json" \
  -d '{"model":"MiniMax-M3","max_tokens":64,"messages":[{"role":"user","content":"hi"}]}'
```

> 注意国内端点是 `api.minimaxi.com`。国际站的 `api.minimax.io` 在国内会返 401。

### agent 软链没更新

正常情况下宿主 launchd（`com.<you>.agent-skills-sync`）会在几秒内自动同步——
它 WatchPaths 监听两个源目录的增删，另有 5 分钟兜底。怀疑没触发或急用时手动跑：

```bash
bash ~/AI/agent-skills/sync.sh
```

注意：**agent 名单取自平台里的 agent tabs**。平台里少一个 agent，它的 skills 目录就同步不到。

### 平台列不出某个 skill，但 agent 能用

先看主页底部的「未纳管」区块（默认折叠）。它会分开列两类：

- **可纳管** — 你的 `~/.<agent>/skills/<name>` 是真目录而不是软链。复制那条 `mv` 命令到终端跑，
  它会进专属源，之后自动变成软链、平台也就列出来了
- **agent 自带** — 那些随 agent 版本自动更新的，不该纳管

区块内容来自 `~/AI/agent-skills-personal/.unmanaged.json`，每次 `sync.sh` 跑完重写。
想立刻刷新：

```bash
bash ~/AI/agent-skills/sync.sh
```

区块整个不见，通常是 `sync.sh` 没跑过或跑失败了——手动跑一次看看有没有报错。
注意 `sync.sh` **只能在宿主跑**，容器内跑是空转。

## 安全

- 仅本机监听（`127.0.0.1:8090`）
- 中央源与专属源按**可写**挂载（平台的安装功能需要真正落盘）；但 skill 软链只在宿主建立
- bash 工具白名单（git/ls/find/grep/cp/mv/mkdir/echo/bash）—— agent 误操作影响有限
- API key 通过文件挂载注入容器（不写入镜像）
