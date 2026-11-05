package builtin

import (
	mw "go_wp/pkg/middleware"
)

// 通用请求与安全执行机制归 pkg/middleware；本包保留既有导出名供过渡。
var (
	SessionAuthMiddleware       = mw.SessionAuthMiddleware
	RequestBodyLimitMiddleware  = mw.RequestBodyLimitMiddleware
	CasbinMiddleware            = mw.CasbinMiddleware
	CasbinMiddlewareForPath     = mw.CasbinMiddlewareForPath
	CasbinMiddlewareForPathAs   = mw.CasbinMiddlewareForPathAs
	CasbinDecide                = mw.CasbinDecide
	GetUserID                   = mw.GetUserID
	GetUsername                 = mw.GetUsername
	RequestRateLimitMiddleware  = mw.RequestRateLimitMiddleware
	RequestIDMiddleware         = mw.RequestIDMiddleware
	Recovery                    = mw.Recovery
	EnforceForUser              = mw.EnforceForUser
	RequestLogCaptureMiddleware = mw.RequestLogCaptureMiddleware
	SecurityHeadersMiddleware   = mw.SecurityHeadersMiddleware
	StaticCacheMiddleware       = mw.StaticCacheMiddleware
	StaticGzipMiddleware        = mw.StaticGzipMiddleware
)
