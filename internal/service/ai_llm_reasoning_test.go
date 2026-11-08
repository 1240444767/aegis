package service

import (
	"encoding/json"
	"strings"
	"testing"

	aidomain "aegis/internal/domain/ai"

	"google.golang.org/genai"
)

func reasoningRequest(effort string) aidomain.ChatRequest {
	return aidomain.ChatRequest{
		Model:     "m",
		Messages:  []aidomain.ChatMessage{aidomain.TextMessage(aidomain.RoleUser, "hi")},
		Reasoning: &aidomain.ReasoningConfig{Effort: effort},
	}
}

func marshalParams(t *testing.T, value any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return out
}

func TestOpenAIReasoningPerProvider(t *testing.T) {
	request := reasoningRequest(aidomain.ReasoningHigh)
	request.MaxTokens = 2000

	official := marshalParams(t, buildOpenAIParams(request, aidomain.ProviderOpenAI))
	if official["reasoning_effort"] != "high" {
		t.Fatalf("openai reasoning_effort = %v", official["reasoning_effort"])
	}
	if official["max_completion_tokens"] != float64(2000) || official["max_tokens"] != nil {
		t.Fatalf("openai should use max_completion_tokens: %v", official)
	}

	deepseek := marshalParams(t, buildOpenAIParams(request, aidomain.ProviderDeepSeek))
	if thinking, _ := deepseek["thinking"].(map[string]any); thinking["type"] != "enabled" {
		t.Fatalf("deepseek thinking = %v", deepseek["thinking"])
	}
	if deepseek["max_tokens"] != float64(2000) {
		t.Fatalf("deepseek should keep max_tokens: %v", deepseek)
	}

	qwen := marshalParams(t, buildOpenAIParams(request, aidomain.ProviderQwen))
	if qwen["enable_thinking"] != true || qwen["thinking_budget"] != float64(16384) {
		t.Fatalf("qwen thinking = %v / %v", qwen["enable_thinking"], qwen["thinking_budget"])
	}

	off := marshalParams(t, buildOpenAIParams(reasoningRequest(aidomain.ReasoningNone), aidomain.ProviderOpenRouter))
	if reasoning, _ := off["reasoning"].(map[string]any); reasoning["enabled"] != false {
		t.Fatalf("openrouter off = %v", off["reasoning"])
	}

	none := marshalParams(t, buildOpenAIParams(aidomain.ChatRequest{Model: "m",
		Messages: []aidomain.ChatMessage{aidomain.TextMessage(aidomain.RoleUser, "hi")}}, aidomain.ProviderOpenAI))
	if _, ok := none["reasoning_effort"]; ok {
		t.Fatalf("no reasoning config should leave reasoning_effort unset: %v", none)
	}
}

func TestOpenAIEchoesReasoningOnToolCallTurns(t *testing.T) {
	request := reasoningRequest(aidomain.ReasoningMedium)
	request.Messages = append(request.Messages,
		aidomain.ChatMessage{Role: aidomain.RoleAssistant, Reasoning: "想一想",
			ToolCalls: []aidomain.ToolCall{{ID: "call_1", Name: "time", Arguments: json.RawMessage(`{}`)}}},
		aidomain.ChatMessage{Role: aidomain.RoleTool, ToolCallID: "call_1",
			Content: []aidomain.ContentPart{{Type: aidomain.PartText, Text: "12:00"}}},
	)
	raw, _ := json.Marshal(buildOpenAIParams(request, aidomain.ProviderDeepSeek))
	if !strings.Contains(string(raw), `"reasoning_content":"想一想"`) {
		t.Fatalf("deepseek should echo reasoning_content: %s", raw)
	}
	raw, _ = json.Marshal(buildOpenAIParams(request, aidomain.ProviderOpenAI))
	if strings.Contains(string(raw), "reasoning_content") {
		t.Fatalf("openai must not receive reasoning_content: %s", raw)
	}
}

func TestAnthropicThinkingBudgetAndTemperature(t *testing.T) {
	temperature := 0.3
	request := reasoningRequest(aidomain.ReasoningMedium)
	request.Temperature = &temperature
	params := marshalParams(t, buildAnthropicParams(request))
	thinking, _ := params["thinking"].(map[string]any)
	if thinking["type"] != "enabled" || thinking["budget_tokens"] != float64(8192) {
		t.Fatalf("thinking = %v", params["thinking"])
	}
	if params["max_tokens"] != float64(8192+8192) {
		t.Fatalf("max_tokens = %v", params["max_tokens"])
	}
	if _, ok := params["temperature"]; ok {
		t.Fatalf("temperature must be dropped while thinking: %v", params)
	}

	// 计费压低了上限：预算让出正文的余量，让不出最低预算就不开。
	request.MaxTokens = 4000
	params = marshalParams(t, buildAnthropicParams(request))
	if thinking, _ := params["thinking"].(map[string]any); thinking["budget_tokens"] != float64(4000-1024) {
		t.Fatalf("clamped thinking = %v", params["thinking"])
	}
	request.MaxTokens = 1500
	params = marshalParams(t, buildAnthropicParams(request))
	if _, ok := params["thinking"]; ok {
		t.Fatalf("too small max_tokens should disable thinking: %v", params["thinking"])
	}

	off := marshalParams(t, buildAnthropicParams(reasoningRequest(aidomain.ReasoningNone)))
	if thinking, _ := off["thinking"].(map[string]any); thinking["type"] != "disabled" {
		t.Fatalf("off thinking = %v", off["thinking"])
	}
}

