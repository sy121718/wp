package builtin

import (
	"context"
	"strconv"
	"sync/atomic"

	"go_wp/pkg/auth"
	"go_wp/pkg/casbin"
	"go_wp/pkg/datarule"
	"go_wp/pkg/logger"

	"github.com/gin-gonic/gin"
)

// DataRuleDeptResolver 部门快照解析器：给定部门 id，返回该部门的**子树** id 列表
// （含自身与全部子孙）。方向是**向下**的 —— 见 datarule.UserContext.DeptSubtreeIDs 的说明。
//
// 由装配期注入（internal/routers/assembly.go：admin 用**同一份**数据权限部门快照实现它），
// 中间件因此不必为「我能看哪些部门」再查一次库 —— 那是改造前每次请求都要付的成本。
type DataRuleDeptResolver func(deptID uint64) []uint64

// 装配期写入、请求期读取，用 atomic.Pointer 存函数值避免装配线程与请求线程的竞态
// （同 pkg/casbin 的 urlCodeMap「rebuild 后 Store、读端 Load」手法）。
var dataRuleSubtreeResolver atomic.Pointer[DataRuleDeptResolver]

// SetDataRuleDeptResolver 注入部门子树解析器（装配期调用一次）。
//
// 未注入时中间件不填 DeptSubtreeIDs，引擎回退到既有的 sys_dept 子查询：
// 语义正确，只是少了这次优化 —— 所以这里不是 fail-fast 的装配缺陷。
func SetDataRuleDeptResolver(subtrees DataRuleDeptResolver) {
	if subtrees != nil {
		dataRuleSubtreeResolver.Store(&subtrees)
	}
}

// resolveDeptIDs 调用注入的解析器；未注入或部门为 0 时返回 nil（调用方据此回退）。
func resolveDeptIDs(slot *atomic.Pointer[DataRuleDeptResolver], deptID uint64) []uint64 {
	if deptID == 0 {
		return nil
	}
	resolver := slot.Load()
	if resolver == nil {
		return nil
	}
	return (*resolver)(deptID)
}

// DataRuleContextMiddleware 将当前用户上下文注入 request context，供 datarule GORM 插件读取。
//
// 前置条件：必须在 SessionAuthMiddleware 之后注册（需要 user_id）。
//
// 流程：
//  1. 从 gin.Context 获取 user_id
//  2. 从 Redis 读取用户会话（含 DeptID、IsAdmin）
//  3. 从 Casbin facade 查询用户角色 codes
//  4. 经装配期注入的部门快照解析器取该部门的**子树**（不查库；未注入则为空，引擎回退子查询）
//  5. 构建 *datarule.UserContext，写入 c.Request.Context()
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
			logger.Scene("middleware").With("user_id", userID).With("err", err).Warn("会话读取失败，数据规则降级")
			c.Next()
			return
		}

		// 从 Casbin 获取角色 codes
		roleCodes, roleErr := casbin.GetRoleCodesByUserID(strconv.FormatUint(userID, 10))
		if roleErr != nil {
			logger.Scene("middleware").With("user_id", userID).With("err", roleErr).Warn("角色读取失败，数据规则降级")
		}

		// 部门子树（向下：本部门及全部子孙）来自 admin 的同一份部门快照（装配期注入）。
		// 只填这一个方向：祖先链（向上）由 admin 的规则快照内部消化，不经过本结构。
		// 解析器为空时该字段留空，引擎侧回退子查询，行为与改造前一致。
		uc := &datarule.UserContext{
			UserID:         userID,
			DeptID:         session.DeptID,
			IsAdmin:        session.IsAdmin,
			Roles:          roleCodes,
			DeptSubtreeIDs: resolveDeptIDs(&dataRuleSubtreeResolver, session.DeptID),
		}

		// 注入到 request context
		ctx := context.WithValue(c.Request.Context(), datarule.UserContextKey{}, uc)
		c.Request = c.Request.WithContext(ctx)

		c.Next()
	}
}
