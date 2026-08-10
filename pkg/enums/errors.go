package enums

// 系统级错误码（10xxx）
const (
	ErrSystemError   = "ErrSystemError"   // 系统异常
	ErrDBQueryError  = "ErrDBQueryError"  // 数据库查询错误
	ErrCacheError    = "ErrCacheError"    // 缓存错误
	ErrInvalidParams = "ErrInvalidParams" // 请求参数错误
	ErrInvalidBody   = "ErrInvalidBody"   // 请求体格式错误
	ErrNotFound      = "ErrNotFound"      // 请求资源不存在
)

// 认证错误码（90xxx）
const (
	ErrUnauthorized          = "ErrUnauthorized"          // 未登录或登录已过期
	ErrInvalidToken          = "ErrInvalidToken"          // Token无效
	ErrTokenExpired          = "ErrTokenExpired"          // Token已过期
	ErrPermissionDenied      = "ErrPermissionDenied"      // 无权限访问
	ErrRateLimited           = "ErrRateLimited"           // 请求触发限流
	ErrRequestEntityTooLarge = "ErrRequestEntityTooLarge" // 请求体过大
)

// 用户模块错误码（20xxx）
const (
	ErrUserNotFound    = "ErrUserNotFound"    // 用户不存在
	ErrUserExists      = "ErrUserExists"      // 用户已存在
	ErrInvalidPassword = "ErrInvalidPassword" // 用户名或密码错误
)

// 上传模块错误码（30xxx）
const (
	ErrUploadSystemError      = "ErrUploadSystemError"      // 上传系统异常
	ErrUploadNotInitialized   = "ErrUploadNotInitialized"   // 上传组件未初始化
	ErrUploadProviderNotFound = "ErrUploadProviderNotFound" // 上传 provider 不存在
	ErrUploadConfigMissing    = "ErrUploadConfigMissing"    // 上传配置缺失
	ErrUploadConfigInvalid    = "ErrUploadConfigInvalid"    // 上传配置无效
	ErrUploadFileEmpty        = "ErrUploadFileEmpty"        // 上传文件为空
	ErrUploadFileNameRequired = "ErrUploadFileNameRequired" // 文件名缺失
	ErrUploadWriteFailed      = "ErrUploadWriteFailed"      // 上传写入失败
	ErrUploadRequestFailed    = "ErrUploadRequestFailed"    // 上传请求失败
	ErrUploadTokenFailed      = "ErrUploadTokenFailed"      // 上传签名/令牌失败
	ErrUploadResponseInvalid  = "ErrUploadResponseInvalid"  // 上传响应无效
)
