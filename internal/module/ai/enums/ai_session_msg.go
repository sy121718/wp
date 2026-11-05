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
