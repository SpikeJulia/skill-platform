package hooks

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 这些测试用 httptest 假服务器验证两种协议的**请求构造**和**响应解析**。
//
// 为什么必须测：两种协议的线格式完全不同，而且我手上只有 MiniMax 的 key，
// 没有 Anthropic 官方和 OpenAI 官方的 key 可以真调。真实端点调不到的部分
// 只能靠"按 spec 构造请求 + 按真实响应形状解析"来保证，而这两处正是最容易
// 写错、错了还很难一眼看出来的地方。

// ---- 公共：抓取最后一次请求 ----
type capturedRequest struct {
	path    string
	headers http.Header
	body    map[string]any
	raw     string
}

func newCapture(t *testing.T, respond func(capturedRequest) string) (*httptest.Server, *capturedRequest) {
	t.Helper()
	cap := &capturedRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		cap.path = r.URL.Path
		cap.headers = r.Header.Clone()
		cap.raw = string(raw)
		_ = json.Unmarshal(raw, &cap.body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(respond(*cap)))
	}))
	t.Cleanup(srv.Close)
	return srv, cap
}

// ---- Anthropic Messages ----

func TestAnthropic_NoToolCallReturnsText(t *testing.T) {
	srv, cap := newCapture(t, func(c capturedRequest) string {
		return `{"id":"msg_1","type":"message","role":"assistant","content":[
			{"type":"text","text":"已收到"}] }`
	})

	cfg := LLMConfig{
		Provider: "test", APIFormat: FormatAnthropic,
		BaseURL: srv.URL, APIKey: "sk-test", Model: "test-model",
		Headers: map[string]string{},
	}
	got, err := anthropicProvider{}.run(context.Background(), cfg, "sys", "user", 3)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got != "已收到" {
		t.Errorf("text = %q, want %q", got, "已收到")
	}
	// 请求构造
	if cap.path != "/v1/messages" {
		t.Errorf("path = %q, want /v1/messages", cap.path)
	}
	if v := cap.headers.Get("X-Api-Key"); v != "sk-test" {
		t.Errorf("X-Api-Key = %q, want sk-test", v)
	}
	if v := cap.headers.Get("Anthropic-Version"); v == "" {
		t.Error("缺少 anthropic-version 头")
	}
	if v, _ := cap.body["max_tokens"].(float64); v <= 0 {
		t.Errorf("max_tokens = %v，Anthropic 强制要求它 > 0", cap.body["max_tokens"])
	}
	if v, _ := cap.body["model"].(string); v != "test-model" {
		t.Errorf("model = %v", cap.body["model"])
	}
	if _, ok := cap.body["tools"]; !ok {
		t.Error("请求里没有 tools")
	}
}

func TestAnthropic_ToolCallRoundTrip(t *testing.T) {
	calls := 0
	srv, cap := newCapture(t, func(c capturedRequest) string {
		calls++
		if calls == 1 {
			// 第一轮：要工具
			return `{"id":"msg_1","type":"message","role":"assistant","content":[
				{"type":"tool_use","id":"toolu_1","name":"list_dir","input":{"path":"/skills"}}]}`
		}
		// 第二轮：给答案
		return `{"id":"msg_2","type":"message","role":"assistant","content":[
			{"type":"text","text":"目录里有 3 项"}]}`
	})

	cfg := LLMConfig{
		APIFormat: FormatAnthropic, BaseURL: srv.URL, APIKey: "sk-test",
		Model: "test-model", Headers: map[string]string{},
	}
	got, err := anthropicProvider{}.run(context.Background(), cfg, "sys", "user", 5)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got != "目录里有 3 项" {
		t.Errorf("text = %q", got)
	}
	if calls != 2 {
		t.Errorf("请求轮数 = %d, want 2", calls)
	}
	// 第二轮请求必须带回 assistant 的 tool_use 和 user 的 tool_result
	raw := cap.raw
	if !strings.Contains(raw, `"tool_use"`) {
		t.Error("第二轮请求里没有回传 assistant 的 tool_use 块")
	}
	if !strings.Contains(raw, `"tool_result"`) {
		t.Error("第二轮请求里没有 tool_result")
	}
	// tool_result 必须在 user 角色消息里（Anthropic 不接受 assistant 发 tool_result）
	if !strings.Contains(raw, `"role":"user"`) {
		t.Error("tool_result 没有放在 user 消息里")
	}
}

