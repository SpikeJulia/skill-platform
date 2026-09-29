package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// responsesProvider 走 OpenAI Responses API：POST {base}/responses
//
// 手写 net/http 而不是引 openai-go：这个 SDK 的 ResponseNewParams 类型极重
// （几十个 union 变体），而我们只需要其中 4 个字段，自己 marshal 更可控也少一个依赖。
// 字段形状对着 openai-go v1.12.0 responses 包的类型定义核过：
//   请求 tools: {"type":"function","name":...,"description":...,"parameters":{...}}
//   响应 output item type=function_call: {call_id, name, arguments}
//   工具结果输入项: {"type":"function_call_output","call_id":...,"output":...}
type responsesProvider struct{}

// responsesRequest 请求体。只用我们需要的字段。
type responsesRequest struct {
	Model           string             `json:"model"`
	Instructions    string             `json:"instructions,omitempty"`
	Input           []responsesItem    `json:"input"`
	Tools           []responsesTool    `json:"tools,omitempty"`
	MaxOutputTokens int                `json:"max_output_tokens,omitempty"`
	Reasoning       *responsesReasoning `json:"reasoning,omitempty"`
}

type responsesReasoning struct {
	Effort string `json:"effort"`
}

type responsesTool struct {
	Type        string         `json:"type"` // 固定 "function"
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters"`
}

// responsesItem 既用于请求 input，也用于解析响应 output —— 两边字段是同一套。
type responsesItem struct {
	Type    string             `json:"type"`           // message / function_call / function_call_output / reasoning
	ID      string             `json:"id,omitempty"`   // 响应回传时要带
	Role    string             `json:"role,omitempty"` // message 时用
	Status  string             `json:"status,omitempty"`
	Content []responsesContent `json:"content,omitempty"`

	// function_call
	CallID    string `json:"call_id,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`

	// function_call_output
	Output string `json:"output,omitempty"`
}

type responsesContent struct {
	Type string `json:"type"` // output_text
	Text string `json:"text"`
}

type responsesResponse struct {
	ID     string          `json:"id"`
	Status string          `json:"status"`
	Output []responsesItem `json:"output"`
	Error  *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

func toolsToResponses() []responsesTool {
	out := make([]responsesTool, 0, len(tools))
	for _, t := range tools {
		out = append(out, responsesTool{
			Type:        "function",
			Name:        t.Name,
			Description: t.Description,
			Parameters:  t.Parameters,
		})
	}
	return out
}

func (responsesProvider) post(ctx context.Context, cfg LLMConfig, body responsesRequest) (*responsesResponse, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	// base 已经去掉尾斜杠；Responses 端点就是 {base}/responses
	endpoint := strings.TrimRight(cfg.BaseURL, "/") + "/responses"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	// 自定义 header 最后设，能覆盖 Authorization（比如某些网关要别的 scheme）
	for k, v := range cfg.Headers {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// 把错误体带上，否则排查时只有一句 400，什么都看不出来
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(data), 600))
	}

	var out responsesResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w（原文前 300 字: %s）", err, truncate(string(data), 300))
	}
	return &out, nil
}

func (p responsesProvider) run(ctx context.Context, cfg LLMConfig, systemPrompt, userPrompt string, maxTurns int) (string, error) {
	input := []responsesItem{{
		Type: "message",
		Role: "user",
		Content: []responsesContent{
			{Type: "input_text", Text: userPrompt},
		},
	}}

	body := responsesRequest{
		Model:           cfg.Model,
		Instructions:    systemPrompt,
		Tools:           toolsToResponses(),
		MaxOutputTokens: cfg.effectiveMaxTokens(),
	}
	if cfg.ReasoningEffort != "" {
		body.Reasoning = &responsesReasoning{Effort: cfg.ReasoningEffort}
	}

	for turn := 0; turn < maxTurns; turn++ {
		body.Input = input
		resp, err := p.post(ctx, cfg, body)
		if err != nil {
			return "", fmt.Errorf("OpenAI Responses 调用失败: %w", err)
		}
		if resp.Error != nil && resp.Error.Message != "" {
			return "", fmt.Errorf("供应商返回错误: %s", resp.Error.Message)
		}
		if resp.Status == "incomplete" {
			return "", fmt.Errorf("响应未完成（可能撞到 max_output_tokens=%d 上限）", body.MaxOutputTokens)
		}

		// 收集本次输出里的文本和工具调用
		var textParts []string
		var calls []responsesItem
		var echo []responsesItem
		for _, item := range resp.Output {
			switch item.Type {
			case "reasoning":
				// 必须原样回传，否则多轮推理链断掉
				echo = append(echo, item)
			case "message":
				for _, c := range item.Content {
					if c.Type == "output_text" && c.Text != "" {
						textParts = append(textParts, c.Text)
					}
				}
				echo = append(echo, item)
			case "function_call":
				calls = append(calls, item)
				echo = append(echo, item)
			}
		}

		// 没有工具调用 → 结束
		if len(calls) == 0 {
			return strings.Join(textParts, "\n"), nil
		}

		// 把模型这一轮的输出整体追加进 input
		input = append(input, echo...)

		// 工具结果
		for _, call := range calls {
			result := executeTool(call.Name, call.Arguments)
			input = append(input, responsesItem{
				Type:    "function_call_output",
				CallID:  call.CallID,
				Output:  result,
			})
		}
	}
	return "", fmt.Errorf("超过最大轮数 (%d)", maxTurns)
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
