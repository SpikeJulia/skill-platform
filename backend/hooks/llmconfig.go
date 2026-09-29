package hooks

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// API 格式。目前只支持两种（用户明确不要 OpenAI Chat Completions）。
const (
	FormatResponses = "openai_responses" // OpenAI Responses API：POST {base}/responses
	FormatAnthropic = "anthropic"         // Anthropic Messages：POST {base}/v1/messages
)

// llmConfigID 固定单条记录的 id——用户选了单模型方案
const llmConfigID = "llmconfig000001"

// llmConfigColName collection 名
const llmConfigColName = "llm_config"

// reasoningEffortPattern 只保证形状安全（单词、无空格、无控制字符），不限定具体取值。
var reasoningEffortPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)

// 内置默认提示词。这段内容会加在六个操作（组合 / 编排 / 合并 / 进化 / 语义搜索 / URL 安装）
// 各自的提示词之前，所以只写跨操作都成立的规则，不写某个操作专属的要求。
//
// 存的值是空的时候回落到它——即"出厂就带默认提示词"，
// 界面上的输入框也因此永远显示真正生效的内容，不会出现"框里有字但不生效"。
const defaultGlobalPrompt = `你在维护一个 skill 中央库。下列规则对所有操作都适用：

- 全程用简体中文。
- 只输出结果本身，不要写"好的""我来帮你"这类开场白和结束语。
- 不确定的地方写 TODO 注释，不要编造目录名、文件名、字段名或命令。
- 提到文件路径用占位符（如 <你的目录>），不要写死任何人的绝对路径。`

// LLMConfig 是一次 LLM 调用的完整配置
type LLMConfig struct {
	Provider        string            `json:"provider"`
	APIFormat       string            `json:"api_format"`
	BaseURL         string            `json:"base_url"`
	APIKey          string            `json:"-"` // ⚠️ 永不出现在任何响应里
	Headers         map[string]string `json:"headers"`
	Model           string            `json:"model"`
	ContextWindow   int               `json:"context_window"`
	MaxOutputTokens int               `json:"max_output_tokens"`
	ReasoningEffort string            `json:"reasoning_effort"`
	GlobalPrompt    string            `json:"global_prompt"`
}

// Defaults 返回出厂配置。Anthropic 协议是默认——
// MiniMax / DeepSeek / Kimi 等都有 Anthropic 兼容端点，且官方推荐（支持 thinking 块）。
func LLMConfigDefaults() LLMConfig {
	return LLMConfig{
		Provider:        "MiniMax",
		APIFormat:       FormatAnthropic,
		BaseURL:         "https://api.minimaxi.com/anthropic",
		Headers:         map[string]string{},
		Model:           "MiniMax-M3",
		ContextWindow:   1000000,
		MaxOutputTokens: 8192,
		ReasoningEffort: "",
		GlobalPrompt:    defaultGlobalPrompt,
	}
}

// anthropicMaxTokensRequired Anthropic Messages API 强制要求 max_tokens，
// 官方上限对多数模型是 64000，这里给一个不会撞上限的保守默认。
const anthropicMaxTokensRequired = 8192

// effectiveMaxTokens 返回该传给供应商的上限。
// Anthropic 不给会被拒，所以没配时兜一个默认值；OpenAI Responses 不传也行。
func (c LLMConfig) effectiveMaxTokens() int {
	if c.MaxOutputTokens > 0 {
		return c.MaxOutputTokens
	}
	if c.APIFormat == FormatAnthropic {
		return anthropicMaxTokensRequired
	}
	return 0
}

// Validate 校验并归一化。改过的字段就地修正，出错返回原因。
func (c *LLMConfig) Validate() error {
	c.Provider = strings.TrimSpace(c.Provider)
	c.Model = strings.TrimSpace(c.Model)
	c.BaseURL = strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	c.ReasoningEffort = strings.TrimSpace(c.ReasoningEffort)

	if c.APIFormat != FormatResponses && c.APIFormat != FormatAnthropic {
		return fmt.Errorf("api_format 必须是 %s 或 %s，收到 %q", FormatResponses, FormatAnthropic, c.APIFormat)
	}
	if c.BaseURL == "" {
		return fmt.Errorf("接口地址不能为空")
	}
	if !strings.HasPrefix(c.BaseURL, "http://") && !strings.HasPrefix(c.BaseURL, "https://") {
		return fmt.Errorf("接口地址必须以 http:// 或 https:// 开头，收到 %q", c.BaseURL)
	}
	if c.Model == "" {
		return fmt.Errorf("模型名称不能为空")
	}
	if c.ContextWindow < 0 {
		return fmt.Errorf("上下文窗口不能为负")
	}
	if c.MaxOutputTokens < 0 {
		return fmt.Errorf("最大输出 Token 不能为负")
	}
	// 推理等级：不同供应商取值不一样（OpenAI 是 low/medium/high，别的可能是 minimal、xhigh…），
	// 所以界面上是自由输入框，这里只挡会破坏请求的形状（空格、换行、控制字符），不限定取值。
	if c.ReasoningEffort != "" && !reasoningEffortPattern.MatchString(c.ReasoningEffort) {
		return fmt.Errorf("推理等级只能是一个不含空格的短词（常见取值 low / medium / high），收到 %q", c.ReasoningEffort)
	}
	if c.Headers == nil {
		c.Headers = map[string]string{}
	}
	return nil
}

