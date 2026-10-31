package aienums

var DSHProtocolAlias = map[string]string{
	"openai-completions":   ProtocolOpenAIChatCompletions,
	"openai-responses":     ProtocolOpenAIResponses,
	"anthropic-messages":   ProtocolAnthropicMessages,
	"google-generative-ai": ProtocolGeminiGenerateContent,
}
