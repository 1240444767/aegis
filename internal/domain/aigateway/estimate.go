package aigateway

import (
	aidomain "aegis/internal/domain/ai"
)

// 用量估算。
//
// 估算只决定**暂扣多少**，不决定收多少：结算一律以上游报回的用量为准。
// 估算偏高的代价是余额刚好够的用户可能被要求更多余量（对话会改为压低输出上限），
// 偏低的代价是结算时要从余额里补扣、补不上的记 shortfall。所以这里宁高勿低。

const (
	// ImageTokenEstimate 消息里每张图按多少输入 token 估算（高清图在主流模型上约 800-1500）。
	ImageTokenEstimate = 1000
	// MessageOverheadTokens 每条消息的格式开销。
	MessageOverheadTokens = 4
	// MinOutputTokens 压低输出上限时至少要付得起这么多输出 token，否则直接拒绝。
	MinOutputTokens = 16
)

// EstimateTokens 文本的 token 估算：按 UTF-8 字节数 / 3 向上取整。
//
// 英文约 4 字符一个 token，这个估算高出约三成；中文一个字 3 个字节、通常 0.6-1.5 个 token，
// 估成 1 个。都在「宁高勿低」的一侧，又不至于高到把正常请求挡掉。
func EstimateTokens(text string) int64 {
	if text == "" {
		return 0
	}
	return int64((len(text) + 2) / 3)
}

// EstimateChatInput 一次对话请求的输入 token 估算。
func EstimateChatInput(req aidomain.ChatRequest) int64 {
	total := EstimateTokens(req.System)
	for _, tool := range req.Tools {
		total += EstimateTokens(tool.Name) + EstimateTokens(tool.Description) + int64((len(tool.InputSchema)+2)/3)
	}
	for _, message := range req.Messages {
		total += MessageOverheadTokens
		for _, part := range message.Content {
			switch part.Type {
			case aidomain.PartImage:
				total += ImageTokenEstimate
			default:
				total += EstimateTokens(part.Text)
			}
		}
		for _, call := range message.ToolCalls {
			total += EstimateTokens(call.Name) + int64((len(call.Arguments)+2)/3)
		}
	}
	return total
}

// EstimateChatOutput 一次对话结果的输出 token 估算（上游没报用量、或流式中途断开时用）。
func EstimateChatOutput(result *aidomain.ChatResponse) int64 {
	if result == nil {
		return 0
	}
	total := EstimateTokens(result.Text) + EstimateTokens(result.Reasoning)
	for _, call := range result.ToolCalls {
		total += EstimateTokens(call.Name) + int64((len(call.Arguments)+2)/3)
	}
	return total
}
