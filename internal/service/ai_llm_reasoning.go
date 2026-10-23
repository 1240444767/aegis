package service

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
	"sync"
	"time"

	aidomain "aegis/internal/domain/ai"
)

// ═══════════════════════════ 思考：签名回填 ═══════════════════════════
//
// 开着思考的工具循环有一条各家都有的硬约束：模型发起工具调用的那一轮，思考的签名要原样带回。
//   - Anthropic：assistant 轮里的 thinking / redacted_thinking 块连同 signature 必须在 tool_use 之前；
//   - Gemini 3：每个 functionCall 分片的 thoughtSignature 必须带回，否则 400。
//
// OpenAI 兼容的客户端（绝大多数 App 与 SDK）根本没有地方放这些签名，回传的历史里只剩
// tool_calls。所以这里按工具调用编号记下签名，下一次请求里同一个编号的调用缺签名时补回去。
// 工具调用编号由上游签发（toolu_…、call_…），Gemini 不给时我们合成一个带随机段的，都不会撞。
//
// 只在本实例内存里记：多实例下另一台接到续轮时查不到，按各适配器的退路处理
// （Anthropic 本轮不开思考、Gemini 用官方允许的跳过校验占位），调用照样成功。

const (
	aiThinkingMemoTTL = 2 * time.Hour
	aiThinkingMemoCap = 50_000
)

type aiThinkingEntry struct {
	thinking  []aidomain.ThinkingBlock
	signature string
	expires   time.Time
}

type aiThinkingMemo struct {
	mu    sync.Mutex
	items map[string]aiThinkingEntry
	now   func() time.Time
}

func newAIThinkingMemo() *aiThinkingMemo {
	return &aiThinkingMemo{items: make(map[string]aiThinkingEntry), now: time.Now}
}

// remember 记下一次响应里工具调用的签名。思考块属于整轮，记在第一个调用上。
func (m *aiThinkingMemo) remember(response *aidomain.ChatResponse) {
	if m == nil || response == nil || len(response.ToolCalls) == 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	expires := m.now().Add(aiThinkingMemoTTL)
	for index, call := range response.ToolCalls {
		if call.ID == "" {
			continue
		}
		entry := aiThinkingEntry{signature: call.Signature, expires: expires}
		if index == 0 {
			entry.thinking = response.Thinking
		}
		if entry.signature == "" && len(entry.thinking) == 0 {
			continue
		}
		m.items[call.ID] = entry
	}
	if len(m.items) > aiThinkingMemoCap {
		m.evictLocked()
	}
}

// evictLocked 先清过期的；仍然超量就丢掉一半（map 遍历顺序随机，近似随机淘汰）。
func (m *aiThinkingMemo) evictLocked() {
	now := m.now()
	for id, entry := range m.items {
		if now.After(entry.expires) {
			delete(m.items, id)
		}
	}
	excess := len(m.items) - aiThinkingMemoCap/2
	for id := range m.items {
		if excess <= 0 {
			break
		}
		delete(m.items, id)
		excess--
	}
}

func (m *aiThinkingMemo) lookup(id string) (aiThinkingEntry, bool) {
	entry, ok := m.items[id]
	if !ok || m.now().After(entry.expires) {
		return aiThinkingEntry{}, false
	}
	return entry, true
}

// restore 给缺签名的 assistant 轮补上记下的思考块与调用签名。没有可补的原样返回，不复制。
func (m *aiThinkingMemo) restore(request aidomain.ChatRequest) aidomain.ChatRequest {
	if m == nil {
		return request
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.items) == 0 {
		return request
	}
	var messages []aidomain.ChatMessage
	for index, message := range request.Messages {
		if message.Role != aidomain.RoleAssistant || len(message.ToolCalls) == 0 {
			continue
		}
		changed := message
		touched := false
		var calls []aidomain.ToolCall
		for callIndex, call := range message.ToolCalls {
			entry, ok := m.lookup(call.ID)
			if !ok {
				continue
			}
			if len(changed.Thinking) == 0 && len(entry.thinking) > 0 {
				changed.Thinking = entry.thinking
				touched = true
			}
			if call.Signature == "" && entry.signature != "" {
				if calls == nil {
					calls = append([]aidomain.ToolCall(nil), message.ToolCalls...)
				}
				calls[callIndex].Signature = entry.signature
				touched = true
			}
		}
		if !touched {
			continue
		}
		if calls != nil {
			changed.ToolCalls = calls
		}
		if messages == nil {
			messages = append([]aidomain.ChatMessage(nil), request.Messages...)
		}
		messages[index] = changed
	}
	if messages != nil {
		request.Messages = messages
	}
	return request
}

// ═══════════════════════════ 思考：协议无关的小工具 ═══════════════════════════

// toolLoopMissingThinking 当前是不是一个「上一轮 assistant 发起了工具调用、却没带思考块」的续轮。
// Anthropic 在这种续轮上开思考会 400，只能本轮不开。
func toolLoopMissingThinking(messages []aidomain.ChatMessage) bool {
	for index := len(messages) - 1; index >= 0; index-- {
		message := messages[index]
		switch message.Role {
		case aidomain.RoleTool:
			continue
		case aidomain.RoleAssistant:
			return len(message.ToolCalls) > 0 && len(message.Thinking) == 0
		default:
			return false
		}
	}
	return false
}

// isGemini3 Gemini 3 起用思考档位（thinkingLevel）代替预算，函数调用必须带思考签名。
func isGemini3(model string) bool {
	model = strings.ToLower(model)
	return strings.Contains(model, "gemini-3") || strings.Contains(model, "gemini-4")
}

// randomCallSuffix 合成工具调用编号的随机段。
func randomCallSuffix() string {
	buffer := make([]byte, 6)
	if _, err := rand.Read(buffer); err != nil {
		return strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	}
	return hex.EncodeToString(buffer)
}
