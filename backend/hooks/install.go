package hooks

import (
	"fmt"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// installRequestContent 粘贴内容安装的请求体
type installRequestContent struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

// installRequestURL 粘贴 URL 安装的请求体
type installRequestURL struct {
	URL  string `json:"url"`
	Name string `json:"name,omitempty"` // 可选：用户指定 skill 名
}

// installFromContent POST /api/install/content
// 纯文件操作，不调 LLM
func installFromContent(c *core.RequestEvent) error {
	var req installRequestContent
	if err := c.BindBody(&req); err != nil {
		return c.JSON(400, map[string]string{"error": err.Error()})
	}
	if req.Name == "" || req.Content == "" {
		return c.JSON(400, map[string]string{"error": "name and content required"})
	}

	// 简单 frontmatter 校验
	if !strings.HasPrefix(strings.TrimSpace(req.Content), "---") {
		return c.JSON(400, map[string]string{"error": "SKILL.md must start with YAML frontmatter (---)"})
	}

	if err := WriteFile(req.Name, "SKILL.md", req.Content); err != nil {
		return c.JSON(500, map[string]string{"error": err.Error()})
	}

	// 不在这里跑 sync.sh：容器里 $HOME=/root，宿主各 agent 的 ~/.{name}/skills
	// 在容器内根本不存在，跑也是空转（而且错误被吞掉，看着像成功了）。
	// 同步交给宿主 launchd：它 WatchPaths 监听中央源/专属源，一有增删就自动跑
	// ~/AI/agent-skills/sync.sh（另有 5 分钟兜底轮询）。
	return c.JSON(200, map[string]any{
		"installed": req.Name,
		"path":      fmt.Sprintf("%s/%s/SKILL.md", skillsDir(), req.Name),
		"sync":      "handled by host launchd (auto-sync.sh), typically within seconds",
		"next":      "open 127.0.0.1:8090 to see the new skill",
	})
}

// installFromURL POST /api/install/url
// 调 LLM：agent 自动 git clone / 找 SKILL.md / 写到中央源 / 跑 sync.sh
func installFromURL(c *core.RequestEvent) error {
	var req installRequestURL
	if err := c.BindBody(&req); err != nil {
		return c.JSON(400, map[string]string{"error": err.Error()})
	}
	if req.URL == "" {
		return c.JSON(400, map[string]string{"error": "url required"})
	}

	systemPrompt := `你是一个 skill 安装助手。用户给你一个 URL（GitHub 仓库 / skills.sh 链接 / 本地路径 / 任何形式的 skill 来源），你的任务是：
1. 用 bash git clone 把它下载到 /tmp/skill-install-XXXX
2. 找到里面的 SKILL.md 文件（可能有多层路径）
3. 读取 SKILL.md 全文
4. 用 write_file 工具把它写到 /skills/<name>/SKILL.md，name 从 SKILL.md frontmatter 的 name 字段取（如果没指定）
5. 最后用 bash 跑 sync.sh（路径在容器内是 /skills/sync.sh，宿主机由 SKILLS_DIR 环境变量决定；用 find 命令从 /skills 找）
6. 输出最终安装的 skill 名和路径

注意：
- 所有写操作限在 /skills/ 子树
- write_file 的 skill_name 必须用合法字符（字母数字下划线连字符）
- git clone 用 --depth 1 加速`

	userPrompt := fmt.Sprintf("请安装这个 URL: %s", req.URL)
	if req.Name != "" {
		userPrompt += fmt.Sprintf("\n用户指定的 skill 名: %s", req.Name)
	}

	result, err := AgentRun(c.Request.Context(), systemPrompt, userPrompt, 15)
	if err != nil {
		return c.JSON(500, map[string]string{"error": err.Error()})
	}

	return c.JSON(200, map[string]any{
		"installed": "see agent output",
		"url":       req.URL,
		"agent_output": result,
	})
}
