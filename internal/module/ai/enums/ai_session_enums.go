package aienums

type EventKind string

const (
	EventKindUser EventKind = "user"

	EventKindAssistant EventKind = "assistant"

	EventKindTool EventKind = "tool"

	EventKindCompactStart EventKind = "compact_start"

	EventKindCompactSummary EventKind = "compact_summary"

	EventKindCompactEnd EventKind = "compact_end"

	EventKindNote EventKind = "note"
)

var eventKindOrder = []EventKind{
	EventKindUser,
	EventKindAssistant,
	EventKindTool,
	EventKindCompactStart,
	EventKindCompactSummary,
	EventKindCompactEnd,
	EventKindNote,
}

func IsValidEventKind(k EventKind) bool {
	for _, v := range eventKindOrder {
		if v == k {
			return true
		}
	}
	return false
}

func EventKinds() []EventKind {
	out := make([]EventKind, len(eventKindOrder))
	copy(out, eventKindOrder)
	return out
}

type SurfaceOp string

const (
	SurfaceAppend SurfaceOp = "append"

	SurfaceReplace SurfaceOp = "replace"
)

func IsValidSurfaceOp(op SurfaceOp) bool {
	return op == SurfaceAppend || op == SurfaceReplace
}

type SessionStatus int16

const (
	SessionArchived SessionStatus = 0

	SessionActive SessionStatus = 1
)

type CallStatus string

const (
	CallStatusOK CallStatus = "ok"

	CallStatusError CallStatus = "error"
)

type ToolCallStatus string

const (
	ToolCallStatusOK ToolCallStatus = "ok"

	ToolCallStatusArgsError ToolCallStatus = "args_error"

	ToolCallStatusForbidden ToolCallStatus = "forbidden"

	ToolCallStatusFailed ToolCallStatus = "failed"
)

func IsValidToolCallStatus(s ToolCallStatus) bool {
	switch s {
	case ToolCallStatusOK, ToolCallStatusArgsError, ToolCallStatusForbidden, ToolCallStatusFailed:
		return true
	}
	return false
}
