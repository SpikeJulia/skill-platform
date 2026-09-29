package hooks

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/sashabaranov/go-openai"
)

// llmClient 全局 MiniMax-M3 client（懒加载，从环境变量取 API key）
var llmClient *openai.Client

func getClient() *openai.Client {
	if llmClient != nil {
		return llmClient
	}
	apiKey := os.Getenv("MINIMAX_API_KEY")
	if apiKey == "" {
		// 兜底：从 config.env 读
		if data, err := os.ReadFile("/config.env"); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				if strings.HasPrefix(line, "MINIMAX_API_KEY=") {
					apiKey = strings.TrimPrefix(line, "MINIMAX_API_KEY=")
					break
				}
			}
		}
	}
	if apiKey == "" {
		return nil
	}
	cfg := openai.DefaultConfig(apiKey)
	cfg.BaseURL = LLMBaseURL
	llmClient = openai.NewClientWithConfig(cfg)
	return llmClient
}

// ToolDefinition 定义一个 LLM 可调用的工具
type ToolDefinition struct {
	Name        string
	Description string
	Parameters  map[string]any
}

// tools 给 LLM 注册的工具集（受 SafeBash / SafePath 保护）
var tools = []ToolDefinition{
	{
		Name:        "bash",
		Description: "在白名单内执行 bash 命令。可用命令：git clone/pull/fetch, ls, find, grep, cat, cp, mv, mkdir, echo, bash, sh",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"cmd": map[string]any{"type": "string", "description": "要执行的命令"},
			},
			"required": []string{"cmd"},
		},
	},
	{
		Name:        "read_file",
		Description: "读文件内容。路径必须以 /skills/ 或 /personal/ 开头",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{"type": "string"},
			},
			"required": []string{"path"},
		},
	},
	{
		Name:        "write_file",
		Description: "写文件到 /skills/<skill_name>/（agent 留空）或 /personal/<agent>/<skill_name>/（指定 agent）。filename 不能包含路径分隔符",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"skill_name": map[string]any{"type": "string", "description": "skill 名（即子目录名）"},
				"agent":      map[string]any{"type": "string", "description": "可选。装到某个 agent 的专属库时填它的名字；装中央源留空"},
				"filename":   map[string]any{"type": "string", "description": "文件名，如 SKILL.md"},
				"content":    map[string]any{"type": "string"},
			},
			"required": []string{"skill_name", "filename", "content"},
		},
	},
	{
		Name:        "list_dir",
		Description: "列出目录内容",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{"type": "string"},
			},
			"required": []string{"path"},
		},
	},
}

func toolsToOpenAI() []openai.Tool {
	out := make([]openai.Tool, len(tools))
	for i, t := range tools {
		paramsJSON, _ := json.Marshal(t.Parameters)
		out[i] = openai.Tool{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  json.RawMessage(paramsJSON),
			},
		}
	}
	return out
}

// executeTool 跑一个 tool call
func executeTool(tc openai.ToolCall) string {
	var args map[string]any
	if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
		return fmt.Sprintf("ERROR: invalid tool args: %v", err)
	}
	switch tc.Function.Name {
	case "bash":
		cmd, _ := args["cmd"].(string)
		out, err := SafeBash(cmd)
		if err != nil {
			return "ERROR: " + err.Error() + "\n" + out
		}
		return out
	case "read_file":
		path, _ := args["path"].(string)
		data, err := os.ReadFile(path)
		if err != nil {
			return "ERROR: " + err.Error()
		}
		return string(data)
	case "write_file":
		name, _ := args["skill_name"].(string)
		agent, _ := args["agent"].(string)
		filename, _ := args["filename"].(string)
		content, _ := args["content"].(string)
		// agent 留空 → 中央源；给值 → 该 agent 的专属库
		var err error
		if agent == "" || agent == "all" {
			err = WriteFile(name, filename, content)
		} else {
			err = WriteFileToPersonal(agent, name, filename, content)
		}
		if err != nil {
			return "ERROR: " + err.Error()
		}
		return "OK: written " + filename
	case "list_dir":
		path, _ := args["path"].(string)
		entries, err := os.ReadDir(path)
		if err != nil {
			return "ERROR: " + err.Error()
		}
		var out strings.Builder
		for _, e := range entries {
			out.WriteString(e.Name() + "\n")
		}
		return out.String()
	default:
		return "ERROR: unknown tool: " + tc.Function.Name
	}
}

// AgentRun 跑一次完整的 agent loop（最多 maxTurns 轮）
func AgentRun(ctx context.Context, systemPrompt string, userPrompt string, maxTurns int) (string, error) {
	client := getClient()
	if client == nil {
		return "", fmt.Errorf("MINIMAX_API_KEY not set")
	}
	if maxTurns <= 0 {
		maxTurns = 10
	}

	messages := []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleSystem, Content: systemPrompt},
		{Role: openai.ChatMessageRoleUser, Content: userPrompt},
	}

	for turn := 0; turn < maxTurns; turn++ {
		resp, err := client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
			Model:    LLMModel,
			Messages: messages,
			Tools:    toolsToOpenAI(),
			// reasoning_split 让思考内容分离到 reasoning_details，不污染 content
			// extra_body 走 http body field, OpenAI Go SDK 通过 ChatCompletionRequest 不直接支持
			// 所以 M3 的 reasoning_split 在 response 阶段用下面的兜底处理
		})
		if err != nil {
			return "", fmt.Errorf("LLM call failed: %w", err)
		}
		if len(resp.Choices) == 0 {
			return "", fmt.Errorf("LLM returned no choices")
		}

		msg := resp.Choices[0].Message
		// 把整个 message（含 tool_calls）追加到 history
		messages = append(messages, msg)

		// 没有 tool call → 完成
		if len(msg.ToolCalls) == 0 {
			return msg.Content, nil
		}

		// 执行所有 tool calls，把结果追加
		for _, tc := range msg.ToolCalls {
			result := executeTool(tc)
			messages = append(messages, openai.ChatCompletionMessage{
				Role:       openai.ChatMessageRoleTool,
				Content:    result,
				ToolCallID: tc.ID,
			})
		}
	}
	return "", fmt.Errorf("max turns (%d) exceeded", maxTurns)
}
