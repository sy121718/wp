// ai_session_enums.go — ai 模块**会话与事件**的枚举（配置层的协议、输入类型见 ai_msg.go）。
//
// 两者放在同一包的不同文件里便于按域查阅：会话层的取值最终落在 ai_session / ai_event 两表上。
package aienums

// EventKind 是 ai_event.kind 的取值：事件类别决定它在投影里扮演什么角色。
//
// 取值是字符串而非数字：事件日志是给人看的真源（排查时要能直接读表），
// 且 kind 只在代码里做白名单判断，不做算术。
type EventKind string

const (
	// EventKindUser 用户消息。
	EventKindUser EventKind = "user"
	// EventKindAssistant 助手回复。
	EventKindAssistant EventKind = "assistant"
	// EventKindTool 工具调用与结果（调用与结果由 meta.phase 区分，避免同一件事写两种 kind）。
	EventKindTool EventKind = "tool"
	// EventKindCompactStart 折叠开始：记录待折叠范围与折叠前预算，是折叠的意图凭证。
	EventKindCompactStart EventKind = "compact_start"
	// EventKindCompactSummary 折叠摘要：surface_op=replace 时，它替换掉被折叠的那一段。
	EventKindCompactSummary EventKind = "compact_summary"
	// EventKindCompactEnd 折叠结束：记录折叠后的计量与净收益（压缩开销 ≤2% 的口径见 docs/16）。
	EventKindCompactEnd EventKind = "compact_end"
	// EventKindNote 系统提示：不进对话语义，但要留在时间线上（如「模型已切换，前缀缓存作废」）。
	EventKindNote EventKind = "note"
)

// eventKindOrder 是白名单与展示排序的唯一真源：写入口用它校验，列表用它排序。
var eventKindOrder = []EventKind{
	EventKindUser,
	EventKindAssistant,
	EventKindTool,
	EventKindCompactStart,
	EventKindCompactSummary,
	EventKindCompactEnd,
	EventKindNote,
}

// IsValidEventKind 判断取值是否在白名单内（写入口必须先过这一关）。
func IsValidEventKind(k EventKind) bool {
	for _, v := range eventKindOrder {
		if v == k {
			return true
		}
	}
	return false
}

// EventKinds 返回白名单的副本，供后台表单渲染下拉。
func EventKinds() []EventKind {
	out := make([]EventKind, len(eventKindOrder))
	copy(out, eventKindOrder)
	return out
}

// SurfaceOp 是 ai_event.surface_op 的取值：本条事件如何影响投影。
type SurfaceOp string

const (
	// SurfaceAppend 本条事件进入投影。
	SurfaceAppend SurfaceOp = "append"
	// SurfaceReplace 本条事件不直接进投影，而是把 [replace_from_seq, replace_to_seq] 段替换成自己。
	SurfaceReplace SurfaceOp = "replace"
)

// IsValidSurfaceOp 判断取值是否合法。replace 只允许 kind=compact_summary 使用（见 service 层折叠流程）。
func IsValidSurfaceOp(op SurfaceOp) bool {
	return op == SurfaceAppend || op == SurfaceReplace
}

// SessionStatus 是 ai_session.status 的取值。
type SessionStatus int16

const (
	// SessionArchived 已归档：不再接受新事件，只在列表里可查。
	SessionArchived SessionStatus = 0
	// SessionActive 进行中：可追加事件、可折叠。
	SessionActive SessionStatus = 1
)

// SessionStatusLabel 返回中文标签（后台列表直接渲染，不走 i18n 词条：状态是运维口径）。
func SessionStatusLabel(s SessionStatus) string {
	if s == SessionActive {
		return "进行中"
	}
	return "已归档"
}
