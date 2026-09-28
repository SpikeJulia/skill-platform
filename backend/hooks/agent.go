package hooks

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// agentRequest 通用 LLM 操作请求
type agentRequest struct {
	Skills []string `json:"skills"`         // 选中的 skill 名
	Prompt string   `json:"prompt,omitempty"` // 用户补充要求
	Target string   `json:"target,omitempty"` // 进化/合并时：新 skill 名
}

// agentResponse 通用响应
type agentResponse struct {
	Operation   string `json:"operation"`
	Result      string `json:"result"`        // LLM 输出（Markdown）
	Affected    []string `json:"affected"`    // 受影响的 skill 名
	TargetPath  string `json:"target_path,omitempty"`
}

// 加载选中的 skill 的 SKILL.md 内容
func loadSkillsContent(names []string) (string, error) {
	var sb strings.Builder
	for _, n := range names {
		content, err := ReadSkillMD(n)
		if err != nil {
			return "", fmt.Errorf("read skill %q: %w", n, err)
		}
		sb.WriteString(fmt.Sprintf("=== Skill: %s ===\n%s\n\n", n, content))
	}
	return sb.String(), nil
}

// agentCompose POST /api/agent/compose - 组合 skill
// 读选中的 skill → 让 LLM 输出"如何组合使用"的方案
func agentCompose(c *core.RequestEvent) error {
	var req agentRequest
	if err := c.BindBody(&req); err != nil {
		return c.JSON(400, map[string]string{"error": err.Error()})
	}
	if len(req.Skills) < 2 {
		return c.JSON(400, map[string]string{"error": "select at least 2 skills"})
	}

	content, err := loadSkillsContent(req.Skills)
	if err != nil {
		return c.JSON(500, map[string]string{"error": err.Error()})
	}

	systemPrompt := `你是 skill 组合设计助手。给定多个 skill 的描述，你设计一个"组合使用方案"——如何把它们的输入输出串联起来解决一个更大的问题。输出 Markdown 格式：
- 一句话总结这个组合解决什么
- 输入 → 处理 → 输出的流程图（用文字描述）
- 涉及的每个 skill 在流程里扮演什么角色
- 关键约束 / 边界`

	userPrompt := fmt.Sprintf("请组合以下 skill：\n\n%s\n\n%s",
		content, optionalPrompt(req.Prompt))

	result, err := AgentRun(c.Request.Context(), systemPrompt, userPrompt, 5)
	if err != nil {
		return c.JSON(500, map[string]string{"error": err.Error()})
	}

	return c.JSON(200, agentResponse{
		Operation: "compose",
		Result:    result,
		Affected:  req.Skills,
	})
}

// agentOrchestrate POST /api/agent/orchestrate - 编排工作流
// 跟 compose 类似但更细：要求 LLM 给出"可执行"的工作流步骤
func agentOrchestrate(c *core.RequestEvent) error {
	var req agentRequest
	if err := c.BindBody(&req); err != nil {
		return c.JSON(400, map[string]string{"error": err.Error()})
	}
	if len(req.Skills) == 0 {
		return c.JSON(400, map[string]string{"error": "select at least 1 skill"})
	}

	content, err := loadSkillsContent(req.Skills)
	if err != nil {
		return c.JSON(500, map[string]string{"error": err.Error()})
	}

	systemPrompt := `你是工作流编排助手。给定多个 skill，输出一个可执行的工作流定义（Markdown 格式）：
- 触发条件（什么时候启动这个工作流）
- 按顺序的执行步骤（每步：哪个 skill / 输入什么 / 期望输出）
- 分支条件（哪些步骤可能跳过）
- 终止条件（什么时候结束）
- 错误处理（某步失败怎么办）

输出要"可执行"——另一个 agent 看了能直接照着跑。`

	userPrompt := fmt.Sprintf("请编排以下 skill 为一个工作流：\n\n%s\n\n%s",
		content, optionalPrompt(req.Prompt))

	result, err := AgentRun(c.Request.Context(), systemPrompt, userPrompt, 5)
	if err != nil {
		return c.JSON(500, map[string]string{"error": err.Error()})
	}

	return c.JSON(200, agentResponse{
		Operation: "orchestrate",
		Result:    result,
		Affected:  req.Skills,
	})
}

