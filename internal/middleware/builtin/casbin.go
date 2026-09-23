package builtin

import (
	"net/http"
	"strconv"

	"go_wp/pkg/casbin"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

// CasbinMiddleware Casbin RBAC 鉴权中间件。
//
// 前置条件：必须先经过 SessionAuthMiddleware，确保 user_id 已写入 Context。
//
// 鉴权流程：
//  1. 从 Context 获取 user_id（路由匹配时 SessionAuthMiddleware 已写入）
//  2. 构造 Casbin 请求三元组：sub=用户ID, obj=请求路径, act=HTTP方法
//  3. 调用 casbin.GetEnforcer().Enforce() 执行权限判断
//
// 失败场景：
//   - 未获取到 user_id → 返回 401 "未获取到用户信息"（通常意味着未挂 SessionAuthMiddleware）
//   - Casbin Enforcer 未初始化 → 返回 500 "权限系统未初始化"
//   - Enforce 返回 false → 返回 403 "无权限访问"
//   - Enforce 执行出错 → 返回 500 "权限验证失败"
//
// 适用位置：需要细粒度权限控制的路由组或单路由。
func CasbinMiddleware() gin.HandlerFunc {
	return casbinMiddleware("", "")
}

// CasbinMiddlewareForPath 显式指定鉴权对象路径（obj）的 Casbin 中间件。
//
// 用于「页面写操作」：后台页面路由（如 /admin/pages/create）与权限点路径
// （如 /api/page/create）不一致时，页面 handler 应复用对应 API 权限点的语义
// 做鉴权，而非直接以页面路径 enforce（权限点表里不存在页面路径，会导致
// 所有用户被拒）。obj 为空时回退到实际请求路径（等价 CasbinMiddleware）。
func CasbinMiddlewareForPath(obj string) gin.HandlerFunc {
	return casbinMiddleware(obj, "")
}

// CasbinMiddlewareForPathAs 显式指定鉴权对象路径（obj）**与动作（act）**的 Casbin 中间件。
//
// 为什么需要它：CasbinMiddlewareForPath 的动作取自真实请求方法（`c.Request.Method`），
// 这隐含假设「页面侧与 API 侧用同一个动词」。该假设对「页面写操作」（POST 页面复用
// POST 权限点）成立，对**页面读入口复用写权限点**则不成立 —— 典型场景是
// `/admin/articles/new`：它是 GET 页面，业务上要求「能创建文章的人才能打开新建页」，
// 于是复用了 `/api/content/create`；但库里那条策略声明的是 POST，
// Enforce(sub, obj, "GET") 必然不匹配 → **含超管在内全员 403**（实测确认）。
//
// act 留空时行为与 CasbinMiddlewareForPath 完全一致（取真实请求方法），
// 既有调用点一个字符都不用改。
//
// 参数校验：act 必须是合法 HTTP 方法，否则 panic —— 拼错的动词会让该路由永久 403，
// 属于装配缺陷，应当在启动期暴露而不是等用户点进去才发现。
func CasbinMiddlewareForPathAs(obj, act string) gin.HandlerFunc {
	if act != "" && !validHTTPMethod(act) {
		panic("CasbinMiddlewareForPathAs 收到非法 HTTP 方法：" + act + "（期望 GET/POST/PUT/DELETE/PATCH/HEAD/OPTIONS）")
	}
	return casbinMiddleware(obj, act)
}

// validHTTPMethod 判断是否为受支持的 HTTP 方法（大小写敏感，与库里的策略一致）。
func validHTTPMethod(m string) bool {
	switch m {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete,
		http.MethodPatch, http.MethodHead, http.MethodOptions:
		return true
	}
	return false
}

// casbinMiddleware forcedAct 非空时覆盖真实请求方法，用于「页面入口复用 API 权限点」
// 但动词不一致的场景（见 CasbinMiddlewareForPathAs）。
func casbinMiddleware(forcedObj, forcedAct string) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, exists := c.Get("user_id")
		if !exists {
			response.ErrorWithMessage(c, 401, "未获取到用户信息")
			c.Abort()
			return
		}

		obj := forcedObj
		if obj == "" {
			obj = c.Request.URL.Path
		}
		act := forcedAct
		if act == "" {
			act = c.Request.Method
		}
		sub := strconv.FormatInt(userID.(int64), 10)

		enforcer := casbin.GetEnforcer()
		if enforcer == nil {
			response.ErrorWithMessage(c, 500, "权限系统未初始化")
			c.Abort()
			return
		}

		ok, err := enforcer.Enforce(sub, obj, act)
		if err != nil {
			logger.Scene("middleware").With("sub", sub).With("obj", obj).With("act", act).Error(err, "鉴权失败")
			response.ErrorWithMessage(c, 500, "权限验证失败")
			c.Abort()
			return
		}

		if !ok {
			logger.Scene("middleware").With("sub", sub).With("obj", obj).With("act", act).Warn("鉴权失败")
			response.ErrorWithMessage(c, 403, "无权限访问")
			c.Abort()
			return
		}

		c.Next()
	}
}

// GetUserID 从 gin.Context 中提取已认证的用户 ID。
// 如果未找到则返回 0，由调用方自行处理空值。
//
// 类型断言带 ok 检查：值来自会话上下文，任何类型异常都应当降级成「未登录」，
// 而不是让裸断言把请求打成 500 —— 中间件写入的是 int64（auth.go），
// 但这层保护让「写入方改了类型」表现为登录态丢失（可观测、可回滚），而不是线上 panic。
func GetUserID(c *gin.Context) int64 {
	v, exists := c.Get("user_id")
	if !exists {
		return 0
	}
	id, ok := v.(int64)
	if !ok {
		return 0
	}
	return id
}

// GetUsername 从 gin.Context 中提取已认证的用户名。
// 如果未找到则返回空字符串，由调用方自行处理空值。
// 类型断言同样带 ok 检查，理由见 GetUserID。
func GetUsername(c *gin.Context) string {
	v, exists := c.Get("username")
	if !exists {
		return ""
	}
	name, ok := v.(string)
	if !ok {
		return ""
	}
	return name
}