// applyGlobalPrompt 把全局提示词拼到各操作自带提示词之前。
// 全局为空时原样返回——不能因为"拼接"而改变原有行为。
func applyGlobalPrompt(global, opPrompt string) string {
	g := strings.TrimSpace(global)
	if g == "" {
		return opPrompt
	}
	if strings.TrimSpace(opPrompt) == "" {
		return g
	}
	return g + "\n\n---\n\n" + opPrompt
}

// ---- 持久化 ----

// loadLLMConfig 读配置。记录不存在（首次迁移前的老库）时返回出厂默认值。
func loadLLMConfig(app core.App) (LLMConfig, error) {
	rec, err := app.FindRecordById(llmConfigColName, llmConfigID, nil)
	if err != nil {
		// 记录缺失不该拖垮整个平台，用默认值
		return LLMConfigDefaults(), nil
	}
	cfg := LLMConfigDefaults()
	cfg.Provider = rec.GetString("provider")
	cfg.APIFormat = rec.GetString("api_format")
	cfg.BaseURL = rec.GetString("base_url")
	cfg.APIKey = rec.GetString("api_key")
	cfg.Model = rec.GetString("model")
	cfg.ContextWindow = rec.GetInt("context_window")
	cfg.MaxOutputTokens = rec.GetInt("max_output_tokens")
	cfg.ReasoningEffort = rec.GetString("reasoning_effort")
	// 空 = 用户没改过 = 用出厂默认。这样界面显示的、实际发出去的、存的默认值三者一致。
	cfg.GlobalPrompt = rec.GetString("global_prompt")
	if strings.TrimSpace(cfg.GlobalPrompt) == "" {
		cfg.GlobalPrompt = defaultGlobalPrompt
	}
	if raw := strings.TrimSpace(rec.GetString("headers")); raw != "" {
		// headers 存坏不该让模型调用挂掉，退回空表
		_ = json.Unmarshal([]byte(raw), &cfg.Headers)
	}
	if cfg.Headers == nil {
		cfg.Headers = map[string]string{}
	}
	return cfg, nil
}

// saveLLMConfig 写配置。记录不存在则建。
func saveLLMConfig(app core.App, cfg LLMConfig) error {
	headers, err := json.Marshal(cfg.Headers)
	if err != nil {
		return fmt.Errorf("headers 不是合法 JSON 对象: %w", err)
	}

	rec, err := app.FindRecordById(llmConfigColName, llmConfigID, nil)
	if err != nil {
		col, cerr := app.FindCollectionByNameOrId(llmConfigColName)
		if cerr != nil {
			return fmt.Errorf("llm_config collection missing: %w", cerr)
		}
		rec = core.NewRecord(col)
		rec.Id = llmConfigID
	}
	rec.Set("provider", cfg.Provider)
	rec.Set("api_format", cfg.APIFormat)
	rec.Set("base_url", cfg.BaseURL)
	rec.Set("api_key", cfg.APIKey)
	rec.Set("headers", string(headers))
	rec.Set("model", cfg.Model)
	rec.Set("context_window", cfg.ContextWindow)
	rec.Set("max_output_tokens", cfg.MaxOutputTokens)
	rec.Set("reasoning_effort", cfg.ReasoningEffort)
	rec.Set("global_prompt", cfg.GlobalPrompt)
	return app.Save(rec)
}

// maskSecret 把密钥打码，只留头尾少量字符。
// GET 接口用它回显，好让人确认"存的是不是我以为的那个 key"而不泄漏原文。
func maskSecret(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if len(s) <= 8 {
		return strings.Repeat("*", len(s))
	}
	return s[:4] + "…" + s[len(s)-4:]
}
