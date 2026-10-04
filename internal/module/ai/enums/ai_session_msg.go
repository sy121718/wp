package aienums

// ai_session_msg.go — ai 会话层的响应文案 key（与 ai_msg.go 同一口径：service 不硬编码文案）。
//
// 会话层与配置层同模块、同前缀，但 key 分开登记在各自文件里，便于对照各自的用例清单。
// 本批同样不 seed 词条：页面侧走 FacingMessages 的中文兜底（inbound/http 的 ai_err 出口），
// 接口侧原样返回 key 由 pkg/response.translate 按请求语言查词条。
const (
	MsgSessionAppended = "ai.msg.sessionAppended"
	MsgSessionRenamed  = "ai.msg.sessionRenamed"
	MsgSessionFolded   = "ai.msg.sessionFolded"
	// MsgSessionSent 会话页「发消息」成功回执：user 事件已写、模型已回复并落成 assistant 事件。
	MsgSessionSent = "ai.msg.sessionSent"

	ErrSessionNotFound   = "ai.err.sessionNotFound"
	ErrSessionKeyMissing = "ai.err.sessionKeyMissing"
	ErrSessionArchived   = "ai.err.sessionArchived"
	ErrSessionConflict   = "ai.err.sessionConflict"
	// ErrSessionProviderMismatch 同一会话键被用于不同的供应商 / 模型：拒绝静默合并
	// （否则一条会话的历史会在两个模型之间来回串，计量与审计都失去意义）。
	ErrSessionProviderMismatch = "ai.err.sessionProviderMismatch"
	ErrEventKindInvalid        = "ai.err.eventKindInvalid"
	ErrEventContentEmpty       = "ai.err.eventContentEmpty"
	ErrFoldRangeInvalid        = "ai.err.foldRangeInvalid"
	ErrFoldSummaryEmpty        = "ai.err.foldSummaryEmpty"
	// ErrSessionChatUnavailable 会话层没接上对话能力（装配缺失），发消息这条路走不通。
	ErrSessionChatUnavailable = "ai.err.sessionChatUnavailable"
	// ErrSessionChatInputRequired 发消息时消息正文为空。
	ErrSessionChatInputRequired = "ai.err.sessionChatInputRequired"
	// ErrSessionChatModelRequired 发消息时没给供应商 / 模型（无法判断打哪个上游）。
	ErrSessionChatModelRequired = "ai.err.sessionChatModelRequired"
	// ErrSessionChatEmptyReply 上游回了空文本（不把空回复写成一条空事件）。
	ErrSessionChatEmptyReply = "ai.err.sessionChatEmptyReply"
)

// sessionFacingMessages 会话层的中文兜底，由 init 追加进 FacingMessages。
//
// 追加而不是另立一张表：inbound 层只认 FacingText 一个入口，
// 多一张表就多一个「有人只查了一张」的机会。
var sessionFacingMessages = map[string]string{
	MsgSessionAppended: "事件已追加",
	MsgSessionRenamed:  "会话已更新",
	MsgSessionFolded:   "已折叠该段历史",
	MsgSessionSent:     "消息已发送，模型已回复",

	ErrSessionNotFound:          "会话不存在或已被删除",
	ErrSessionKeyMissing:        "会话标识缺失",
	ErrSessionArchived:          "会话已归档，不能再写入",
	ErrSessionConflict:          "该会话已被其他人修改，请刷新页面后重试",
	ErrSessionProviderMismatch:  "该会话标识已绑定其它供应商或模型，请换一个会话标识",
	ErrEventKindInvalid:         "不支持的事件类型",
	ErrEventContentEmpty:        "事件内容不能为空",
	ErrFoldRangeInvalid:         "折叠区间不正确，请重新选择",
	ErrFoldSummaryEmpty:         "折叠摘要不能为空",
	ErrSessionChatUnavailable:   "当前未接入对话能力，无法在这里发消息",
	ErrSessionChatInputRequired: "请输入消息内容",
	ErrSessionChatModelRequired: "请选择要使用的供应商与模型",
	ErrSessionChatEmptyReply:    "模型没有返回内容，请重试",
}

func init() {
	for k, v := range sessionFacingMessages {
		FacingMessages[k] = v
	}
}
