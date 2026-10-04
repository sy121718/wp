package aienums

import "strings"

// ai_msg.go — ai 模块的枚举与响应文案（handle / service 不硬编码文案，一律取这里）。
//
// 取值是 **i18n key** 而不是中文原文（与 webhook / mail / sysconfig 一致）：
// pkg/response.ErrorAuto 按「值是不是 key 形态」判定业务错误 —— 中文原文会被判成
// 内部错误而返回 500 + 通用文案；pkg/response.translate 按请求语言查 sys_i18n，
// 未命中原样返回 key（可见的降级）。
//
// 词条由迁移 513（register_ai_i18n.go）seed；FacingMessages 的键值对是**未接词条时的
// 中文兜底**（页面侧渲染用，见 inbound/http/ai_err.go），两处的中文必须逐字一致。
const (
	MsgSaved          = "ai.msg.saved"
	MsgDeleted        = "ai.msg.deleted"
	MsgStatusChanged  = "ai.msg.statusChanged"
	MsgModelsSaved    = "ai.msg.modelsSaved"
	MsgModelsRestored = "ai.msg.modelsRestored"
	MsgModelsFetched  = "ai.msg.modelsFetched"
	// MsgModelsFetchedDetail 带计数的「获取可用模型」回执：`{n}` 拉回数、`{m}` 新增数，
	// 由 FormatFacing 填充（PRG 的 query 回执与 HTMX 片段共用同一条 key）。
	MsgModelsFetchedDetail = "ai.msg.modelsFetchedDetail"

	ErrInvalidParam        = "ai.err.invalidParam"
	ErrProviderNotFound    = "ai.err.providerNotFound"
	ErrProviderKeyRequired = "ai.err.providerKeyRequired"
	ErrProviderKeyExists   = "ai.err.providerKeyExists"
	ErrDisplayNameRequired = "ai.err.displayNameRequired"
	// ErrAPIKeyRequiredOnBaseURLChange 改 API 地址却没给新密钥（旧的不能跟着新地址走）。
	ErrAPIKeyRequiredOnBaseURLChange = "ai.err.apiKeyRequiredOnBaseUrlChange"
	ErrVersionRequired               = "ai.err.versionRequired"
	ErrVersionConflict               = "ai.err.versionConflict"
	ErrProtocolUnsupported           = "ai.err.protocolUnsupported"
	ErrCipherUnavailable             = "ai.err.cipherUnavailable"
	ErrNoBuiltinModels               = "ai.err.noBuiltinModels"
	ErrBaseURLRequired               = "ai.err.baseUrlRequired"
	ErrModelIDRequired               = "ai.err.modelIdRequired"
	ErrModelIDDuplicated             = "ai.err.modelIdDuplicated"
	ErrModelsFetchFailed             = "ai.err.modelsFetchFailed"

	// 出站目标（SSRF 防护，口径同 webhook 的 SEC-015）：校验失败是**用户可纠正的
	// 输入问题**，必须是业务错误（key 形态）—— 用中文会被 ErrorAuto 判成内部错误，
	// 用户填了个内网地址却看到「服务器内部错误」。
	ErrURLMalformed         = "ai.err.urlMalformed"
	ErrURLSchemeUnsupported = "ai.err.urlSchemeUnsupported"
	ErrURLHostMissing       = "ai.err.urlHostMissing"
	ErrURLUnresolvable      = "ai.err.urlUnresolvable"
	ErrURLDenied            = "ai.err.urlDenied"

	// ErrInternal 归口：未登记的底层错误在页面上统一显示它（原文只进日志）。
	ErrInternal = "ai.err.internal"
)

