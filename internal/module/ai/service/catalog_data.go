// catalog_data.go — AI 供应商内置预设目录：类型定义 + 供应商表。
//
// 数据来源：dsh 内置的 pi-ai 供应商预置包
//
//	@earendil-works/pi-ai/dist/providers/data/*.json（.manifest.json schemaVersion 3，
//	generatedAt 2026-09-22T19:31:44.346Z）：41 家供应商、1495 个模型。
//
// 文件分工：
//
//	catalog_data.go             —— 本文件：类型 + 供应商表（展示名、协议端点、模型清单引用）
//	catalog_data_models.go      —— 中小型供应商的模型清单
//	catalog_data_models_bulk.go —— 大目录供应商（聚合平台 / 托管平台，30 个模型以上）的模型清单
//	ai_model_catalog.go         —— 只留读取与合并逻辑，不再硬编码目录数据
//
// 模型 id / 展示名 / 上下文窗口 / 最大输出 token / 输入类型逐字段原样搬运，
// **不做前缀加工**：openrouter 的 deepseek/deepseek-v4.1-flash 与 opencode 的 muse-spark-1.3-contributor
// 都是各家服务端自己的形态，客户端既不添加也不剥离。「用的是哪一家」由 provider_key 这一维承担。
//
// 协议取值：dsh 用连字符串（openai-completions …），本模块用下划线枚举，经 aienums.DSHProtocolAlias
// 翻译。未登记的 dsh 协议（bedrock-converse-stream / azure-openai-responses / google-vertex /
// mistral-conversations / openai-codex-responses / pi-messages）在本模块没有实现 → Protocol 留空，
// 由管理员在界面上自选（禁止静默回落成默认协议）。
package aiservice

import (
	"sort"
	"strings"

	aidto "go_wp/internal/module/ai/dto"
	aienums "go_wp/internal/module/ai/enums"
)

// presetEndpoint 是一家供应商的一个协议端点：同一家的不同协议分组地址可能不同
// （openrouter 的 anthropic-messages 是 https://openrouter.ai/api，openai-completions 是
// https://openrouter.ai/api/v1），所以地址挂在端点上而不是供应商上。
type presetEndpoint struct {
	DSHAPI   string // dsh 的协议字符串原文
	Protocol string // 本模块协议枚举；未登记协议为空串
	BaseURL  string // 该协议分组的默认地址
}

// presetProvider 是一家内置预设供应商。
type presetProvider struct {
	Key         string // provider_key（与 dsh 的文件名一致）
	DisplayName string // 默认展示名（用户可改）
	Endpoints   []presetEndpoint
	Models      []aidto.ModelEntry
}

// 输入类型集合：模型行共享这两个只读切片，BuiltinModels 返回时逐条深拷贝。
var (
	inputTextOnly  = []string{aienums.InputTypeText}
	inputTextImage = []string{aienums.InputTypeText, aienums.InputTypeImage}
)

// primaryEndpoint 返回默认端点：按本模块已实现协议的优先级
// （openai_chat_completions > anthropic_messages > openai_responses > gemini_generate_content）
// 取第一个可选中的协议分组。
//
// 一家都没有已映射协议时（amazon-bedrock / azure-openai-responses / google-vertex / mistral /
// openai-codex / radius）回退到第一个端点、只取它的地址：**协议为空不等于没有地址** ——
// 直接把默认地址一起丢掉，界面上会显示成「这家没有内置默认地址」，用户于是手填一个错地址，
// 而真相只是「这家协议要自己选」。
func (p presetProvider) primaryEndpoint() presetEndpoint {
	for _, want := range []string{
		aienums.ProtocolOpenAIChatCompletions,
		aienums.ProtocolAnthropicMessages,
		aienums.ProtocolOpenAIResponses,
		aienums.ProtocolGeminiGenerateContent,
	} {
		for _, ep := range p.Endpoints {
			if ep.Protocol == want {
				return ep
			}
		}
	}
	for _, ep := range p.Endpoints {
		return presetEndpoint{DSHAPI: ep.DSHAPI, BaseURL: ep.BaseURL}
	}
	return presetEndpoint{}
}

// BuiltinPresetOption 是内置预设暴露给界面的元信息（不含任何密钥）。
type BuiltinPresetOption struct {
	Key         string
	DisplayName string
	BaseURL     string // 默认端点地址（dsh 侧没给地址的家为空，见下）
	Protocol    string // 默认协议（未登记协议时为空，界面要求用户自选）
	ModelCount  int
}

