package hooks

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/packages/param"
)

// anthropicProvider 走 Anthropic Messages API：POST {base}/v1/messages
//
// 选它当默认协议是有原因的：MiniMax / DeepSeek / Kimi 等国内供应商都提供
// Anthropic 兼容端点，MiniMax 官方还把它列为推荐路径（支持 thinking 块等进阶特性）。
type anthropicProvider struct{}

// anthropicVersion 缺省请求头。Anthropic 要求带上；用户自定义 header 可覆盖它。
const anthropicVersion = "2023-06-01"

func (anthropicProvider) client(cfg LLMConfig) anthropic.Client {
	opts := []option.RequestOption{
		option.WithAPIKey(cfg.APIKey),
		option.WithBaseURL(cfg.BaseURL),
		option.WithHeaderAdd("anthropic-version", anthropicVersion),
	}
	// 自定义 header 最后加，所以能覆盖默认的。
	// 这不是可有可无的字段：MiniMax 的 Anthropic 端点同时接受
	// x-api-key 和 Authorization: Bearer，两者行为不同，得按端点要求选。
	for k, v := range cfg.Headers {
		opts = append(opts, option.WithHeaderAdd(k, v))
	}
	return anthropic.NewClient(opts...)
}

// toolsToAnthropic 把内部工具定义转成 Anthropic 的 ToolParam
func toolsToAnthropic() []anthropic.ToolUnionParam {
	out := make([]anthropic.ToolUnionParam, 0, len(tools))
	for _, t := range tools {
		// ToolInputSchemaParam 只有 properties/required/type 三个具名字段，
		// 我们内部定义的是完整 JSON Schema（可能带 additionalProperties 等），
		// 所以整体塞进 ExtraFields 原样透传，不做字段级映射（映射会丢字段）。
		schema := anthropic.ToolInputSchemaParam{
			Properties:  t.Parameters["properties"],
			Required:    toStringSlice(t.Parameters["required"]),
			ExtraFields: t.Parameters,
		}
		out = append(out, anthropic.ToolUnionParam{
			OfTool: &anthropic.ToolParam{
				Name:        t.Name,
				Description: param.NewOpt(t.Description),
				InputSchema: schema,
			},
		})
	}
	return out
}

// toStringSlice 把 any 转成 []string，失败返回 nil
func toStringSlice(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, e := range arr {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func (anthropicProvider) run(ctx context.Context, cfg LLMConfig, systemPrompt, userPrompt string, maxTurns int) (string, error) {
	client := anthropicProvider{}.client(cfg)

	messages := []anthropic.MessageParam{{
		Role:    anthropic.MessageParamRoleUser,
		Content: []anthropic.ContentBlockParamUnion{anthropic.NewTextBlock(userPrompt)},
	}}
	tools := toolsToAnthropic()

	// Anthropic 强制要求 max_tokens，不给直接 400
	maxTokens := int64(cfg.effectiveMaxTokens())
	if maxTokens <= 0 {
		maxTokens = anthropicMaxTokensRequired
	}

	for turn := 0; turn < maxTurns; turn++ {
		msg, err := client.Messages.New(ctx, anthropic.MessageNewParams{
			Model:     cfg.Model,
			MaxTokens: maxTokens,
			System:    []anthropic.TextBlockParam{{Text: systemPrompt}},
			Messages:  messages,
			Tools:     tools,
		})
		if err != nil {
			return "", fmt.Errorf("Anthropic 调用失败: %w", err)
		}

		var textParts []string
		assistantBlocks := make([]anthropic.ContentBlockParamUnion, 0, len(msg.Content))
		type pendingCall struct {
			id, name string
			input    any
		}
		var calls []pendingCall

		for _, blk := range msg.Content {
			switch blk.Type {
			case "text":
				if blk.Text != "" {
					textParts = append(textParts, blk.Text)
					assistantBlocks = append(assistantBlocks, anthropic.NewTextBlock(blk.Text))
				}
			case "tool_use":
				// Input 是 any；存下来原样回传，不做 map 往返（避免丢字段）
				assistantBlocks = append(assistantBlocks,
					anthropic.NewToolUseBlock(blk.ID, blk.Input, blk.Name))
				calls = append(calls, pendingCall{id: blk.ID, name: blk.Name, input: blk.Input})
			default:
				// thinking / redacted_thinking：回传需要签名，SDK 不支持用户侧构造，跳过
			}
		}

		// 没有工具调用 → 结束
		if len(calls) == 0 {
			return strings.Join(textParts, "\n"), nil
		}

		// assistant 的完整 content 列表要原样回填，否则多轮工具调用会断推理链
		// （MiniMax 文档明确要求包含 thinking/text/tool_use 全部块）
		if len(assistantBlocks) > 0 {
			messages = append(messages, anthropic.MessageParam{
				Role:    anthropic.MessageParamRoleAssistant,
				Content: assistantBlocks,
			})
		}

		// 工具结果必须放在 user 角色消息里 —— Anthropic 不接受 assistant 发 tool_result
		resultBlocks := make([]anthropic.ContentBlockParamUnion, 0, len(calls))
		for _, call := range calls {
			raw, err := json.Marshal(call.input)
			if err != nil {
				raw = []byte("{}")
			}
			result := executeTool(call.name, string(raw))
			resultBlocks = append(resultBlocks,
				anthropic.NewToolResultBlock(call.id, result, strings.HasPrefix(result, "ERROR:")))
		}
		messages = append(messages, anthropic.MessageParam{
			Role:    anthropic.MessageParamRoleUser,
			Content: resultBlocks,
		})
	}
	return "", fmt.Errorf("超过最大轮数 (%d)", maxTurns)
}
