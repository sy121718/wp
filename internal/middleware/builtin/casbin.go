package builtin

import (
	"strconv"

	"go_wp/pkg/casbin"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

// CasbinMiddleware Casbin RBAC 鉴权中间件。
//
// 前置条件：必须先经过 JWTAuthMiddleware，确保 user_id 已写入 Context。
//
// 鉴权流程：
//  1. 从 Context 获取 user_id（路由匹配时 JWT 中间件已写入）
//  2. 构造 Casbin 请求三元组：sub=用户ID, obj=请求路径, act=HTTP方法
//  3. 调用 casbin.GetEnforcer().Enforce() 执行权限判断
//
// 失败场景：
//   - 未获取到 user_id → 返回 401 "未获取到用户信息"（通常意味着未挂 JWT 中间件）
//   - Casbin Enforcer 未初始化 → 返回 500 "权限系统未初始化"
//   - Enforce 返回 false → 返回 403 "无权限访问"
//   - Enforce 执行出错 → 返回 500 "权限验证失败"
//
// 适用位置：需要细粒度权限控制的路由组或单路由。
func CasbinMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, exists := c.Get("user_id")
		if !exists {
			response.ErrorWithMessage(c, 401, "未获取到用户信息")
			c.Abort()
			return
		}

		obj := c.Request.URL.Path
		act := c.Request.Method
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
func GetUserID(c *gin.Context) int64 {
	userID, exists := c.Get("user_id")
	if !exists {
		return 0
	}
	return userID.(int64)
}

// GetUsername 从 gin.Context 中提取已认证的用户名。
// 如果未找到则返回空字符串，由调用方自行处理空值。
func GetUsername(c *gin.Context) string {
	username, exists := c.Get("username")
	if !exists {
		return ""
	}
	return username.(string)
}