// BuiltinPresets 返回全部内置预设（按 key 字典序），供「第三方模型提供商」下拉使用。
func BuiltinPresets() []BuiltinPresetOption {
	out := make([]BuiltinPresetOption, 0, len(presetProviders))
	for _, p := range presetProviders {
		ep := p.primaryEndpoint()
		out = append(out, BuiltinPresetOption{
			Key:         p.Key,
			DisplayName: p.DisplayName,
			BaseURL:     ep.BaseURL,
			Protocol:    ep.Protocol,
			ModelCount:  len(p.Models),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// BuiltinPreset 按 provider_key 取一家内置预设（key 归一大小写与空白；未登记回 false）。
//
// 页面「第三方模型提供商」提交时用它兜底两处默认值：展示名留空 → 用预设展示名；
// 协议留空 → 用预设协议。未登记协议的家（amazon-bedrock 等）预设协议本身为空，
// 兜底后仍为空、由服务端校验拒绝 —— 不静默回落成默认协议。
func BuiltinPreset(providerKey string) (opt BuiltinPresetOption, ok bool) {
	key := strings.ToLower(strings.TrimSpace(providerKey))
	for _, p := range presetProviders {
		if p.Key != key {
			continue
		}
		ep := p.primaryEndpoint()
		return BuiltinPresetOption{
			Key:         p.Key,
			DisplayName: p.DisplayName,
			BaseURL:     ep.BaseURL,
			Protocol:    ep.Protocol,
			ModelCount:  len(p.Models),
		}, true
	}
	return BuiltinPresetOption{}, false
}

// presetProviders 内置预设表：41 家供应商、1495 个模型（生成数据，勿手改）。
var presetProviders = []presetProvider{
	{
		Key:         "amazon-bedrock",
		DisplayName: "Amazon Bedrock",
		Endpoints: []presetEndpoint{
			// dsh 里同一协议按区域给了两个地址；默认只登记 us-east-1，其它区域由管理员在界面上改。
			{DSHAPI: "bedrock-converse-stream", Protocol: "", BaseURL: "https://bedrock-runtime.us-east-1.amazonaws.com"},
		},
		Models: presetModelsAmazonBedrock,
	},
	{
		Key:         "ant-ling",
		DisplayName: "Ant Ling",
		Endpoints: []presetEndpoint{
			{DSHAPI: "openai-completions", Protocol: aienums.ProtocolOpenAIChatCompletions, BaseURL: "https://api.ant-ling.com/v1"},
		},
		Models: presetModelsAntLing,
	},
	{
		Key:         "anthropic",
		DisplayName: "Anthropic",
		Endpoints: []presetEndpoint{
			{DSHAPI: "anthropic-messages", Protocol: aienums.ProtocolAnthropicMessages, BaseURL: "https://api.anthropic.com"},
		},
		Models: presetModelsAnthropic,
	},
	{
		Key:         "azure-openai-responses",
		DisplayName: "Azure OpenAI",
		Endpoints: []presetEndpoint{
			{DSHAPI: "azure-openai-responses", Protocol: "", BaseURL: ""},
		},
		Models: presetModelsAzureOpenaiResponses,
	},
	{
		Key:         "baseten",
		DisplayName: "Baseten",
		Endpoints: []presetEndpoint{
			{DSHAPI: "openai-completions", Protocol: aienums.ProtocolOpenAIChatCompletions, BaseURL: "https://inference.baseten.co/v1"},
		},
		Models: presetModelsBaseten,
	},
	{
		Key:         "cerebras",
		DisplayName: "Cerebras",
		Endpoints: []presetEndpoint{
			{DSHAPI: "openai-completions", Protocol: aienums.ProtocolOpenAIChatCompletions, BaseURL: "https://api.cerebras.ai/v1"},
		},
		Models: presetModelsCerebras,
	},
	{
		Key:         "cloudflare-ai-gateway",
		DisplayName: "Cloudflare AI Gateway",
		Endpoints: []presetEndpoint{
			{DSHAPI: "anthropic-messages", Protocol: aienums.ProtocolAnthropicMessages, BaseURL: "https://gateway.ai.cloudflare.com/v1/{CLOUDFLARE_ACCOUNT_ID}/{CLOUDFLARE_GATEWAY_ID}/anthropic"},
			{DSHAPI: "openai-completions", Protocol: aienums.ProtocolOpenAIChatCompletions, BaseURL: "https://gateway.ai.cloudflare.com/v1/{CLOUDFLARE_ACCOUNT_ID}/{CLOUDFLARE_GATEWAY_ID}/compat"},
			{DSHAPI: "openai-responses", Protocol: aienums.ProtocolOpenAIResponses, BaseURL: "https://gateway.ai.cloudflare.com/v1/{CLOUDFLARE_ACCOUNT_ID}/{CLOUDFLARE_GATEWAY_ID}/openai"},
		},
		Models: presetModelsCloudflareAiGateway,
	},
	{
		Key:         "cloudflare-workers-ai",
		DisplayName: "Cloudflare Workers AI",
		Endpoints: []presetEndpoint{
			{DSHAPI: "openai-completions", Protocol: aienums.ProtocolOpenAIChatCompletions, BaseURL: "https://api.cloudflare.com/client/v4/accounts/{CLOUDFLARE_ACCOUNT_ID}/ai/v1"},
		},
		Models: presetModelsCloudflareWorkersAi,
	},
	{
		Key:         "deepseek",
		DisplayName: "DeepSeek",
		Endpoints: []presetEndpoint{
			{DSHAPI: "openai-completions", Protocol: aienums.ProtocolOpenAIChatCompletions, BaseURL: "https://api.deepseek.com"},
		},
		Models: presetModelsDeepseek,
	},
	{
		Key:         "fireworks",
		DisplayName: "Fireworks AI",
		Endpoints: []presetEndpoint{
			{DSHAPI: "anthropic-messages", Protocol: aienums.ProtocolAnthropicMessages, BaseURL: "https://api.fireworks.ai/inference"},
			{DSHAPI: "openai-completions", Protocol: aienums.ProtocolOpenAIChatCompletions, BaseURL: "https://api.fireworks.ai/inference/v1"},
		},
		Models: presetModelsFireworks,
	},
	{
		Key:         "github-copilot",
		DisplayName: "GitHub Copilot",
		Endpoints: []presetEndpoint{
			{DSHAPI: "anthropic-messages", Protocol: aienums.ProtocolAnthropicMessages, BaseURL: "https://api.individual.githubcopilot.com"},
			{DSHAPI: "openai-completions", Protocol: aienums.ProtocolOpenAIChatCompletions, BaseURL: "https://api.individual.githubcopilot.com"},
			{DSHAPI: "openai-responses", Protocol: aienums.ProtocolOpenAIResponses, BaseURL: "https://api.individual.githubcopilot.com"},
		},
		Models: presetModelsGithubCopilot,
	},
	{
		Key:         "google",
		DisplayName: "Google Gemini",
		Endpoints: []presetEndpoint{
			{DSHAPI: "google-generative-ai", Protocol: aienums.ProtocolGeminiGenerateContent, BaseURL: "https://generativelanguage.googleapis.com/v1beta"},
		},
		Models: presetModelsGoogle,
	},
	{
		Key:         "google-vertex",
		DisplayName: "Google Vertex AI",
		Endpoints: []presetEndpoint{
			{DSHAPI: "google-vertex", Protocol: "", BaseURL: "https://{location}-aiplatform.googleapis.com"},
		},
		Models: presetModelsGoogleVertex,
	},
	{
		Key:         "groq",
		DisplayName: "Groq",
		Endpoints: []presetEndpoint{
			{DSHAPI: "openai-completions", Protocol: aienums.ProtocolOpenAIChatCompletions, BaseURL: "https://api.groq.com/openai/v1"},
		},
		Models: presetModelsGroq,
	},
	{
		Key:         "huggingface",
		DisplayName: "Hugging Face",
		Endpoints: []presetEndpoint{
			{DSHAPI: "openai-completions", Protocol: aienums.ProtocolOpenAIChatCompletions, BaseURL: "https://router.huggingface.co/v1"},
		},
		Models: presetModelsHuggingface,
	},
	{
		Key:         "kimi-coding",
		DisplayName: "Kimi Coding",
		Endpoints: []presetEndpoint{
			{DSHAPI: "anthropic-messages", Protocol: aienums.ProtocolAnthropicMessages, BaseURL: "https://api.kimi.com/coding"},
		},
		Models: presetModelsKimiCoding,
	},
	{
		Key:         "meta",
		DisplayName: "Meta",
		Endpoints: []presetEndpoint{
			{DSHAPI: "openai-responses", Protocol: aienums.ProtocolOpenAIResponses, BaseURL: "https://api.meta.ai/v1"},
		},
		Models: presetModelsMeta,
	},
	{
		Key:         "minimax",
		DisplayName: "MiniMax",
		Endpoints: []presetEndpoint{
			{DSHAPI: "anthropic-messages", Protocol: aienums.ProtocolAnthropicMessages, BaseURL: "https://api.minimax.io/anthropic"},
		},
		Models: presetModelsMinimax,
	},
	{
		Key:         "minimax-cn",
		DisplayName: "MiniMax (China)",
		Endpoints: []presetEndpoint{
			{DSHAPI: "anthropic-messages", Protocol: aienums.ProtocolAnthropicMessages, BaseURL: "https://api.minimaxi.com/anthropic"},
		},
		Models: presetModelsMinimaxCn,
	},
	{
		Key:         "mistral",
		DisplayName: "Mistral AI",
		Endpoints: []presetEndpoint{
			{DSHAPI: "mistral-conversations", Protocol: "", BaseURL: "https://api.mistral.ai"},
		},
		Models: presetModelsMistral,
	},
	{
		Key:         "moonshotai",
		DisplayName: "Moonshot AI",
		Endpoints: []presetEndpoint{
			{DSHAPI: "openai-completions", Protocol: aienums.ProtocolOpenAIChatCompletions, BaseURL: "https://api.moonshot.ai/v1"},
		},
		Models: presetModelsMoonshotai,
	},
	{
		Key:         "moonshotai-cn",
		DisplayName: "Moonshot AI (China)",
		Endpoints: []presetEndpoint{
			{DSHAPI: "openai-completions", Protocol: aienums.ProtocolOpenAIChatCompletions, BaseURL: "https://api.moonshot.cn/v1"},
		},
		Models: presetModelsMoonshotaiCn,
	},
	{
		Key:         "nvidia",
		DisplayName: "NVIDIA",
		Endpoints: []presetEndpoint{
			{DSHAPI: "openai-completions", Protocol: aienums.ProtocolOpenAIChatCompletions, BaseURL: "https://integrate.api.nvidia.com/v1"},
		},
		Models: presetModelsNvidia,
	},
	{
		Key:         "openai",
		DisplayName: "OpenAI",
		Endpoints: []presetEndpoint{
			{DSHAPI: "openai-responses", Protocol: aienums.ProtocolOpenAIResponses, BaseURL: "https://api.openai.com/v1"},
		},
		Models: presetModelsOpenai,
	},
	{
		Key:         "openai-codex",
		DisplayName: "OpenAI Codex",
		Endpoints: []presetEndpoint{
			{DSHAPI: "openai-codex-responses", Protocol: "", BaseURL: "https://chatgpt.com/backend-api"},
		},
		Models: presetModelsOpenaiCodex,
	},
	{
		Key:         "opencode",
		DisplayName: "OpenCode Zen",
		Endpoints: []presetEndpoint{
			{DSHAPI: "anthropic-messages", Protocol: aienums.ProtocolAnthropicMessages, BaseURL: "https://opencode.ai/zen"},
			{DSHAPI: "google-generative-ai", Protocol: aienums.ProtocolGeminiGenerateContent, BaseURL: "https://opencode.ai/zen/v1"},
			{DSHAPI: "openai-completions", Protocol: aienums.ProtocolOpenAIChatCompletions, BaseURL: "https://opencode.ai/zen/v1"},
			{DSHAPI: "openai-responses", Protocol: aienums.ProtocolOpenAIResponses, BaseURL: "https://opencode.ai/zen/v1"},
		},
		Models: presetModelsOpencode,
	},
	{
		Key:         "opencode-go",
		DisplayName: "OpenCode Zen Go",
		Endpoints: []presetEndpoint{
			{DSHAPI: "anthropic-messages", Protocol: aienums.ProtocolAnthropicMessages, BaseURL: "https://opencode.ai/zen/go"},
			{DSHAPI: "openai-completions", Protocol: aienums.ProtocolOpenAIChatCompletions, BaseURL: "https://opencode.ai/zen/go/v1"},
			{DSHAPI: "openai-responses", Protocol: aienums.ProtocolOpenAIResponses, BaseURL: "https://opencode.ai/zen/go/v1"},
		},
		Models: presetModelsOpencodeGo,
	},
	{
		Key:         "openrouter",
		DisplayName: "OpenRouter",
		Endpoints: []presetEndpoint{
			{DSHAPI: "anthropic-messages", Protocol: aienums.ProtocolAnthropicMessages, BaseURL: "https://openrouter.ai/api"},
			{DSHAPI: "openai-completions", Protocol: aienums.ProtocolOpenAIChatCompletions, BaseURL: "https://openrouter.ai/api/v1"},
		},
		Models: presetModelsOpenrouter,
	},
	{
		Key:         "qwen-token-plan",
		DisplayName: "Qwen Token Plan",
		Endpoints: []presetEndpoint{
			{DSHAPI: "openai-completions", Protocol: aienums.ProtocolOpenAIChatCompletions, BaseURL: "https://token-plan.ap-southeast-1.maas.aliyuncs.com/compatible-mode/v1"},
		},
		Models: presetModelsQwenTokenPlan,
	},
	{
		Key:         "qwen-token-plan-cn",
		DisplayName: "Qwen Token Plan (China)",
		Endpoints: []presetEndpoint{
			{DSHAPI: "openai-completions", Protocol: aienums.ProtocolOpenAIChatCompletions, BaseURL: "https://token-plan.cn-beijing.maas.aliyuncs.com/compatible-mode/v1"},
		},
		Models: presetModelsQwenTokenPlanCn,
	},
	{
		Key:         "qwen-token-plan-individual",
		DisplayName: "Qwen Token Plan (Individual)",
		Endpoints: []presetEndpoint{
			{DSHAPI: "openai-completions", Protocol: aienums.ProtocolOpenAIChatCompletions, BaseURL: "https://token-plan.ap-southeast-1.maas.aliyuncs.com/compatible-mode/v1"},
		},
		Models: presetModelsQwenTokenPlanIndividual,
	},
	{
		Key:         "radius",
		DisplayName: "Radius",
		Endpoints: []presetEndpoint{
			{DSHAPI: "pi-messages", Protocol: "", BaseURL: "https://radius.pi.dev/v1"},
		},
		Models: presetModelsRadius,
	},
	{
		Key:         "together",
		DisplayName: "Together AI",
		Endpoints: []presetEndpoint{
			{DSHAPI: "openai-completions", Protocol: aienums.ProtocolOpenAIChatCompletions, BaseURL: "https://api.together.ai/v1"},
		},
		Models: presetModelsTogether,
	},
	{
		Key:         "vercel-ai-gateway",
		DisplayName: "Vercel AI Gateway",
		Endpoints: []presetEndpoint{
			{DSHAPI: "anthropic-messages", Protocol: aienums.ProtocolAnthropicMessages, BaseURL: "https://ai-gateway.vercel.sh"},
		},
		Models: presetModelsVercelAiGateway,
	},
	{
		Key:         "xai",
		DisplayName: "xAI",
		Endpoints: []presetEndpoint{
			{DSHAPI: "openai-responses", Protocol: aienums.ProtocolOpenAIResponses, BaseURL: "https://api.x.ai/v1"},
		},
		Models: presetModelsXai,
	},
	{
		Key:         "xiaomi",
		DisplayName: "Xiaomi MiMo",
		Endpoints: []presetEndpoint{
			{DSHAPI: "openai-completions", Protocol: aienums.ProtocolOpenAIChatCompletions, BaseURL: "https://api.xiaomimimo.com/v1"},
		},
		Models: presetModelsXiaomi,
	},
	{
		Key:         "xiaomi-token-plan-ams",
		DisplayName: "Xiaomi MiMo Token Plan (AMS)",
		Endpoints: []presetEndpoint{
			{DSHAPI: "openai-completions", Protocol: aienums.ProtocolOpenAIChatCompletions, BaseURL: "https://token-plan-ams.xiaomimimo.com/v1"},
		},
		Models: presetModelsXiaomiTokenPlanAms,
	},
	{
		Key:         "xiaomi-token-plan-cn",
		DisplayName: "Xiaomi MiMo Token Plan (China)",
		Endpoints: []presetEndpoint{
			{DSHAPI: "openai-completions", Protocol: aienums.ProtocolOpenAIChatCompletions, BaseURL: "https://token-plan-cn.xiaomimimo.com/v1"},
		},
		Models: presetModelsXiaomiTokenPlanCn,
	},
	{
		Key:         "xiaomi-token-plan-sgp",
		DisplayName: "Xiaomi MiMo Token Plan (SGP)",
		Endpoints: []presetEndpoint{
			{DSHAPI: "openai-completions", Protocol: aienums.ProtocolOpenAIChatCompletions, BaseURL: "https://token-plan-sgp.xiaomimimo.com/v1"},
		},
		Models: presetModelsXiaomiTokenPlanSgp,
	},
	{
		Key:         "zai",
		DisplayName: "Z.ai",
		Endpoints: []presetEndpoint{
			{DSHAPI: "openai-completions", Protocol: aienums.ProtocolOpenAIChatCompletions, BaseURL: "https://api.z.ai/api/coding/paas/v4"},
		},
		Models: presetModelsZai,
	},
	{
		Key:         "zai-coding-cn",
		DisplayName: "Z.ai Coding (China)",
		Endpoints: []presetEndpoint{
			{DSHAPI: "openai-completions", Protocol: aienums.ProtocolOpenAIChatCompletions, BaseURL: "https://open.bigmodel.cn/api/coding/paas/v4"},
		},
		Models: presetModelsZaiCodingCn,
	},
}
