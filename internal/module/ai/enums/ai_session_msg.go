package aienums

const (
	MsgSessionAppended = "ai.msg.sessionAppended"
	MsgSessionRenamed  = "ai.msg.sessionRenamed"
	MsgSessionFolded   = "ai.msg.sessionFolded"

	MsgSessionSent = "ai.msg.sessionSent"

	ErrSessionNotFound   = "ai.err.sessionNotFound"
	ErrSessionKeyMissing = "ai.err.sessionKeyMissing"
	ErrSessionArchived   = "ai.err.sessionArchived"
	ErrSessionConflict   = "ai.err.sessionConflict"

	ErrSessionProviderMismatch = "ai.err.sessionProviderMismatch"
	ErrEventKindInvalid        = "ai.err.eventKindInvalid"
	ErrEventContentEmpty       = "ai.err.eventContentEmpty"
	ErrFoldRangeInvalid        = "ai.err.foldRangeInvalid"
	ErrFoldSummaryEmpty        = "ai.err.foldSummaryEmpty"

	ErrSessionChatUnavailable = "ai.err.sessionChatUnavailable"

	ErrSessionChatInputRequired = "ai.err.sessionChatInputRequired"

	ErrSessionChatModelRequired = "ai.err.sessionChatModelRequired"

	ErrSessionChatEmptyReply = "ai.err.sessionChatEmptyReply"

	ErrSessionToolRoundsExceeded = "ai.err.sessionToolRoundsExceeded"

	ErrToolForbidden = "ai.err.toolForbidden"

	ErrToolRunFailed = "ai.err.toolRunFailed"
)

var sessionFacingMessages = map[string]string{
	MsgSessionAppended: "事件已追加",
	MsgSessionRenamed:  "会话已更新",
	MsgSessionFolded:   "已折叠该段历史",
	MsgSessionSent:     "消息已发送，模型已回复",

	ErrSessionNotFound:           "会话不存在或已被删除",
	ErrSessionKeyMissing:         "会话标识缺失",
	ErrSessionArchived:           "会话已归档，不能再写入",
	ErrSessionConflict:           "该会话已被其他人修改，请刷新页面后重试",
	ErrSessionProviderMismatch:   "该会话标识已绑定其它供应商或模型，请换一个会话标识",
	ErrEventKindInvalid:          "不支持的事件类型",
	ErrEventContentEmpty:         "事件内容不能为空",
	ErrFoldRangeInvalid:          "折叠区间不正确，请重新选择",
	ErrFoldSummaryEmpty:          "折叠摘要不能为空",
	ErrSessionChatUnavailable:    "当前未接入对话能力，无法在这里发消息",
	ErrSessionChatInputRequired:  "请输入消息内容",
	ErrSessionChatModelRequired:  "请选择要使用的供应商与模型",
	ErrSessionChatEmptyReply:     "模型没有返回内容，请重试",
	ErrSessionToolRoundsExceeded: "模型连续调用工具次数过多，已停止，请重试或换个说法",
	ErrToolForbidden:             "没有权限执行该操作",
	ErrToolRunFailed:             "工具执行失败",
}

func init() {
	for k, v := range sessionFacingMessages {
		FacingMessages[k] = v
	}
}
