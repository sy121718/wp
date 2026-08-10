package builtin

import (
	"context"
	"strconv"

	"go_wp/pkg/auth"
	"go_wp/pkg/casbin"
	"go_wp/pkg/datarule"

	"github.com/gin-gonic/gin"
)

// DataRuleContextMiddleware 将当前用户上下文注入 request context，供 datarule GORM 插件读取。
//
// 前置条件：必须在 JWTAuthMiddleware 之后注册（需要 user_id）。
//
// 流程：
//  1. 从 gin.Context 获取 user_id
//  2. 从 Redis 读取用户会话（含 DeptID、IsAdmin）
//  3. 从 Casbin facade 查询用户角色 codes
//  4. 构建 *datarule.UserContext，写入 c.Request.Context()
//
// 读取失败时降级（不阻止请求），只是该请求的数据规则不会生效。
func DataRuleContextMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		userIDVal, exists := c.Get("user_id")
		if !exists {
			c.Next()
			return
		}
		userID := uint64(userIDVal.(int64))
		if userID == 0 {
			c.Next()
			return
		}

		// 从 Redis 获取会话
		session, err := auth.GetUserSession(c.Request.Context(), userID)
		if err != nil || session == nil {
			c.Next()
			return
		}

		// 从 Casbin 获取角色 codes
		roleCodes, _ := casbin.GetRoleCodesByUserID(strconv.FormatUint(userID, 10))

		uc := &datarule.UserContext{
			UserID:  userID,
			DeptID:  session.DeptID,
			IsAdmin: session.IsAdmin,
			Roles:   roleCodes,
		}

		// 注入到 request context
		ctx := context.WithValue(c.Request.Context(), datarule.UserContextKey{}, uc)
		c.Request = c.Request.WithContext(ctx)

		c.Next()
	}
}