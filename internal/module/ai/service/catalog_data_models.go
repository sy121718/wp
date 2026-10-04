// catalog_data_models.go — AI 供应商内置预设（318 个模型 / 30 家）。
//
// 数据来源：dsh 内置的 pi-ai 供应商预置包
//
//	@earendil-works/pi-ai/dist/providers/data/*.json
//	.manifest.json schemaVersion 3，generatedAt 2026-09-22T19:31:44.346Z
//
// 逐字段搬运：模型 id / 展示名 / 上下文窗口 / 最大输出 token / 输入类型原样保留，
// **不做前缀加工** —— 前缀形态是各家服务端自己要求的（openrouter 的 deepseek/deepseek-v4.1-flash
// 与 opencode 的裸 id 都是原样），客户端既不添加也不剥离。
//
// 不要手改本文件的数据行：改数据要重跑生成脚本（见 ai_model_catalog.go 文件头）。
package aiservice

import aidto "go_wp/internal/module/ai/dto"

// presetModelsAntLing —— ant-ling（3 个模型）。
var presetModelsAntLing = []aidto.ModelEntry{
	{ID: "Ling-2.6-1T", DisplayName: "Ling 2.6 1T", ContextWindow: 262144, MaxOutputTokens: 65536, InputTypes: inputTextOnly},
	{ID: "Ling-2.6-flash", DisplayName: "Ling 2.6 Flash", ContextWindow: 262144, MaxOutputTokens: 65536, InputTypes: inputTextOnly},
	{ID: "Ring-2.6-1T", DisplayName: "Ring 2.6 1T", ContextWindow: 262144, MaxOutputTokens: 65536, InputTypes: inputTextOnly},
}

