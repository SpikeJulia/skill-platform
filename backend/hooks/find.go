package hooks

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// findRequest 搜索请求
type findRequest struct {
	Query string `json:"query"`
	TopK  int    `json:"topK,omitempty"`
}

// findResponse 搜索响应
type findResponse struct {
	Query   string     `json:"query"`
	Matches []SkillMatch `json:"matches"`
	AgentOutput string `json:"agent_output,omitempty"`
}

type SkillMatch struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// findSkills POST /api/find - 语义搜
// 1. 列所有 skill 描述
// 2. 让 LLM 排序选 top 3
func findSkills(c *core.RequestEvent) error {
	var req findRequest
	if err := c.BindBody(&req); err != nil {
		return c.JSON(400, map[string]string{"error": err.Error()})
	}
	if req.Query == "" {
		return c.JSON(400, map[string]string{"error": "query required"})
	}
	if req.TopK <= 0 {
		req.TopK = 3
	}

	skills, err := ListSkills()
	if err != nil {
		return c.JSON(500, map[string]string{"error": err.Error()})
	}
	if len(skills) == 0 {
		return c.JSON(200, findResponse{Query: req.Query, Matches: []SkillMatch{}})
	}

	// 构造候选列表（name + description）
	var sb strings.Builder
	for _, s := range skills {
		sb.WriteString(fmt.Sprintf("- name: %s\n  description: %s\n", s.Name, s.Description))
	}
	candidates := sb.String()

	systemPrompt := `你是一个 skill 语义搜索引擎。用户描述需求，你从给定的 skill 列表中按相关度排序，返回 top K 个最匹配的。返回 JSON 格式：{"matches": [{"name": "...", "reason": "为什么匹配（一句话中文）"}]}

严格要求：
- 只返回 JSON，不要任何其他文字
- matches 按相关度降序
- reason 用中文，简洁
- 如果都不相关，返回 {"matches": []}`

	userPrompt := fmt.Sprintf(`用户需求: "%s"

候选 skill 列表:
%s

请返回 top %d 个最匹配的，按 JSON 格式输出。`, req.Query, candidates, req.TopK)

	rawOutput, err := AgentRun(c.Request.Context(), systemPrompt, userPrompt, 3)
	if err != nil {
		return c.JSON(500, map[string]string{"error": err.Error()})
	}

	// 解析 LLM 返回的 JSON
	var parsed struct {
		Matches []SkillMatch `json:"matches"`
	}
	if err := extractJSON(rawOutput, &parsed); err != nil {
		// 兜底：返回原始输出
		return c.JSON(200, findResponse{
			Query:       req.Query,
			Matches:     []SkillMatch{},
			AgentOutput: rawOutput,
		})
	}

	return c.JSON(200, findResponse{
		Query:   req.Query,
		Matches: parsed.Matches,
	})
}

// extractJSON 从 LLM 输出里抠出 JSON
// LLM 输出通常形如：<think>...</think>\n\n```json\n{...}\n```\n或直接是 {..."matches":...}
// 策略：找第一个 { 配对到匹配的 }（按括号深度）
func extractJSON(s string, v any) error {
	s = strings.TrimSpace(s)
	// 找 markdown 围栏内的 JSON 优先
	if idx := strings.Index(s, "```"); idx >= 0 {
		rest := s[idx+3:]
		// 跳过语言标识
		if nl := strings.Index(rest, "\n"); nl >= 0 {
			rest = rest[nl+1:]
		} else {
			rest = ""
		}
		if end := strings.Index(rest, "```"); end >= 0 {
			rest = rest[:end]
			rest = strings.TrimSpace(rest)
			if strings.HasPrefix(rest, "{") {
				return json.Unmarshal([]byte(rest), v)
			}
		}
	}
	// 退化：找第一个 { 配对到匹配的 }
	start := strings.Index(s, "{")
	if start < 0 {
		return fmt.Errorf("no JSON found")
	}
	depth := 0
	inStr := false
	escape := false
	for i := start; i < len(s); i++ {
		c := s[i]
		if escape {
			escape = false
			continue
		}
		if c == '\\' && inStr {
			escape = true
			continue
		}
		if c == '"' {
			inStr = !inStr
			continue
		}
		if inStr {
			continue
		}
		if c == '{' {
			depth++
		} else if c == '}' {
			depth--
			if depth == 0 {
				return json.Unmarshal([]byte(s[start:i+1]), v)
			}
		}
	}
	return fmt.Errorf("no matching JSON braces")
}
