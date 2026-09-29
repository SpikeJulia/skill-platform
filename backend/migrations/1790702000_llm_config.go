package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
	m "github.com/pocketbase/pocketbase/migrations"
)

// v0.4：llm_config collection —— agent 的模型与提示词配置
//
// 之前模型配置是写死在 Go 常量里的（LLMBaseURL / LLMModel），API key 从环境变量读，
// 换供应商要改代码重编译。现在开放到界面上。
//
// 固定只存 **一条** 记录（id 固定为 "llmconfig"）——用户选了单模型方案。
//
// 字段：
//   - provider          提供商显示名（DeepSeek / MiniMax / …，纯展示）
//   - api_format        调用协议：openai_chat | openai_responses | anthropic
//   - base_url          接口地址
//   - api_key           API key。⚠️ GET 接口绝不回显明文，只回 has_api_key + 掩码
//   - headers           自定义 header，JSON 对象字符串
//   - model             模型名称
//   - context_window    上下文窗口（仅展示/记录，调用时不截断）
//   - max_output_tokens 最大输出 token；>0 时作为上限传给供应商
//   - reasoning_effort  推理等级：low | medium | high（仅部分供应商支持）
//   - global_prompt     全局提示词，追加到所有 LLM 操作之前。空 = 用内置默认
func init() {
	m.Register(func(app core.App) error {
		col := core.NewBaseCollection("llm_config")
		col.Fields.Add(
			&core.TextField{Name: "provider", Max: 128},
			&core.TextField{Name: "api_format", Max: 64},
			&core.TextField{Name: "base_url", Max: 512},
			&core.TextField{Name: "api_key", Max: 1024},
			&core.TextField{Name: "headers", Max: 8192},
			&core.TextField{Name: "model", Max: 128},
			&core.NumberField{Name: "context_window"},
			&core.NumberField{Name: "max_output_tokens"},
			&core.TextField{Name: "reasoning_effort", Max: 32},
			&core.EditorField{Name: "global_prompt"},
			&core.AutodateField{Name: "created_at", OnCreate: true, OnUpdate: false},
			&core.AutodateField{Name: "updated_at", OnCreate: true, OnUpdate: true},
		)
		// 单用户本地 → 所有 API rule 公开（无 auth）
		col.ListRule = nil
		col.ViewRule = nil
		col.CreateRule = nil
		col.UpdateRule = nil
		col.DeleteRule = nil

		if err := app.Save(col); err != nil {
			return err
		}

		// 种子一条：MiniMax 走 Anthropic 兼容端点。
		// 为什么不是原来的 https://api.minimax.cn/v1（Chat Completions）：
		// 用户明确不要 Chat Completions 协议，而 MiniMax 原生提供 Anthropic 端点
		// （https://api.minimaxi.com/anthropic），官方还标为推荐路径——支持 thinking 块。
		// 同一个 key 直接可用，不用改任何东西。
		rec := core.NewRecord(col)
		// 固定 id（与 hooks.llmConfigID 对应；migrations 包不能 import hooks）。
		// ⚠️ PocketBase 要求 record id 至少 15 个字符，短 id 会在 migration 阶段直接失败。
		rec.Id = "llmconfig000001"
		rec.Set("provider", "MiniMax")
		rec.Set("api_format", "anthropic")
		rec.Set("base_url", "https://api.minimaxi.com/anthropic")
		rec.Set("api_key", "")
		rec.Set("headers", "{}")
		rec.Set("model", "MiniMax-M3")
		rec.Set("context_window", 1000000)
		rec.Set("max_output_tokens", 8192)
		rec.Set("reasoning_effort", "")
		rec.Set("global_prompt", "")
		rec.Set("created_at", types.NowDateTime())
		return app.Save(rec)
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("llm_config")
		if err != nil {
			return err
		}
		return app.Delete(col)
	})
}
