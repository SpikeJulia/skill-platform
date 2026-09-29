<div align="center">

# 🗂️ Skill Platform

#### 自己管 skill 的小平台

[![License](https://img.shields.io/badge/License-MIT-10B981?style=for-the-badge)](./LICENSE)
[![MrTang-Skills](https://img.shields.io/badge/MrTang_Skills-8B5CF6?style=for-the-badge)](https://github.com/SpikeJulia/MrTang-Skills)

![Claude Code](https://img.shields.io/badge/Claude_Code-Compatible-D97706?style=flat-square&logo=anthropic&logoColor=white)
![Codex](https://img.shields.io/badge/Codex-Compatible-10B981?style=flat-square&logo=openai&logoColor=white)
![OpenCode](https://img.shields.io/badge/OpenCode-Compatible-3B82F6?style=flat-square)

</div>

自己搭着搭着顺手抽出来的一个东西。起因很简单：skill 越攒越多，但「我到底有哪些、装没装上」全靠翻文件夹，每次都得靠记忆。

装一次，所有 agent 都能用。

---

## 📋 目录

| 名字 | 一句话 |
|---|---|
| 📦 [**快速开始**](#-快速开始) | 一条命令 |
| ✨ [**它能做什么**](#-它能做什么) | 看得清、找得到、管得住 |
| 🌐 [**谁能用**](#-谁能用) | 任何会读 skill 目录的 agent |
| 📄 [**更多细节**](#-更多细节) | 想改的话看这里 |

---

## 📦 快速开始

要装 Docker，然后：

```bash
git clone https://github.com/SpikeJulia/skill-platform.git
cd skill-platform
docker compose up -d --build
```

浏览器打开 `http://127.0.0.1:8090` 就能用了。

界面上的浏览、搜索、排序、管理都不用配任何东西。

**只有「用一句话描述来找 skill」这类功能**需要一个模型 API 密钥（就是 AI 服务那边的那个 key），
放进一个配置文件告诉平台就行。没有它，那几个功能用不了，别的照常。

---

## ✨ 它能做什么

<table>
<tr><td>

### 🗂️ 看得清

> *"我到底装了多少个 skill，愣是没答上来。"*

**为什么需要这个**

skill 一多，靠翻文件夹就废了。更麻烦的是「你看到的」和「agent 实际能用的」经常不是一回事——同一个 skill，有的 agent 有、有的没有，界面上却只看得到一份。

**它能做什么**

- 📋 **一屏看全部** — 有哪些、什么描述，一眼扫完
- 🔍 **顶栏搜索** — 敲关键词就出结果
- 🏷️ **按 agent 分页签** — 点进去看某个 agent 到底装了哪些
- 💬 **搜到了别的 agent 专属的会提示你** — 「其他 agent 还有 3 个匹配」，点一下就切过去看

</td></tr>
<tr><td>

### ↕️ 排得顺

> *"排序本来是件顺手的事，直到我发现它把我没看见的东西弄没了。"*

**为什么需要这个**

不是排序本身，是**一边搜索一边排序**的时候最容易出事。早期版本会把屏幕上没显示的那些漏掉，一拖就真没了。

**它能做什么**

- 🎯 **拖一下就改顺序**，改完自动记住，下次打开还在
- 🔒 **开着搜索也能拖** — 不会把没显示出来的那些顺带弄丢

</td></tr>
<tr><td>

### ⚙️ 管得住

> *"有个 agent 一直没收到新 skill，查了两天才发现是名单里少了它。"*

**为什么需要这个**

往各 agent 装东西是按一份名单来的。名单和实际用着的对不上时，**不会报错**——那边就是安静地收不到，最难查。

**它能做什么**

- ➕ **加 / 删 agent** — 删之前会问你一次
- 🧩 **名单随便改**，不是写死的几个
- 🧹 **不乱加** — 名单里没有的 agent，它就是没有，不会自作主张给你补回来

</td></tr>
<tr><td>

### 🔌 装一次，所有 agent 都能用

> *"同一个 skill 抄五份，五份很快就会不一样。"*

**为什么需要这个**

不同 agent 各放一份，改一处要同步改五处，很快就会走偏。

**它能做什么**

- 🔗 **装一次** — 自动出现在每个 agent 的 skill 目录下
- ✏️ **改一处，全体生效** — 因为大家指的是同一份
- 🧱 **专属能力可以单独装** — 只给某个 agent 用，不影响别人
- 👀 **漏掉的会告诉你** — 有 skill 存在但没纳管，主页底部会列出来并给你一条命令搞定

</td></tr>
</table>

---

## 🌐 谁能用

任何会读自己 skill 目录的 agent 都行——把 agent 加进平台，链接就自动建好了，不用手动配。

Claude Code、Codex、OpenCode 都能接。

界面之外还留了一组接口，脚本或者别的 agent 想直接调用也行。

---

## 📄 更多细节

想改点什么、或者用着出问题了：

- [docs/architecture.md](./docs/architecture.md) — 它是怎么搭的
- [docs/operations.md](./docs/operations.md) — 日常怎么维护

---

## 🌟 关于

我是 Mr.Tang，一个为人民服务的普通人，喜欢鼓捣 AI 相关的小玩意儿。

这个平台是自己用着顺手才抽出来的。用着有问题，欢迎在 Issues 里说一声。

Skill 本体在另一个仓库：[SpikeJulia/MrTang-Skills](https://github.com/SpikeJulia/MrTang-Skills)。

---

## 📜 License

[MIT](./LICENSE)
