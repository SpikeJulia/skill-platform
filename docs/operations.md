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
~/AI/skill-platform-data/        ← PB SQLite + 上传文件
~/AI/agent-skills/               ← 中央源（实际就是 skill 本身，建议另外 backup 到 NAS）
~/AI/asr/config.env              ← 已有 MINIMAX_API_KEY
```

## 升级

### 升级 Skill Platform

```bash
cd ~/AI/skill-platform
git pull
docker compose up -d --build
```

### 升级 MiniMax API 模型

如果以后用更新的模型，编辑 `backend/hooks/register.go`：
```go
const LLMModel = "MiniMax-M3"  // 改这里
```
然后 `docker compose up -d --build`。

## 故障排查

### 容器起不来

```bash
docker compose logs skill-platform
# 看 panic 信息
```

常见：
- `/api/health conflicts` → 检查 hooks/register.go 不要注册内置端点
- `MINIMAX_API_KEY not set` → 检查 ~/AI/asr/config.env 是否挂载

### LLM 调用超时

MiniMax-M3 是推理模型，慢（30s-2min 都有可能）。前端 "调 LLM 中..." 转圈耐心等。

如果持续超时：直接 `curl` 测 API：
```bash
curl -X POST https://api.minimax.cn/v1/chat/completions \
  -H "Authorization: Bearer $MINIMAX_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"MiniMax-M3","messages":[{"role":"user","content":"hi"}]}'
```

### 5 个 agent 软链没更新

```bash
# 手动跑 sync
bash ~/AI/agent-skills/sync.sh
```

## 安全

- 仅本机监听（`127.0.0.1:8090`）
- 中央源只读挂载（容器里不能改中央源；要装新 skill 通过 API）
- bash 工具白名单（git/ls/find/grep/cp/mv/mkdir/echo/bash）—— agent 误操作影响有限
- API key 通过环境变量注入容器（不写入镜像）
