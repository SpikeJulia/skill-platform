package hooks

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// provider 是一个 LLM 协议的适配器。三种协议的消息格式、工具调用格式都不同，
// 所以各自实现完整的 agent loop（结构一样，线格式不同）。
type provider interface {
	// run 跑一次完整的多轮工具调用循环，直到模型不再要工具或达到 maxTurns。
	run(ctx context.Context, cfg LLMConfig, systemPrompt, userPrompt string, maxTurns int, tdefs []ToolDefinition) (string, error)
}

// llmRequestTimeout 是单次 HTTP 请求的上限。
//
// 为什么要显式给：① Anthropic SDK 在 max_tokens 较大时会拒绝非流式请求
// （"streaming is required for operations that may take longer than 10 minutes"），
// 唯一合法的绕法就是自己指定超时——SDK 见到显式超时就不再做那个检查；
// ② 之前用 http.DefaultClient（无超时），服务端不响应时 goroutine 会永远挂着。
//
// 30 分钟是"够跑完一次整篇 SKILL.md 重写"的上限，又不至于把连接无限期占着。
const llmRequestTimeout = 30 * time.Minute

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

// readOnlyTools 只给模型"看"的能力：读文件、列目录。
//
// 为什么组合/编排/合并/进化这几个操作不用完整工具集：
//   - 它们的结果最终由 handler 自己写盘（LLM 只负责产出内容）
//   - 把 write_file / bash 也给模型，就会出现**两个写入方**：
//     模型用工具写一遍，handler 再用"从回复里抠出来的代码围栏"写一遍。
//     两边内容不一致时以最后一次为准；更糟的是模型没返回围栏时，
//     handler 会把"改动说明"当 SKILL.md 写进去，直接毁掉 skill。
//   - 顺带省掉模型"写完再读回来确认"这种来回，每一轮都是一次真实 HTTP 调用，
//     轮数预算本来就不富裕。
var readOnlyTools = []ToolDefinition{
	tools[1], // read_file
	tools[3], // list_dir
}

// toolByName 按名字取工具定义
func toolByName(name string) (ToolDefinition, bool) {
	for _, t := range tools {
		if t.Name == name {
			return t, true
		}
	}
	return ToolDefinition{}, false
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

// agentTrail 记录 agent loop 里发生过的工具调用，两个用途：
//  1. 检测模型重复调同一个工具（死循环，真实遇到过：进化功能卡在"超过最大轮数"）
//  2. 轮数用尽时把轨迹带进报错，让用户一眼看出卡在哪一步
type agentTrail struct {
	seq  []string       // 人类可读的调用轨迹
	seen map[string]int // name+args → 出现次数
}

func newAgentTrail() *agentTrail {
	return &agentTrail{seen: map[string]int{}}
}

// record 记一次调用，返回这是第几次调用同样的东西
func (t *agentTrail) record(name, args string) int {
	key := name + "\x00" + args
	t.seen[key]++
	n := t.seen[key]
	t.seq = append(t.seq, fmt.Sprintf("%s(%s)", name, truncate(args, 90)))
	return n
}

// repeatHint 是喂回给模型的提示：别再重复同一个调用了
func (t *agentTrail) repeatHint(name, args string, n int) string {
	return fmt.Sprintf(
		"你刚才已经用完全相同的参数调用过 %s %d 次了，结果都是一样的。\n"+
			"别再重复这个调用。请基于已有的信息直接给出最终结果。",
		name, n-1)
}

// summary 把轨迹压成一行，给报错信息用
func (t *agentTrail) summary() string {
	if len(t.seq) == 0 {
		return "（模型没有调用任何工具）"
	}
	if len(t.seq) > 8 {
		head := t.seq[:4]
		tail := t.seq[len(t.seq)-4:]
		parts := append([]string{}, head...)
		parts = append(parts, "…")
		parts = append(parts, tail...)
		return strings.Join(parts, " → ")
	}
	return strings.Join(t.seq, " → ")
}

// AgentRun 跑一次完整的 agent loop（最多 maxTurns 轮）。
// 现在按配置里的 api_format 分发到对应协议的适配器，并把全局提示词拼到操作提示词之前。
//
// tdefs 是本次允许模型用的工具集。给不同操作传不同的集合是有意的：
// 见 readOnlyTools 的注释。
func AgentRun(ctx context.Context, systemPrompt string, userPrompt string, maxTurns int, tdefs []ToolDefinition) (string, error) {
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
		return "", fmt.Errorf("尚未配置 API Key：到顶栏的「Agent 配置」里填")
	}
	prov, err := resolveProvider(cfg.APIFormat)
	if err != nil {
		return "", err
	}
	if maxTurns <= 0 {
		maxTurns = 10
	}
	if tdefs == nil {
		tdefs = tools
	}
	return prov.run(ctx, cfg, applyGlobalPrompt(cfg.GlobalPrompt, systemPrompt), userPrompt, maxTurns, tdefs)
}

// pbApp 返回全局 PocketBase app（main.go 里注入）
var _app core.App

func SetApp(app core.App) { _app = app }

func pbApp() core.App { return _app }
