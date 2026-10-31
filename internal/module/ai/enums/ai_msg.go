package aienums

import "go_wp/pkg/facing"

import "strings"

const (
	MsgSaved          = "ai.msg.saved"
	MsgDeleted        = "ai.msg.deleted"
	MsgStatusChanged  = "ai.msg.statusChanged"
	MsgModelsSaved    = "ai.msg.modelsSaved"
	MsgModelsRestored = "ai.msg.modelsRestored"
	MsgModelsFetched  = "ai.msg.modelsFetched"

	MsgModelsFetchedDetail = "ai.msg.modelsFetchedDetail"

	MsgFabNoModel     = "ai.fab.noModel"
	MsgFabEmptyInput  = "ai.fab.emptyInput"
	MsgFabFailed      = "ai.fab.failed"
	MsgFabTruncated   = "ai.fab.truncated"
	MsgFabToolsPrefix = "ai.fab.tools"
	MsgFabToolsSuffix = "ai.fab.toolsUnit"
	MsgFabEmpty       = "ai.fab.empty"
	MsgFabThinkLabel  = "ai.fab.thinkLabel"

	MsgFabImageBadFormat = "ai.fab.imageBadFormat"
	MsgFabImageTooMany   = "ai.fab.imageTooMany"
	MsgFabImageTooLarge  = "ai.fab.imageTooLarge"

	ErrInvalidParam        = "ai.err.invalidParam"
	ErrProviderNotFound    = "ai.err.providerNotFound"
	ErrProviderKeyRequired = "ai.err.providerKeyRequired"
	ErrProviderKeyExists   = "ai.err.providerKeyExists"
	ErrDisplayNameRequired = "ai.err.displayNameRequired"

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

	ErrURLMalformed         = "ai.err.urlMalformed"
	ErrURLSchemeUnsupported = "ai.err.urlSchemeUnsupported"
	ErrURLHostMissing       = "ai.err.urlHostMissing"
	ErrURLUnresolvable      = "ai.err.urlUnresolvable"
	ErrURLDenied            = "ai.err.urlDenied"

	ErrUserRequired = "ai.err.userRequired"

	ErrInternal = "ai.err.internal"
)

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

	ErrUserRequired: "请先登录后再使用 AI 功能",

	ErrInternal: "服务器内部错误，请稍后重试",

	MsgFabNoModel:        "还没有可用的模型。请先在「大模型管理」里配置供应商与 API 密钥。",
	MsgFabEmptyInput:     "还没写问题。",
	MsgFabFailed:         "这次提问没有成功，请稍后再试，或到「全部会话」里看详情。",
	MsgFabTruncated:      "问题太长，已按前 4000 字截断。",
	MsgFabToolsPrefix:    "这轮查了数据",
	MsgFabToolsSuffix:    " 次。",
	MsgFabEmpty:          "没有拿到回答。可以到「全部会话」里看这一轮的详情。",
	MsgFabThinkLabel:     "思考过程",
	MsgFabImageBadFormat: "图片格式不认识，只支持直接粘贴或选择的图片文件。",
	MsgFabImageTooMany:   "一次最多带 4 张图。",
	MsgFabImageTooLarge:  "有图片太大了（单张上限约 3MB），请换一张小的。",
}

const (
	AdminLLMTitle = "admin.ai.menu.title"

	McpTitle = "admin.ai.mcp.title"
	McpLead  = "admin.ai.mcp.lead"

	McpTokenRevokeDone = "admin.ai.mcp.token.revokeDone"
	McpSwitchOnDone    = "admin.ai.mcp.switch.onDone"
	McpSwitchOffDone   = "admin.ai.mcp.switch.offDone"
)

func FacingText(key string) (string, bool) {
	text, ok := FacingMessages[key]
	return text, ok
}

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

const (
	StatusDisabled = 0
	StatusEnabled  = 1
)

const (
	ProtocolOpenAIChatCompletions = "openai_chat_completions"
	ProtocolOpenAIResponses       = "openai_responses"
	ProtocolAnthropicMessages     = "anthropic_messages"
	ProtocolGeminiGenerateContent = "gemini_generate_content"
)

const (
	InputTypeText  = "text"
	InputTypeImage = "image"
)

type ProtocolOption struct {
	Value string
	Label string
}

var ProtocolOptions = []ProtocolOption{
	{ProtocolOpenAIChatCompletions, "OpenAI Chat Completions"},
	{ProtocolOpenAIResponses, "OpenAI Responses"},
	{ProtocolAnthropicMessages, "Anthropic Messages"},
	{ProtocolGeminiGenerateContent, "Gemini Generate Content"},
}

func IsSupportedProtocol(p string) bool {
	for _, o := range ProtocolOptions {
		if o.Value == p {
			return true
		}
	}
	return false
}

func ProtocolLabel(p string) string {
	for _, o := range ProtocolOptions {
		if o.Value == p {
			return o.Label
		}
	}
	return p
}

var errFallbacks = map[string]string{
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
	ErrURLMalformed:                  "API 地址格式不正确",
	ErrURLSchemeUnsupported:          "API 地址只支持 http / https",
	ErrURLHostMissing:                "API 地址缺少主机名",
	ErrURLUnresolvable:               "API 地址无法解析",
	ErrURLDenied:                     "API 地址指向内网或保留地址，已拒绝",
	ErrUserRequired:                  "请先登录后再使用 AI 功能",
	ErrInternal:                      "服务器内部错误，请稍后重试",
	ErrNoModelSelected:               "请先勾选要添加的模型。",
	ErrSessionNotFound:               "会话不存在或已被删除",
	ErrSessionKeyMissing:             "会话标识缺失",
	ErrSessionArchived:               "会话已归档，不能再写入",
	ErrSessionConflict:               "该会话已被其他人修改，请刷新页面后重试",
	ErrSessionProviderMismatch:       "该会话标识已绑定其它供应商或模型，请换一个会话标识",
	ErrEventKindInvalid:              "不支持的事件类型",
	ErrEventContentEmpty:             "事件内容不能为空",
	ErrFoldRangeInvalid:              "折叠区间不正确，请重新选择",
	ErrFoldSummaryEmpty:              "折叠摘要不能为空",
	ErrSessionChatUnavailable:        "当前未接入对话能力，无法在这里发消息",
	ErrSessionChatInputRequired:      "请输入消息内容",
	ErrSessionChatModelRequired:      "请选择要使用的供应商与模型",
	ErrSessionChatEmptyReply:         "模型没有返回内容，请重试",
	ErrSessionToolRoundsExceeded:     "模型连续调用工具次数过多，已停止，请重试或换个说法",
	ErrToolForbidden:                 "没有权限执行该操作",
	ErrToolRunFailed:                 "工具执行失败",
	ErrTokenNotFound:                 "令牌不存在或已被删除",
	ErrTokenInvalid:                  "访问令牌无效",
	ErrTokenNameRequired:             "请填写令牌用途",
	ErrTokenScopeRequired:            "请至少选择一个权限点",
	ErrTokenScopeUnknown:             "包含未知的权限点",
	ErrTokenExpiredInPast:            "过期时间必须晚于当前时间",
}

func Err(key string) error { return facing.New(key, errFallbacks[key]) }

func FallbackFor(key string) string { return errFallbacks[key] }
