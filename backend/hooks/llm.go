package hooks

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// provider 是一个 LLM 协议的适配器。三种协议的消息格式、工具调用格式都不同，
// 所以各自实现完整的 agent loop（结构一样，线格式不同）。
type provider interface {
	// run 跑一次完整的多轮工具调用循环，直到模型不再要工具或达到 maxTurns。
	run(ctx context.Context, cfg LLMConfig, systemPrompt, userPrompt string, maxTurns int) (string, error)
}

// resolveProvider 按 api_format 返回对应适配器
func resolveProvider(format string) (provider, error) {
	switch format {
	case FormatAnthropic:
		return anthropicProvider{}, nil
	case FormatResponses:
		return responsesProvider{}, nil
	default:
		return nil, fmt.Errorf("不支持的 api_format: %q（可选 %s / %s）", format, FormatAnthropic, FormatResponses)
	}
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

// executeTool 执行一次工具调用并返回结果文本。
// 三种协议的 tool call 结构不同，统一在这里解析成 {name, arguments(JSON字符串)} 后调用，
// 工具本身和协议解耦。
func executeTool(name string, argumentsJSON string) string {
	var args map[string]any
	if err := json.Unmarshal([]byte(argumentsJSON), &args); err != nil {
		return fmt.Sprintf("ERROR: invalid tool args: %v", err)
	}
	str := func(k string) string {
		s, _ := args[k].(string)
		return s
	}
	switch name {
	case "bash":
		out, err := SafeBash(str("cmd"))
		if err != nil {
			return "ERROR: " + err.Error() + "\n" + out
		}
		return out
	case "read_file":
		data, err := os.ReadFile(str("path"))
		if err != nil {
			return "ERROR: " + err.Error()
		}
		return string(data)
	case "write_file":
		agent := str("agent")
		// agent 留空 → 中央源；给值 → 该 agent 的专属库
		var err error
		if agent == "" || agent == "all" {
			err = WriteFile(str("skill_name"), str("filename"), str("content"))
		} else {
			err = WriteFileToPersonal(agent, str("skill_name"), str("filename"), str("content"))
		}
		if err != nil {
			return "ERROR: " + err.Error()
		}
		return "OK: written " + str("filename")
	case "list_dir":
		entries, err := os.ReadDir(str("path"))
		if err != nil {
			return "ERROR: " + err.Error()
		}
		var out strings.Builder
		for _, e := range entries {
			out.WriteString(e.Name() + "\n")
		}
		return out.String()
	default:
		return "ERROR: unknown tool: " + name
	}
}

// AgentRun 跑一次完整的 agent loop（最多 maxTurns 轮）。
// 现在按配置里的 api_format 分发到对应协议的适配器，并把全局提示词拼到操作提示词之前。
func AgentRun(ctx context.Context, systemPrompt string, userPrompt string, maxTurns int) (string, error) {
	app := pbApp()
	if app == nil {
		return "", fmt.Errorf("PocketBase app 未初始化")
	}
	cfg, err := loadLLMConfig(app)
	if err != nil {
		return "", fmt.Errorf("读取模型配置失败: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return "", fmt.Errorf("模型配置无效: %w", err)
	}
	if cfg.APIKey == "" {
		// 没配 key：兜底读环境变量 / config.env，兼容迁移前的老部署
		cfg.APIKey = fallbackAPIKey()
	}
	if cfg.APIKey == "" {
		return "", fmt.Errorf("尚未配置 API Key：到「设置」里填，或设环境变量 MINIMAX_API_KEY")
	}
	prov, err := resolveProvider(cfg.APIFormat)
	if err != nil {
		return "", err
	}
	if maxTurns <= 0 {
		maxTurns = 10
	}
	return prov.run(ctx, cfg, applyGlobalPrompt(cfg.GlobalPrompt, systemPrompt), userPrompt, maxTurns)
}

// fallbackAPIKey 迁移前的部署靠环境变量 / /config.env 提供 key。
// 新配置没填时才走这里，保证升级不炸。
func fallbackAPIKey() string {
	if k := os.Getenv("MINIMAX_API_KEY"); k != "" {
		return strings.TrimSpace(k)
	}
	data, err := os.ReadFile("/config.env")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "MINIMAX_API_KEY=") {
			return strings.Trim(strings.TrimPrefix(line, "MINIMAX_API_KEY="), "\"'")
		}
	}
	return ""
}

// pbApp 返回全局 PocketBase app（main.go 里注入）
var _app core.App

func SetApp(app core.App) { _app = app }

func pbApp() core.App { return _app }