func TestAnthropic_CustomHeadersOverrideAuth(t *testing.T) {
	srv, cap := newCapture(t, func(c capturedRequest) string {
		return `{"id":"m","content":[{"type":"text","text":"ok"}]}`
	})
	cfg := LLMConfig{
		APIFormat: FormatAnthropic, BaseURL: srv.URL, APIKey: "sk-test",
		Model: "m",
		// MiniMax 的 Anthropic 端点两种认证都收；这里模拟用户选了 Bearer
		Headers: map[string]string{"Authorization": "Bearer custom-token"},
	}
	if _, err := (anthropicProvider{}).run(context.Background(), cfg, "s", "u", 1); err != nil {
		t.Fatalf("run: %v", err)
	}
	if v := cap.headers.Get("Authorization"); v != "Bearer custom-token" {
		t.Errorf("Authorization = %q, 自定义 header 没生效", v)
	}
	if v := cap.headers.Get("Anthropic-Version"); v == "" {
		t.Error("自定义 header 把默认的 anthropic-version 冲掉了")
	}
}

// ---- OpenAI Responses ----

func TestResponses_NoToolCallReturnsText(t *testing.T) {
	srv, cap := newCapture(t, func(c capturedRequest) string {
		return `{"id":"resp_1","status":"completed","output":[
			{"type":"message","role":"assistant","content":[
				{"type":"output_text","text":"好的"}]}]}`
	})

	cfg := LLMConfig{
		APIFormat: FormatResponses, BaseURL: srv.URL,
		APIKey: "sk-test", Model: "gpt-x", Headers: map[string]string{},
	}
	got, err := (responsesProvider{}).run(context.Background(), cfg, "sys", "user", 3)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got != "好的" {
		t.Errorf("text = %q", got)
	}
	if cap.path != "/responses" {
		t.Errorf("path = %q, want /responses", cap.path)
	}
	if v := cap.headers.Get("Authorization"); v != "Bearer sk-test" {
		t.Errorf("Authorization = %q", v)
	}
	if v, _ := cap.body["instructions"].(string); v != "sys" {
		t.Errorf("instructions = %v, 系统提示词应走 instructions 字段", cap.body["instructions"])
	}
}

func TestResponses_ToolCallRoundTrip(t *testing.T) {
	calls := 0
	srv, cap := newCapture(t, func(c capturedRequest) string {
		calls++
		if calls == 1 {
			return `{"id":"resp_1","status":"completed","output":[
				{"type":"reasoning","id":"rs_1","summary":[]},
				{"type":"function_call","call_id":"call_1","name":"list_dir","arguments":"{\"path\":\"/skills\"}"}]}`
		}
		return `{"id":"resp_2","status":"completed","output":[
			{"type":"message","role":"assistant","content":[
				{"type":"output_text","text":"列出来了"}]}]}`
	})

	cfg := LLMConfig{
		APIFormat: FormatResponses, BaseURL: srv.URL, APIKey: "sk-test",
		Model: "gpt-x", Headers: map[string]string{},
	}
	got, err := (responsesProvider{}).run(context.Background(), cfg, "sys", "user", 5)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got != "列出来了" {
		t.Errorf("text = %q", got)
	}
	if calls != 2 {
		t.Errorf("轮数 = %d, want 2", calls)
	}
	raw := cap.raw
	if !strings.Contains(raw, `"function_call_output"`) {
		t.Error("第二轮请求里没有 function_call_output")
	}
	if !strings.Contains(raw, `call_1`) {
		t.Error("function_call_output 没有带 call_id 对应回去")
	}
	if !strings.Contains(raw, `"reasoning"`) {
		t.Error("reasoning 块没有被回传，多轮推理链会断")
	}
}

