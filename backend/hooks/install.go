package hooks

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// installRequestContent 粘贴内容安装的请求体
type installRequestContent struct {
	Name    string `json:"name"`
	Content string `json:"content"`
	// Agent 可选：空 = 装到中央源（所有 agent 都有）；给值 = 只装到这个 agent 的专属库。
	// 前端从当前 tab 传，所以「在 minimax 页签点安装」真的会装到 minimax。
	Agent string `json:"agent,omitempty"`
}

// installRequestURL 粘贴 URL 安装的请求体
type installRequestURL struct {
	URL   string `json:"url"`
	Name  string `json:"name,omitempty"`  // 可选：用户指定 skill 名
	Agent string `json:"agent,omitempty"` // 可选：同上，装到某个 agent 的专属库
}

// resolveInstallTarget 返回安装目标根目录和人类可读的说明。
// agent 为空时是中央源。
func resolveInstallTarget(agent string) (base, note string, err error) {
	if agent == "" {
		return skillsDir(), "中央源（所有 agent 共享）", nil
	}
	if agent == "all" {
		// 前端把「已纳管」tab（name=all）传过来时，等同于装中央源
		return skillsDir(), "中央源（所有 agent 共享）", nil
	}
	if err := validateAgentName(agent); err != nil {
		return "", "", err
	}
	return filepath.Join(personalDir(), agent), agent + " 的专属库", nil
}

func validateAgentName(agent string) error {
	if agent == "" || !agentNameRe.MatchString(agent) {
		return fmt.Errorf("invalid agent name: %q", agent)
	}
	return nil
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

	base, targetNote, err := resolveInstallTarget(req.Agent)
	if err != nil {
		return c.JSON(400, map[string]string{"error": err.Error()})
	}

	// 简单 frontmatter 校验
	if !strings.HasPrefix(strings.TrimSpace(req.Content), "---") {
		return c.JSON(400, map[string]string{"error": "SKILL.md must start with YAML frontmatter (---)"})
	}

	if req.Agent == "" || req.Agent == "all" {
		err = WriteFile(req.Name, "SKILL.md", req.Content)
	} else {
		err = WriteFileToPersonal(req.Agent, req.Name, "SKILL.md", req.Content)
	}
	if err != nil {
		return c.JSON(500, map[string]string{"error": err.Error()})
	}

	// 不在这里跑 sync.sh：容器里 $HOME=/root，宿主各 agent 的 ~/.{name}/skills
	// 在容器内根本不存在，跑也是空转（而且错误被吞掉，看着像成功了）。
	// 同步交给宿主 launchd：它 WatchPaths 监听中央源/专属源，一有增删就自动跑
	// ~/AI/agent-skills/sync.sh（另有 5 分钟兜底轮询）。
	return c.JSON(200, map[string]any{
		"installed": req.Name,
		"path":      filepath.Join(base, req.Name, "SKILL.md"),
		"target":    targetNote,
		"sync":      "handled by host launchd (auto-sync.sh), typically within seconds",
		"next":      "open 127.0.0.1:8090 to see the new skill",
	})
}

// installFromURL POST /api/install/url
// 调 LLM：agent 自动 git clone / 找 SKILL.md / 写到目标目录
func installFromURL(c *core.RequestEvent) error {
	var req installRequestURL
	if err := c.BindBody(&req); err != nil {
		return c.JSON(400, map[string]string{"error": err.Error()})
	}
	if req.URL == "" {
		return c.JSON(400, map[string]string{"error": "url required"})
	}

	// "all" = 中央源；给具体 agent = 它的专属库。write_file 工具的 agent 参数跟着走。
	agent := req.Agent
	if agent == "all" {
		agent = ""
	}
	if agent != "" {
		if err := validateAgentName(agent); err != nil {
			return c.JSON(400, map[string]string{"error": err.Error()})
		}
	}

	targetDesc := "/skills/（中央源，所有 agent 共享）"
	writeHint := "write_file 时 agent 留空"
	if agent != "" {
		targetDesc = fmt.Sprintf("/personal/%s/（%s 的专属库）", agent, agent)
		writeHint = fmt.Sprintf("write_file 时 agent 传 %q", agent)
	}

	systemPrompt := fmt.Sprintf(`你是一个 skill 安装助手。用户给你一个 URL（GitHub 仓库 / skills.sh 链接 / 本地路径 / 任何形式的 skill 来源），你的任务是：
1. 用 bash git clone 把它下载到 /tmp/skill-install-XXXX
2. 找到里面的 SKILL.md 文件（可能有多层路径）
3. 读取 SKILL.md 全文
4. 用 write_file 工具把它写到 %s<name>/SKILL.md，name 从 SKILL.md frontmatter 的 name 字段取（如果没指定）。%s
5. 输出最终安装的 skill 名和路径

注意：
- 写操作限在 /skills/ 和 /personal/ 两个子树内
- write_file 的 skill_name 必须用合法字符（字母数字下划线连字符）
- git clone 用 --depth 1 加速`, targetDesc, writeHint)

	userPrompt := fmt.Sprintf("请安装这个 URL: %s", req.URL)
	if req.Name != "" {
		userPrompt += fmt.Sprintf("\n用户指定的 skill 名: %s", req.Name)
	}

	result, err := AgentRun(c.Request.Context(), systemPrompt, userPrompt, 15)
	if err != nil {
		return c.JSON(500, map[string]string{"error": err.Error()})
	}

	return c.JSON(200, map[string]any{
		"installed":    "see agent output",
		"url":          req.URL,
		"target":       targetDesc,
		"agent_output": result,
	})
}
