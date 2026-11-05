package aienums

type TokenStatus int16

const (
	TokenStatusRevoked TokenStatus = 0

	TokenStatusActive TokenStatus = 1
)

const (
	ErrTokenNotFound = "ai.err.tokenNotFound"

	ErrTokenInvalid = "ai.err.tokenInvalid"

	ErrTokenNameRequired = "ai.err.tokenNameRequired"

	ErrTokenScopeRequired = "ai.err.tokenScopeRequired"

	ErrTokenScopeUnknown = "ai.err.tokenScopeUnknown"

	ErrTokenExpiredInPast = "ai.err.tokenExpiredInPast"
)
