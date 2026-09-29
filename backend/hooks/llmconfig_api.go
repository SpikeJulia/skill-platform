package hooks

import (
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// llmConfigView 对外返回的配置视图。
//
// ⚠️ 关键安全约定：**这里永远没有 api_key 字段**。
// 想要确认「存的 key 对不对」，看 has_api_key 和 api_key_masked 就行。
// 明文只在服务端内部和 POST 时使用。
type llmConfigView struct {
	Provider        string            `json:"provider"`
	APIFormat       string            `json:"api_format"`
	BaseURL         string            `json:"base_url"`
	HasAPIKey       bool              `json:"has_api_key"`
	APIKeyMasked    string            `json:"api_key_masked"`
	Headers         map[string]string `json:"headers"`
	Model           string            `json:"model"`
	ContextWindow   int               `json:"context_window"`
	MaxOutputTokens int               `json:"max_output_tokens"`
	ReasoningEffort string            `json:"reasoning_effort"`
	GlobalPrompt    string            `json:"global_prompt"`
	// DefaultGlobalPrompt 供界面「还原默认」用
	DefaultGlobalPrompt string `json:"default_global_prompt"`
	// SupportedFormats 供界面渲染 API 格式下拉
	SupportedFormats []string `json:"supported_formats"`
	// KeyFromEnv 表示当前生效的 key 来自环境变量而不是配置（界面提示用）
	KeyFromEnv bool `json:"key_from_env"`
}

func toView(cfg LLMConfig) llmConfigView {
	return llmConfigView{
		Provider:            cfg.Provider,
		APIFormat:           cfg.APIFormat,
		BaseURL:             cfg.BaseURL,
		HasAPIKey:           cfg.APIKey != "",
		APIKeyMasked:        maskSecret(cfg.APIKey),
		Headers:             cfg.Headers,
		Model:               cfg.Model,
		ContextWindow:       cfg.ContextWindow,
		MaxOutputTokens:     cfg.MaxOutputTokens,
		ReasoningEffort:     cfg.ReasoningEffort,
		GlobalPrompt:        cfg.GlobalPrompt,
		DefaultGlobalPrompt: defaultGlobalPrompt,
		SupportedFormats:    []string{FormatAnthropic, FormatResponses},
		KeyFromEnv:          cfg.APIKey == "" && fallbackAPIKey() != "",
	}
}

// getConfig GET /api/config - 读当前 agent 配置
func getConfig(c *core.RequestEvent) error {
	cfg, err := loadLLMConfig(c.App)
	if err != nil {
		return c.JSON(500, map[string]string{"error": err.Error()})
	}
	return c.JSON(200, toView(cfg))
}

// putConfig PUT /api/config - 改配置
//
// api_key 的语义是 write-only：
//   - 字段缺省 或 空字符串 → 保持原值不变（界面回显时不会把 key 传回来，
//     否则每次改个模型名都得把 key 重新输一遍）
//   - 显式传 "__clear__"   → 清除
//
// 这样明文密钥永远不会因为一次普通保存而丢失。
func putConfig(c *core.RequestEvent) error {
	var body struct {
		Provider        *string            `json:"provider"`
		APIFormat       *string            `json:"api_format"`
		BaseURL         *string            `json:"base_url"`
		APIKey          *string            `json:"api_key"`
		Headers         *map[string]string `json:"headers"`
		Model           *string            `json:"model"`
		ContextWindow   *int               `json:"context_window"`
		MaxOutputTokens *int               `json:"max_output_tokens"`
		ReasoningEffort *string            `json:"reasoning_effort"`
		GlobalPrompt    *string            `json:"global_prompt"`
	}
	if err := c.BindBody(&body); err != nil {
		return c.JSON(400, map[string]string{"error": "invalid body: " + err.Error()})
	}

	cfg, err := loadLLMConfig(c.App)
	if err != nil {
		return c.JSON(500, map[string]string{"error": err.Error()})
	}

	if body.Provider != nil {
		cfg.Provider = *body.Provider
	}
	if body.APIFormat != nil {
		cfg.APIFormat = strings.TrimSpace(*body.APIFormat)
	}
	if body.BaseURL != nil {
		cfg.BaseURL = *body.BaseURL
	}
	if body.Model != nil {
		cfg.Model = *body.Model
	}
	if body.ContextWindow != nil {
		cfg.ContextWindow = *body.ContextWindow
	}
	if body.MaxOutputTokens != nil {
		cfg.MaxOutputTokens = *body.MaxOutputTokens
	}
	if body.ReasoningEffort != nil {
		cfg.ReasoningEffort = *body.ReasoningEffort
	}
	if body.GlobalPrompt != nil {
		cfg.GlobalPrompt = *body.GlobalPrompt
	}
	if body.Headers != nil {
		cfg.Headers = *body.Headers
	}
	// API key：空 = 不动；__clear__ = 清除
	if body.APIKey != nil {
		switch k := strings.TrimSpace(*body.APIKey); k {
		case "":
			// 不动
		case "__clear__":
			cfg.APIKey = ""
		default:
			cfg.APIKey = k
		}
	}

	if err := cfg.Validate(); err != nil {
		return c.JSON(400, map[string]string{"error": err.Error()})
	}
	if err := saveLLMConfig(c.App, cfg); err != nil {
		return c.JSON(500, map[string]string{"error": err.Error()})
	}

	// 存完就试一次真实调用。配置看着对但 key 错/地址错是最常见的坑，
	// 让用户在保存这一步就知道，而不是等到跑组合才发现。
	testResult := ""
	if cfg.APIKey == "" && fallbackAPIKey() == "" {
		testResult = "未配置 API Key，本次未做连通性测试"
	} else {
		if _, err := AgentRun(c.Request.Context(), "回复 OK 两个字即可。", "健康检查", 1); err != nil {
			testResult = "已保存，但连通性测试失败: " + err.Error()
		} else {
			testResult = "已保存，模型调用正常"
		}
	}

	return c.JSON(200, map[string]any{
		"config":   toView(cfg),
		"test":     testResult,
		"saved_at": time.Now().Format(time.RFC3339),
	})
}

// resetConfig POST /api/config/reset - 还原出厂配置
//
// 只还原除 API Key 之外的一切。key 是用户自己填的凭据，
// 「还原默认」不该把他的 key 抹掉——那是丢数据，不是还原。
func resetConfig(c *core.RequestEvent) error {
	cfg := LLMConfigDefaults()
	if current, err := loadLLMConfig(c.App); err == nil {
		cfg.APIKey = current.APIKey
	}
	if err := saveLLMConfig(c.App, cfg); err != nil {
		return c.JSON(500, map[string]string{"error": err.Error()})
	}
	return c.JSON(200, map[string]any{"config": toView(cfg)})
}

// listAPIFormats GET /api/config/formats - 支持的协议及各自的默认端点提示
func listAPIFormats(c *core.RequestEvent) error {
	return c.JSON(200, map[string]any{
		"formats": []map[string]string{
			{
				"id":           FormatAnthropic,
				"label":        "Anthropic Messages",
				"endpoint":     "{base}/v1/messages",
				"note":         "MiniMax / DeepSeek / Kimi 等都有兼容端点；支持 thinking 块",
				"auth_default": "x-api-key（可用自定义 header 覆盖成 Authorization: Bearer）",
			},
			{
				"id":           FormatResponses,
				"label":        "OpenAI Responses",
				"endpoint":     "{base}/responses",
				"note":         "OpenAI 官方新接口；部分网关也提供",
				"auth_default": "Authorization: Bearer",
			},
		},
	})
}
