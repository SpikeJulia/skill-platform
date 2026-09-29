<div align="center">

# 🗂️ Skill Platform

#### 自己管 skill 的一个小平台，跑在 Mac mini 上

[![License](https://img.shields.io/badge/License-MIT-10B981?style=for-the-badge)](./LICENSE)
[![Stack](https://img.shields.io/badge/Stack-PocketBase_0.25%20%2B%20SvelteKit-3B82F6?style=for-the-badge)](#-技术栈)
[![MrTang-Skills](https://img.shields.io/badge/MrTang_Skills-8B5CF6?style=for-the-badge)](https://github.com/SpikeJulia/MrTang-Skills)

![Claude Code](https://img.shields.io/badge/Claude_Code-Compatible-D97706?style=flat-square&logo=anthropic&logoColor=white)
![Codex](https://img.shields.io/badge/Codex-Compatible-10B981?style=flat-square&logo=openai&logoColor=white)
![OpenCode](https://img.shields.io/badge/OpenCode-Compatible-3B82F6?style=flat-square)

</div>

> [!NOTE]
> **只开源这个管理平台，skill 内容不进这个仓库。** 平台读的是你本机的 skill 目录，不打包、不分发。

自己搭着搭着就顺手抽出来的一个东西。起因很简单：skill 越攒越多，但「有哪些、谁在用、装没装上」全靠翻文件夹，
每次都得靠记忆。

---

## 📋 目录

| 名字 | 一句话 |
|---|---|
| 🗂️ [**它能做什么**](#-它能做什么) | 浏览、搜索、按 agent 分 tab、拖排序、管 agent 名单 |
| 📦 [**快速开始**](#-快速开始) | 一条 `docker compose` |
| 🔌 [**投递链**](#-投递链) | 平台写文件，launchd 建软链 |
| 🧱 [**技术栈**](#-技术栈) | 单容器，PB + Go hooks + SvelteKit |
| ⚠️ [**已知限制**](#️-已知限制) | 还差点东西 |

---

## 📦 快速开始

```bash
git clone https://github.com/SpikeJulia/skill-platform.git
cd skill-platform
docker compose up -d --build
open http://127.0.0.1:8090
```

要一个 `MINIMAX_API_KEY` 放 `~/AI/asr/config.env`（compose 里挂进去当 `MINIMAX_API_KEY_FILE`）。
**只想浏览和管理 skill、不用语义搜索和组合功能的话，这个 key 可以先不配**，那几个接口返回不了而已，别的照常用。

---

## ✨ 它能做什么

<table>
<tr><td>

### 🗂️ 浏览与搜索

> *"我有五个 agent，skill 装没装上全靠 `ls`。少一个装不上，还不一定看得出来。"*

主页一屏看全部 skill，搜索框就在顶栏，不用切页面。

**为什么需要这个**

skill 一旦过十个，靠翻目录就废了——更要命的是「你看到的」和「agent 实际能用的」经常不是一回事。

**它能做什么**

- 🔍 **顶栏搜索** — 名字和描述本地匹配，不调 LLM，敲完就出结果
- 🏷️ **按 agent 分 tab** — `全部 / minimax / codex / …`，看某个 agent 到底装了哪些
- 🔗 **「其他 agent 还有 N 个匹配」** — 在「全部」里搜到的东西，顺手告诉你哪些是别的 agent 专属的，点一下就切过去
- 📄 **看 SKILL.md 原文**，不用 `cat`

</td></tr>
<tr><td>

### ↕️ 拖拽排序

> *"排序本来就是件小事——直到你发现它会把你没看见的东西删掉。"*

卡片直接拖，顺序存本地。

**为什么需要这个**

不是排序本身，是排序的时候**正在筛选**。旧库那个实现把筛选结果直接写回列表，
你在搜索状态下拖一次，就真把搜不到的那批 skill 弄没了。

**它能做什么**

- 🎯 **拖的是原始数组的 id，不是可见列表的下标** — 搜索和 tab 开着也能拖
- 🫳 **跟手的浮动卡片** — 拖起来是"拎着"的感觉，不是两张卡瞬间换位
- 💾 **顺序存 localStorage** — 不入库，重启容器还在

**踩过的坑**

`svelte-dnd-action` 0.9.79 在 Svelte 5 下会把被拖的节点摘出 DOM 再隐藏挂回，表现为"拖一个少一个"。
所以这两处排序都是自己写的 pointer 拖拽。细节在 [docs/architecture.md](./docs/architecture.md)。

</td></tr>
<tr><td>

### ⚙️ agent 名单管理

> *"平台里少一个 agent，它的 skill 目录就永远同步不到——而且不报错。"*

agent 列表在平台上增删，写进 PocketBase。

**为什么需要这个**

投递链是拿平台的 agent 名单去建软链的。名单和实际装着的 agent 对不上时，
**表现是静默失败**：那边收不到新 skill，日志也不报错，最难查。

**它能做什么**

- ➕ **加 / 删 agent** — 删之前确认一次，入口在顶栏齿轮
- 🧱 **名单随便增删**，不是写死的（现在留了 `全部 / minimax / codex` 三个）
- 🚨 **不做"补全"** — 平台里没有的 agent 就是没有了，不会自作主张给你加回来

</td></tr>
<tr><td>

### 🔌 投递链

> *"'在 Docker 里建软链' 这件事我试了很久，它是做不到的。"*

平台把 skill 写成文件，launchd 在宿主机上建软链。

**为什么需要这个**

容器里 `$HOME=/root`，看不到宿主的 home。`sync.sh` 在容器内跑是**空转**——
以前 `install.go` 里那句还被 `SafeBash` 吞了错误，返回体看着挺正常，误导了很久。

**它能做什么**

- 📝 **平台只写文件** — `/skills` 和 `/personal` 是可写挂载
- 🔗 **软链交给 launchd** — `WatchPaths` 监听增删 + `StartInterval=300` 兜底
- 🔒 **专属库物理隔离** — 别的 agent 的专属 skill 要领用，是**复制**进你自己的库，不是软链过去

**⚠️ 一条铁律：`sync.sh` 永远只能在宿主机跑。**

</td></tr>
<tr><td>

### 🤖 13 个 API

> *"界面够用的话，剩下的就当后台服务用。"*

13 个接口，前端只用了其中一部分，剩下的留给脚本和别的 agent。

**它能做什么**

| | 接口 | 调 LLM |
|---|---|---|
| 📋 | `GET /api/skills` | ❌ |
| 📄 | `GET /api/skills/{name}` | ❌ |
| 🗑️ | `DELETE /api/skills/{name}` | ❌ |
| 📥 | `POST /api/install/content` | ❌ |
| 🔗 | `POST /api/install/url` | ✅ |
| 🔍 | `POST /api/find` | ✅ |
| 👥 | `GET /api/agents` | ❌ |
| 👥 | `POST /api/agents` | ❌ |
| 👥 | `DELETE /api/agents/{name}` | ❌ |
| 🧩 | `POST /api/agent/compose` | ✅ |
| 🎼 | `POST /api/agent/orchestrate` | ✅ |
| 🔀 | `POST /api/agent/merge` | ✅ |
| 🧬 | `POST /api/agent/evolve` | ✅ |

</td></tr>
</table>

---

## 🧱 技术栈

- **单容器**：PocketBase 0.25（Go 单二进制）+ SvelteKit 静态前端，静态产物 `go:embed` 进二进制
- **数据**：`~/AI/skill-platform-data/`（PB SQLite）
- **前端**：SvelteKit 2 + Svelte 5 + Tailwind 4 + shadcn-svelte
- **模型**：MiniMax-M3 直调
- **投递**：宿主 launchd（`com.tangxuan.agent-skills-sync`）建软链

架构细节和 12 条修复记录在 [docs/architecture.md](./docs/architecture.md)，运维在 [docs/operations.md](./docs/operations.md)。

**🌐 兼容任何读 `~/.<agent>/skills/` 的 agent** — 把 agent 加进平台，`sync.sh` 就会建对应的软链。
Claude Code / Codex / OpenCode 都能接。

---

## ⚠️ 已知限制

- 仅本机，`127.0.0.1:8090`，没有 Tailscale 暴露
- 排序只存 localStorage，没进 PB
- Auth 关闭（localhost 默认安全）
- 不接 pi，节点画布也没做
- 拖拽排序**没在触摸设备上试过**

---

## 🌟 关于

我是 Mr.Tang，一个为人民服务的普通人，喜欢鼓捣 AI 相关的小玩意儿。

这个平台是自己用着顺手才抽出来的，不是给别人设计的产品。用着有问题，欢迎在 Issues 里说一声。

Skill 本体在另一个仓库：[SpikeJulia/MrTang-Skills](https://github.com/SpikeJulia/MrTang-Skills)。

---

## 📜 License

[MIT](./LICENSE)
