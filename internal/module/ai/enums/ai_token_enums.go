package aienums

type TokenStatus int16

const (
	TokenStatusRevoked TokenStatus = 0

	TokenStatusActive TokenStatus = 1
)

func TokenStatusLabel(s int16) string {
	if s == int16(TokenStatusActive) {
		return "启用"
	}
	return "已撤销"
}

const (
	ErrTokenNotFound = "ai.err.tokenNotFound"

	ErrTokenInvalid = "ai.err.tokenInvalid"

	ErrTokenNameRequired = "ai.err.tokenNameRequired"

	ErrTokenScopeRequired = "ai.err.tokenScopeRequired"

	ErrTokenScopeUnknown = "ai.err.tokenScopeUnknown"

	ErrTokenExpiredInPast = "ai.err.tokenExpiredInPast"
)
