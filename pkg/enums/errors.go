package enums

const (
	ErrSystemError   = "ErrSystemError"
	ErrDBQueryError  = "ErrDBQueryError"
	ErrCacheError    = "ErrCacheError"
	ErrInvalidParams = "sysErrBadParam"
	ErrInvalidBody   = "ErrInvalidBody"
	ErrNotFound      = "ErrNotFound"
)

const (
	ErrUnauthorized          = "ErrUnauthorized"
	ErrInvalidToken          = "ErrInvalidToken"
	ErrTokenExpired          = "ErrTokenExpired"
	ErrPermissionDenied      = "sysErrNoPerm"
	ErrRateLimited           = "ErrRateLimited"
	ErrRequestEntityTooLarge = "sysErrTooLarge"

	ErrEnvironmentAbnormal = "ErrEnvironmentAbnormal"
	ErrAuthUnavailable     = "ErrAuthUnavailable"
	ErrForcedLogout        = "ErrForcedLogout"
	ErrSessionExpired      = "ErrSessionExpired"
	ErrUserInfoMissing     = "ErrUserInfoMissing"
	ErrAuthzNotReady       = "ErrAuthzNotReady"
	ErrAuthzFailed         = "ErrAuthzFailed"
)

const (
	ErrUserNotFound    = "ErrUserNotFound"
	ErrUserExists      = "ErrUserExists"
	ErrInvalidPassword = "ErrInvalidPassword"
)

const (
	ErrUploadSystemError      = "ErrUploadSystemError"
	ErrUploadNotInitialized   = "ErrUploadNotInitialized"
	ErrUploadProviderNotFound = "ErrUploadProviderNotFound"
	ErrUploadConfigMissing    = "ErrUploadConfigMissing"
	ErrUploadConfigInvalid    = "ErrUploadConfigInvalid"
	ErrUploadFileEmpty        = "ErrUploadFileEmpty"
	ErrUploadFileNameRequired = "ErrUploadFileNameRequired"
	ErrUploadWriteFailed      = "ErrUploadWriteFailed"
	ErrUploadRequestFailed    = "ErrUploadRequestFailed"
	ErrUploadTokenFailed      = "ErrUploadTokenFailed"
	ErrUploadResponseInvalid  = "ErrUploadResponseInvalid"
)
