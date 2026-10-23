package httptransport

import (
	"encoding/json"
	"testing"

	aidomain "aegis/internal/domain/ai"
)

func TestOpenAIGatewayParsesReasoning(t *testing.T) {
	var req openAIGatewayRequest
	body := `{"model":"m","reasoning_effort":"X-High","messages":[
		{"role":"user","content":"hi"},
		{"role":"assistant","content":null,"reasoning_content":"想",
		 "tool_calls":[{"id":"c1","type":"function","function":{"name":"f","arguments":"{}"}}]},
		{"role":"tool","tool_call_id":"c1","content":"ok"}]}`
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatal(err)
	}
	out, err := openAIGatewayToChatRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	if out.Reasoning == nil || out.Reasoning.Effort != aidomain.ReasoningXHigh {
		t.Fatalf("reasoning = %+v", out.Reasoning)
	}
	if out.Messages[1].Reasoning != "想" {
		t.Fatalf("assistant reasoning_content not kept: %+v", out.Messages[1])
	}

	req = openAIGatewayRequest{}
	_ = json.Unmarshal([]byte(`{"model":"m","messages":[{"role":"user","content":"hi"}],"reasoning":{"enabled":false}}`), &req)
	if out, _ := openAIGatewayToChatRequest(req); !out.Reasoning.Off() {
		t.Fatalf("reasoning.enabled=false should turn thinking off: %+v", out.Reasoning)
	}

	req = openAIGatewayRequest{}
	_ = json.Unmarshal([]byte(`{"model":"m","messages":[{"role":"user","content":"hi"}],"thinking":{"type":"enabled","budget_tokens":5000}}`), &req)
	if out, _ := openAIGatewayToChatRequest(req); out.Reasoning.BudgetTokens != 5000 || out.Reasoning.EffectiveEffort() != aidomain.ReasoningMedium {
		t.Fatalf("thinking budget = %+v", out.Reasoning)
	}

	req = openAIGatewayRequest{}
	_ = json.Unmarshal([]byte(`{"model":"m","messages":[{"role":"user","content":"hi"}],"reasoning_effort":"turbo"}`), &req)
	if _, err := openAIGatewayToChatRequest(req); err == nil {
		t.Fatal("unknown effort must be rejected")
	}
}

func TestAnthropicGatewayParsesThinking(t *testing.T) {
	var req anthropicGatewayRequest
	body := `{"model":"m","max_tokens":4000,"thinking":{"type":"enabled","budget_tokens":2048},"messages":[
		{"role":"user","content":"hi"},
		{"role":"assistant","content":[{"type":"thinking","thinking":"想","signature":"sig"},
			{"type":"tool_use","id":"toolu_1","name":"f","input":{}}]},
		{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"ok"}]}]}`
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatal(err)
	}
	out, err := anthropicGatewayToChatRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	if out.Reasoning == nil || out.Reasoning.BudgetTokens != 2048 {
		t.Fatalf("reasoning = %+v", out.Reasoning)
	}
	assistant := out.Messages[1]
	if len(assistant.Thinking) != 1 || assistant.Thinking[0].Signature != "sig" || assistant.Reasoning != "想" {
		t.Fatalf("thinking block not kept: %+v", assistant)
	}

	req = anthropicGatewayRequest{}
	_ = json.Unmarshal([]byte(`{"model":"m","max_tokens":10,"output_config":{"effort":"max"},"thinking":{"type":"adaptive"},"messages":[{"role":"user","content":"hi"}]}`), &req)
	if out, _ := anthropicGatewayToChatRequest(req); out.Reasoning.Effort != aidomain.ReasoningMax {
		t.Fatalf("output_config.effort = %+v", out.Reasoning)
	}
}
