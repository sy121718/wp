package aienums

import (
	"net/http"
)

// StatusOf 声明每个 ai 业务错误对应的 HTTP 状态码。
//
// 为什么状态码写在这里而不是读词库的 http_code：词库是**文案**的事实源，状态码是
// **协议**的事实源。混在一起会让人以为改一句译文就能改协议（也确实发生过
// `ai.err.userRequired` 在词库里被记成 200）。声明在 Go 里，出口只认这一份。
//
// 分档依据是「用户下一步该做什么」，不是错误字符串的形状：
//
//	400 参数不对，改输入再来
//	401 没有身份，先登录
//	403 有身份但没这个权限
//	404 对象不存在，刷新列表
//	409 对象在但状态/版本不允许，重新读一遍再决定
//	500 我们这边出错，用户改什么都没用
//
// 未登记的 key 由调用方（controller 的 aiErrExit）按 400 兜底：能走到业务出口的错误
// 至少说明请求有问题。缺词的情况另有归口文案，不受这里影响。
func StatusOf(key string) int {
	switch key {
	case ErrProviderNotFound, ErrSessionNotFound, ErrTokenNotFound:
		return http.StatusNotFound

	case ErrTokenInvalid:
		return http.StatusUnauthorized
	case ErrUserRequired:
		return http.StatusUnauthorized

	case ErrToolForbidden:
		return http.StatusForbidden

	case ErrVersionConflict, ErrSessionConflict, ErrSessionProviderMismatch:
		return http.StatusConflict

	case ErrCipherUnavailable, ErrToolRunFailed, ErrInternal:
		return http.StatusInternalServerError

	case ErrInvalidParam,
		ErrProviderKeyRequired, ErrProviderKeyExists, ErrDisplayNameRequired,
		ErrAPIKeyRequiredOnBaseURLChange, ErrVersionRequired, ErrProtocolUnsupported,
		ErrNoBuiltinModels, ErrBaseURLRequired,
		ErrModelIDRequired, ErrModelIDDuplicated, ErrModelsFetchFailed,
		ErrURLMalformed, ErrURLSchemeUnsupported, ErrURLHostMissing,
		ErrURLUnresolvable, ErrURLDenied,
		ErrNoModelSelected,
		ErrSessionKeyMissing, ErrSessionArchived,
		ErrEventKindInvalid, ErrEventContentEmpty,
		ErrFoldRangeInvalid, ErrFoldSummaryEmpty,
		ErrSessionChatUnavailable, ErrSessionChatInputRequired,
		ErrSessionChatModelRequired, ErrSessionChatEmptyReply,
		ErrSessionToolRoundsExceeded,
		ErrTokenNameRequired,
		ErrTokenScopeRequired, ErrTokenScopeUnknown, ErrTokenExpiredInPast:
		return http.StatusBadRequest
	}
	return 0
}