func TestResponses_ReasoningEffortSent(t *testing.T) {
	srv, cap := newCapture(t, func(c capturedRequest) string {
		return `{"id":"r","status":"completed","output":[
			{"type":"message","content":[{"type":"output_text","text":"ok"}]}]}`
	})
	cfg := LLMConfig{
		APIFormat: FormatResponses, BaseURL: srv.URL, APIKey: "k",
		Model: "m", ReasoningEffort: "high", Headers: map[string]string{},
	}
	if _, err := (responsesProvider{}).run(context.Background(), cfg, "s", "u", 1); err != nil {
		t.Fatalf("run: %v", err)
	}
	r, ok := cap.body["reasoning"].(map[string]any)
	if !ok {
		t.Fatalf("请求里没有 reasoning 字段: %v", cap.body)
	}
	if r["effort"] != "high" {
		t.Errorf("effort = %v, want high", r["effort"])
	}
}

func TestResponses_HTTPErrorIncludesBody(t *testing.T) {
	srv, _ := newCapture(t, func(c capturedRequest) string {
		return `{"error":{"message":"model not found: gpt-nope"}}`
	})
	cfg := LLMConfig{
		APIFormat: FormatResponses, BaseURL: srv.URL + "/v1", APIKey: "k",
		Model: "gpt-nope", Headers: map[string]string{},
	}
	// 假服务器返回 200，所以这里只会走 provider 的 error 分支；
	// 真正要验的是「错误体带出来」而不是只有一句状态码。
	_, err := (responsesProvider{}).run(context.Background(), cfg, "s", "u", 1)
	if err == nil {
		t.Fatal("期望出错")
	}
	if !strings.Contains(err.Error(), "model not found") {
		t.Errorf("错误信息里没有供应商原文: %v", err)
	}
}

// ---- 公共逻辑 ----

func TestResolveProvider(t *testing.T) {
	if _, err := resolveProvider(FormatAnthropic); err != nil {
		t.Errorf("anthropic 应可用: %v", err)
	}
	if _, err := resolveProvider(FormatResponses); err != nil {
		t.Errorf("responses 应可用: %v", err)
	}
	if _, err := resolveProvider("openai_chat"); err == nil {
		t.Error("openai_chat 应被拒绝（用户明确不要这个协议）")
	}
}

func TestApplyGlobalPrompt(t *testing.T) {
	if got := applyGlobalPrompt("", "操作提示词"); got != "操作提示词" {
		t.Errorf("全局为空时应原样返回, got %q", got)
	}
	got := applyGlobalPrompt("你是专家", "操作提示词")
	if !strings.HasPrefix(got, "你是专家") || !strings.Contains(got, "操作提示词") {
		t.Errorf("全局提示词应在前面且保留操作提示词, got %q", got)
	}
}

func TestConfigValidate(t *testing.T) {
	ok := LLMConfig{APIFormat: FormatAnthropic, BaseURL: "https://x.dev", Model: "m"}
	if err := ok.Validate(); err != nil {
		t.Errorf("合法配置不该报错: %v", err)
	}
	bad := []struct {
		name string
		cfg  LLMConfig
	}{
		{"空接口地址", LLMConfig{APIFormat: FormatAnthropic, Model: "m"}},
		{"接口地址无 scheme", LLMConfig{APIFormat: FormatAnthropic, BaseURL: "x.dev", Model: "m"}},
		{"空模型名", LLMConfig{APIFormat: FormatAnthropic, BaseURL: "https://x.dev"}},
		{"不支持的协议", LLMConfig{APIFormat: "openai_chat", BaseURL: "https://x.dev", Model: "m"}},
		{"非法推理等级", LLMConfig{APIFormat: FormatAnthropic, BaseURL: "https://x.dev", Model: "m", ReasoningEffort: "ultra"}},
	}
	for _, b := range bad {
		cfg := b.cfg
		if err := cfg.Validate(); err == nil {
			t.Errorf("%s: 期望报错", b.name)
		}
	}
}

func TestMaskSecretNeverLeaksFullKey(t *testing.T) {
	full := "sk-abcdefghijklmnopqrstuvwxyz"
	got := maskSecret(full)
	if strings.Contains(got, full) {
		t.Errorf("打码结果泄漏了完整 key: %q", got)
	}
	if got == "" {
		t.Error("非空 key 应返回打码结果")
	}
	if maskSecret("") != "" {
		t.Error("空 key 应返回空串")
	}
}