// FacingMessages 面向用户的文案白名单：**只有登记在这里的 key** 才会被页面原样显示，
// 未登记的一律归口到 ErrInternal（避免把底层错误串渲染到界面上）。
//
// 值是与 enums key 逐字对应的中文兜底（词条缺失时的可见降级）。
var FacingMessages = map[string]string{
	MsgSaved:          "已保存",
	MsgDeleted:        "已删除",
	MsgStatusChanged:  "状态已更新",
	MsgModelsSaved:    "模型目录已保存",
	MsgModelsRestored: "已恢复默认模型",
	MsgModelsFetched:  "已获取可用模型",

	MsgModelsFetchedDetail: "已拉回 {n} 个可用模型，新增 {m} 个",

	ErrInvalidParam:                  "请求参数不正确",
	ErrProviderNotFound:              "供应商不存在或已被删除",
	ErrProviderKeyRequired:           "请填写供应商标识（如 openai、deepseek）",
	ErrProviderKeyExists:             "该供应商标识已存在",
	ErrDisplayNameRequired:           "请填写显示名称",
	ErrAPIKeyRequiredOnBaseURLChange: "更换 API 地址后需要重新填写密钥",
	ErrVersionRequired:               "缺少版本号，请刷新页面后重试",
	ErrVersionConflict:               "该供应商已被其他人修改，请刷新页面后重试",
	ErrProtocolUnsupported:           "不支持的 API 协议",
	ErrCipherUnavailable:             "未配置加密密钥，无法保存 API 密钥",
	ErrNoBuiltinModels:               "该供应商没有内置默认模型",
	ErrBaseURLRequired:               "请先填写 API 地址，再获取可用模型",
	ErrModelIDRequired:               "模型 ID 不能为空",
	ErrModelIDDuplicated:             "模型 ID 重复，请检查后再保存",
	ErrModelsFetchFailed:             "获取可用模型失败，请检查 API 地址与密钥",

	ErrURLMalformed:         "API 地址格式不正确",
	ErrURLSchemeUnsupported: "API 地址只支持 http / https",
	ErrURLHostMissing:       "API 地址缺少主机名",
	ErrURLUnresolvable:      "API 地址无法解析",
	ErrURLDenied:            "API 地址指向内网或保留地址，已拒绝",

	ErrInternal: "服务器内部错误，请稍后重试",
}

// FacingText 查面向用户的文案（key → 中文兜底）；未登记返回 ("", false)。
func FacingText(key string) (string, bool) {
	text, ok := FacingMessages[key]
	return text, ok
}

// FormatFacing 把文案里的 `{name}` 占位符替换成 params 的值。
//
// 形态与 internal/web/shell 的 notice_param.go 一致（`{n}` / `{m}`）—— 页面提示里的
// 计数一律走参数化，不把数字拼进中文串（拼出来的串不在白名单里，PRG 回执会被丢弃）。
// params 里没有的占位符保持原样；params 为空直接返回原文。
func FormatFacing(text string, params map[string]string) string {
	if len(params) == 0 || text == "" {
		return text
	}
	for name, value := range params {
		if name == "" {
			continue
		}
		text = strings.ReplaceAll(text, "{"+name+"}", value)
	}
	return text
}

// 供应商状态。
const (
	StatusDisabled = 0
	StatusEnabled  = 1
)

// API 协议常量（protocol 列；下拉选项见 ProtocolOptions）。
const (
	ProtocolOpenAIChatCompletions = "openai_chat_completions"
	ProtocolOpenAIResponses       = "openai_responses"
	ProtocolAnthropicMessages     = "anthropic_messages"
	ProtocolGeminiGenerateContent = "gemini_generate_content"
)

// 模型输入类型（config_data.models[].input_types）。
const (
	InputTypeText  = "text"
	InputTypeImage = "image"
)

// ProtocolOption 协议下拉项。
//
// Label 是产品名（OpenAI / Anthropic / Gemini），不是可翻译文案 —— 不接 i18n。
type ProtocolOption struct {
	Value string
	Label string
}

// ProtocolOptions 协议下拉的全部选项（顺序即展示顺序，第一项是默认）。
var ProtocolOptions = []ProtocolOption{
	{ProtocolOpenAIChatCompletions, "OpenAI Chat Completions"},
	{ProtocolOpenAIResponses, "OpenAI Responses"},
	{ProtocolAnthropicMessages, "Anthropic Messages"},
	{ProtocolGeminiGenerateContent, "Gemini Generate Content"},
}

// IsSupportedProtocol 协议白名单判据（保存时用；空值按默认协议处理由 service 负责）。
func IsSupportedProtocol(p string) bool {
	for _, o := range ProtocolOptions {
		if o.Value == p {
			return true
		}
	}
	return false
}

// ProtocolLabel 协议值的展示名；未知值原样返回。
func ProtocolLabel(p string) string {
	for _, o := range ProtocolOptions {
		if o.Value == p {
			return o.Label
		}
	}
	return p
}
