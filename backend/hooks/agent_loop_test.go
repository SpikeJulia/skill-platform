package hooks

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ---- agent loop 的健壮性 ----
//
// 背景：用户反馈「进化」功能报"超过最大轮数 (8)"。根因有四个，这几个测试分别钉住：
//  1. 轮数用尽时，已经产出��文本不能丢（原来直接 return "", err，白干）
//  2. 模型重复调同一个工具时要打断它（原来一直重复到轮数耗尽）
//  3. 组合/合并/进化这类"由 handler 写盘"的操作，模型不能拿 write_file
//     （两个写入方 → 内容打架；没围栏时还会把说明文字当 SKILL.md 写进去）
//  4. 没有围栏且内容不像 SKILL.md 时，必须拒绝写入

// 造一个"永远调工具、从不给正文"的假服务，用来触发轮数耗尽
func newLoopingServer(t *testing.T, toolName string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"r1","status":"completed","output":[
			{"type":"function_call","call_id":"c1","name":"` + toolName + `","arguments":"{\"path\":\"/skills\"}"}]}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// 1) 轮数用尽但模型说过话 → 要把已产出的内容返回去，而不是报错
func TestResponses_TurnBudgetExhaustedKeepsProducedText(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		if calls == 1 {
			// 第一轮：先说一句，再调工具
			_, _ = w.Write([]byte(`{"id":"r1","status":"completed","output":[
				{"type":"message","role":"assistant","content":[{"type":"output_text","text":"已经写好的内容"}]},
				{"type":"function_call","call_id":"c1","name":"list_dir","arguments":"{\"path\":\"/skills\"}"}]}`))
			return
		}
		// 之后一直调工具
		_, _ = w.Write([]byte(`{"id":"r2","status":"completed","output":[
			{"type":"function_call","call_id":"c2","name":"list_dir","arguments":"{\"path\":\"/skills\"}"}]}`))
	}))
	defer srv.Close()

	cfg := LLMConfig{
		APIFormat: FormatResponses, BaseURL: srv.URL,
		APIKey: "sk-test", Model: "m", Headers: map[string]string{},
	}
	got, err := (responsesProvider{}).run(context.Background(), cfg, "s", "u", 3, readOnlyTools)
	if err != nil {
		t.Fatalf("不该报错（模型已产出内容时应返回它）: %v", err)
	}
	if !strings.Contains(got, "已经写好的内容") {
		t.Errorf("返回的内容里没有模型已经产出的正文: %q", got)
	}
}

// 2) 轮数用尽且模型一句话都没说 → 报错要带工具调用轨迹（不然没法排查）
func TestResponses_TurnBudgetExhaustedErrorHasTrail(t *testing.T) {
	srv := newLoopingServer(t, "list_dir")
	cfg := LLMConfig{
		APIFormat: FormatResponses, BaseURL: srv.URL,
		APIKey: "sk-test", Model: "m", Headers: map[string]string{},
	}
	_, err := (responsesProvider{}).run(context.Background(), cfg, "s", "u", 3, readOnlyTools)
	if err == nil {
		t.Fatal("没有任何输出时应该报错")
	}
	if !strings.Contains(err.Error(), "list_dir") {
		t.Errorf("报错里应带工具调用轨迹，实际: %v", err)
	}
}

// 3) 模型重复调同一个工具 → 第二次起要收到"别再重复"的提示
func TestResponses_RepeatedToolCallGetsHint(t *testing.T) {
	// 每次请求的 input 都带着之前的全部工具结果，所以只留**本次**收到的那些
	var lastOutputs []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		lastOutputs = nil
		if inp, ok := body["input"].([]any); ok {
			for _, it := range inp {
				m, _ := it.(map[string]any)
				if m["type"] == "function_call_output" {
					lastOutputs = append(lastOutputs, m["output"].(string))
				}
			}
		}
		_, _ = w.Write([]byte(`{"id":"r1","status":"completed","output":[
			{"type":"function_call","call_id":"c1","name":"list_dir","arguments":"{\"path\":\"/skills\"}"}]}`))
	}))
	defer srv.Close()

	cfg := LLMConfig{
		APIFormat: FormatResponses, BaseURL: srv.URL,
		APIKey: "sk-test", Model: "m", Headers: map[string]string{},
	}
	_, _ = (responsesProvider{}).run(context.Background(), cfg, "s", "u", 3, readOnlyTools)

	// 最后一轮请求的 input 里，装着前两轮的工具结果
	if len(lastOutputs) < 2 {
		t.Fatalf("最后一轮应带上 2 条工具结果，实际 %d 条", len(lastOutputs))
	}
	if strings.Contains(lastOutputs[0], "别再重复") {
		t.Errorf("第 1 次是首次调用，不该带重复提示: %q", lastOutputs[0])
	}
	if !strings.Contains(lastOutputs[1], "别再重复") {
		t.Errorf("第 2 次应带上「别再重复」的提示: %q", lastOutputs[1])
	}
}

// 4) 只读工具集里不能有 write_file / bash
//    组合/合并/进化由 handler 写盘，模型再能写就是两个写入方
func TestReadOnlyToolsExcludeWriters(t *testing.T) {
	for _, td := range readOnlyTools {
		if td.Name == "write_file" || td.Name == "bash" {
			t.Errorf("只读工具集里不该有 %q", td.Name)
		}
	}
	got := map[string]bool{}
	for _, td := range readOnlyTools {
		got[td.Name] = true
	}
	if !got["read_file"] || !got["list_dir"] {
		t.Errorf("只读工具集应该含 read_file 和 list_dir，实际: %v", got)
	}
}

// 5) 模型没给出完整 SKILL.md 时，handler 不能把说明文字当文件内容写进去
func TestLooksLikeSkillMD(t *testing.T) {
	good := "---\nname: tdd\ndescription: Test driven development\n---\n\n# TDD\n\n正文"
	bad := "我做了以下改动：\n- 改进了描述\n- 补充了触发条件"

	if !looksLikeSkillMD(good) {
		t.Error("完整 SKILL.md 应被识别")
	}
	if looksLikeSkillMD(bad) {
		t.Error("改动说明不该被当成 SKILL.md")
	}
	if looksLikeSkillMD("") {
		t.Error("空串不该被识别")
	}
	if looksLikeSkillMD("---\nname: x\n---\n没有 description") {
		t.Error("缺 description 的不该被识别")
	}
}

// 6) 传给模型的工具集要真的反映在请求体里
func TestResponses_SendsOnlyGivenTools(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		for _, td := range body.Tools {
			got = append(got, td.Name)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"r1","status":"completed","output":[
			{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}]}`))
	}))
	defer srv.Close()

	cfg := LLMConfig{
		APIFormat: FormatResponses, BaseURL: srv.URL,
		APIKey: "sk-test", Model: "m", Headers: map[string]string{},
	}
	if _, err := (responsesProvider{}).run(context.Background(), cfg, "s", "u", 2, readOnlyTools); err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, n := range got {
		if n == "write_file" || n == "bash" {
			t.Errorf("请求里不该带 %q，实际带了: %v", n, got)
		}
	}
	if len(got) != len(readOnlyTools) {
		t.Errorf("请求里的工具数 %d，应为 %d", len(got), len(readOnlyTools))
	}
}
