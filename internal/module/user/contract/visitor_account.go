package usercontract

// VisitorTokenContextKey 是可信访客中间件传给业务 API 的会话令牌键。
// 只可用于认证与撤销，不能进入对外 JSON、静态产物或 HTML。
const VisitorTokenContextKey = "gowp_visitor_session_token"