// presetModelsAnthropic —— anthropic（15 个模型）。
var presetModelsAnthropic = []aidto.ModelEntry{
	{ID: "claude-fable-5", DisplayName: "Claude Fable 5", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "claude-fable-5-1", DisplayName: "Claude Fable 5.1", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "claude-haiku-4-5", DisplayName: "Claude Haiku 4.5 (latest)", ContextWindow: 200000, MaxOutputTokens: 64000, InputTypes: inputTextImage},
	{ID: "claude-haiku-4-5-20251001", DisplayName: "Claude Haiku 4.5", ContextWindow: 200000, MaxOutputTokens: 64000, InputTypes: inputTextImage},
	{ID: "claude-opus-4-5", DisplayName: "Claude Opus 4.5 (latest)", ContextWindow: 200000, MaxOutputTokens: 64000, InputTypes: inputTextImage},
	{ID: "claude-opus-4-5-20251101", DisplayName: "Claude Opus 4.5", ContextWindow: 200000, MaxOutputTokens: 64000, InputTypes: inputTextImage},
	{ID: "claude-opus-4-6", DisplayName: "Claude Opus 4.6", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "claude-opus-4-7", DisplayName: "Claude Opus 4.7", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "claude-opus-4-8", DisplayName: "Claude Opus 4.8", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "claude-opus-5", DisplayName: "Claude Opus 5", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "claude-opus-5-5", DisplayName: "Claude Opus 5.5", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "claude-sonnet-4-5", DisplayName: "Claude Sonnet 4.5 (latest)", ContextWindow: 1000000, MaxOutputTokens: 64000, InputTypes: inputTextImage},
	{ID: "claude-sonnet-4-5-20250929", DisplayName: "Claude Sonnet 4.5", ContextWindow: 1000000, MaxOutputTokens: 64000, InputTypes: inputTextImage},
	{ID: "claude-sonnet-4-6", DisplayName: "Claude Sonnet 4.6", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "claude-sonnet-5", DisplayName: "Claude Sonnet 5", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
}

// presetModelsBaseten —— baseten（21 个模型）。
var presetModelsBaseten = []aidto.ModelEntry{
	{ID: "deepseek-ai/DeepSeek-V4-Flash-0731", DisplayName: "DeepSeek V4 Flash 0731", ContextWindow: 1048576, MaxOutputTokens: 384000, InputTypes: inputTextOnly},
	{ID: "deepseek-ai/DeepSeek-V4-Pro", DisplayName: "DeepSeek V4 Pro", ContextWindow: 1048576, MaxOutputTokens: 262144, InputTypes: inputTextOnly},
	{ID: "deepseek-ai/DeepSeek-V4-Pro-0813", DisplayName: "DeepSeek V4 Pro 0813", ContextWindow: 1048576, MaxOutputTokens: 262144, InputTypes: inputTextOnly},
	{ID: "deepseek-ai/DeepSeek-V4.1-Flash", DisplayName: "DeepSeek V4.1 Flash", ContextWindow: 1048576, MaxOutputTokens: 32768, InputTypes: inputTextImage},
	{ID: "moonshotai/Kimi-K2.5", DisplayName: "Kimi K2.5", ContextWindow: 262000, MaxOutputTokens: 262000, InputTypes: inputTextImage},
	{ID: "moonshotai/Kimi-K2.6", DisplayName: "Kimi K2.6", ContextWindow: 262000, MaxOutputTokens: 262000, InputTypes: inputTextImage},
	{ID: "moonshotai/Kimi-K2.7-Code", DisplayName: "Kimi K2.7 Code", ContextWindow: 262000, MaxOutputTokens: 262000, InputTypes: inputTextImage},
	{ID: "moonshotai/Kimi-K3", DisplayName: "Kimi K3", ContextWindow: 1048576, MaxOutputTokens: 262144, InputTypes: inputTextImage},
	{ID: "nvidia/NVIDIA-Nemotron-3-Ultra-550B-A55B", DisplayName: "Nemotron Ultra", ContextWindow: 202800, MaxOutputTokens: 202800, InputTypes: inputTextOnly},
	{ID: "nvidia/Nemotron-120B-A12B", DisplayName: "Nemotron Super", ContextWindow: 202800, MaxOutputTokens: 202800, InputTypes: inputTextOnly},
	{ID: "openai/gpt-oss-120b", DisplayName: "OpenAI GPT 120B", ContextWindow: 128072, MaxOutputTokens: 128072, InputTypes: inputTextOnly},
	{ID: "thinkingmachines/inkling", DisplayName: "Inkling", ContextWindow: 1048576, MaxOutputTokens: 32768, InputTypes: inputTextImage},
	{ID: "thinkingmachines/inkling-small", DisplayName: "Inkling Small", ContextWindow: 1048576, MaxOutputTokens: 32768, InputTypes: inputTextImage},
	{ID: "zai-org/GLM-4.7", DisplayName: "GLM 4.7", ContextWindow: 200000, MaxOutputTokens: 200000, InputTypes: inputTextOnly},
	{ID: "zai-org/GLM-5", DisplayName: "GLM 5", ContextWindow: 202800, MaxOutputTokens: 202800, InputTypes: inputTextOnly},
	{ID: "zai-org/GLM-5.1", DisplayName: "GLM 5.1", ContextWindow: 202800, MaxOutputTokens: 202800, InputTypes: inputTextOnly},
	{ID: "zai-org/GLM-5.2", DisplayName: "GLM 5.2", ContextWindow: 1048576, MaxOutputTokens: 262144, InputTypes: inputTextOnly},
	{ID: "zai-org/GLM-5.2-Fast", DisplayName: "GLM 5.2 Fast", ContextWindow: 1048576, MaxOutputTokens: 262144, InputTypes: inputTextOnly},
	{ID: "zai-org/GLM-5.3", DisplayName: "GLM 5.3", ContextWindow: 1048576, MaxOutputTokens: 262144, InputTypes: inputTextImage},
	{ID: "zai-org/GLM-5.3-Fast", DisplayName: "GLM 5.3 Fast", ContextWindow: 1048576, MaxOutputTokens: 262144, InputTypes: inputTextImage},
	{ID: "zai-org/GLM-5.3-Flash", DisplayName: "GLM 5.3 Flash", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
}

// presetModelsCerebras —— cerebras（2 个模型）。
var presetModelsCerebras = []aidto.ModelEntry{
	{ID: "gpt-oss-120b", DisplayName: "GPT OSS 120B", ContextWindow: 131072, MaxOutputTokens: 40960, InputTypes: inputTextOnly},
	{ID: "qwen-3.8-27b", DisplayName: "Qwen3.8 27B", ContextWindow: 65536, MaxOutputTokens: 32768, InputTypes: inputTextImage},
}

// presetModelsCloudflareWorkersAi —— cloudflare-workers-ai（18 个模型）。
var presetModelsCloudflareWorkersAi = []aidto.ModelEntry{
	{ID: "@cf/deepseek-ai/deepseek-v4-flash-0731", DisplayName: "DeepSeek V4 Flash 0731", ContextWindow: 1310720, MaxOutputTokens: 1048576, InputTypes: inputTextOnly},
	{ID: "@cf/deepseek-ai/deepseek-v4-pro-0813", DisplayName: "DeepSeek V4 Pro 0813", ContextWindow: 1048576, MaxOutputTokens: 1048576, InputTypes: inputTextOnly},
	{ID: "@cf/google/gemma-4-26b-a4b-it", DisplayName: "Gemma 4 26B A4B IT", ContextWindow: 256000, MaxOutputTokens: 16384, InputTypes: inputTextImage},
	{ID: "@cf/ibm-granite/granite-4.0-h-micro", DisplayName: "Granite 4.0 H Micro", ContextWindow: 131000, MaxOutputTokens: 131000, InputTypes: inputTextOnly},
	{ID: "@cf/meta/llama-3.3-70b-instruct-fp8-fast", DisplayName: "Llama 3.3 70B Instruct fp8 Fast", ContextWindow: 24000, MaxOutputTokens: 24000, InputTypes: inputTextOnly},
	{ID: "@cf/meta/llama-4-scout-17b-16e-instruct", DisplayName: "Llama 4 Scout 17B 16E Instruct", ContextWindow: 131000, MaxOutputTokens: 16384, InputTypes: inputTextImage},
	{ID: "@cf/mistralai/mistral-small-3.1-24b-instruct", DisplayName: "Mistral Small 3.1 24B Instruct", ContextWindow: 128000, MaxOutputTokens: 128000, InputTypes: inputTextOnly},
	{ID: "@cf/moonshotai/kimi-k2.6", DisplayName: "Kimi K2.6", ContextWindow: 262144, MaxOutputTokens: 256000, InputTypes: inputTextImage},
	{ID: "@cf/moonshotai/kimi-k2.7-code", DisplayName: "Kimi K2.7 Code", ContextWindow: 262144, MaxOutputTokens: 262144, InputTypes: inputTextImage},
	{ID: "@cf/nvidia/nemotron-3-120b-a12b", DisplayName: "Nemotron 3 Super 120B", ContextWindow: 256000, MaxOutputTokens: 256000, InputTypes: inputTextOnly},
	{ID: "@cf/openai/gpt-oss-120b", DisplayName: "GPT OSS 120B", ContextWindow: 128000, MaxOutputTokens: 16384, InputTypes: inputTextOnly},
	{ID: "@cf/openai/gpt-oss-20b", DisplayName: "GPT OSS 20B", ContextWindow: 128000, MaxOutputTokens: 16384, InputTypes: inputTextOnly},
	{ID: "@cf/qwen/qwen3-30b-a3b-fp8", DisplayName: "Qwen3 30B A3b fp8", ContextWindow: 32768, MaxOutputTokens: 32768, InputTypes: inputTextOnly},
	{ID: "@cf/qwen/qwen3.8-27b", DisplayName: "Qwen3.8 27B", ContextWindow: 262144, MaxOutputTokens: 262144, InputTypes: inputTextImage},
	{ID: "@cf/zai-org/glm-4.7-flash", DisplayName: "GLM-4.7-Flash", ContextWindow: 131072, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "@cf/zai-org/glm-5.2", DisplayName: "Glm 5.2", ContextWindow: 262144, MaxOutputTokens: 256000, InputTypes: inputTextOnly},
	{ID: "@cf/zai-org/glm-5.3", DisplayName: "Glm 5.3", ContextWindow: 1310720, MaxOutputTokens: 1048576, InputTypes: inputTextOnly},
	{ID: "@cf/zai-org/glm-5.3-flash", DisplayName: "Glm 5.3 Flash", ContextWindow: 1310720, MaxOutputTokens: 1048576, InputTypes: inputTextImage},
}

// presetModelsDeepseek —— deepseek（2 个模型）。
var presetModelsDeepseek = []aidto.ModelEntry{
	{ID: "deepseek-flash", DisplayName: "DeepSeek V4.1 Flash", ContextWindow: 1000000, MaxOutputTokens: 384000, InputTypes: inputTextImage},
	{ID: "deepseek-v4-pro", DisplayName: "DeepSeek V4 Pro", ContextWindow: 1000000, MaxOutputTokens: 384000, InputTypes: inputTextOnly},
}

// presetModelsGoogle —— google（22 个模型）。
var presetModelsGoogle = []aidto.ModelEntry{
	{ID: "deep-research-max-preview-04-2026", DisplayName: "Deep Research Max Preview (Apr-21-2026)", ContextWindow: 131072, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "deep-research-preview-04-2026", DisplayName: "Deep Research Preview (Apr-21-2026)", ContextWindow: 131072, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "gemini-2.5-computer-use-preview-10-2025", DisplayName: "Gemini 2.5 Computer Use Preview 10-2025", ContextWindow: 131072, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "gemini-2.5-flash", DisplayName: "Gemini 2.5 Flash", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "gemini-2.5-flash-lite", DisplayName: "Gemini 2.5 Flash-Lite", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "gemini-2.5-pro", DisplayName: "Gemini 2.5 Pro", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "gemini-3-flash-preview", DisplayName: "Gemini 3 Flash Preview", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "gemini-3.1-flash-lite", DisplayName: "Gemini 3.1 Flash Lite", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "gemini-3.1-flash-lite-image", DisplayName: "Nano Banana 2 Lite", ContextWindow: 65536, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "gemini-3.1-flash-lite-preview", DisplayName: "Gemini 3.1 Flash Lite Preview", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "gemini-3.1-flash-live-preview", DisplayName: "Gemini 3.1 Flash Live Preview", ContextWindow: 131072, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "gemini-3.1-pro-preview", DisplayName: "Gemini 3.1 Pro Preview", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "gemini-3.1-pro-preview-customtools", DisplayName: "Gemini 3.1 Pro Preview Custom Tools", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "gemini-3.5-flash", DisplayName: "Gemini 3.5 Flash", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "gemini-3.5-flash-lite", DisplayName: "Gemini 3.5 Flash Lite", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "gemini-3.6-flash", DisplayName: "Gemini 3.6 Flash", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "gemini-3.7-flash", DisplayName: "Gemini 3.7 Flash", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "gemini-3.8-flash", DisplayName: "Gemini 3.8 Flash", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "gemini-flash-latest", DisplayName: "Gemini Flash Latest", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "gemini-flash-lite-latest", DisplayName: "Gemini Flash-Lite Latest", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "gemma-4-26b-a4b-it", DisplayName: "Gemma 4 26B A4B IT", ContextWindow: 262144, MaxOutputTokens: 32768, InputTypes: inputTextImage},
	{ID: "gemma-4-31b-it", DisplayName: "Gemma 4 31B IT", ContextWindow: 262144, MaxOutputTokens: 32768, InputTypes: inputTextImage},
}

// presetModelsGoogleVertex —— google-vertex（14 个模型）。
var presetModelsGoogleVertex = []aidto.ModelEntry{
	{ID: "gemini-2.5-flash", DisplayName: "Gemini 2.5 Flash", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "gemini-2.5-flash-lite", DisplayName: "Gemini 2.5 Flash-Lite", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "gemini-2.5-pro", DisplayName: "Gemini 2.5 Pro", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "gemini-3-flash-preview", DisplayName: "Gemini 3 Flash Preview", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "gemini-3.1-flash-lite", DisplayName: "Gemini 3.1 Flash Lite", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "gemini-3.1-pro-preview", DisplayName: "Gemini 3.1 Pro Preview", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "gemini-3.1-pro-preview-customtools", DisplayName: "Gemini 3.1 Pro Preview Custom Tools", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "gemini-3.5-flash", DisplayName: "Gemini 3.5 Flash", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "gemini-3.5-flash-lite", DisplayName: "Gemini 3.5 Flash Lite", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "gemini-3.6-flash", DisplayName: "Gemini 3.6 Flash", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "gemini-3.7-flash", DisplayName: "Gemini 3.7 Flash", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "gemini-3.8-flash", DisplayName: "Gemini 3.8 Flash", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "gemini-flash-latest", DisplayName: "Gemini Flash Latest", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "gemini-flash-lite-latest", DisplayName: "Gemini Flash-Lite Latest", ContextWindow: 1048576, MaxOutputTokens: 65536, InputTypes: inputTextImage},
}

// presetModelsGroq —— groq（7 个模型）。
var presetModelsGroq = []aidto.ModelEntry{
	{ID: "llama-3.1-8b-instant", DisplayName: "Llama 3.1 8B", ContextWindow: 131072, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "llama-3.3-70b-versatile", DisplayName: "Llama 3.3 70B", ContextWindow: 131072, MaxOutputTokens: 32768, InputTypes: inputTextOnly},
	{ID: "openai/gpt-oss-120b", DisplayName: "GPT OSS 120B", ContextWindow: 131072, MaxOutputTokens: 65536, InputTypes: inputTextOnly},
	{ID: "openai/gpt-oss-20b", DisplayName: "GPT OSS 20B", ContextWindow: 131072, MaxOutputTokens: 65536, InputTypes: inputTextOnly},
	{ID: "openai/gpt-oss-safeguard-20b", DisplayName: "Safety GPT OSS 20B", ContextWindow: 131072, MaxOutputTokens: 65536, InputTypes: inputTextOnly},
	{ID: "qwen/qwen3.6-27b", DisplayName: "Qwen3.6 27B", ContextWindow: 131072, MaxOutputTokens: 16384, InputTypes: inputTextImage},
	{ID: "qwen/qwen3.8-27b", DisplayName: "Qwen3.8 27B", ContextWindow: 131042, MaxOutputTokens: 16384, InputTypes: inputTextImage},
}

// presetModelsKimiCoding —— kimi-coding（4 个模型）。
var presetModelsKimiCoding = []aidto.ModelEntry{
	{ID: "k3", DisplayName: "Kimi K3", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "k3-256k", DisplayName: "Kimi K3-256K", ContextWindow: 262144, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "kimi-for-coding", DisplayName: "kimi-for-coding", ContextWindow: 1048576, MaxOutputTokens: 32768, InputTypes: inputTextImage},
	{ID: "kimi-for-coding-highspeed", DisplayName: "Kimi For Coding HighSpeed", ContextWindow: 262144, MaxOutputTokens: 32768, InputTypes: inputTextImage},
}

// presetModelsMeta —— meta（5 个模型）。
var presetModelsMeta = []aidto.ModelEntry{
	{ID: "muse-spark-1.1", DisplayName: "Muse Spark 1.1", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "muse-spark-1.2", DisplayName: "Muse Spark 1.2", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "muse-spark-1.2-contributor", DisplayName: "Muse Spark 1.2 Contributor", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "muse-spark-1.3", DisplayName: "Muse Spark 1.3", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "muse-spark-1.3-contributor", DisplayName: "Muse Spark 1.3 Contributor", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
}

// presetModelsMinimax —— minimax（3 个模型）。
var presetModelsMinimax = []aidto.ModelEntry{
	{ID: "MiniMax-M2.7", DisplayName: "MiniMax-M2.7", ContextWindow: 204800, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "MiniMax-M2.7-highspeed", DisplayName: "MiniMax-M2.7-highspeed", ContextWindow: 204800, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "MiniMax-M3", DisplayName: "MiniMax-M3", ContextWindow: 1048576, MaxOutputTokens: 512000, InputTypes: inputTextImage},
}

// presetModelsMinimaxCn —— minimax-cn（3 个模型）。
var presetModelsMinimaxCn = []aidto.ModelEntry{
	{ID: "MiniMax-M2.7", DisplayName: "MiniMax-M2.7", ContextWindow: 204800, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "MiniMax-M2.7-highspeed", DisplayName: "MiniMax-M2.7-highspeed", ContextWindow: 204800, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "MiniMax-M3", DisplayName: "MiniMax-M3", ContextWindow: 1048576, MaxOutputTokens: 512000, InputTypes: inputTextImage},
}

// presetModelsMoonshotai —— moonshotai（4 个模型）。
var presetModelsMoonshotai = []aidto.ModelEntry{
	{ID: "kimi-k2.6", DisplayName: "Kimi K2.6", ContextWindow: 262144, MaxOutputTokens: 262144, InputTypes: inputTextImage},
	{ID: "kimi-k2.7-code", DisplayName: "Kimi K2.7 Code", ContextWindow: 262144, MaxOutputTokens: 262144, InputTypes: inputTextImage},
	{ID: "kimi-k2.7-code-highspeed", DisplayName: "Kimi K2.7 Code HighSpeed", ContextWindow: 262144, MaxOutputTokens: 262144, InputTypes: inputTextImage},
	{ID: "kimi-k3", DisplayName: "Kimi K3", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
}

// presetModelsMoonshotaiCn —— moonshotai-cn（4 个模型）。
var presetModelsMoonshotaiCn = []aidto.ModelEntry{
	{ID: "kimi-k2.6", DisplayName: "Kimi K2.6", ContextWindow: 262144, MaxOutputTokens: 262144, InputTypes: inputTextImage},
	{ID: "kimi-k2.7-code", DisplayName: "Kimi K2.7 Code", ContextWindow: 262144, MaxOutputTokens: 262144, InputTypes: inputTextImage},
	{ID: "kimi-k2.7-code-highspeed", DisplayName: "Kimi K2.7 Code HighSpeed", ContextWindow: 262144, MaxOutputTokens: 262144, InputTypes: inputTextImage},
	{ID: "kimi-k3", DisplayName: "Kimi K3", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
}

// presetModelsNvidia —— nvidia（19 个模型）。
var presetModelsNvidia = []aidto.ModelEntry{
	{ID: "google/gemma-3-12b-it", DisplayName: "Gemma 3 12B IT", ContextWindow: 131072, MaxOutputTokens: 16384, InputTypes: inputTextImage},
	{ID: "google/gemma-3-4b-it", DisplayName: "Gemma 3 4B IT", ContextWindow: 131072, MaxOutputTokens: 16384, InputTypes: inputTextImage},
	{ID: "meta/llama-3.2-11b-vision-instruct", DisplayName: "Llama 3.2 11b Vision Instruct", ContextWindow: 128000, MaxOutputTokens: 4096, InputTypes: inputTextImage},
	{ID: "meta/llama-3.2-90b-vision-instruct", DisplayName: "Llama-3.2-90B-Vision-Instruct", ContextWindow: 128000, MaxOutputTokens: 8192, InputTypes: inputTextImage},
	{ID: "meta/muse-glimmer-30b", DisplayName: "Muse Glimmer 30B", ContextWindow: 131072, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "mistralai/mistral-7b-instruct-v0.3", DisplayName: "Mistral-7B-Instruct-v0.3", ContextWindow: 65536, MaxOutputTokens: 65536, InputTypes: inputTextOnly},
	{ID: "moonshotai/kimi-k2.6", DisplayName: "Kimi K2.6", ContextWindow: 262144, MaxOutputTokens: 262144, InputTypes: inputTextImage},
	{ID: "moonshotai/kimi-k3", DisplayName: "Kimi K3", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "nvidia/cosmos-reason2-8b", DisplayName: "Cosmos Reason2 8B", ContextWindow: 131072, MaxOutputTokens: 16384, InputTypes: inputTextImage},
	{ID: "nvidia/llama-3.1-nemotron-70b-instruct", DisplayName: "Llama 3.1 Nemotron 70B Instruct", ContextWindow: 128000, MaxOutputTokens: 8192, InputTypes: inputTextOnly},
	{ID: "nvidia/llama-3.1-nemotron-ultra-253b-v1", DisplayName: "Llama 3.1 Nemotron Ultra 253B", ContextWindow: 128000, MaxOutputTokens: 16384, InputTypes: inputTextOnly},
	{ID: "nvidia/nemotron-3-nano-omni-30b-a3b-reasoning", DisplayName: "Nemotron 3 Nano Omni", ContextWindow: 256000, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "nvidia/nemotron-3-super-120b-a12b", DisplayName: "Nemotron 3 Super", ContextWindow: 262144, MaxOutputTokens: 262144, InputTypes: inputTextOnly},
	{ID: "nvidia/nemotron-3-ultra-550b-a55b", DisplayName: "Nemotron 3 Ultra 550B A55B", ContextWindow: 1000000, MaxOutputTokens: 65536, InputTypes: inputTextOnly},
	{ID: "nvidia/nemotron-3.5-lightning-30b-a3b", DisplayName: "Nemotron 3.5 Lightning 30B A3B", ContextWindow: 262144, MaxOutputTokens: 262144, InputTypes: inputTextOnly},
	{ID: "openai/gpt-oss-20b", DisplayName: "GPT OSS 20B", ContextWindow: 131072, MaxOutputTokens: 32768, InputTypes: inputTextOnly},
	{ID: "poolside/laguna-xs-2.1", DisplayName: "Laguna XS 2.1", ContextWindow: 262144, MaxOutputTokens: 16384, InputTypes: inputTextOnly},
	{ID: "z-ai/glm-5.3", DisplayName: "GLM-5.3", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "z-ai/glm-5.3-flash", DisplayName: "GLM-5.3-Flash", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextImage},
}

// presetModelsOpenaiCodex —— openai-codex（8 个模型）。
var presetModelsOpenaiCodex = []aidto.ModelEntry{
	{ID: "gpt-5.3-codex-spark", DisplayName: "GPT-5.3 Codex Spark", ContextWindow: 128000, MaxOutputTokens: 128000, InputTypes: inputTextOnly},
	{ID: "gpt-5.5", DisplayName: "GPT-5.5", ContextWindow: 272000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.6-luna", DisplayName: "GPT-5.6 Luna", ContextWindow: 272000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.6-sol", DisplayName: "GPT-5.6 Sol", ContextWindow: 272000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.6-terra", DisplayName: "GPT-5.6 Terra", ContextWindow: 272000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-6-astra", DisplayName: "GPT-6 Astra", ContextWindow: 272000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-6-luna", DisplayName: "GPT-6 Luna", ContextWindow: 272000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-6-sol", DisplayName: "GPT-6 Sol", ContextWindow: 272000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
}

// presetModelsOpencodeGo —— opencode-go（30 个模型）。
var presetModelsOpencodeGo = []aidto.ModelEntry{
	{ID: "minimax-m3", DisplayName: "MiniMax-M3", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "qwen3.8-flash", DisplayName: "Qwen3.8 Flash", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "deepseek-v4-flash", DisplayName: "DeepSeek V4 Flash", ContextWindow: 1000000, MaxOutputTokens: 384000, InputTypes: inputTextOnly},
	{ID: "deepseek-v4-flash-vision-exp", DisplayName: "DeepSeek V4 Flash Vision Exp", ContextWindow: 1000000, MaxOutputTokens: 384000, InputTypes: inputTextImage},
	{ID: "deepseek-v4-pro", DisplayName: "DeepSeek V4 Pro (New)", ContextWindow: 1000000, MaxOutputTokens: 384000, InputTypes: inputTextOnly},
	{ID: "deepseek-v4.1-flash", DisplayName: "DeepSeek V4.1 Flash", ContextWindow: 1000000, MaxOutputTokens: 384000, InputTypes: inputTextImage},
	{ID: "glm-5.1", DisplayName: "GLM-5.1", ContextWindow: 202752, MaxOutputTokens: 32768, InputTypes: inputTextOnly},
	{ID: "glm-5.2", DisplayName: "GLM-5.2", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "glm-5.3", DisplayName: "GLM-5.3", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "glm-5.3-flash", DisplayName: "GLM-5.3-Flash", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "hy3", DisplayName: "Hy3", ContextWindow: 256000, MaxOutputTokens: 128000, InputTypes: inputTextOnly},
	{ID: "hy4-preview", DisplayName: "Hy4 preview", ContextWindow: 1024000, MaxOutputTokens: 64000, InputTypes: inputTextOnly},
	{ID: "kimi-k2.6", DisplayName: "Kimi K2.6", ContextWindow: 262144, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "kimi-k2.7-code", DisplayName: "Kimi K2.7 Code", ContextWindow: 262144, MaxOutputTokens: 262144, InputTypes: inputTextImage},
	{ID: "kimi-k3", DisplayName: "Kimi K3", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "longcat-2.0", DisplayName: "LongCat-2.0", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "mimo-v2.5", DisplayName: "MiMo V2.5", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "mimo-v2.5-pro", DisplayName: "MiMo V2.5 Pro", ContextWindow: 1048576, MaxOutputTokens: 128000, InputTypes: inputTextOnly},
	{ID: "mimo-v2.6-flash", DisplayName: "MiMo-V2.6-Flash", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "mimo-v2.6-pro", DisplayName: "MiMo-V2.6-Pro", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "minimax-m2.7", DisplayName: "MiniMax-M2.7", ContextWindow: 204800, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "qwen3.6-plus", DisplayName: "Qwen3.6 Plus", ContextWindow: 1000000, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "qwen3.7-max", DisplayName: "Qwen3.7 Max", ContextWindow: 1000000, MaxOutputTokens: 65536, InputTypes: inputTextOnly},
	{ID: "qwen3.7-plus", DisplayName: "Qwen3.7 Plus", ContextWindow: 1000000, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "qwen3.8-max", DisplayName: "Qwen3.8 Max", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "gpt-5.6-luna", DisplayName: "GPT-5.6 Luna", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "grok-4.6", DisplayName: "Grok 4.6", ContextWindow: 500000, MaxOutputTokens: 500000, InputTypes: inputTextImage},
	{ID: "grok-4.7", DisplayName: "Grok 4.7", ContextWindow: 500000, MaxOutputTokens: 500000, InputTypes: inputTextImage},
	{ID: "muse-spark-1.2-contributor", DisplayName: "Muse Spark 1.2 Contributor", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "muse-spark-1.3-contributor", DisplayName: "Muse Spark 1.3 Contributor", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
}

// presetModelsQwenTokenPlan —— qwen-token-plan（20 个模型）。
var presetModelsQwenTokenPlan = []aidto.ModelEntry{
	{ID: "MiniMax-M2.5", DisplayName: "MiniMax-M2.5", ContextWindow: 196608, MaxOutputTokens: 32768, InputTypes: inputTextOnly},
	{ID: "deepseek-v3.2", DisplayName: "DeepSeek V3.2", ContextWindow: 131072, MaxOutputTokens: 65536, InputTypes: inputTextOnly},
	{ID: "deepseek-v4-flash", DisplayName: "DeepSeek V4 Flash", ContextWindow: 1000000, MaxOutputTokens: 384000, InputTypes: inputTextOnly},
	{ID: "deepseek-v4-flash-0731", DisplayName: "DeepSeek V4 Flash 0731", ContextWindow: 1000000, MaxOutputTokens: 384000, InputTypes: inputTextOnly},
	{ID: "deepseek-v4-pro", DisplayName: "DeepSeek V4 Pro", ContextWindow: 1000000, MaxOutputTokens: 384000, InputTypes: inputTextOnly},
	{ID: "deepseek-v4-pro-0813", DisplayName: "DeepSeek V4 Pro 0813", ContextWindow: 1000000, MaxOutputTokens: 384000, InputTypes: inputTextOnly},
	{ID: "deepseek-v4.1-flash", DisplayName: "DeepSeek V4.1 Flash", ContextWindow: 1000000, MaxOutputTokens: 384000, InputTypes: inputTextImage},
	{ID: "glm-5", DisplayName: "GLM-5", ContextWindow: 202752, MaxOutputTokens: 16384, InputTypes: inputTextOnly},
	{ID: "glm-5.1", DisplayName: "GLM-5.1", ContextWindow: 202752, MaxOutputTokens: 128000, InputTypes: inputTextOnly},
	{ID: "glm-5.2", DisplayName: "GLM-5.2", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "glm-5.3", DisplayName: "GLM-5.3", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "kimi-k2.5", DisplayName: "Kimi K2.5", ContextWindow: 262144, MaxOutputTokens: 98304, InputTypes: inputTextImage},
	{ID: "kimi-k2.6", DisplayName: "Kimi K2.6", ContextWindow: 262144, MaxOutputTokens: 262144, InputTypes: inputTextImage},
	{ID: "kimi-k2.7-code", DisplayName: "Kimi K2.7 Code", ContextWindow: 262144, MaxOutputTokens: 262144, InputTypes: inputTextImage},
	{ID: "qwen3.6-flash", DisplayName: "Qwen3.6 Flash", ContextWindow: 1000000, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "qwen3.6-plus", DisplayName: "Qwen3.6 Plus", ContextWindow: 1000000, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "qwen3.7-max", DisplayName: "Qwen3.7 Max", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "qwen3.7-plus", DisplayName: "Qwen3.7 Plus", ContextWindow: 1000000, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "qwen3.8-flash", DisplayName: "Qwen3.8 Flash", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "qwen3.8-max", DisplayName: "Qwen3.8 Max", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextImage},
}

// presetModelsQwenTokenPlanCn —— qwen-token-plan-cn（20 个模型）。
var presetModelsQwenTokenPlanCn = []aidto.ModelEntry{
	{ID: "MiniMax-M2.5", DisplayName: "MiniMax-M2.5", ContextWindow: 196608, MaxOutputTokens: 32768, InputTypes: inputTextOnly},
	{ID: "deepseek-v3.2", DisplayName: "DeepSeek V3.2", ContextWindow: 131072, MaxOutputTokens: 65536, InputTypes: inputTextOnly},
	{ID: "deepseek-v4-flash", DisplayName: "DeepSeek V4 Flash", ContextWindow: 1000000, MaxOutputTokens: 384000, InputTypes: inputTextOnly},
	{ID: "deepseek-v4-flash-0731", DisplayName: "DeepSeek V4 Flash 0731", ContextWindow: 1000000, MaxOutputTokens: 384000, InputTypes: inputTextOnly},
	{ID: "deepseek-v4-pro", DisplayName: "DeepSeek V4 Pro", ContextWindow: 1000000, MaxOutputTokens: 384000, InputTypes: inputTextOnly},
	{ID: "deepseek-v4-pro-0813", DisplayName: "DeepSeek V4 Pro 0813", ContextWindow: 1000000, MaxOutputTokens: 384000, InputTypes: inputTextOnly},
	{ID: "deepseek-v4.1-flash", DisplayName: "DeepSeek V4.1 Flash", ContextWindow: 1000000, MaxOutputTokens: 384000, InputTypes: inputTextImage},
	{ID: "glm-5", DisplayName: "GLM-5", ContextWindow: 202752, MaxOutputTokens: 16384, InputTypes: inputTextOnly},
	{ID: "glm-5.1", DisplayName: "GLM-5.1", ContextWindow: 202752, MaxOutputTokens: 128000, InputTypes: inputTextOnly},
	{ID: "glm-5.2", DisplayName: "GLM-5.2", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "glm-5.3", DisplayName: "GLM-5.3", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "kimi-k2.5", DisplayName: "Kimi K2.5", ContextWindow: 262144, MaxOutputTokens: 98304, InputTypes: inputTextImage},
	{ID: "kimi-k2.6", DisplayName: "Kimi K2.6", ContextWindow: 262144, MaxOutputTokens: 262144, InputTypes: inputTextImage},
	{ID: "kimi-k2.7-code", DisplayName: "Kimi K2.7 Code", ContextWindow: 262144, MaxOutputTokens: 262144, InputTypes: inputTextImage},
	{ID: "qwen3.6-flash", DisplayName: "Qwen3.6 Flash", ContextWindow: 1000000, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "qwen3.6-plus", DisplayName: "Qwen3.6 Plus", ContextWindow: 1000000, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "qwen3.7-max", DisplayName: "Qwen3.7 Max", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "qwen3.7-plus", DisplayName: "Qwen3.7 Plus", ContextWindow: 1000000, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "qwen3.8-flash", DisplayName: "Qwen3.8 Flash", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "qwen3.8-max", DisplayName: "Qwen3.8 Max", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextImage},
}

// presetModelsQwenTokenPlanIndividual —— qwen-token-plan-individual（9 个模型）。
var presetModelsQwenTokenPlanIndividual = []aidto.ModelEntry{
	{ID: "deepseek-v4-flash-0731", DisplayName: "DeepSeek V4 Flash 0731", ContextWindow: 1000000, MaxOutputTokens: 384000, InputTypes: inputTextOnly},
	{ID: "deepseek-v4-pro", DisplayName: "DeepSeek V4 Pro", ContextWindow: 1000000, MaxOutputTokens: 384000, InputTypes: inputTextOnly},
	{ID: "deepseek-v4-pro-0813", DisplayName: "DeepSeek V4 Pro 0813", ContextWindow: 1000000, MaxOutputTokens: 384000, InputTypes: inputTextOnly},
	{ID: "glm-5.2", DisplayName: "GLM-5.2", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "qwen3.6-flash", DisplayName: "Qwen3.6 Flash", ContextWindow: 1000000, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "qwen3.7-max", DisplayName: "Qwen3.7 Max", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "qwen3.7-plus", DisplayName: "Qwen3.7 Plus", ContextWindow: 1000000, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "qwen3.8-flash", DisplayName: "Qwen3.8 Flash", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "qwen3.8-max", DisplayName: "Qwen3.8 Max", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextImage},
}

// presetModelsRadius —— radius（30 个模型）。
var presetModelsRadius = []aidto.ModelEntry{
	{ID: "balanced", DisplayName: "Balanced", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "cheap", DisplayName: "Cheap", ContextWindow: 1000000, MaxOutputTokens: 384000, InputTypes: inputTextOnly},
	{ID: "claude-fable-5", DisplayName: "Claude Fable 5", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "claude-fable-5-1", DisplayName: "Claude Fable 5.1", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "claude-haiku-4-5", DisplayName: "Claude Haiku 4.5", ContextWindow: 200000, MaxOutputTokens: 64000, InputTypes: inputTextImage},
	{ID: "claude-opus-4-5", DisplayName: "Claude Opus 4.5", ContextWindow: 200000, MaxOutputTokens: 64000, InputTypes: inputTextImage},
	{ID: "claude-opus-4-8", DisplayName: "Claude Opus 4.8", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "claude-opus-5", DisplayName: "Claude Opus 5", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "claude-opus-5-5", DisplayName: "Claude Opus 5.5", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "claude-sonnet-4-5", DisplayName: "Claude Sonnet 4.5", ContextWindow: 1000000, MaxOutputTokens: 64000, InputTypes: inputTextImage},
	{ID: "claude-sonnet-5", DisplayName: "Claude Sonnet 5", ContextWindow: 1000000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "deepseek-v4-flash", DisplayName: "DeepSeek V4.1 Flash", ContextWindow: 1000000, MaxOutputTokens: 384000, InputTypes: inputTextOnly},
	{ID: "deepseek-v4-pro", DisplayName: "DeepSeek V4 Pro", ContextWindow: 1000000, MaxOutputTokens: 262144, InputTypes: inputTextOnly},
	{ID: "deepseek-v4.1-flash", DisplayName: "DeepSeek V4.1 Flash", ContextWindow: 1000000, MaxOutputTokens: 384000, InputTypes: inputTextOnly},
	{ID: "glm-5.2", DisplayName: "GLM 5.2", ContextWindow: 432000, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "glm-5.3", DisplayName: "GLM-5.3", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "glm-5.3-flash", DisplayName: "GLM-5.3 Flash", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "gpt-5.3-codex", DisplayName: "GPT 5.3 Codex", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.4", DisplayName: "GPT 5.4", ContextWindow: 272000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.4-mini", DisplayName: "GPT 5.4 Mini", ContextWindow: 400000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.5", DisplayName: "GPT 5.5", ContextWindow: 272000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.6-luna", DisplayName: "GPT 5.6 Luna", ContextWindow: 272000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.6-sol", DisplayName: "GPT 5.6 Sol", ContextWindow: 272000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-5.6-terra", DisplayName: "GPT 5.6 Terra", ContextWindow: 272000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-6-astra", DisplayName: "GPT 6 Astra", ContextWindow: 272000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-6-luna", DisplayName: "GPT 6 Luna", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "gpt-6-sol", DisplayName: "GPT 6 Sol", ContextWindow: 1050000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
	{ID: "kimi-k2.7-code", DisplayName: "Kimi K2.7 Code", ContextWindow: 262000, MaxOutputTokens: 262000, InputTypes: inputTextImage},
	{ID: "kimi-k3", DisplayName: "Kimi K3", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "precise", DisplayName: "Precise", ContextWindow: 272000, MaxOutputTokens: 128000, InputTypes: inputTextImage},
}

// presetModelsTogether —— together（22 个模型）。
var presetModelsTogether = []aidto.ModelEntry{
	{ID: "MiniMaxAI/MiniMax-M2.7", DisplayName: "MiniMax-M2.7", ContextWindow: 202752, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "MiniMaxAI/MiniMax-M3", DisplayName: "MiniMax-M3", ContextWindow: 524288, MaxOutputTokens: 250000, InputTypes: inputTextImage},
	{ID: "Qwen/Qwen2.5-7B-Instruct-Turbo", DisplayName: "Qwen 2.5 7B Instruct Turbo", ContextWindow: 32768, MaxOutputTokens: 32768, InputTypes: inputTextOnly},
	{ID: "Qwen/Qwen3.5-9B", DisplayName: "Qwen3.5 9B", ContextWindow: 262144, MaxOutputTokens: 65536, InputTypes: inputTextImage},
	{ID: "Qwen/Qwen3.6-Plus", DisplayName: "Qwen3.6 Plus", ContextWindow: 1000000, MaxOutputTokens: 500000, InputTypes: inputTextOnly},
	{ID: "Qwen/Qwen3.7-Max", DisplayName: "Qwen3.7 Max", ContextWindow: 1000000, MaxOutputTokens: 500000, InputTypes: inputTextOnly},
	{ID: "deepseek-ai/DeepSeek-V4-Flash-0731", DisplayName: "DeepSeek V4 Flash 0731", ContextWindow: 1000000, MaxOutputTokens: 384000, InputTypes: inputTextOnly},
	{ID: "deepseek-ai/DeepSeek-V4-Pro", DisplayName: "DeepSeek V4 Pro", ContextWindow: 512000, MaxOutputTokens: 384000, InputTypes: inputTextOnly},
	{ID: "deepseek-ai/DeepSeek-V4-Pro-0813", DisplayName: "DeepSeek V4 Pro 0813", ContextWindow: 1048576, MaxOutputTokens: 384000, InputTypes: inputTextOnly},
	{ID: "deepseek-ai/DeepSeek-V4.1-Flash", DisplayName: "DeepSeek V4.1 Flash", ContextWindow: 1048576, MaxOutputTokens: 384000, InputTypes: inputTextImage},
	{ID: "google/gemma-4-31B-it", DisplayName: "Gemma 4 31B Instruct", ContextWindow: 262144, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "meta-llama/Llama-3.3-70B-Instruct-Turbo", DisplayName: "Llama 3.3 70B", ContextWindow: 131072, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "moonshotai/Kimi-K2.6", DisplayName: "Kimi K2.6", ContextWindow: 262144, MaxOutputTokens: 131000, InputTypes: inputTextImage},
	{ID: "moonshotai/Kimi-K2.7-Code", DisplayName: "Kimi K2.7 Code", ContextWindow: 262144, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "moonshotai/Kimi-K3", DisplayName: "Kimi K3", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "nvidia/nemotron-3-ultra-550b-a55b", DisplayName: "Nemotron 3 Ultra 550B A55B", ContextWindow: 512300, MaxOutputTokens: 512300, InputTypes: inputTextOnly},
	{ID: "openai/gpt-oss-120b", DisplayName: "GPT OSS 120B", ContextWindow: 131072, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "openai/gpt-oss-20b", DisplayName: "GPT OSS 20B", ContextWindow: 131072, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "thinkingmachines/Inkling", DisplayName: "Inkling", ContextWindow: 524288, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "zai-org/GLM-5.2", DisplayName: "GLM-5.2", ContextWindow: 512000, MaxOutputTokens: 164000, InputTypes: inputTextOnly},
	{ID: "zai-org/GLM-5.3", DisplayName: "GLM-5.3", ContextWindow: 1048576, MaxOutputTokens: 262144, InputTypes: inputTextOnly},
	{ID: "zai-org/GLM-5.3-Flash", DisplayName: "GLM-5.3-Flash", ContextWindow: 1048575, MaxOutputTokens: 400000, InputTypes: inputTextImage},
}

// presetModelsXai —— xai（4 个模型）。
var presetModelsXai = []aidto.ModelEntry{
	{ID: "grok-4.3", DisplayName: "Grok 4.3", ContextWindow: 1000000, MaxOutputTokens: 30000, InputTypes: inputTextImage},
	{ID: "grok-4.5", DisplayName: "Grok 4.5", ContextWindow: 500000, MaxOutputTokens: 500000, InputTypes: inputTextImage},
	{ID: "grok-4.6", DisplayName: "Grok 4.6", ContextWindow: 500000, MaxOutputTokens: 500000, InputTypes: inputTextImage},
	{ID: "grok-4.7", DisplayName: "Grok 4.7", ContextWindow: 500000, MaxOutputTokens: 500000, InputTypes: inputTextImage},
}

// presetModelsXiaomi —— xiaomi（6 个模型）。
var presetModelsXiaomi = []aidto.ModelEntry{
	{ID: "mimo-v2.5", DisplayName: "MiMo-V2.5", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "mimo-v2.5-pro", DisplayName: "MiMo-V2.5-Pro", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "mimo-v2.5-pro-ultraspeed", DisplayName: "MiMo-V2.5-Pro-UltraSpeed", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "mimo-v2.6-flash", DisplayName: "MiMo-V2.6-Flash", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "mimo-v2.6-pro", DisplayName: "MiMo-V2.6-Pro", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "mimo-v2.6-pro-ultraspeed", DisplayName: "MiMo-V2.6-Pro-UltraSpeed", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
}

// presetModelsXiaomiTokenPlanAms —— xiaomi-token-plan-ams（4 个模型）。
var presetModelsXiaomiTokenPlanAms = []aidto.ModelEntry{
	{ID: "mimo-v2.5", DisplayName: "MiMo-V2.5", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "mimo-v2.5-pro", DisplayName: "MiMo-V2.5-Pro", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "mimo-v2.6-flash", DisplayName: "MiMo-V2.6-Flash", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "mimo-v2.6-pro", DisplayName: "MiMo-V2.6-Pro", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
}

// presetModelsXiaomiTokenPlanCn —— xiaomi-token-plan-cn（4 个模型）。
var presetModelsXiaomiTokenPlanCn = []aidto.ModelEntry{
	{ID: "mimo-v2.5", DisplayName: "MiMo-V2.5", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "mimo-v2.5-pro", DisplayName: "MiMo-V2.5-Pro", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "mimo-v2.6-flash", DisplayName: "MiMo-V2.6-Flash", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "mimo-v2.6-pro", DisplayName: "MiMo-V2.6-Pro", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
}

// presetModelsXiaomiTokenPlanSgp —— xiaomi-token-plan-sgp（4 个模型）。
var presetModelsXiaomiTokenPlanSgp = []aidto.ModelEntry{
	{ID: "mimo-v2.5", DisplayName: "MiMo-V2.5", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "mimo-v2.5-pro", DisplayName: "MiMo-V2.5-Pro", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "mimo-v2.6-flash", DisplayName: "MiMo-V2.6-Flash", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "mimo-v2.6-pro", DisplayName: "MiMo-V2.6-Pro", ContextWindow: 1048576, MaxOutputTokens: 131072, InputTypes: inputTextImage},
}

// presetModelsZai —— zai（7 个模型）。
var presetModelsZai = []aidto.ModelEntry{
	{ID: "glm-4.7", DisplayName: "GLM-4.7", ContextWindow: 204800, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "glm-5-turbo", DisplayName: "GLM-5-Turbo", ContextWindow: 200000, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "glm-5.2", DisplayName: "GLM-5.2", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "glm-5.2-highspeed", DisplayName: "GLM-5.2 Highspeed", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "glm-5.3", DisplayName: "GLM-5.3", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "glm-5.3-flash", DisplayName: "GLM-5.3-Flash", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "glm-5.3-highspeed", DisplayName: "GLM-5.3 Highspeed", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
}

// presetModelsZaiCodingCn —— zai-coding-cn（4 个模型）。
var presetModelsZaiCodingCn = []aidto.ModelEntry{
	{ID: "glm-4.6v", DisplayName: "GLM-4.6V", ContextWindow: 128000, MaxOutputTokens: 32768, InputTypes: inputTextImage},
	{ID: "glm-5.3", DisplayName: "GLM-5.3", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
	{ID: "glm-5.3-flash", DisplayName: "GLM-5.3-Flash", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextImage},
	{ID: "glm-5.3-highspeed", DisplayName: "GLM-5.3 Highspeed", ContextWindow: 1000000, MaxOutputTokens: 131072, InputTypes: inputTextOnly},
}
