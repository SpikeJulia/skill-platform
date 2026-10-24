package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
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

// llmHTTPClient 是 OpenAI Responses 用的 HTTP 客户端。
// 显式给超时——http.DefaultClient 没有超时，服务端不响应时 goroutine 会永远挂着。
var llmHTTPClient = &http.Client{Timeout: llmRequestTimeout}

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
	Usage  *responsesUsage `json:"usage"`
	// IncompleteDetails 说明为什么没写完（max_output_tokens 等）。
	// 没有这个字段时只能报一句"未完成"，用户不知道该调哪个旋钮。
	IncompleteDetails *struct {
		Reason string `json:"reason"`
	} `json:"incomplete_details"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// responsesUsage 是这次调用的 token 用量。
type responsesUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
	// OutputTokensDetails.ReasoningTokens 是"推理"占掉的额度。
	// ⚠️ 实测 MiniMax 的推理 token 和正文**共用**同一份 max_output_tokens 预算：
	// 给 300 的一次请求里 reasoning_tokens=299、正文 0 字。
	// 所以额度不够时正文是"静默"消失的，必须把它单独报出来。
	OutputTokensDetails *struct {
		ReasoningTokens int `json:"reasoning_tokens"`
	} `json:"output_tokens_details"`
}

func toolsToResponses(tdefs []ToolDefinition) []responsesTool {
	out := make([]responsesTool, 0, len(tdefs))
	for _, t := range tdefs {
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

	resp, err := llmHTTPClient.Do(req)
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

func (p responsesProvider) run(ctx context.Context, cfg LLMConfig, systemPrompt, userPrompt string, maxTurns int, tdefs []ToolDefinition) (string, error) {
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
		Tools:           toolsToResponses(tdefs),
		MaxOutputTokens: cfg.effectiveMaxTokens(),
	}
	if cfg.ReasoningEffort != "" {
		body.Reasoning = &responsesReasoning{Effort: cfg.ReasoningEffort}
	}

	trail := newAgentTrail()
	// lastText 记住模型最近一次说过的正文。轮数用尽时它就是"已经做出来的成果"，
	// 直接丢掉等于白跑——之前就是这么把已经写好的内容变成一句报错。
	lastText := ""

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
			reason := "原因未给出"
			if resp.IncompleteDetails != nil && resp.IncompleteDetails.Reason != "" {
				reason = resp.IncompleteDetails.Reason
			}
			outTok, thinkTok := 0, 0
			if resp.Usage != nil {
				outTok = resp.Usage.OutputTokens
				if resp.Usage.OutputTokensDetails != nil {
					thinkTok = resp.Usage.OutputTokensDetails.ReasoningTokens
				}
			}
			log.Printf("[agent] 响应 incomplete（reason=%s）output_tokens=%d 其中推理=%d 上限=%d",
				reason, outTok, thinkTok, body.MaxOutputTokens)
			return "", fmt.Errorf(
				"输出被截断（%s）：用完了 max_output_tokens=%d 的额度，其中 %d 个被推理占掉，只剩 %d 个留给正文。"+
					"这次没有写盘，原文件未改动。请把「最大输出 Token」调大后重试。",
				reason, body.MaxOutputTokens, thinkTok, outTok-thinkTok)
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
		if len(textParts) > 0 {
			lastText = strings.Join(textParts, "\n")
		}

		// 没有工具调用 → 结束
		if len(calls) == 0 {
			return lastText, nil
		}

		// 把模型这一轮的输出整体追加进 input
		input = append(input, echo...)

		// 工具结果
		for _, call := range calls {
			n := trail.record(call.Name, call.Arguments)
			result := executeTool(call.Name, call.Arguments)
			log.Printf("[agent] turn=%d 调用 %s (第 %d 次) → %s",
				turn+1, call.Name, n, truncate(strings.ReplaceAll(result, "\n", " "), 120))
			// 同一次调用重复出现：告诉模型别再重复了，给它一条出路
			if n >= 2 {
				result = trail.repeatHint(call.Name, call.Arguments, n) + "\n\n（上次的结果）\n" + result
			}
			input = append(input, responsesItem{
				Type:    "function_call_output",
				CallID:  call.CallID,
				Output:  result,
			})
		}
	}

	// 轮数用尽。如果模型已经说过话，就把它说的返回去（附带提示），
	// 而不是把整轮工作丢掉只报一句"超过最大轮数"。
	if lastText != "" {
		log.Printf("[agent] 轮数用尽（%d），但模型已产出 %d 字，返回已产出内容。轨迹: %s",
			maxTurns, len(lastText), trail.summary())
		return lastText, nil
	}
	log.Printf("[agent] 轮数用尽（%d）且无任何输出。轨迹: %s", maxTurns, trail.summary())
	return "", fmt.Errorf("超过最大轮数 (%d)：模型一直在调工具没给出结果。工具调用轨迹: %s",
		maxTurns, trail.summary())
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