func TestAnthropicToolLoopNeedsSignedThinking(t *testing.T) {
	loop := reasoningRequest(aidomain.ReasoningHigh)
	assistant := aidomain.ChatMessage{Role: aidomain.RoleAssistant,
		ToolCalls: []aidomain.ToolCall{{ID: "toolu_1", Name: "time", Arguments: json.RawMessage(`{}`)}}}
	tool := aidomain.ChatMessage{Role: aidomain.RoleTool, ToolCallID: "toolu_1",
		Content: []aidomain.ContentPart{{Type: aidomain.PartText, Text: "12:00"}}}
	loop.Messages = append(loop.Messages, assistant, tool)
	if params := marshalParams(t, buildAnthropicParams(loop)); params["thinking"] != nil {
		t.Fatalf("missing thinking block must not enable thinking: %v", params["thinking"])
	}

	assistant.Thinking = []aidomain.ThinkingBlock{{Text: "推理", Signature: "sig"}}
	loop.Messages[1] = assistant
	raw, _ := json.Marshal(buildAnthropicParams(loop))
	text := string(raw)
	if !strings.Contains(text, `"budget_tokens":16384`) {
		t.Fatalf("signed thinking should keep thinking on: %s", text)
	}
	if strings.Index(text, `"type":"thinking"`) > strings.Index(text, `"type":"tool_use"`) {
		t.Fatalf("thinking block must precede tool_use: %s", text)
	}
}

func TestGeminiThinkingConfig(t *testing.T) {
	legacy := geminiThinking("gemini-2.5-flash", &aidomain.ReasoningConfig{Effort: aidomain.ReasoningLow})
	if legacy == nil || legacy.ThinkingBudget == nil || *legacy.ThinkingBudget != 2048 || !legacy.IncludeThoughts {
		t.Fatalf("2.5 low = %+v", legacy)
	}
	off := geminiThinking("gemini-2.5-pro", &aidomain.ReasoningConfig{Effort: aidomain.ReasoningNone})
	if off.ThinkingBudget == nil || *off.ThinkingBudget != 128 || off.IncludeThoughts {
		t.Fatalf("2.5 pro off = %+v", off)
	}
	next := geminiThinking("gemini-3-pro-preview", &aidomain.ReasoningConfig{Effort: aidomain.ReasoningXHigh})
	if next.ThinkingLevel != genai.ThinkingLevelHigh || next.ThinkingBudget != nil {
		t.Fatalf("3 xhigh = %+v", next)
	}
	if geminiThinking("gemini-2.5-flash", nil) != nil {
		t.Fatalf("nil reasoning should leave thinkingConfig unset")
	}
}

func TestGeminiSignaturesRoundTrip(t *testing.T) {
	request := aidomain.ChatRequest{Model: "gemini-3-flash", Messages: []aidomain.ChatMessage{
		aidomain.TextMessage(aidomain.RoleUser, "hi"),
		{Role: aidomain.RoleAssistant, ToolCalls: []aidomain.ToolCall{
			{ID: "a", Name: "f", Arguments: json.RawMessage(`{}`), Signature: "c2ln"},
			{ID: "b", Name: "g", Arguments: json.RawMessage(`{}`)},
		}},
	}}
	contents := buildGeminiContents(request)
	parts := contents[1].Parts
	if string(parts[0].ThoughtSignature) != "sig" {
		t.Fatalf("real signature not restored: %q", parts[0].ThoughtSignature)
	}
	if string(parts[1].ThoughtSignature) != geminiSkipSignature {
		t.Fatalf("missing signature on gemini 3 should use the skip placeholder: %q", parts[1].ThoughtSignature)
	}
}

func TestThinkingMemoRestoresSignatures(t *testing.T) {
	memo := newAIThinkingMemo()
	memo.remember(&aidomain.ChatResponse{
		Thinking:  []aidomain.ThinkingBlock{{Text: "推理", Signature: "sig"}},
		ToolCalls: []aidomain.ToolCall{{ID: "toolu_9", Name: "f"}, {ID: "fc_2", Name: "g", Signature: "gsig"}},
	})
	original := aidomain.ChatRequest{Messages: []aidomain.ChatMessage{
		aidomain.TextMessage(aidomain.RoleUser, "hi"),
		{Role: aidomain.RoleAssistant, ToolCalls: []aidomain.ToolCall{{ID: "toolu_9", Name: "f"}, {ID: "fc_2", Name: "g"}}},
	}}
	restored := memo.restore(original)
	assistant := restored.Messages[1]
	if len(assistant.Thinking) != 1 || assistant.Thinking[0].Signature != "sig" {
		t.Fatalf("thinking not restored: %+v", assistant.Thinking)
	}
	if assistant.ToolCalls[1].Signature != "gsig" {
		t.Fatalf("call signature not restored: %+v", assistant.ToolCalls)
	}
	if len(original.Messages[1].Thinking) != 0 || original.Messages[1].ToolCalls[1].Signature != "" {
		t.Fatalf("restore must not mutate the caller's request")
	}
	untouched := aidomain.ChatRequest{Messages: []aidomain.ChatMessage{aidomain.TextMessage(aidomain.RoleUser, "hi")}}
	if got := memo.restore(untouched); &got.Messages[0] != &untouched.Messages[0] {
		t.Fatalf("nothing to restore should return the same slice")
	}
}
