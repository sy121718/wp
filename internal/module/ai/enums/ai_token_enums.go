// ai_token_enums.go — 对外访问令牌（PAT）的枚举与文案 key。
//
// 与会话层分开一个文件：令牌是**对外身份**这一域的取值（谁能调用、能调到什么程度），
// 与会话（站内对话怎么进行）不是同一件事；混在一个文件里以后没人敢动。
package aienums

// TokenStatus 是 ai_access_token.status 的取值。
//
// 只有两态：启用与撤销。**没有「过期」这一态** —— 过期由 expires_at 与当前时间比较得出，
// 存成状态位就要有定时任务去翻，而一个「到点没跑任务就仍然可用」的令牌
// 是最不该有的东西（判据留到校验时现算，永远准确）。
type TokenStatus int16

const (
	// TokenStatusRevoked 已撤销（保留行，审计要能回答「什么时候撤的」）。
	TokenStatusRevoked TokenStatus = 0
	// TokenStatusActive 启用中。
	TokenStatusActive TokenStatus = 1
)

// TokenStatusLabel 返回中文标签（后台列表直接渲染，与 SessionStatusLabel 同口径）。
func TokenStatusLabel(s int16) string {
	if s == int16(TokenStatusActive) {
		return "启用"
	}
	return "已撤销"
}

// 令牌域的错误与提示 key（与其它 key 同源：值即 i18n key，中文兜底见 ai_token_msg.go）。
const (
	// ErrTokenNotFound 令牌不存在（管理页操作一个不存在的 id）。
	ErrTokenNotFound = "ai.err.tokenNotFound"
	// ErrTokenInvalid 外部调用带来的令牌不可用（不存在 / 已撤销 / 已过期）——
	// 对外的说法**只有这一种**：区分「不存在」与「已撤销」等于告诉爆破者哪一半猜对了。
	ErrTokenInvalid = "ai.err.tokenInvalid"
	// ErrTokenNameRequired 创建令牌必须写用途备注（列表里认不出是谁干什么的令牌没法治理）。
	ErrTokenNameRequired = "ai.err.tokenNameRequired"
	// ErrTokenScopeRequired 创建令牌必须给至少一个权限点：空 scope 的令牌什么都没有，
	// 唯一作用是让人以为「能用」。
	ErrTokenScopeRequired = "ai.err.tokenScopeRequired"
	// ErrTokenScopeUnknown 带了未知权限点（拼错或已下线），拒绝而不是忽略 ——
	// 静默忽略会生成一把「比申请人以为的更弱」的令牌，问题在第一次调用时才暴露。
	ErrTokenScopeUnknown = "ai.err.tokenScopeUnknown"
	// ErrTokenExpiredInPast 过期时刻已过去（建一把一来就失效的令牌没有意义）。
	ErrTokenExpiredInPast = "ai.err.tokenExpiredInPast"
)