// agentMerge POST /api/agent/merge - 合并 skill
// 读 2+ skill → LLM 输出合并后的 SKILL.md → 写到 Target 名的新 skill
func agentMerge(c *core.RequestEvent) error {
	var req agentRequest
	if err := c.BindBody(&req); err != nil {
		return c.JSON(400, map[string]string{"error": err.Error()})
	}
	if len(req.Skills) < 2 {
		return c.JSON(400, map[string]string{"error": "select at least 2 skills to merge"})
	}
	if req.Target == "" {
		return c.JSON(400, map[string]string{"error": "target name required for merge result"})
	}

	content, err := loadSkillsContent(req.Skills)
	if err != nil {
		return c.JSON(500, map[string]string{"error": err.Error()})
	}

	systemPrompt := `你是 skill 合并助手。给定多个 skill 的 SKILL.md，你要：
1. 找出它们的共同点和差异
2. 去重 / 整合功能
3. 写出一个合并后的 SKILL.md（保留 YAML frontmatter，name 用指定的新名，description 综合）
4. 合并后的 skill 应该比单个 skill 更通用，但不要丢失核心能力

输出格式：先简短解释合并决策（Markdown），然后用 markdown 代码围栏（三个反引号）写出合并后的完整 SKILL.md。`

	userPrompt := fmt.Sprintf("请合并以下 skill 为一个新 skill，名为: %s\n\n%s\n\n%s",
		req.Target, content, optionalPrompt(req.Prompt))

	result, err := AgentRun(c.Request.Context(), systemPrompt, userPrompt, 8)
	if err != nil {
		return c.JSON(500, map[string]string{"error": err.Error()})
	}

	// 从 LLM 输出抠出合并后的 SKILL.md
	merged := extractCodeBlock(result, "markdown")
	if merged == "" {
		merged = extractCodeBlock(result, "")
	}
	if merged == "" {
		// LLM 没返回围栏，整个结果当 SKILL.md
		merged = result
	}

	// 写新文件
	if err := WriteFile(req.Target, "SKILL.md", merged); err != nil {
		return c.JSON(500, map[string]string{"error": "agent output ok but write failed: " + err.Error()})
	}

	// 跑 sync
	SafeBash("bash " + filepath.Join(skillsDir(), "sync.sh"))

	return c.JSON(200, agentResponse{
		Operation:  "merge",
		Result:     result,
		Affected:   req.Skills,
		TargetPath: fmt.Sprintf("%s/%s/SKILL.md", skillsDir(), req.Target),
	})
}

// agentEvolve POST /api/agent/evolve - 进化 skill
// 读 1 个 skill → LLM 根据用户反馈改进 → 写回（同名覆盖）
func agentEvolve(c *core.RequestEvent) error {
	var req agentRequest
	if err := c.BindBody(&req); err != nil {
		return c.JSON(400, map[string]string{"error": err.Error()})
	}
	if len(req.Skills) != 1 {
		return c.JSON(400, map[string]string{"error": "evolve operates on exactly 1 skill"})
	}
	name := req.Skills[0]
	content, err := loadSkillsContent([]string{name})
	if err != nil {
		return c.JSON(500, map[string]string{"error": err.Error()})
	}
	if req.Prompt == "" {
		return c.JSON(400, map[string]string{"error": "prompt required: how should this skill evolve?"})
	}

	systemPrompt := `你是 skill 进化助手。给定一个 skill 的 SKILL.md 和用户的"进化方向"（反馈、改进要求、新增能力），你要：
1. 保留 skill 的核心能力
2. 根据用户反馈做最小必要的修改
3. 写出改进后的完整 SKILL.md（保持 YAML frontmatter 格式，name 保持不变）
4. 简短说明你做了哪些改动

输出格式：先简短列出改动点（Markdown），然后用 markdown 代码围栏（三个反引号）写出完整的 SKILL.md。`

	userPrompt := fmt.Sprintf("Skill: %s\n\n当前 SKILL.md:\n%s\n\n进化方向 / 改进要求:\n%s",
		name, content, req.Prompt)

	result, err := AgentRun(c.Request.Context(), systemPrompt, userPrompt, 8)
	if err != nil {
		return c.JSON(500, map[string]string{"error": err.Error()})
	}

	evolved := extractCodeBlock(result, "markdown")
	if evolved == "" {
		evolved = extractCodeBlock(result, "")
	}
	if evolved == "" {
		evolved = result
	}

	if err := WriteFile(name, "SKILL.md", evolved); err != nil {
		return c.JSON(500, map[string]string{"error": "agent output ok but write failed: " + err.Error()})
	}

	SafeBash("bash /skills/sync.sh")

	return c.JSON(200, agentResponse{
		Operation:  "evolve",
		Result:     result,
		Affected:   []string{name},
		TargetPath: fmt.Sprintf("%s/%s/SKILL.md", skillsDir(), name),
	})
}

func optionalPrompt(p string) string {
	if p == "" {
		return ""
	}
	return "用户补充要求: " + p + "\n"
}

// extractCodeBlock 从 markdown 文本抠 ```lang ... ``` 围栏
func extractCodeBlock(s, lang string) string {
	marker := "```" + lang
	start := strings.Index(s, marker)
	if start < 0 {
		// 试试任意语言
		if lang == "" {
			start = strings.Index(s, "```")
		}
		if start < 0 {
			return ""
		}
	}
	// 跳过 ```xxx\n
	rest := s[start:]
	nl := strings.Index(rest, "\n")
	if nl < 0 {
		return ""
	}
	rest = rest[nl+1:]
	end := strings.Index(rest, "```")
	if end < 0 {
		return ""
	}
	return rest[:end]
}
