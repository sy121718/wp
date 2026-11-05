package aienums

import "go_wp/pkg/facing"

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

// 界面 key（页面标题、导语）不在这里登记：它们由模板直接写 key + 中文兜底、用全局 t 取词。
// 这里只留要走 JSON 出口 message 的消息 key。
const (
	McpTokenRevokeDone = "admin.ai.mcp.token.revokeDone"
	McpSwitchOnDone    = "admin.ai.mcp.switch.onDone"
	McpSwitchOffDone   = "admin.ai.mcp.switch.offDone"

	// MCP（JSON-RPC）的 error / content 字段是**给客户端看的文案**，不是错误事实：
	// 它是界面域的字符串，key 必须落在界面域，不能拿 ai.err.* 去当 message。
	// 词库里这两条已存在（543_ai_token_i18n.sql / 539_ai_session_tools_i18n.sql），
	// 这里只是给同一批文案一个界面域的 key。
	MsgMcpTokenInvalid  = "admin.ai.mcp.error.tokenInvalid"
	MsgMcpToolRunFailed = "admin.ai.mcp.error.toolRunFailed"
)

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

func Err(key string) error { return facing.New(key) }
